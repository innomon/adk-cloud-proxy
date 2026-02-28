# ADK Server Router Proxy Implementation Plan

This plan outlines the steps to build and deploy the ADK Server Router Proxy and its Connector.

## Phase 1: Research & Setup
- [x] Initialize Go modules for the project.
- [ ] Research ADK Go SDK's `launcher` and `runner` to understand request interception.
- [x] Install dependencies:
    - `google.golang.org/adk`
    - `github.com/nats-io/jwt/v2`
    - `github.com/nats-io/nkeys`
    - `google.golang.org/grpc`
- [x] Set up project structure:
    - `cmd/router-proxy/`: Entry point for Cloud Run.
    - `cmd/connector/`: Entry point for the reverse proxy agent.
    - `pkg/auth/`: JWT and NKey verification logic.
    - `pkg/router/`: Core routing and stream management.
    - `pkg/tunnel/`: gRPC protocol definitions for the bi-directional stream.

## Phase 2: Core Components Development

### 2.1 Authentication (pkg/auth)
- [x] Implement JWT decoder and validator using `nats-io/jwt/v2`.
- [x] Implement middleware to extract and verify `userid`, `appid`, and `sessionid` from incoming ADK requests.

### 2.2 Tunnel Protocol (pkg/tunnel)
- [x] Define the `.proto` for the bi-directional gRPC stream.
- [x] Generate Go code from the proto.
- [x] The stream should support sending ADK request payloads and receiving responses.

### 2.3 Router Proxy (cmd/router-proxy)
- [x] Implement the Connector Registration gRPC service.
- [x] Implement an in-memory `Registry` to track active Connector streams.
- [x] Implement the HTTP/gRPC interceptor to route incoming ADK requests to the appropriate stream.
- [x] Add structured logging (`log/slog`) with Google Cloud Logging support (`pkg/logging`).
- [ ] Add support for multiple Cloud Run instances (optional, use Redis for session/stream tracking if needed).

## Phase 3: Connector Agent (cmd/connector)
- [x] Implement logic to connect to the Router Proxy via gRPC.
- [x] Implement heartbeat/reconnection logic for the tunnel.
- [x] Implement the request handler:
    - Receive ADK request payload from the tunnel.
    - Forward the request to a local ADK server (using the ADK Go SDK client).
    - Send the response back through the tunnel.

## Phase 3.5: ADK2Goose Connector (cmd/adk2goose-connector)
- [x] Create `pkg/goose/` package with Goose API client, types, translator, session manager, and ADK HTTP handler.
- [x] Implement ADK↔Goose request/response translation (adapted from `adk2goose` project).
- [x] Implement `cmd/adk2goose-connector/main.go`:
    - Connects to Router Proxy via gRPC tunnel (same auth/reconnect logic as ADK connector).
    - Embeds an ADK2Goose proxy handler using `httptest.ResponseRecorder`.
    - Translates ADK requests to Goose API calls and Goose SSE responses back to ADK events.
- [x] Create `Dockerfile.adk2goose-connector` for containerized deployment.
- [ ] Add unit tests for `pkg/goose/` translator and session manager.
- [ ] End-to-end test: Chatbot → Router Proxy → ADK2Goose Connector → Goose Server.

## Phase 4: Integration & Testing
- [x] Create a "Hello World" ADK server (target server).
- [x] Create a "Mock Chatbot" client that signs JWTs with NKeys.
- [x] Perform end-to-end testing:
    - Chatbot -> Router Proxy -> Connector -> Target Server.
- [x] Validate authentication failures and routing edge cases.

## Phase 5: Cloud Run Deployment
- [x] Create a multi-stage `Dockerfile` for the Router Proxy.
- [x] Create a `Dockerfile` (or build binary) for the Connector.
- [x] Set up Google Cloud Run with:
    - HTTP/2 enabled.
    - Proper IAM roles.
    - Environment variables for NKey Public Keys.
- [x] Write a `README.md` with setup and deployment instructions.

## Phase 6: WhatsApp OAuth (EdDSA JWT) Support

> Enable SPA users authenticated via the WhatsApp Gateway's OAuth flow to access the Router Proxy. The gateway issues standard EdDSA JWTs (`golang-jwt/jwt/v5`). This is **in addition** to the existing NATS NKey JWT auth used by connectors and chatbot clients.

