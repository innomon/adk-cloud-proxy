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
  router-proxy/    Cloud Run entry point
  connector/       Reverse-proxy agent for private networks
  mock-chatbot/    Test client that signs JWTs with NKeys
  target-server/   Sample "Hello World" ADK server
pkg/
  auth/            JWT validation using NATS NKeys (Ed25519)
  router/          In-memory registry mapping (userid, appid) → active streams
  tunnel/          Protobuf-defined gRPC bi-directional streaming service
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
ISSUER_PUBLIC_KEY=<operator-public-key> go run ./cmd/router-proxy
```

### 4. Start the Connector

```bash
ROUTER_PROXY_ADDR=localhost:50051 \
NKEY_SEED=<connector-nkey-seed> \
TARGET_ADK_SERVER_URL=http://localhost:8080 \
go run ./cmd/connector
```

### 5. Send a Request via the Mock Chatbot

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

## Running Tests

```bash
go test ./...
```

## Deployment

Cloud Run deployment support is planned. See [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) for current status.

## License

TBD
