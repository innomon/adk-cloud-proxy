package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/innomon/adk-cloud-proxy/pkg/auth"
	"github.com/innomon/adk-cloud-proxy/pkg/config"
	"github.com/innomon/adk-cloud-proxy/pkg/logging"
	"github.com/innomon/adk-cloud-proxy/pkg/pubsub"
	"github.com/innomon/adk-cloud-proxy/pkg/router"
	pb "github.com/innomon/adk-cloud-proxy/pkg/tunnel"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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
	validator *auth.DualValidator
	pubsub    pubsub.PubSub
	config    *config.Config
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

	claims, err := s.validator.Validate(tokenStr, "")
	if err != nil {
		slog.Warn("connector authentication failed", "error", err)
		return status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
	}

	slog.Info("connector registered", "appid", claims.AppID)
	cs := s.registry.Register(claims.AppID, stream)
	defer func() {
		s.registry.Unregister(claims.AppID)
		cs.CleanupPending()
		slog.Info("connector disconnected", "appid", claims.AppID)
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
	appID := r.Header.Get("X-App-ID")

	claims, err := s.validator.Validate(tokenStr, appID)
	if err != nil {
		slog.Warn("client authentication failed", "error", err, "method", r.Method, "path", r.URL.Path)
		http.Error(w, fmt.Sprintf("authentication failed: %v", err), http.StatusUnauthorized)
		return
	}

	requestID := uuid.New().String()
	logger := slog.With("request_id", requestID, "userid", claims.UserID, "appid", claims.AppID)

	logger.Info("request received", "method", r.Method, "path", r.URL.Path)

	// Look up the connector stream.
	cs, err := s.registry.Lookup(claims.AppID)
	if err != nil {
		logger.Warn("no connector available, sending invite", "error", err)

		// Send Pub/Sub invite
		invite := &pubsub.InviteMessage{
			AppID:    claims.AppID,
			UserID:   claims.UserID,
			ProxyURL: s.config.Proxy.URL,
		}
		data, err := pubsub.EncodeInviteMessage(invite)
		if err == nil && s.pubsub != nil {
			subject := fmt.Sprintf("invites.%s", claims.AppID)
			if pErr := s.pubsub.Publish(r.Context(), subject, data); pErr != nil {
				logger.Error("failed to publish invite", "error", pErr)
			} else {
				logger.Info("invite published", "subject", subject)
			}
		}

		http.Error(w, "please wait, preparing connection", http.StatusServiceUnavailable)
		return
	}

	// Read the request body.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("failed to read request body", "error", err)
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
	// Inject identity headers for the target ADK server.
	headers["X-User-ID"] = claims.UserID
	headers["X-App-ID"] = claims.AppID

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
		logger.Error("failed to send request to connector", "error", err)
		http.Error(w, "failed to send request to connector", http.StatusBadGateway)
		return
	}

	// Wait for the response with a timeout.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	select {
	case resp, ok := <-respCh:
		if !ok {
			logger.Warn("connector disconnected during request")
			http.Error(w, "connector disconnected", http.StatusBadGateway)
			return
		}
		httpResp := resp.GetHttpResponse()
		if httpResp == nil {
			logger.Error("invalid response from connector")
			http.Error(w, "invalid response from connector", http.StatusBadGateway)
			return
		}
		for k, v := range httpResp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(int(httpResp.StatusCode))
		w.Write(httpResp.Body)
		logger.Info("response sent", "status", httpResp.StatusCode)
	case <-ctx.Done():
		logger.Warn("request timed out")
		http.Error(w, "request timed out", http.StatusGatewayTimeout)
	}
}

func main() {
	logging.Setup()

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		slog.Warn("failed to load config.yaml, using defaults", "error", err)
		cfg = &config.Config{}
	}

	// Override from env if provided
	if proxyURL := os.Getenv("PROXY_URL"); proxyURL != "" {
		cfg.Proxy.URL = proxyURL
	}

	var ps pubsub.PubSub
	if cfg.PubSub.Type != "" {
		ps, err = pubsub.New(cfg.PubSub.Type, cfg.PubSub.Config)
		if err != nil {
			slog.Error("failed to initialize pubsub", "error", err)
			os.Exit(1)
		}
		defer ps.Close()
		slog.Info("pubsub initialized", "type", cfg.PubSub.Type)
	}

	issuerPubKey := os.Getenv("ISSUER_PUBLIC_KEY")
	if issuerPubKey == "" {
		slog.Error("ISSUER_PUBLIC_KEY environment variable is required")
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	grpcPort := os.Getenv("GRPC_PORT")

	natsValidator, err := auth.NewValidator(issuerPubKey)
	if err != nil {
		slog.Error("failed to create NATS validator", "error", err)
		os.Exit(1)
	}

	var oauthValidator *auth.OAuthValidator
	if oauthPubKey := os.Getenv("OAUTH_PUBLIC_KEY"); oauthPubKey != "" {
		oauthIssuer := os.Getenv("OAUTH_ISSUER")
		if oauthIssuer == "" {
			oauthIssuer = "whatsadk-gateway"
		}
		oauthAudience := os.Getenv("OAUTH_AUDIENCE")
		if oauthAudience == "" {
			oauthAudience = "adk-cloud-proxy"
		}
		oauthValidator, err = auth.NewOAuthValidator(oauthPubKey, oauthIssuer, oauthAudience)
		if err != nil {
			slog.Error("failed to create OAuth validator", "error", err)
			os.Exit(1)
		}
		slog.Info("🔑 WhatsApp OAuth verification enabled (EdDSA)")
	}

	srv := &server{
		registry:  router.NewRegistry(),
		validator: auth.NewDualValidator(natsValidator, oauthValidator),
		pubsub:    ps,
		config:    cfg,
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTunnelServiceServer(grpcServer, srv)

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleADKRequest)

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if grpcPort == "" || grpcPort == port {
		// Combined mode: serve HTTP and gRPC on a single port (required for Cloud Run).
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
				grpcServer.ServeHTTP(w, r)
			} else {
				mux.ServeHTTP(w, r)
			}
		})

		httpServer := &http.Server{
			Addr:    ":" + port,
			Handler: h2c.NewHandler(handler, &http2.Server{}),
		}

		go func() {
			slog.Info("combined HTTP+gRPC server started", "port", port)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server failed", "error", err)
				os.Exit(1)
			}
		}()

		<-sigCh
		slog.Info("shutting down")
		grpcServer.GracefulStop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	} else {
		// Dual port mode: separate HTTP and gRPC ports (for local development).
		grpcLis, err := net.Listen("tcp", ":"+grpcPort)
		if err != nil {
			slog.Error("failed to listen on gRPC port", "port", grpcPort, "error", err)
			os.Exit(1)
		}

		go func() {
			slog.Info("gRPC server started", "port", grpcPort)
			if err := grpcServer.Serve(grpcLis); err != nil {
				slog.Error("gRPC server failed", "error", err)
				os.Exit(1)
			}
		}()

		httpServer := &http.Server{
			Addr:    ":" + port,
			Handler: mux,
		}

		go func() {
			slog.Info("HTTP server started", "port", port)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTP server failed", "error", err)
				os.Exit(1)
			}
		}()

		<-sigCh
		slog.Info("shutting down")
		grpcServer.GracefulStop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}
}
