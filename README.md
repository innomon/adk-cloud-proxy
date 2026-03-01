# ADK Cloud Proxy

ADK Cloud Proxy routes requests from chatbot clients to [ADK](https://google.github.io/adk-docs/) servers running behind firewalls. It uses reactive, Just-In-Time (JIT) "Connectors" that establish gRPC bi-directional tunnels only when needed, allowing the Proxy to scale to zero on Cloud Run.

## Architecture

```
Chatbot ──► Router Proxy (Cloud Run) ◄── (Invite via Pub/Sub) ── Connector ──► Target ADK Server
```

1. **JIT Invitation:** If a Proxy instance receives a request but has no active tunnel for the user/app, it publishes an **Invite** via Pub/Sub.
2. **Reactive Connection:** The **Connector** (listening on Pub/Sub) receives the invite and dials the specific Proxy instance.
3. **Tunneling:** Once the gRPC tunnel is established, the request is forwarded, processed by the local ADK server, and the response is returned.
4. **Graceful Idle:** The Connector shuts down tunnels after 5 minutes of inactivity and closes itself after 10 minutes of total idleness.

## Configuration

Both the Proxy and Connector load settings from a `config.yaml` file in their working directory.

### config.yaml Example

```yaml
pubsub:
  type: "nats" # Options: "nats", "redis", "gcp"
  config:
    url: "nats://localhost:4222" # For NATS
    # address: "localhost:6379"  # For Redis
    # project_id: "my-gcp-proj"  # For GCP
proxy:
  url: "https://router-proxy-xyz.a.run.app" # Required for JIT invites
```

## Quick Start (Local)

1. **Generate NKeys:** Use `nk` to create operator key pairs.
2. **Start Pub/Sub:** e.g., `docker run -p 4222:4222 nats:latest`.
3. **Start Target ADK Server:** `go run ./cmd/target-server`.
4. **Start Router Proxy:**
   ```bash
   ISSUER_PUBLIC_KEY=<pub-key> go run ./cmd/router-proxy
   ```
5. **Start Connector:**
   ```bash
   USER_ID=<uid> APP_ID=<aid> NKEY_SEED=<seed> TARGET_ADK_SERVER_URL=http://localhost:8080 go run ./cmd/connector
   ```

## Authentication

The Router Proxy supports **dual authentication**:
1. **NATS NKey JWTs:** Used by programmatic clients and connectors.
2. **WhatsApp OAuth EdDSA JWTs:** Used by SPA clients (enabled via `OAUTH_PUBLIC_KEY`).

## Environment Variables

### Router Proxy
- `ISSUER_PUBLIC_KEY`: (Required) NKey public key for JWT verification.
- `PROXY_URL`: Overrides `proxy.url` in `config.yaml`.
- `OAUTH_PUBLIC_KEY`: Enables EdDSA OAuth validation.

### Connector
- `USER_ID`, `APP_ID`: (Required) Routing identity.
- `NKEY_SEED`: (Required) Seed for signing JWTs.
- `TARGET_ADK_SERVER_URL`: Local server to proxy for.
- `ROUTER_PROXY_URL`: (Legacy/Fallback) Immediate connection target.

## License
Apache 2.0
