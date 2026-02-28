# ADK Cloud Proxy

> ⚠️ **Work In Progress** — This project is under active development. APIs and configuration are subject to change.

ADK Cloud Proxy routes requests from chatbot clients to [ADK](https://google.github.io/adk-docs/) servers running behind firewalls. It uses reverse-proxy "Connectors" that maintain gRPC bi-directional streams through a centralized Router Proxy deployed on Google Cloud Run.

## Architecture

```
Chatbot ──► Router Proxy (Cloud Run) ◄── Connector ──► Target ADK Server
              (public internet)           (private network)
```

1. A **Connector** in the private network opens a persistent gRPC tunnel to the Router Proxy.
2. A **Chatbot** sends an ADK request with a signed JWT to the Router Proxy.
3. The Router Proxy authenticates the JWT, looks up the matching Connector by `(userid, appid)`, and forwards the request through the tunnel.
4. The Connector forwards the request to the local ADK server and returns the response.

## Project Structure

```
cmd/
  router-proxy/           Cloud Run entry point
  connector/              Reverse-proxy agent for private networks (ADK target)
  adk2goose-connector/    Reverse-proxy agent that bridges ADK to Goose servers
  mock-chatbot/           Test client that signs JWTs with NKeys
  target-server/          Sample "Hello World" ADK server
pkg/
  auth/                   JWT validation using NATS NKeys (Ed25519)
  goose/                  Goose API client, ADK↔Goose translator, session manager
  logging/                Structured logging (slog) with Google Cloud Logging support
  router/                 In-memory registry mapping (userid, appid) → active streams
  tunnel/                 Protobuf-defined gRPC bi-directional streaming service
```

## Prerequisites

- Go 1.25.6+
- [protoc](https://grpc.io/docs/protoc-installation/) with `protoc-gen-go` and `protoc-gen-go-grpc`
- A NATS NKey pair (Ed25519) for signing/verifying JWTs

## Quick Start (Local)

### 1. Generate NKeys

Use the [nk](https://github.com/nats-io/nkeys#nk---nkeys-tool) tool or any NKey generator to create an operator key pair. Note the **seed** and **public key**.

### 2. Start the Target ADK Server

```bash
go run ./cmd/target-server
```

### 3. Start the Router Proxy

```bash
ISSUER_PUBLIC_KEY=<operator-public-key> GRPC_PORT=9090 go run ./cmd/router-proxy
```

### 4. Start the Connector

```bash
ROUTER_PROXY_ADDR=localhost:50051 \
NKEY_SEED=<connector-nkey-seed> \
TARGET_ADK_SERVER_URL=http://localhost:8080 \
go run ./cmd/connector
```

### 5. (Option A) Start the ADK2Goose Connector

Instead of the standard Connector, you can use the ADK2Goose Connector to bridge ADK requests to a [Goose](https://github.com/block/goose) server:

```bash
ROUTER_PROXY_URL=localhost:50051 \
NKEY_SEED=<connector-nkey-seed> \
USER_ID=<user-id> \
APP_ID=<app-id> \
GOOSE_BASE_URL=http://localhost:3000 \
go run ./cmd/adk2goose-connector
```

### 6. Send a Request via the Mock Chatbot

```bash
ROUTER_PROXY_URL=http://localhost:8080 \
NKEY_SEED=<chatbot-nkey-seed> \
go run ./cmd/mock-chatbot
```

## Authentication

All authentication uses **NATS NKeys** (Ed25519). JWTs are signed with NKey seeds and verified against the issuer's public key. Required JWT claims:

| Claim | Description |
|-------|-------------|
| `sub` | Identity of the requester |
| `iss` | Public key of the signer |
| `userid` | User identifier for routing |
| `appid` | Application identifier for routing |
| `sessionid` | *(Optional)* Session affinity |

## Logging

The Router Proxy uses Go's `log/slog` for structured logging, configured via `pkg/logging`.

| Environment | Output | Format | Destination |
|---|---|---|---|
| **Cloud Run** | `stdout` | JSON (Google Cloud Logging compatible) | GCP Logs Explorer |
| **Local** | `stderr` | Human-readable text | Terminal |

Cloud Run is detected automatically via the `K_SERVICE` environment variable (set by Cloud Run). No configuration is needed — logs appear in the [GCP Logs Explorer](https://console.cloud.google.com/logs) with proper severity levels and structured fields (`request_id`, `userid`, `appid`, `method`, `path`, `status`).

## Running Tests

```bash
go test ./...
```

## Cloud Run Deployment

### Prerequisites

- [Google Cloud SDK](https://cloud.google.com/sdk/docs/install) (`gcloud`)
- A GCP project with Cloud Run and Artifact Registry enabled
- A NATS NKey pair for JWT signing/verification

### 1. Set Up Environment

```bash
export PROJECT_ID=<your-gcp-project>
export REGION=us-central1
export REPO=adk-cloud-proxy
export ISSUER_PUBLIC_KEY=<operator-public-key>

gcloud config set project $PROJECT_ID
```

### 2. Create Artifact Registry Repository

```bash
gcloud artifacts repositories create $REPO \
  --repository-format=docker \
  --location=$REGION
```

### 3. Build & Deploy the Router Proxy

```bash
# Build the image
gcloud builds submit \
  --tag ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/router-proxy \
  --dockerfile Dockerfile.router-proxy

# Deploy to Cloud Run with HTTP/2 enabled
gcloud run deploy router-proxy \
  --image ${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/router-proxy \
  --region $REGION \
  --use-http2 \
  --allow-unauthenticated \
  --set-env-vars "ISSUER_PUBLIC_KEY=${ISSUER_PUBLIC_KEY}" \
  --port 8080
```

> **Note:** The router proxy uses combined HTTP+gRPC mode on a single port when `GRPC_PORT` is not set, which is required for Cloud Run.

### 4. Build & Run the Connector

The Connector runs in your private network alongside the target ADK server.

```bash
# Build the image
docker build -f Dockerfile.connector -t connector .

# Run the connector
docker run \
  -e ROUTER_PROXY_URL=<cloud-run-service-url>:443 \
  -e TLS_ENABLED=true \
  -e NKEY_SEED=<connector-nkey-seed> \
  -e TARGET_ADK_SERVER_URL=http://host.docker.internal:8080 \
  -e USER_ID=<user-id> \
  -e APP_ID=<app-id> \
  connector
```

### Environment Variables

#### Router Proxy

| Variable | Required | Description |
|---|---|---|
| `ISSUER_PUBLIC_KEY` | Yes | NKey public key for JWT verification |
| `PORT` | No | HTTP port (default: `8080`, auto-set by Cloud Run) |
| `GRPC_PORT` | No | If set, gRPC runs on a separate port (local dev). If unset, combined mode is used. |

#### Connector

| Variable | Required | Description |
|---|---|---|
| `ROUTER_PROXY_URL` | Yes | Address of the Router Proxy (e.g., `my-service.a.run.app:443`) |
| `NKEY_SEED` | Yes | NKey seed for signing the connector JWT |
| `TARGET_ADK_SERVER_URL` | Yes | URL of the local ADK server |
| `USER_ID` | Yes | User identifier for routing |
| `APP_ID` | Yes | Application identifier for routing |
| `TLS_ENABLED` | No | Set to `true` for Cloud Run connections (default: `false`) |

#### ADK2Goose Connector

| Variable | Required | Description |
|---|---|---|
| `ROUTER_PROXY_URL` | Yes | Address of the Router Proxy (e.g., `my-service.a.run.app:443`) |
| `NKEY_SEED` | Yes | NKey seed for signing the connector JWT |
| `USER_ID` | Yes | User identifier for routing |
| `APP_ID` | Yes | Application identifier for routing |
| `GOOSE_BASE_URL` | No | URL of the Goose server (default: `http://127.0.0.1:3000`) |
| `GOOSE_SECRET_KEY` | No | Secret key for Goose API authentication |
| `WORKING_DIR` | No | Working directory for Goose agent sessions (default: `.`) |
| `TLS_ENABLED` | No | Set to `true` for Cloud Run connections (default: `false`) |

## License

TBD
