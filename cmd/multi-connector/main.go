package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/innomon/adk-cloud-proxy/pkg/auth"
	"github.com/innomon/adk-cloud-proxy/pkg/config"
	"github.com/innomon/adk-cloud-proxy/pkg/adk"
	"github.com/innomon/adk-cloud-proxy/pkg/pubsub"
	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type MultiConnector struct {
	mu             sync.Mutex
	connections    map[string]context.CancelFunc // key: proxyURL+appID -> cancel func
	activeSessions int32
	lastActive     time.Time
	handlers       map[string]*adk.InProcessADKHandler // appID -> InProcessADKHandler
}

func main() {
	cfg, err := config.LoadMultiConfig("multi-config.yaml")
	if err != nil {
		log.Fatalf("Failed to load multi-config.yaml: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	connector := &MultiConnector{
		connections: make(map[string]context.CancelFunc),
		lastActive:  time.Now(),
		handlers:    make(map[string]*adk.InProcessADKHandler),
	}

	go func() {
		<-sigCh
		log.Println("Shutting down multi-connector...")
		cancel()
	}()

	// Initialize PubSub
	ps, err := pubsub.New(cfg.PubSub.Type, cfg.PubSub.Config)
	if err != nil {
		log.Fatalf("Failed to initialize pubsub: %v", err)
	}
	defer ps.Close()
	log.Printf("PubSub initialized: %s", cfg.PubSub.Type)

	// Initialize runners and handlers for each connector
	for _, ccfg := range cfg.Connectors {
		mr, err := adk.NewMultiRunner(ctx, ccfg.AgenticConfig)
		if err != nil {
			log.Printf("Warning: failed to initialize runners for AppID %s: %v", ccfg.AppID, err)
			continue
		}
		connector.handlers[ccfg.AppID] = adk.NewInProcessADKHandler(mr)

		nkeySeed := os.Getenv(ccfg.NKeySeedEnv)
		if nkeySeed == "" {
			log.Printf("Warning: NKEY_SEED environment variable %s is not set for AppID %s", ccfg.NKeySeedEnv, ccfg.AppID)
			continue
		}

		subject := "invites." + ccfg.AppID
		// Capture loop variables
		appID := ccfg.AppID
		seed := nkeySeed
		userID := ccfg.UserID

		err = ps.Subscribe(ctx, subject, func(msg *pubsub.Message) {
			invite, err := pubsub.DecodeInviteMessage(msg.Payload)
			if err != nil {
				log.Printf("Failed to decode invite: %v", err)
				return
			}
			if invite.AppID != appID {
				return
			}
			if userID != "" && invite.UserID != userID {
				return
			}

			key := invite.ProxyURL + ":" + appID
			connector.mu.Lock()
			if _, exists := connector.connections[key]; !exists {
				log.Printf("Received invite to connect to %s for AppID %s", invite.ProxyURL, appID)
				connCtx, connCancel := context.WithCancel(ctx)
				connector.connections[key] = connCancel
				go func() {
					defer func() {
						connector.mu.Lock()
						delete(connector.connections, key)
						connector.mu.Unlock()
					}()
					// useTLS logic - for now assume false or use env
					useTLS := strings.EqualFold(os.Getenv("TLS_ENABLED"), "true")
					if err := runTunnel(connCtx, invite.ProxyURL, []byte(seed), userID, appID, useTLS, connector); err != nil {
						log.Printf("Tunnel to %s for AppID %s failed: %v", invite.ProxyURL, appID, err)
					}
				}()
			}
			connector.mu.Unlock()
		})
		if err != nil {
			log.Fatalf("Failed to subscribe to invites for AppID %s: %v", appID, err)
		}
		log.Printf("Subscribed to %s", subject)
	}

	// Inactivity monitor (optional, but good for keeping it alive)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Similar to original connector
			case <-ctx.Done():
				return
			}
		}
	}()

	<-ctx.Done()
}

func runTunnel(ctx context.Context, proxyURL string, seed []byte, userID, appID string, useTLS bool, c *MultiConnector) error {
	token, err := auth.GenerateToken(seed, userID, appID, "", 1*time.Hour)
	if err != nil {
		return err
	}

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
	md := metadata.Pairs("authorization", "Bearer "+token)
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.Connect(streamCtx)
	if err != nil {
		return err
	}

	log.Printf("Connected to Router Proxy at %s for AppID %s", proxyURL, appID)

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		go handleTunnelRequest(stream, msg, appID, c)
	}
}

func handleTunnelRequest(stream pb.TunnelService_ConnectClient, msg *pb.TunnelMessage, appID string, c *MultiConnector) {
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
		return
	}

	log.Printf("[%s] Forwarding request: %s %s (request_id=%s)", appID, httpReq.Method, httpReq.Path, msg.RequestId)

	h, ok := c.handlers[appID]
	if !ok {
		sendErrorResponse(stream, msg.RequestId, http.StatusNotFound, "AppID not found")
		return
	}

	// Prepare the HTTP request for the handler.
	req, err := http.NewRequest(httpReq.Method, httpReq.Path, bytes.NewReader(httpReq.Body))
	if err != nil {
		sendErrorResponse(stream, msg.RequestId, http.StatusInternalServerError, "failed to create request")
		return
	}
	for k, v := range httpReq.Headers {
		req.Header.Set(k, v)
	}

	// Use ResponseRecorder to capture the output.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	headers := make(map[string]string)
	for k, v := range resp.Header {
		headers[k] = v[0]
	}

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
