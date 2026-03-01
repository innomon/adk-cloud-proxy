package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/innomon/adk-cloud-proxy/pkg/auth"
	"github.com/innomon/adk-cloud-proxy/pkg/config"
	"github.com/innomon/adk-cloud-proxy/pkg/pubsub"
	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type Connector struct {
	mu           sync.Mutex
	connections  map[string]context.CancelFunc // proxyURL -> cancel func
	activeSessions int32
	lastActive   time.Time
}

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Printf("Warning: failed to load config.yaml: %v", err)
		cfg = &config.Config{}
	}

	nkeySeed := os.Getenv("NKEY_SEED")
	if nkeySeed == "" {
		log.Fatal("NKEY_SEED environment variable is required")
	}

	targetURL := os.Getenv("TARGET_ADK_SERVER_URL")
	if targetURL == "" {
		log.Fatal("TARGET_ADK_SERVER_URL environment variable is required")
	}

	userID := os.Getenv("USER_ID")
	if userID == "" {
		log.Fatal("USER_ID environment variable is required")
	}

	appID := os.Getenv("APP_ID")
	if appID == "" {
		log.Fatal("APP_ID environment variable is required")
	}

	useTLS := strings.EqualFold(os.Getenv("TLS_ENABLED"), "true")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	connector := &Connector{
		connections: make(map[string]context.CancelFunc),
		lastActive:  time.Now(),
	}

	go func() {
		<-sigCh
		log.Println("Shutting down connector...")
		cancel()
	}()

	// Initialize PubSub if configured
	var ps pubsub.PubSub
	if cfg.PubSub.Type != "" {
		ps, err = pubsub.New(cfg.PubSub.Type, cfg.PubSub.Config)
		if err != nil {
			log.Fatalf("Failed to initialize pubsub: %v", err)
		}
		defer ps.Close()
		log.Printf("PubSub initialized: %s", cfg.PubSub.Type)

		subject := "invites." + appID
		err = ps.Subscribe(ctx, subject, func(msg *pubsub.Message) {
			invite, err := pubsub.DecodeInviteMessage(msg.Payload)
			if err != nil {
				log.Printf("Failed to decode invite: %v", err)
				return
			}
			if invite.AppID != appID || invite.UserID != userID {
				return
			}

			connector.mu.Lock()
			if _, exists := connector.connections[invite.ProxyURL]; !exists {
				log.Printf("Received invite to connect to %s", invite.ProxyURL)
				connCtx, connCancel := context.WithCancel(ctx)
				connector.connections[invite.ProxyURL] = connCancel
				go func() {
					defer func() {
						connector.mu.Lock()
						delete(connector.connections, invite.ProxyURL)
						connector.mu.Unlock()
					}()
					if err := runTunnel(connCtx, invite.ProxyURL, []byte(nkeySeed), targetURL, userID, appID, useTLS, connector); err != nil {
						log.Printf("Tunnel to %s failed: %v", invite.ProxyURL, err)
					}
				}()
			}
			connector.mu.Unlock()
		})
		if err != nil {
			log.Fatalf("Failed to subscribe to invites: %v", err)
		}
		log.Printf("Subscribed to %s", subject)
	} else {
		// Legacy behavior if no pubsub configured: connect immediately to ROUTER_PROXY_URL
		routerProxyURL := os.Getenv("ROUTER_PROXY_URL")
		if routerProxyURL != "" {
			go runTunnel(ctx, routerProxyURL, []byte(nkeySeed), targetURL, userID, appID, useTLS, connector)
		}
	}

	// Inactivity monitor
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				connector.mu.Lock()
				sessions := connector.activeSessions
				idleTime := time.Since(connector.lastActive)
				numConns := len(connector.connections)
				connector.mu.Unlock()

				if sessions == 0 && numConns > 0 && idleTime > 5*time.Minute {
					log.Println("Inactivity timeout reached, closing connections...")
					connector.mu.Lock()
					for url, cancelFunc := range connector.connections {
						log.Printf("Closing connection to %s", url)
						cancelFunc()
						delete(connector.connections, url)
					}
					connector.mu.Unlock()
				}
				
				if sessions == 0 && numConns == 0 && idleTime > 10*time.Minute {
					log.Println("No active connections or sessions for 10 minutes, shutting down connector...")
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	<-ctx.Done()
}

func runTunnel(ctx context.Context, proxyURL string, seed []byte, targetURL, userID, appID string, useTLS bool, c *Connector) error {
	// Generate JWT.
	token, err := auth.GenerateToken(seed, userID, appID, "", 1*time.Hour)
	if err != nil {
		return err
	}

	// Connect to the Router Proxy.
	var transportCreds grpc.DialOption
	if useTLS {
		transportCreds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	} else {
		transportCreds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	conn, err := grpc.NewClient(proxyURL, transportCreds)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewTunnelServiceClient(conn)

	// Attach JWT as metadata.
	md := metadata.Pairs("authorization", "Bearer "+token)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.Connect(streamCtx)
	if err != nil {
		return err
	}

	log.Printf("Connected to Router Proxy at %s", proxyURL)

	// Process incoming requests from the tunnel.
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		go handleTunnelRequest(stream, msg, targetURL, c)
	}
}

func handleTunnelRequest(stream pb.TunnelService_ConnectClient, msg *pb.TunnelMessage, targetURL string, c *Connector) {
	c.mu.Lock()
	c.activeSessions++
	c.lastActive = time.Now()
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.activeSessions--
		c.lastActive = time.Now()
		c.mu.Unlock()
	}()

	httpReq := msg.GetHttpRequest()
	if httpReq == nil {
		log.Printf("Received non-request message for request_id=%s, ignoring", msg.RequestId)
		return
	}

	log.Printf("Forwarding request: %s %s (request_id=%s)", httpReq.Method, httpReq.Path, msg.RequestId)

	// Build the HTTP request to the local ADK server.
	url := targetURL + httpReq.Path
	req, err := http.NewRequest(httpReq.Method, url, bytes.NewReader(httpReq.Body))
	if err != nil {
		sendErrorResponse(stream, msg.RequestId, http.StatusInternalServerError, "failed to create request")
		return
	}
	for k, v := range httpReq.Headers {
		req.Header.Set(k, v)
	}

	// Forward to the local ADK server.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sendErrorResponse(stream, msg.RequestId, http.StatusBadGateway, "failed to reach target ADK server")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		sendErrorResponse(stream, msg.RequestId, http.StatusBadGateway, "failed to read response body")
		return
	}

	// Build response headers.
	headers := make(map[string]string)
	for k, v := range resp.Header {
		headers[k] = v[0]
	}

	// Send response back through the tunnel.
	respMsg := &pb.TunnelMessage{
		RequestId: msg.RequestId,
		Payload: &pb.TunnelMessage_HttpResponse{
			HttpResponse: &pb.HttpResponse{
				StatusCode: int32(resp.StatusCode),
				Headers:    headers,
				Body:       body,
			},
		},
	}

	if err := stream.Send(respMsg); err != nil {
		log.Printf("Failed to send response for request_id=%s: %v", msg.RequestId, err)
	}
}

func sendErrorResponse(stream pb.TunnelService_ConnectClient, requestID string, statusCode int, message string) {
	respMsg := &pb.TunnelMessage{
		RequestId: requestID,
		Payload: &pb.TunnelMessage_HttpResponse{
			HttpResponse: &pb.HttpResponse{
				StatusCode: int32(statusCode),
				Body:       []byte(message),
			},
		},
	}
	if err := stream.Send(respMsg); err != nil {
		log.Printf("Failed to send error response for request_id=%s: %v", requestID, err)
	}
}
