# Local DevTest Deployment Scenario

This scenario demonstrates running the ADK Cloud Proxy components locally for development and testing purposes. It uses NATS as the Pub/Sub service for Just-In-Time (JIT) activation.

## Architecture

1.  **NATS Server:** Runs locally, providing the messaging layer.
2.  **Router Proxy:** Receives incoming HTTP requests and initiates gRPC tunnels.
3.  **Connector:** Listens for invitations on NATS and dials back to the Router Proxy.
4.  **Target Server:** Mock server that simulates the final backend destination.

## Prerequisites

- **Go 1.25.6** or higher.
- **Docker:** (Optional, but easiest for running NATS)
- **NATS Server:** Running on `localhost:4222`.

## Step-by-Step Guide

### 1. Start NATS Server
Using Docker:
```bash
docker run -d --name nats-main -p 4222:4222 nats
```

### 2. Start the Target Server (Mock)
In a new terminal:
```bash
go run cmd/target-server/main.go
```
This starts the mock server on `:8081`.

### 3. Start the Router Proxy
In a new terminal:
```bash
# Use the local-devtest configuration
export ISSUER_PUBLIC_KEY=your_pub_key
go run cmd/router-proxy/main.go -config config-examples/local-devtest/config.yaml
```
This starts the Router Proxy on `:8080` (HTTP and gRPC).

### 4. Start the Connector
In a new terminal:
```bash
# Use the local-devtest configuration
export NKEY_SEED=your_seed
export APP_ID=myapp
export TARGET_ADK_SERVER_URL=http://localhost:8081
go run cmd/connector/main.go -config config-examples/local-devtest/config.yaml
```
The connector will stay idle until it receives an invitation via NATS.

### 5. Test the Proxy
Send a request to the Router Proxy (requires a valid JWT signed by your `ISSUER_SEED`):
```bash
curl -X POST http://localhost:8080/apps/myapp/users/user1/sessions \
     -H "Authorization: Bearer <your_jwt_token>" \
     -H "X-App-ID: myapp"
```
You should see:
1. `router-proxy` publishing an invitation to NATS.
2. `connector` receiving the invitation and establishing a gRPC tunnel to `router-proxy`.
3. `router-proxy` forwarding the request over the tunnel to `connector`.
4. `connector` forwarding the request to the `target-server`.
5. `target-server` responding.

## Configuration Files

- `config.yaml`: Shared configuration for both `router-proxy` and `connector`.
