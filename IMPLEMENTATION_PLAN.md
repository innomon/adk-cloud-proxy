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

## Phase 4: ADK Layer (pkg/adk)
- [x] **pkg/adk:** Translation layer and session manager (renamed from `pkg/goose`).
- [x] **Reactive Support:** Updated connectors to support JIT activation.
- [x] Add unit tests for `pkg/adk/`.

## Phase 5: Testing & Deployment
- [x] End-to-end testing with mock clients.
- [x] Cloud Run multi-stage Dockerfiles.
- [x] Support for shared Pub/Sub across Cloud Run instances.

## Phase 6: Documentation & Maintenance
- [x] Update README with JIT/PubSub instructions.
- [x] Update Specification with architecture diagrams.
- [x] Final build verification (`go build ./cmd/...`).

## Phase 7: OpenAI Proxy Support
- [x] **Config Extension:** Add `OpenAIConfig` to `pkg/config`.
- [x] **OpenAI Types (pkg/openai):** Define OpenAI-compatible request/response types.
- [x] **Translation Logic (pkg/openai):** Implement OpenAI-to-ADK conversion.
- [x] **Router Proxy Integration:**
    - [x] Add `/v1/chat/completions` and `/v1/models` handlers.
    - [x] Support internal JWT signing using `ISSUER_SEED`.
    - [x] Handle streaming (SSE) and non-streaming responses.
- [x] **Validation:** Verify with OpenAI SDK or `curl`.

## Phase 8: OpenAI Connector
- [ ] **Reactive Core:** Implement `cmd/openai-connector` with JIT activation support.
- [ ] **ADK to OpenAI Translation:** Translate incoming ADK `run_sse` requests to local OpenAI API calls.
- [ ] **OpenAI to ADK Translation:** Translate local OpenAI SSE streams back to ADK events.
- [ ] **Inactivity Monitor:** Support graceful shutdown when idle.
- [ ] **Testing:** Verify tunnel from OpenAI Proxy -> OpenAI Connector -> Ollama.

## Phase 9: Multi-Connector (New)
- [x] **Config Support:** Add `MultiConnectorConfig` to `pkg/config`.
- [x] **In-Process Runners:** Use `agentic` to run multiple agents in a single process.
- [x] **ADK REST Handler:** Implement local REST surface that calls internal runners.
- [x] **Multi-Tunnel Logic:** Support multiple AppIDs with distinct NKeys in one connector.
