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
	"syscall"
	"time"

	"github.com/innomon/adk-cloud-proxy/pkg/auth"
	"github.com/innomon/adk-cloud-proxy/pkg/goose"
	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func main() {
	routerProxyURL := os.Getenv("ROUTER_PROXY_URL")
	if routerProxyURL == "" {
		log.Fatal("ROUTER_PROXY_URL environment variable is required")
	}

	nkeySeed := os.Getenv("NKEY_SEED")
	if nkeySeed == "" {
		log.Fatal("NKEY_SEED environment variable is required")
	}

	userID := os.Getenv("USER_ID")
	if userID == "" {
		log.Fatal("USER_ID environment variable is required")
	}

	appID := os.Getenv("APP_ID")
	if appID == "" {
		log.Fatal("APP_ID environment variable is required")
	}

	gooseBaseURL := envOrDefault("GOOSE_BASE_URL", "http://127.0.0.1:3000")
	gooseSecret := os.Getenv("GOOSE_SECRET_KEY")
	workingDir := envOrDefault("WORKING_DIR", ".")

	useTLS := strings.EqualFold(os.Getenv("TLS_ENABLED"), "true")

	// Create the embedded Goose proxy handler.
	gooseClient := goose.NewClient(gooseBaseURL, gooseSecret)
	sessionMgr := goose.NewSessionManager(gooseClient, workingDir)
	handler := goose.NewHandler(sessionMgr, gooseClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down adk2goose connector...")
		cancel()
	}()

	// Reconnect loop.
	for {
		if err := runTunnel(ctx, routerProxyURL, []byte(nkeySeed), userID, appID, useTLS, handler); err != nil {
			if ctx.Err() != nil {
				log.Println("Connector stopped")
				return
			}
			log.Printf("Tunnel disconnected: %v. Reconnecting in 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		return
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runTunnel(ctx context.Context, proxyURL string, seed []byte, userID, appID string, useTLS bool, handler http.Handler) error {
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

		go handleTunnelRequest(stream, msg, handler)
	}
}

func handleTunnelRequest(stream pb.TunnelService_ConnectClient, msg *pb.TunnelMessage, handler http.Handler) {
	httpReq := msg.GetHttpRequest()
	if httpReq == nil {
		log.Printf("Received non-request message for request_id=%s, ignoring", msg.RequestId)
		return
	}

	log.Printf("Processing ADK→Goose request: %s %s (request_id=%s)", httpReq.Method, httpReq.Path, msg.RequestId)

	// Build HTTP request for the embedded handler.
	req, err := http.NewRequest(httpReq.Method, httpReq.Path, bytes.NewReader(httpReq.Body))
	if err != nil {
		sendErrorResponse(stream, msg.RequestId, http.StatusInternalServerError, "failed to create request")
		return
	}
	for k, v := range httpReq.Headers {
		req.Header.Set(k, v)
	}

	// Serve through the embedded adk2goose handler.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	result := recorder.Result()
	body, err := io.ReadAll(result.Body)
	result.Body.Close()
	if err != nil {
		sendErrorResponse(stream, msg.RequestId, http.StatusInternalServerError, "failed to read response body")
		return
	}

	// Build response headers.
	headers := make(map[string]string)
	for k, v := range result.Header {
		headers[k] = v[0]
	}

	// Send response back through the tunnel.
	respMsg := &pb.TunnelMessage{
		RequestId: msg.RequestId,
		Payload: &pb.TunnelMessage_HttpResponse{
			HttpResponse: &pb.HttpResponse{
				StatusCode: int32(result.StatusCode),
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
