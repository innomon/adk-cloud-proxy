package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/innomon/adk-cloud-proxy/pkg/auth"
	"github.com/innomon/adk-cloud-proxy/pkg/router"
	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
	"github.com/nats-io/nkeys"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// testEnv holds all the servers and credentials needed for integration tests.
type testEnv struct {
	// NKey credentials
	seed   []byte
	pubKey string

	// Target ADK server
	targetAddr string

	// Router Proxy
	httpAddr string
	grpcAddr string

	// Cleanup
	cleanup func()
}

// proxyServer duplicates the router-proxy server logic for in-process testing.
type proxyServer struct {
	pb.UnimplementedTunnelServiceServer
	registry  *router.Registry
	validator *auth.Validator
}

func (s *proxyServer) Connect(stream pb.TunnelService_ConnectServer) error {
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

	cs := s.registry.Register(claims.AppID, stream)
	defer func() {
		s.registry.Unregister(claims.AppID)
		cs.CleanupPending()
	}()

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

func (s *proxyServer) handleADKRequest(w http.ResponseWriter, r *http.Request) {
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

	cs, err := s.registry.Lookup(claims.AppID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		if !strings.EqualFold(k, "Authorization") {
			headers[k] = v[0]
		}
	}
	headers["X-User-ID"] = claims.UserID
	headers["X-App-ID"] = claims.AppID

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

	respCh := cs.RegisterPending(requestID)

	if err := cs.Stream.Send(tunnelMsg); err != nil {
		http.Error(w, "failed to send request to connector", http.StatusBadGateway)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
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

// startTargetServer starts a simple HTTP server that echoes requests.
func startTargetServer(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		resp := map[string]string{
			"message": "Hello from target ADK server",
			"echo":    string(body),
			"path":    r.URL.Path,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for target server: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(lis)
	return lis.Addr().String(), func() { srv.Close() }
}

// startConnector connects a connector to the router proxy and forwards to the target server.
func startConnector(t *testing.T, grpcAddr, targetAddr string, seed []byte, userID, appID string) (cancel context.CancelFunc) {
	t.Helper()
	token, err := auth.GenerateToken(seed, userID, appID, "", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate connector token: %v", err)
	}

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to grpc: %v", err)
	}

	client := pb.NewTunnelServiceClient(conn)
	md := metadata.Pairs("authorization", "Bearer "+token)
	ctx, cancelFn := context.WithCancel(context.Background())
	streamCtx := metadata.NewOutgoingContext(ctx, md)

	stream, err := client.Connect(streamCtx)
	if err != nil {
		conn.Close()
		cancelFn()
		t.Fatalf("failed to open stream: %v", err)
	}

	go func() {
		defer conn.Close()
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			go func(msg *pb.TunnelMessage) {
				httpReq := msg.GetHttpRequest()
				if httpReq == nil {
					return
				}
				url := "http://" + targetAddr + httpReq.Path
				req, err := http.NewRequest(httpReq.Method, url, bytes.NewReader(httpReq.Body))
				if err != nil {
					return
				}
				for k, v := range httpReq.Headers {
					req.Header.Set(k, v)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					stream.Send(&pb.TunnelMessage{
						RequestId: msg.RequestId,
						Payload: &pb.TunnelMessage_HttpResponse{
							HttpResponse: &pb.HttpResponse{
								StatusCode: http.StatusBadGateway,
								Body:       []byte("failed to reach target"),
							},
						},
					})
					return
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				headers := make(map[string]string)
				for k, v := range resp.Header {
					headers[k] = v[0]
				}
				stream.Send(&pb.TunnelMessage{
					RequestId: msg.RequestId,
					Payload: &pb.TunnelMessage_HttpResponse{
						HttpResponse: &pb.HttpResponse{
							StatusCode: int32(resp.StatusCode),
							Headers:    headers,
							Body:       body,
						},
					},
				})
			}(msg)
		}
	}()

	return cancelFn
}

// setupTestEnv starts all components and returns the test environment.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	// Create NKey keypair.
	kp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatalf("failed to create keypair: %v", err)
	}
	seed, err := kp.Seed()
	if err != nil {
		t.Fatalf("failed to get seed: %v", err)
	}
	pubKey, err := kp.PublicKey()
	if err != nil {
		t.Fatalf("failed to get public key: %v", err)
	}

	// Start target server.
	targetAddr, targetCleanup := startTargetServer(t)

	// Start router proxy (gRPC + HTTP).
	validator, err := auth.NewValidator(pubKey)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	srv := &proxyServer{
		registry:  router.NewRegistry(),
		validator: validator,
	}

	grpcLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for grpc: %v", err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterTunnelServiceServer(grpcServer, srv)
	go grpcServer.Serve(grpcLis)

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", srv.handleADKRequest)
	httpLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for http: %v", err)
	}
	httpServer := &http.Server{Handler: httpMux}
	go httpServer.Serve(httpLis)

	// Start connector.
	connectorCancel := startConnector(t, grpcLis.Addr().String(), targetAddr, seed, "user1", "app1")

	// Give the connector time to register.
	time.Sleep(200 * time.Millisecond)

	return &testEnv{
		seed:       seed,
		pubKey:     pubKey,
		targetAddr: targetAddr,
		httpAddr:   httpLis.Addr().String(),
		grpcAddr:   grpcLis.Addr().String(),
		cleanup: func() {
			connectorCancel()
			grpcServer.GracefulStop()
			httpServer.Close()
			targetCleanup()
		},
	}
}

