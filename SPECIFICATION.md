# ADK Server Router Proxy Specification

This document specifies the architecture and design for an ADK (Agent Development Kit) Server Router Proxy running on Google Cloud Run. This proxy enables chatbots and other clients to connect to ADK servers running behind firewalls via "Connectors" (reverse proxy agents).

## 1. Overview

The system facilitates communication between a client (e.g., a chatbot on a user's computer) and a target ADK server located in a private network. The core component is the **Router Proxy**, which acts as a centralized routing hub.

### 1.1 Architecture Diagram

```mermaid
graph TD
    subgraph "Public Internet"
        Chatbot["Chatbot (User Computer)"]
        RouterProxy["ADK Router Proxy (Cloud Run)"]
    end

    subgraph "Private Network (Behind Firewall)"
        Connector["ADK Connector (Reverse Proxy Agent)"]
        TargetADKServer["Target ADK Server"]
    end

    Chatbot -- "1. Request + JWT" --> RouterProxy
    Connector -- "2. Register & Maintain Stream" --> RouterProxy
    RouterProxy -- "3. Forward Request via Stream" --> Connector
    Connector -- "4. Forward to Local Server" --> TargetADKServer
```

## 2. Components

### 2.1 ADK Router Proxy (Cloud Run)
The Router Proxy is the entry point for all client requests.
- **Responsibilities:**
    - Authenticate clients and connectors using NATS JWTs signed with Ed25519 NKeys.
    - Maintain a registry of active Connector connections.
    - Extract routing metadata (`userid`, `appid`, `session`) from JWT claims.
    - Forward ADK requests to the appropriate Connector.
- **Logging:** Structured logging via `log/slog`. On Cloud Run, outputs JSON to stdout with fields mapped for Google Cloud Logging (`severity`, `message`, `timestamp`). Locally, outputs human-readable text to stderr.
- **Tech Stack:** Go, ADK Go SDK, `github.com/nats-io/jwt/v2`, `github.com/nats-io/nkeys`.

### 2.2 ADK Connector (Reverse Proxy Agent)
The Connector runs in the same private network as the target ADK server.
- **Responsibilities:**
    - Establish an outbound connection to the Router Proxy.
    - Authenticate using its own NKey-signed JWT.
    - Keep a bi-directional gRPC stream open to receive forwarded requests.
    - Act as an ADK client to forward requests to the local ADK server.
    - Return responses back through the stream.

### 2.3 ADK2Goose Connector
A variant of the ADK Connector that bridges ADK clients to a [Goose](https://github.com/block/goose) agent server instead of a standard ADK server.
- **Responsibilities:**
    - Establish an outbound gRPC tunnel to the Router Proxy (same as the ADK Connector).
    - Authenticate using its own NKey-signed JWT.
    - Embed an ADK-to-Goose translation layer that converts ADK REST API requests into Goose API calls.
    - Manage Goose agent sessions (start, stop, session mapping).
    - Translate Goose SSE streaming responses back into ADK event format.
    - Return translated responses back through the tunnel.

### 2.4 Chatbot / Client
Any application using the ADK protocol that needs to reach a remote agent.
- **Responsibilities:**
    - Generate a JWT signed with an NKey.
    - Include required claims for routing.
    - Send requests to the Router Proxy endpoint.

## 3. Authentication & Security

### 3.1 NKeys & JWT
Authentication uses Ed25519 signatures via NKeys.
- **Issuer:** A trusted authority (Operator/Account) that signs the JWTs.
- **Verification:** The Router Proxy validates the signature using the Issuer's Public Key.

### 3.2 JWT Claims
The JWT MUST include the following claims for routing:
- `sub` (Subject): The identity of the requester.
- `iss` (Issuer): The public key of the signer.
- `userid`: Custom claim identifying the user.
- `appid`: Custom claim identifying the application.
- `sessionid`: (Optional) For session-affinity routing.

### 3.3 Connector Authentication
Connectors also authenticate with JWTs. The Router Proxy uses the `userid` and `appid` claims in the Connector's JWT to register it in the routing table.

## 4. Routing Logic

The Router Proxy maintains an in-memory map:
`Map[(userid, appid)] -> ActiveStream`

1. **Connector Registration:** When a Connector connects, it sends its JWT. The Proxy validates it and stores the gRPC stream associated with the `userid` and `appid`.
2. **Request Handling:** When a Chatbot sends a request, the Proxy:
    - Verifies the Chatbot's JWT.
    - Extracts `userid` and `appid`.
    - Looks up the `ActiveStream` in the map.
    - If found, it wraps the ADK request and sends it through the stream.
    - If not found, it returns a `404 Not Found` or `503 Service Unavailable`.

## 5. Protocols

- **Client <-> Router Proxy:** HTTP/gRPC (Standard ADK/A2A Protocol).
- **Connector <-> Router Proxy:** gRPC Bi-directional Stream (Custom "Tunnel" Service).
- **Connector <-> Target ADK Server:** HTTP/gRPC (Standard ADK/A2A Protocol).

## 6. Deployment

### 6.1 Google Cloud Run
- The Router Proxy is deployed as a Cloud Run service.
- **Configuration:**
    - HTTP/2 must be enabled for gRPC streams.
    - Authentication Public Keys should be provided via environment variables or Secret Manager.

### 6.2 Connector Configuration
- The Connector requires:
    - `ROUTER_PROXY_URL`: Endpoint of the Cloud Run service.
    - `NKEY_SEED`: Its private seed for signing its own JWT.
    - `TARGET_ADK_SERVER_URL`: Local URL of the agent it proxies for.

### 6.3 ADK2Goose Connector Configuration
- The ADK2Goose Connector requires:
    - `ROUTER_PROXY_URL`: Endpoint of the Cloud Run service.
    - `NKEY_SEED`: Its private seed for signing its own JWT.
    - `USER_ID`: User identifier for routing.
    - `APP_ID`: Application identifier for routing.
    - `GOOSE_BASE_URL`: URL of the Goose server (default: `http://127.0.0.1:3000`).
    - `GOOSE_SECRET_KEY`: *(Optional)* Secret key for Goose API authentication.
    - `WORKING_DIR`: *(Optional)* Working directory for Goose agent sessions (default: `.`).
