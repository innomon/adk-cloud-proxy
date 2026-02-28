# ADK Server Router Proxy Implementation Plan

This plan outlines the steps to build and deploy the ADK Server Router Proxy and its Connector.

## Phase 1: Research & Setup
- [ ] Initialize Go modules for the project.
- [ ] Research ADK Go SDK's `launcher` and `runner` to understand request interception.
- [ ] Install dependencies:
    - `google.golang.org/adk`
    - `github.com/nats-io/jwt/v2`
    - `github.com/nats-io/nkeys`
    - `google.golang.org/grpc`
- [ ] Set up project structure:
    - `cmd/router-proxy/`: Entry point for Cloud Run.
    - `cmd/connector/`: Entry point for the reverse proxy agent.
    - `pkg/auth/`: JWT and NKey verification logic.
    - `pkg/router/`: Core routing and stream management.
    - `pkg/tunnel/`: gRPC protocol definitions for the bi-directional stream.

## Phase 2: Core Components Development

### 2.1 Authentication (pkg/auth)
- [ ] Implement JWT decoder and validator using `nats-io/jwt/v2`.
- [ ] Implement middleware to extract and verify `userid`, `appid`, and `sessionid` from incoming ADK requests.

### 2.2 Tunnel Protocol (pkg/tunnel)
- [ ] Define the `.proto` for the bi-directional gRPC stream.
- [ ] Generate Go code from the proto.
- [ ] The stream should support sending ADK request payloads and receiving responses.

### 2.3 Router Proxy (cmd/router-proxy)
- [ ] Implement the Connector Registration gRPC service.
- [ ] Implement an in-memory `Registry` to track active Connector streams.
- [ ] Implement the HTTP/gRPC interceptor to route incoming ADK requests to the appropriate stream.
- [ ] Add support for multiple Cloud Run instances (optional, use Redis for session/stream tracking if needed).

## Phase 3: Connector Agent (cmd/connector)
- [ ] Implement logic to connect to the Router Proxy via gRPC.
- [ ] Implement heartbeat/reconnection logic for the tunnel.
- [ ] Implement the request handler:
    - Receive ADK request payload from the tunnel.
    - Forward the request to a local ADK server (using the ADK Go SDK client).
    - Send the response back through the tunnel.

## Phase 4: Integration & Testing
- [ ] Create a "Hello World" ADK server (target server).
- [ ] Create a "Mock Chatbot" client that signs JWTs with NKeys.
- [ ] Perform end-to-end testing:
    - Chatbot -> Router Proxy -> Connector -> Target Server.
- [ ] Validate authentication failures and routing edge cases.

## Phase 5: Cloud Run Deployment
- [ ] Create a multi-stage `Dockerfile` for the Router Proxy.
- [ ] Create a `Dockerfile` (or build binary) for the Connector.
- [ ] Set up Google Cloud Run with:
    - HTTP/2 enabled.
    - Proper IAM roles.
    - Environment variables for NKey Public Keys.
- [ ] Write a `README.md` with setup and deployment instructions.