// sendChatbotRequest sends an HTTP request to the router proxy as a chatbot would.
func sendChatbotRequest(t *testing.T, httpAddr, token, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+httpAddr+"/test-path", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp, string(respBody)
}

// --- End-to-End Tests ---

func TestE2E_ChatbotToTargetServer(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	token, err := auth.GenerateToken(env.seed, "user1", "app1", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	resp, body := sendChatbotRequest(t, env.httpAddr, token, `{"message":"hello"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]string
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["message"] != "Hello from target ADK server" {
		t.Errorf("unexpected message: %q", result["message"])
	}
	if result["path"] != "/test-path" {
		t.Errorf("unexpected path: %q", result["path"])
	}
	if !strings.Contains(result["echo"], "hello") {
		t.Errorf("expected echo to contain 'hello', got %q", result["echo"])
	}
}

func TestE2E_MultipleRequests(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	token, err := auth.GenerateToken(env.seed, "user1", "app1", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	for i := 0; i < 5; i++ {
		body := fmt.Sprintf(`{"request":%d}`, i)
		resp, respBody := sendChatbotRequest(t, env.httpAddr, token, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d: %s", i, resp.StatusCode, respBody)
		}

		var result map[string]string
		if err := json.Unmarshal([]byte(respBody), &result); err != nil {
			t.Fatalf("request %d: failed to parse response: %v", i, err)
		}
		if !strings.Contains(result["echo"], fmt.Sprintf(`"request":%d`, i)) {
			t.Errorf("request %d: echo mismatch: %q", i, result["echo"])
		}
	}
}

// --- Authentication Failure Tests ---

func TestE2E_MissingAuthHeader(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	req, _ := http.NewRequest(http.MethodPost, "http://"+env.httpAddr+"/test", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestE2E_InvalidJWT(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	resp, body := sendChatbotRequest(t, env.httpAddr, "invalid-token-string", "{}")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2E_WrongIssuer(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	// Generate a token with a different keypair.
	otherKP, _ := nkeys.CreateAccount()
	otherSeed, _ := otherKP.Seed()

	token, err := auth.GenerateToken(otherSeed, "user1", "app1", "", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	resp, body := sendChatbotRequest(t, env.httpAddr, token, "{}")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2E_ExpiredToken(t *testing.T) {
	env := setupTestEnv(t)
	defer env.cleanup()

	token, err := auth.GenerateToken(env.seed, "user1", "app1", "", -1*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	resp, body := sendChatbotRequest(t, env.httpAddr, token, "{}")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", resp.StatusCode, body)
	}
}

// --- Routing Edge Case Tests ---

func TestE2E_NoConnectorRegistered(t *testing.T) {
	// Create a minimal env without connector.
	kp, _ := nkeys.CreateAccount()
	seed, _ := kp.Seed()
	pubKey, _ := kp.PublicKey()

	validator, _ := auth.NewValidator(pubKey)
	srv := &proxyServer{
		registry:  router.NewRegistry(),
		validator: validator,
	}

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", srv.handleADKRequest)
	httpLis, _ := net.Listen("tcp", "127.0.0.1:0")
	httpServer := &http.Server{Handler: httpMux}
	go httpServer.Serve(httpLis)
	defer httpServer.Close()

	token, _ := auth.GenerateToken(seed, "nouser", "noapp", "", 1*time.Hour)
	resp, body := sendChatbotRequest(t, httpLis.Addr().String(), token, "{}")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2E_ConnectorDisconnectMidRequest(t *testing.T) {
	kp, _ := nkeys.CreateAccount()
	seed, _ := kp.Seed()
	pubKey, _ := kp.PublicKey()

	targetAddr, targetCleanup := startTargetServer(t)
	defer targetCleanup()

	validator, _ := auth.NewValidator(pubKey)
	srv := &proxyServer{
		registry:  router.NewRegistry(),
		validator: validator,
	}

	grpcLis, _ := net.Listen("tcp", "127.0.0.1:0")
	grpcServer := grpc.NewServer()
	pb.RegisterTunnelServiceServer(grpcServer, srv)
	go grpcServer.Serve(grpcLis)
	defer grpcServer.GracefulStop()

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/", srv.handleADKRequest)
	httpLis, _ := net.Listen("tcp", "127.0.0.1:0")
	httpServer := &http.Server{Handler: httpMux}
	go httpServer.Serve(httpLis)
	defer httpServer.Close()

	// Start a connector that will be cancelled.
	connectorCancel := startConnector(t, grpcLis.Addr().String(), targetAddr, seed, "user1", "app1")
	time.Sleep(200 * time.Millisecond)

	// First request should succeed.
	token, _ := auth.GenerateToken(seed, "user1", "app1", "", 1*time.Hour)
	resp, body := sendChatbotRequest(t, httpLis.Addr().String(), token, `{"test":"before disconnect"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 before disconnect, got %d: %s", resp.StatusCode, body)
	}

	// Disconnect the connector.
	connectorCancel()
	time.Sleep(200 * time.Millisecond)

	// Request after disconnect should fail with 503 (no connector registered).
	resp, body = sendChatbotRequest(t, httpLis.Addr().String(), token, `{"test":"after disconnect"}`)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after disconnect, got %d: %s", resp.StatusCode, body)
	}
}

func TestE2E_ConnectorGRPCAuthFailure(t *testing.T) {
	kp, _ := nkeys.CreateAccount()
	pubKey, _ := kp.PublicKey()

	validator, _ := auth.NewValidator(pubKey)
	srv := &proxyServer{
		registry:  router.NewRegistry(),
		validator: validator,
	}

	grpcLis, _ := net.Listen("tcp", "127.0.0.1:0")
	grpcServer := grpc.NewServer()
	pb.RegisterTunnelServiceServer(grpcServer, srv)
	go grpcServer.Serve(grpcLis)
	defer grpcServer.GracefulStop()

	// Try connecting with a different key.
	otherKP, _ := nkeys.CreateAccount()
	otherSeed, _ := otherKP.Seed()

	token, _ := auth.GenerateToken(otherSeed, "user1", "app1", "", 1*time.Hour)
	conn, err := grpc.NewClient(grpcLis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTunnelServiceClient(conn)
	md := metadata.Pairs("authorization", "Bearer "+token)
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// The server should close the stream with Unauthenticated.
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error from server")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}
}
