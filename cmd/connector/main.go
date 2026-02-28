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
	"syscall"
	"time"

	"github.com/innomon/adk-cloud-proxy/pkg/auth"
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

	go func() {
		<-sigCh
		log.Println("Shutting down connector...")
		cancel()
	}()

	// Reconnect loop.
	for {
		if err := runTunnel(ctx, routerProxyURL, []byte(nkeySeed), targetURL, userID, appID, useTLS); err != nil {
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

func runTunnel(ctx context.Context, proxyURL string, seed []byte, targetURL, userID, appID string, useTLS bool) error {
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

		go handleTunnelRequest(stream, msg, targetURL)
	}
}

func handleTunnelRequest(stream pb.TunnelService_ConnectClient, msg *pb.TunnelMessage, targetURL string) {
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
