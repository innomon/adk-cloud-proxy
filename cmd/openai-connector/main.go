package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
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
	"github.com/innomon/adk-cloud-proxy/pkg/openai"
	"github.com/innomon/adk-cloud-proxy/pkg/pubsub"
	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

type Connector struct {
	mu             sync.Mutex
	connections    map[string]context.CancelFunc // proxyURL -> cancel func
	activeSessions int32
	lastActive     time.Time
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

	targetURL := os.Getenv("TARGET_OPENAI_SERVER_URL")
	if targetURL == "" {
		targetURL = "http://localhost:11434" // Default to local Ollama
	}

	appID := os.Getenv("APP_ID")
	if appID == "" {
		log.Fatal("APP_ID environment variable is required")
	}

	userID := os.Getenv("USER_ID")
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
		log.Println("Shutting down OpenAI connector...")
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
			if invite.AppID != appID {
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
	}

	// Inactivity monitor (same as standard connector)
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

				if sessions == 0 && numConns > 0 && idleTime > 5*time.Minute {
					log.Println("Inactivity timeout reached, closing connections...")
					for url, cancelFunc := range connector.connections {
						cancelFunc()
						delete(connector.connections, url)
					}
				}
				
				if sessions == 0 && numConns == 0 && idleTime > 10*time.Minute {
					connector.mu.Unlock()
					log.Println("No activity for 10 minutes, shutting down OpenAI connector...")
					cancel()
					return
				}
				connector.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	<-ctx.Done()
}

func runTunnel(ctx context.Context, proxyURL string, seed []byte, targetURL, userID, appID string, useTLS bool, c *Connector) error {
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

	log.Printf("OpenAI Connector connected to Router Proxy at %s", proxyURL)

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
		return
	}

	// We only translate session/run_sse requests.
	// Others (like session creation) are mocked locally.
	if strings.HasSuffix(httpReq.Path, "/run_sse") {
		handleRunSSE(stream, msg.RequestId, httpReq, targetURL)
	} else if httpReq.Method == http.MethodPost && strings.HasSuffix(httpReq.Path, "/sessions") {
		// Mock session creation
		sendJSONResponse(stream, msg.RequestId, http.StatusOK, map[string]string{"id": "oa-session-" + msg.RequestId})
	} else {
		// Default: Not Found
		sendJSONResponse(stream, msg.RequestId, http.StatusNotFound, map[string]string{"error": "not supported by openai-connector"})
	}
}

func handleRunSSE(stream pb.TunnelService_ConnectClient, requestID string, httpReq *pb.HttpRequest, targetURL string) {
	var adkReq openai.ADKRunRequest
	if err := json.Unmarshal(httpReq.Body, &adkReq); err != nil {
		sendErrorResponse(stream, requestID, http.StatusBadRequest, "failed to decode ADK request")
		return
	}

	// Translate ADK request to OpenAI Chat Completion Request
	messages := openai.ADKContentToOpenAIMessages(adkReq.NewMessage)
	oaReq := openai.ChatCompletionRequest{
		Model:    "ollama", // or detect from path/headers if needed
		Messages: messages,
		Stream:   true,
	}

	body, _ := json.Marshal(oaReq)
	url := targetURL + "/v1/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		sendErrorResponse(stream, requestID, http.StatusInternalServerError, "failed to create local request")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sendErrorResponse(stream, requestID, http.StatusBadGateway, "failed to reach local OpenAI server")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		sendErrorResponse(stream, requestID, resp.StatusCode, string(body))
		return
	}

	// Translate OpenAI SSE stream back to ADK events
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			// Send turn complete
			sendADKEvent(stream, requestID, &openai.ADKEvent{TurnComplete: true})
			break
		}

		var chunk openai.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		event := openai.OpenAIChunkToADKEvent(&chunk)
		if event != nil {
			sendADKEvent(stream, requestID, event)
		}
	}
}

func sendADKEvent(stream pb.TunnelService_ConnectClient, requestID string, event *openai.ADKEvent) {
	data, _ := json.Marshal(event)
	respMsg := &pb.TunnelMessage{
		RequestId: requestID,
		Payload: &pb.TunnelMessage_HttpResponse{
			HttpResponse: &pb.HttpResponse{
				StatusCode: http.StatusOK,
				Headers:    map[string]string{"Content-Type": "text/event-stream"},
				Body:       []byte(fmt.Sprintf("data: %s\n\n", data)),
			},
		},
	}
	stream.Send(respMsg)
}

func sendJSONResponse(stream pb.TunnelService_ConnectClient, requestID string, statusCode int, v any) {
	body, _ := json.Marshal(v)
	respMsg := &pb.TunnelMessage{
		RequestId: requestID,
		Payload: &pb.TunnelMessage_HttpResponse{
			HttpResponse: &pb.HttpResponse{
				StatusCode: int32(statusCode),
				Headers:    map[string]string{"Content-Type": "application/json"},
				Body:       body,
			},
		},
	}
	stream.Send(respMsg)
}

func sendErrorResponse(stream pb.TunnelService_ConnectClient, requestID string, statusCode int, message string) {
	sendJSONResponse(stream, requestID, statusCode, map[string]string{"error": message})
}
