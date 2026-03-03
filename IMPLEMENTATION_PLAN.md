# ADK Server Router Proxy Implementation Plan

## Phase 1: Research & Setup
- [x] Initialize Go modules.
- [x] Set up project structure.
- [x] Install dependencies (`nats-io`, `grpc`, `yaml.v3`, `redis`, `google-cloud-pubsub`).

## Phase 2: Core Components Development
- [x] **Authentication (pkg/auth):** NATS JWT + EdDSA OAuth validation.
- [x] **Tunnel Protocol (pkg/tunnel):** gRPC bi-directional stream.
- [x] **Router Proxy (cmd/router-proxy):** Registry and request routing.
- [x] **Connector Agent (cmd/connector):** Multi-proxy gRPC tunnel support.

## Phase 3: JIT Activation & Pub/Sub (New)
- [x] **Pub/Sub Interface (pkg/pubsub):** Abstract interface and Registry.
- [x] **NATS Implementation:** Initial backend for invites.
- [x] **Redis & GCP Implementations:** Cloud-native backends for serverless scaling.
- [x] **JIT Invite Logic:** Proxy publishes `InviteMessage` on missing connector.
- [x] **Reactive Connector:** Connector listens for invites on `invites.<AppID>`.
- [x] **Inactivity Monitor:** Connector gracefully shuts down after 10m idle.
- [x] **Config Loader (pkg/config):** Support for `config.yaml`.

## Phase 4: ADK2Goose Connector
- [x] **pkg/goose:** Translation layer and session manager.
- [x] **Reactive Support:** Updated `adk2goose-connector` to support JIT activation.
- [x] Add unit tests for `pkg/goose/`.

## Phase 5: Testing & Deployment
- [x] End-to-end testing with mock clients.
- [x] Cloud Run multi-stage Dockerfiles.
- [x] Support for shared Pub/Sub across Cloud Run instances.

## Phase 6: Documentation & Maintenance
- [x] Update README with JIT/PubSub instructions.
- [x] Update Specification with architecture diagrams.
- [x] Final build verification (`go build ./cmd/...`).
