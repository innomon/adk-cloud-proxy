# ADK Cloud Proxy – Agent Guidelines

## Project Overview

This is an ADK (Agent Development Kit) Server Router Proxy system written in Go, deployed on Google Cloud Run. It routes requests from chatbot clients to ADK servers behind firewalls via reverse-proxy "Connectors" over gRPC bi-directional streams.

## Architecture

- **Router Proxy** (`cmd/router-proxy/`): Cloud Run service that authenticates clients/connectors and routes ADK requests to the correct connector stream.
- **Connector** (`cmd/connector/`): Runs in private networks, maintains a gRPC tunnel to the Router Proxy, and forwards requests to a local ADK server.
- **Auth** (`pkg/auth/`): JWT validation using NATS NKeys (Ed25519). Verifies `sub`, `iss`, `userid`, `appid`, `sessionid` claims.
- **Logging** (`pkg/logging/`): Structured logging setup using `log/slog`. Auto-detects Cloud Run via `K_SERVICE` for JSON output compatible with Google Cloud Logging.
- **Router** (`pkg/router/`): In-memory registry mapping `(userid, appid)` to active gRPC streams.
- **Tunnel** (`pkg/tunnel/`): Protobuf-defined gRPC bi-directional streaming service for connector communication.

## Tech Stack & Dependencies

- **Language:** Go
- **Key libraries:**
  - `google.golang.org/adk` – ADK Go SDK
  - `github.com/nats-io/jwt/v2` – JWT encoding/decoding
  - `github.com/nats-io/nkeys` – Ed25519 NKey operations
  - `google.golang.org/grpc` – gRPC framework
- **Deployment:** Google Cloud Run with HTTP/2 enabled

## Code Conventions

- Follow standard Go project layout (`cmd/`, `pkg/`).
- Protobuf definitions live in `pkg/tunnel/` and generated code stays alongside them.
- Authentication is NKey/JWT-based (not OAuth, not API keys). Use `nats-io/jwt/v2` for all JWT operations.
- Routing keys are always the tuple `(userid, appid)` extracted from JWT claims.
- The connector-to-proxy link is a gRPC bi-directional stream ("tunnel"), not regular unary RPCs.

## Security Rules

- Never log or expose NKey seeds, private keys, or raw JWT tokens.
- Always validate JWT signatures against the configured issuer public key before processing any request.
- Connector and client authentication are both mandatory; never bypass auth checks.

## Testing

- End-to-end flow: Chatbot → Router Proxy → Connector → Target ADK Server.
- Test authentication failures (invalid JWT, wrong issuer, expired tokens).
- Test routing edge cases (no connector registered, connector disconnects mid-request).

## ADK SDK

- This project uses the **Go ADK SDK** (`google.golang.org/adk`). Do NOT use Python, Node.js, or any other language SDK.
- Refer to Go ADK SDK documentation and APIs for all ADK-related implementation.

## Documentation

- Keep `AGENTS.md`, `SPECIFICATION.md`, and `IMPLEMENTATION_PLAN.md` up to date as the project evolves.
- When adding new packages, endpoints, or changing architecture, update the relevant documentation files.
- Mark completed tasks in `IMPLEMENTATION_PLAN.md` as done (`[x]`).
