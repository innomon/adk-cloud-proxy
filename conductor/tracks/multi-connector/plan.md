# Implementation Plan - Multi-Connector Support

## Objective
Create a `multi-connector` that supports multiple AppIds and runs multiple ADK instances in-process using `agentic` configurations.

## Proposed Changes

### 1. Configuration
- Introduce a new configuration structure for the multi-connector.
- Support loading from a dedicated `multi-config.yaml`.
- Schema:
  ```yaml
  connectors:
    - app_id: "example-app"
      nkey_seed_env: "NKEY_SEED_EXAMPLE"
      agentic_config: "config/example-agentic.yaml"
      user_id: "optional-user-id"
  pubsub:
    # shared pubsub config
  ```

### 2. Multi-Connector Main (`cmd/multi-connector/main.go`)
- **Initialization**:
  - Load multi-config.
  - Initialize PubSub.
  - For each connector:
    - Load `agentic` config.
    - Initialize `registry.Registry` and `runner.Runner`s for each agent.
    - Store in `map[string]map[string]*runner.Runner` (AppID -> AgentName -> Runner).
    - Subscribe to `invites.{AppID}`.
- **Tunnel Management**:
  - When an invite is received, start a tunnel for that `AppID`.
  - Pass the set of runners for that `AppID` to the tunnel handler.
- **Request Routing**:
  - Parse `HttpRequest.Path` to extract `{app}` (AgentName).
  - Look up the `runner.Runner`.
  - If found, invoke it using a custom `http.ResponseWriter` that forwards results back to the gRPC stream.
  - If not found, return 404.

### 3. Dependencies
- Add `github.com/innomon/agentic` to `go.mod` (if possible, or assume it will be provided).
- Add `google.golang.org/adk` to `go.mod`.

## Checklist

### Phase 1: Setup & Config
- [x] Define `MultiConnectorConfig` struct in `pkg/config/config.go` or a new file.
- [x] Create a sample `multi-config.yaml`.
- [x] Implement loading logic for the new config.

### Phase 2: In-Process ADK Initialization
- [x] Implement `initRunners(agenticConfigPath string)` that returns `map[string]*runner.Runner`.
- [x] Integrate with `github.com/innomon/agentic` registry and launcher.

### Phase 3: Multi-Tunnel Logic
- [x] Create `cmd/multi-connector/main.go` based on `cmd/connector/main.go`.
- [x] Refactor `runTunnel` and `handleTunnelRequest` to be more generic and support in-process routing.
- [x] Implement `ResponseWriter` wrapper for gRPC stream (using `httptest.ResponseRecorder`).

### Phase 4: Routing & Dispatch
- [x] Extract `AgentName` from ADK REST paths (e.g., `/apps/{app}/...`).
- [x] Implement lookup logic for the correct runner based on `AppID` (from tunnel) and `AgentName` (from path).

### Phase 5: Verification
- [ ] Create a mock `agentic` config for testing.
- [ ] Run `multi-connector` and verify it responds to invites for multiple AppIds.
- [ ] Verify requests are correctly routed to the in-process agents.

## Verification Plan
- Use `target-server` as a reference for expected ADK REST responses.
- Use a local PubSub (e.g., NATS or Redis) to trigger invites.
- Use `curl` through the `router-proxy` to verify end-to-end connectivity.
