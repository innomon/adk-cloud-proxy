# Gemini CLI Project Context

## Project Overview
ADK Cloud Proxy is a specialized reverse proxy for the [ADK (Agent Development Kit)](https://google.github.io/adk-docs/). It enables "Connectors" (agents behind firewalls) to connect to a "Router Proxy" (public-facing) using a Just-In-Time (JIT) activation model via Pub/Sub.

## Key Architecture Patterns
- **JIT Activation:** The Router Proxy publishes an invitation to Pub/Sub when it receives a request for an AppID that has no active tunnel.
- **Tunneling:** Communication happens over a bi-directional gRPC stream.
- **Dual Authentication:** Supports both NATS NKey JWTs and EdDSA-signed OAuth JWTs.
- **Reactive Connector:** The connector remains idle and only connects to the proxy when invited or when it has active sessions.

## Technical Standards
- **Go Version:** 1.25.6
- **ADK REST API Surface:** Both the `target-server` (mock) and the `adk2goose-connector` (translator) must implement the following REST routes:
    - `POST /apps/{app}/users/{user}/sessions`
    - `GET /apps/{app}/users/{user}/sessions`
    - `GET /apps/{app}/users/{user}/sessions/{session}`
    - `POST /apps/{app}/users/{user}/sessions/{session}/run_sse`
    - `DELETE /apps/{app}/users/{user}/sessions/{session}`
- **Standard Mux:** Uses the Go 1.22+ standard library `http.ServeMux` with path parameters (e.g., `{app}`).

## Key Components
- **pkg/adk:** Core ADK logic, translators, and REST handlers.
- **cmd/multi-connector:** Supports multiple AppIDs and in-process agents via `agentic`.

## Testing
- Unit tests for logic should be placed alongside the code (e.g., `pkg/adk/handler_test.go`).
- The `target-server` is a mock that can be used for end-to-end testing of the proxy and connector.

## Common Tasks
- **Updating REST Routes:** Ensure any changes to the ADK REST API surface are applied to both `cmd/target-server/main.go` and `pkg/adk/handler.go`.
- **Config Changes:** Configuration is managed in `pkg/config/config.go` and loaded from `config.yaml` or `multi-config.yaml`.
