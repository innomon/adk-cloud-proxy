package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/innomon/adk-cloud-proxy/pkg/auth"
	"github.com/innomon/adk-cloud-proxy/pkg/router"
	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// server implements the TunnelService gRPC service and an HTTP handler
// for incoming ADK client requests.
type server struct {
	pb.UnimplementedTunnelServiceServer
	registry  *router.Registry
	validator *auth.Validator
}

// Connect handles the bi-directional stream from a Connector.
func (s *server) Connect(stream pb.TunnelService_ConnectServer) error {
	// Extract JWT from gRPC metadata.
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	tokens := md.Get("authorization")
	if len(tokens) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization token")
	}
	tokenStr := strings.TrimPrefix(tokens[0], "Bearer ")

	claims, err := s.validator.Validate(tokenStr)
	if err != nil {
		return status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
	}

	log.Printf("Connector registered: userid=%s appid=%s", claims.UserID, claims.AppID)
	cs := s.registry.Register(claims.UserID, claims.AppID, stream)
	defer func() {
		s.registry.Unregister(claims.UserID, claims.AppID)
		cs.CleanupPending()
		log.Printf("Connector disconnected: userid=%s appid=%s", claims.UserID, claims.AppID)
	}()

	// Read responses from the connector and resolve pending requests.
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		cs.ResolvePending(msg.RequestId, msg)
	}
}

// handleADKRequest is the HTTP handler for incoming ADK client requests.
func (s *server) handleADKRequest(w http.ResponseWriter, r *http.Request) {
	// Extract and validate JWT.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "missing authorization header", http.StatusUnauthorized)
		return
	}
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

	claims, err := s.validator.Validate(tokenStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("authentication failed: %v", err), http.StatusUnauthorized)
		return
	}

	// Look up the connector stream.
	cs, err := s.registry.Lookup(claims.UserID, claims.AppID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Read the request body.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	// Build headers map.
	headers := make(map[string]string)
	for k, v := range r.Header {
		if !strings.EqualFold(k, "Authorization") {
			headers[k] = v[0]
		}
	}

	requestID := uuid.New().String()
	tunnelMsg := &pb.TunnelMessage{
		RequestId: requestID,
		Payload: &pb.TunnelMessage_HttpRequest{
			HttpRequest: &pb.HttpRequest{
				Method:  r.Method,
				Path:    r.URL.Path,
				Headers: headers,
				Body:    body,
			},
		},
	}

	// Register a pending response channel before sending.
	respCh := cs.RegisterPending(requestID)

	// Send the request through the tunnel.
	if err := cs.Stream.Send(tunnelMsg); err != nil {
		http.Error(w, "failed to send request to connector", http.StatusBadGateway)
		return
	}

	// Wait for the response with a timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	select {
	case resp, ok := <-respCh:
		if !ok {
			http.Error(w, "connector disconnected", http.StatusBadGateway)
			return
		}
		httpResp := resp.GetHttpResponse()
		if httpResp == nil {
			http.Error(w, "invalid response from connector", http.StatusBadGateway)
			return
		}
		for k, v := range httpResp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(int(httpResp.StatusCode))
		w.Write(httpResp.Body)
	case <-ctx.Done():
		http.Error(w, "request timed out", http.StatusGatewayTimeout)
	}
}

func main() {
	issuerPubKey := os.Getenv("ISSUER_PUBLIC_KEY")
	if issuerPubKey == "" {
		log.Fatal("ISSUER_PUBLIC_KEY environment variable is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "9090"
	}

	validator, err := auth.NewValidator(issuerPubKey)
	if err != nil {
		log.Fatalf("Failed to create validator: %v", err)
	}

	srv := &server{
		registry:  router.NewRegistry(),
		validator: validator,
	}

	// Start gRPC server for connector tunnels.
	grpcServer := grpc.NewServer()
	pb.RegisterTunnelServiceServer(grpcServer, srv)

	grpcLis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen on gRPC port %s: %v", grpcPort, err)
	}

	go func() {
		log.Printf("gRPC server listening on :%s", grpcPort)
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC server failed: %v", err)
		}
	}()

	// Start HTTP server for ADK client requests.
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleADKRequest)

	httpServer := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	go func() {
		log.Printf("HTTP server listening on :%s", port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	grpcServer.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpServer.Shutdown(ctx)
}