### 6.1 Dependencies
- [ ] Add `github.com/golang-jwt/jwt/v5` to `go.mod` (the gateway already uses it; this service needs it for verification).
- Note: `crypto/ed25519` is in the Go standard library — no additional dependency.

### 6.2 OAuth Validator (`pkg/auth/oauth.go`)
- [ ] Define `OAuthClaims` struct:
  - `Sub` (phone number), `Iss`, `Aud`, `Nonce`, `PubKey` string fields.
  - Embed `jwt.RegisteredClaims` from `golang-jwt/jwt/v5`.
- [ ] Implement `OAuthValidator` struct:
  - `publicKey ed25519.PublicKey` — loaded from `OAUTH_PUBLIC_KEY` env var (base64url-encoded raw 32-byte key).
  - `issuer string` — expected `iss` claim (default: `whatsadk-gateway`).
  - `audience string` — expected `aud` claim (default: `adk-cloud-proxy`).
- [ ] `NewOAuthValidator(pubKeyBase64, issuer, audience string) (*OAuthValidator, error)` — decodes the public key, validates it is 32 bytes.
- [ ] `Validate(tokenStr string) (*Claims, error)`:
  - Parse with `jwt.Parse()` using `jwt.WithValidMethods([]string{"EdDSA"})`.
  - Verify signature with the Ed25519 public key.
  - Check `iss` matches expected issuer.
  - Check `aud` matches expected audience.
  - Check `exp` is not past.
  - Map `sub` → `Claims.UserID` (phone number used for routing).
  - Set `Claims.AppID` from request context (provided via `X-App-ID` header).
  - Return the unified `Claims` struct (same as NATS validator).

### 6.3 Dual Auth Middleware (`pkg/auth/auth.go`)
- [ ] Refactor `Validate()` or add a `DualValidator` that:
  1. Attempts NATS JWT validation first (existing `Validator.Validate()`).
  2. If NATS validation fails **and** an `OAuthValidator` is configured, attempts EdDSA OAuth validation.
  3. Returns the first successful `*Claims` or an aggregate error.
- [ ] The `DualValidator` is optional — if `OAUTH_PUBLIC_KEY` is not set, only NATS auth is active.
- [ ] Extract `X-App-ID` header from the HTTP request for OAuth JWT routing (NATS JWTs already embed `appid`).

### 6.4 Router Proxy HTTP Handler Update
- [ ] Update the auth middleware in `cmd/router-proxy/` to use `DualValidator`.
- [ ] Pass the `X-App-ID` header value into the auth context for OAuth token claim mapping.
- [ ] Ensure the unified `Claims` struct flows into the existing routing logic (`Registry.Lookup(userID, appID)`).

### 6.5 Configuration & Startup
- [ ] Read `OAUTH_PUBLIC_KEY` from environment at startup.
- [ ] Read `OAUTH_ISSUER` (default: `whatsadk-gateway`) and `OAUTH_AUDIENCE` (default: `adk-cloud-proxy`) from environment.
- [ ] If `OAUTH_PUBLIC_KEY` is set, create an `OAuthValidator` and pass it to `DualValidator`.
- [ ] Log: `🔑 WhatsApp OAuth verification enabled (EdDSA)` on startup.
- [ ] Update `Dockerfile.router-proxy` to document the new env vars.

### 6.6 Testing
- [ ] `pkg/auth/oauth_test.go`:
  - Generate an Ed25519 key pair in the test.
  - Sign a JWT with the private key using `golang-jwt/jwt/v5`.
  - Verify it with `OAuthValidator` using the public key.
  - Test: valid token → correct claims extracted.
  - Test: expired token → error.
  - Test: wrong issuer → error.
  - Test: wrong audience → error.
  - Test: tampered signature → error.
- [ ] `pkg/auth/dual_validator_test.go`:
  - NATS JWT passes → returns NATS claims.
  - NATS JWT fails, OAuth JWT passes → returns OAuth claims.
  - Both fail → returns error.
  - OAuth not configured, NATS fails → returns error (no fallback).
- [ ] Integration test: SPA-style OAuth JWT → Router Proxy → Connector → Target Server.

### 6.7 Documentation
- [ ] Update `README.md` with:
  - WhatsApp OAuth setup instructions (how to set `OAUTH_PUBLIC_KEY`).
  - Explanation of dual auth (NATS + OAuth).
  - `X-App-ID` header requirement for SPA clients.
