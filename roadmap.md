# Distributed Payment Authorization Engine — Roadmap & Design Doc

> A Visa-style polyglot platform that authorizes a card-present transaction end-to-end in **< 100 ms p99** under sustained 2k RPS, with idempotent ledger writes, saga-based compensation, real-time fraud scoring (rules + ML), and a full GitOps deploy story to EKS.

**Status legend** — `[x]` done · `[~]` in progress · `[ ]` todo
> One `[~]` per agent at a time. Do not delete `[x]` items — they are the audit trail.

---

## Table of Contents

1. [Why this project exists](#1-why-this-project-exists)
2. [North Star Metrics](#2-north-star-metrics)
3. [System architecture](#3-system-architecture)
4. [Hot-path contract (< 100 ms p99)](#4-hot-path-contract--100-ms-p99)
5. [Tech stack decisions](#5-tech-stack-decisions)
6. [Service inventory](#6-service-inventory)
7. [Domain model & ubiquitous language](#7-domain-model--ubiquitous-language)
8. [Advanced pattern map](#8-advanced-pattern-map)
9. [Roadmap by phase](#9-roadmap-by-phase) ← the checklist
10. [Stretch goals](#10-stretch-goals)
11. [Out of scope](#11-out-of-scope)
12. [Risks & mitigations](#12-risks--mitigations)
13. [Verification matrix](#13-verification-matrix)
14. [Resume talking points](#14-resume-talking-points)

---

## 1. Why this project exists

**Target role:** Visa-style backend / platform / SRE roles where the JD lists Go, Java, AWS (SQS/SNS, DynamoDB), Redis, Prometheus, Terraform, Kubernetes.

**Problem domain:** When a card is swiped, the network must authorize the transaction in under 100 ms while doing fraud screening, balance verification, merchant validation, idempotency checks, and atomic ledger writes — all distributed, all concurrent, all under bursty traffic.

**What makes this hard (and worth building):**
- Latency budget is brutal — every cross-service hop costs the budget
- Race conditions on concurrent balance updates can lose money
- Network retries cause duplicate charges without idempotency
- Partial failures mid-transaction need compensation (saga)
- Fraud signals must be evaluated synchronously without becoming a bottleneck
- Every component must be observable enough to root-cause a latency regression at 3 a.m.

This project deliberately mirrors the *real* Visa architecture rather than a toy CRUD app.

---

## 2. North Star Metrics

These are the non-negotiables. Every phase ladder back to one of them.

- [ ] **Hot-path latency** — Auth p99 < 100 ms under 2k RPS sustained for 10 min (k6 soak in staging on EKS)
- [ ] **Idempotency correctness** — 10 000 concurrent duplicate auth requests with the same `Idempotency-Key` produce **exactly one** ledger row and **one** response (property-based test)
- [ ] **Saga correctness** — Killing any single service mid-transaction (chaos experiment) leaves **zero** stuck reservations after 5 s
- [ ] **Availability** — Synthetic 99.9 % over rolling 30 days in staging, tracked via multi-window burn-rate alerts
- [ ] **Inner-loop speed** — `make up && make smoke` returns first APPROVE in **< 60 s** on a warm laptop
- [ ] **Resilience** — With fraud-service throwing 50 % errors, auth-service p99 stays bounded (circuit breaker visibly open in Grafana)

---

## 3. System architecture

```
                         ┌──────────────────────────────┐
   POS / Merchant SDK ─▶ │   Edge / API Gateway (Envoy) │ mTLS, JWT, WAF, rate-limit
                         └──────────────┬───────────────┘
                                        │ gRPC + HTTP/2
                                        ▼
            ┌───────────────────────────────────────────────┐
            │   Authorization Service  (Go, hot path)       │
            │   ── orchestrator, saga coordinator           │
            └─┬───────────┬───────────┬───────────┬─────────┘
              │ sync gRPC │ sync gRPC │ sync gRPC │ async (SNS fan-out)
              ▼           ▼           ▼           ▼
       ┌───────────┐┌───────────┐┌───────────┐┌──────────────────┐
       │ Balance / ││  Fraud    ││  Ledger   ││  Settlement /    │
       │ Account   ││  Service  ││  Service  ││  Reconciliation  │
       │  (Go)     ││ (Java SB3 ││  (Go)     ││   (Java)         │
       │  Redis    ││  WebFlux) ││  Dynamo   ││  RDS Postgres    │
       └─────┬─────┘└─────┬─────┘└─────┬─────┘└─────────┬────────┘
             │            │             │                │
             │            ▼             │                │
             │   ┌───────────────┐      │                │
             │   │ ML Scorer     │      │                │
             │   │ (Python gRPC, │      │                │
             │   │  XGBoost)     │      │                │
             │   └───────────────┘      │                │
             ▼                          ▼                ▼
        ┌─────────┐                ┌──────────┐    ┌──────────┐
        │ Redis   │                │ DynamoDB │    │ Postgres │
        │ Cluster │                │ (ledger) │    │ (RDS)    │
        └─────────┘                └──────────┘    └──────────┘

           Event backbone:  SQS FIFO (ledger.events) + SNS (fan-out)
           Observability:   OTel SDK → OTel Collector → {Prometheus, Tempo, Loki}
                            Grafana single-pane dashboard
           Secrets/Keys:    AWS KMS + Secrets Manager (LocalStack in dev)
```

**Two planes of traffic:**

- **Synchronous hot path** (must hit < 100 ms p99): Edge → Auth → (Balance ∥ Fraud) → Ledger → response.
- **Asynchronous tail** (NOT on hot path): outbox → SNS → fan-out to settlement, notification, fraud-feedback, analytics.

---

## 4. Hot-path contract (< 100 ms p99)

```
EDGE → AUTH → (BALANCE ∥ FRAUD ∥ VELOCITY) → LEDGER WRITE → RESPONSE
        │            ↑         ↑                    │
        │     Redis lookup  gRPC to JVM       Dynamo conditional write
        │     ~ 1–3 ms      ~ 15–25 ms        ~ 10–20 ms
        │
        └── idempotency check first (Dynamo GetItem ~5 ms)
```

**Budget split (target):**

| Hop | Budget (ms) | Notes |
|---|---|---|
| Edge ingress + JWT | 5 | Envoy + ext-authz cache |
| Auth orchestration overhead | 5 | dispatch + saga init |
| Idempotency lookup | 5 | Dynamo GetItem (single-digit ms) |
| Balance check (Redis) | 3 | Cached account state |
| Fraud + ML (parallel to balance) | 25 | Rules + XGBoost inference |
| Ledger conditional write | 15 | Dynamo PutItem with version check |
| Response marshalling + egress | 5 | gRPC → HTTP/JSON |
| **Slack / variance buffer** | **37** | Buffer for GC, network jitter |
| **Total target** | **100** | p99 |

If any hop exceeds budget twice in a row → that owns the next sprint.

---

## 5. Tech stack decisions

| Layer | Choice | Reason |
|---|---|---|
| Hot-path language | **Go 1.22+** | Goroutines, low GC, gRPC ecosystem, matches Visa |
| JVM scoring | **Java 21 + Spring Boot 3 + WebFlux** | Reactor non-blocking, virtual threads option, industry standard for fintech |
| ML sidecar | **Python 3.12 + grpcio + XGBoost** | Real ML inference on synthetic data, gRPC contract = stable boundary |
| Sync RPC | **gRPC + protobuf (buf)** | Strict contracts, codegen, streaming for batch capture |
| External API | **HTTP/JSON via Envoy** | POS SDKs are HTTP; Envoy terminates TLS + JWT |
| Async messaging | **AWS SNS → SQS (FIFO + Standard)** | Visa JD line item; FIFO for ledger events, Standard for fan-out |
| Hot store | **Redis 7 (cluster mode)** | Account state cache, rate-limit buckets, idempotency TTL |
| Ledger | **DynamoDB single-table** | Atomic conditional writes, version-based optimistic locking |
| Relational | **PostgreSQL 16 (RDS)** | Merchant catalog, cardholder accounts, settlement batches |
| Stream (stretch) | **Kafka (MSK)** | Optional replayable event sourcing — Phase 9+ |
| Secrets | **AWS Secrets Manager + KMS** | Envelope encryption for PAN tokens, JWT keys |
| IaC | **Terraform 1.7+ (modules)** | Direct JD match; multi-env (local/dev/staging/prod) |
| Container | **Docker + distroless base** | Smallest attack surface |
| Orchestration | **Kubernetes (EKS) + Helm 3 + ArgoCD** | GitOps, blue/green via Argo Rollouts |
| Local dev | **Docker Compose + LocalStack** | Fast inner loop, AWS-compatible |
| CI/CD | **GitHub Actions** | Matrix builds per service, OIDC to AWS (no static creds) |
| Tracing | **OpenTelemetry SDK → Tempo** | W3C `traceparent` propagated Go ↔ JVM ↔ Python |
| Metrics | **Prometheus + Grafana + Alertmanager** | RED + USE + SLO burn-rate |
| Logs | **Loki + structured JSON** (`zap`, Logback) | Correlated to traces via `trace_id` |
| Load test | **k6** (JS-scripted) | 2k RPS sustained, latency histograms |
| Chaos | **Chaos Mesh** | Pod-kill, latency injection, network partition |
| Contract test | **Pact** (Go ↔ JVM ↔ Python) | Catches breaking proto changes pre-merge |

ADRs live in `docs/adr/` — each non-trivial choice gets a record. Read them before contradicting them.

---

## 6. Service inventory

Eight services, each with its own folder, Dockerfile, Helm subchart, and CI matrix slot. Plus three supporting components.

| # | Service | Lang | Responsibility | Mode |
|---|---|---|---|---|
| 1 | `edge-gateway` | Envoy + Go ext-authz | mTLS, JWT, WAF, global rate limit | Sync |
| 2 | `auth-service` | Go | Orchestrator, saga coordinator, idempotency keeper | Sync hot path |
| 3 | `balance-service` | Go | Account state, Redis-backed, write-through to Postgres | Sync gRPC |
| 4 | `fraud-service` | Java SB3 WebFlux | Rule engine + ML sidecar caller; emits 0–1000 score | Sync gRPC |
| 5 | `ml-scorer` | Python gRPC | XGBoost real-time inference | Sync gRPC (downstream of fraud) |
| 6 | `ledger-service` | Go | DynamoDB writes, optimistic lock, outbox emitter | Sync gRPC + async |
| 7 | `settlement-service` | Java | Batch settlement (T+1), reconciliation, chargeback | Async SQS consumer |
| 8 | `notification-service` | Go | Signed webhooks to merchants, retry with backoff | Async SQS consumer |

Supporting:
- `loadgen` — k6 scenarios (smoke / soak / spike / breakpoint)
- `chaos` — Chaos Mesh manifests + scenario runner
- `dashboards` — Grafana JSON + Prometheus rules + alert routes

---

## 7. Domain model & ubiquitous language

```
Card                ── PAN-token + BIN + expiry + status
Account             ── balance + currency + holds[] + version (optimistic-lock)
Cardholder          ── profile + KYC tier + device fingerprints
Merchant            ── id + MCC + country + risk tier + acquirer
Transaction         ── { Authorization, Capture, Reversal, Refund, Chargeback }
   AuthorizationReq ── amount, currency, merchant, card-token,
                       idempotency-key, geo, channel, device_id
   AuthorizationRsp ── decision { APPROVE, DECLINE, REVIEW },
                       risk_score, reason_code, trace_id, auth_code
RiskSignal          ── { velocity, geo_anomaly, mcc_risk,
                         device_mismatch, time_of_day, model_score }
LedgerEntry         ── double-entry: debit(account_hold) + credit(merchant_pending)
OutboxEvent         ── { event_id, aggregate_id, type, payload, status, attempts }
SettlementBatch     ── window + entries + state machine
                       (OPEN → CLOSING → SETTLED | RECONCILED | DISPUTED)
SagaInstance        ── saga_id + step[] + compensation[] + state
                       (PENDING | RESERVED | SCORED | COMMITTED | COMPENSATED | FAILED)
```

ISO-8583-inspired **reason codes** (`05` Do Not Honor, `51` Insufficient Funds, `54` Expired, `61` Exceeds Limit, `91` Issuer Unavailable…) flow end-to-end from edge → response. PCI-aware: **real PAN never leaves the tokenizer**; everything downstream uses opaque tokens.

---

## 8. Advanced pattern map

| Pattern | Lives in | Guards against |
|---|---|---|
| **Idempotency keys** | `auth-service/internal/idempotency/` — Dynamo conditional `PutItem` with TTL | Duplicate charges on POS retry |
| **Optimistic locking** | `ledger-service` — `version` attr + `ConditionExpression` | Lost-update on concurrent holds |
| **Saga (orchestration)** | `auth-service/internal/saga/` — state machine + compensations | Stuck reservations after partial failure |
| **Outbox pattern** | `ledger-service` — event row written in same Dynamo txn as ledger row; relay polls | Lost events on crash between DB + SNS |
| **Circuit breaker** | `gobreaker` (Go), Resilience4j (Java) on every cross-service call | Cascading failure when fraud-service degrades |
| **Bulkhead** | Separate gRPC channels + pools per downstream | Saturating one dep doesn't starve others |
| **Retry w/ jitter** | Custom Go middleware + Spring Retry | Thundering herd on transient blips |
| **Timeout budgets** | `context.WithTimeout` propagation; split per hop | Tail latency blow-up |
| **Hedged requests** | auth → fraud, fire 2nd after p99 deadline | Tail of fraud-service drags p99 |
| **Rate limiting** | Redis sliding-window in Envoy ext-authz + per-tenant token bucket | DoS & noisy neighbor |
| **mTLS** | Envoy front + cert-manager for in-cluster | Lateral movement, MITM |
| **PAN tokenization** | Edge gateway → KMS-envelope-encrypted token | PCI DSS scope reduction |
| **CQRS-lite** | Writes → Dynamo; reads → Postgres projection | Hot-path writes never blocked by reports |
| **Event sourcing (light)** | Outbox table is append-only; replay rebuilds projections | Audit + debugging |
| **Property-based tests** | `gopter` (Go), `jqwik` (Java) for idempotency + saga invariants | "Looks right" but breaks under odd inputs |
| **Chaos experiments** | `infra/k8s/chaos/` — pod-kill, network-delay, partition | Untested failure modes |
| **Feature flags** | OpenFeature + GoFeatureFlag | Safe rollout of new fraud rules |
| **SLO + error budget** | `observability/prometheus/rules/slo.yaml` — burn-rate alerts | Latency regressions caught fast |

---

## 9. Roadmap by phase

> Pick the next `[ ]` from this list. Flip to `[~]` in the same PR that starts the work. Flip to `[x]` only when (1) merged, (2) tests added, (3) docs/ADR updated if behavior changed, (4) observability hook added.

### Phase 0 — Foundation  *(Week 1, days 1–3)*

#### 0.1 Repo & tooling
- [x] Monorepo skeleton matching the layout in `agents.md`
- [x] `Makefile` targets: `up`, `down`, `test`, `lint`, `load`, `chaos`, `sbom`, `proto`
- [x] Pre-commit hooks: `gofmt`, `goimports`, `golangci-lint`, `spotless`, `ruff`, `hadolint`, `gitleaks`
- [x] `.editorconfig`, `.gitattributes`, `.gitignore`
- [x] ADR-0001: record architecture decisions

#### 0.2 Proto contracts
- [x] `proto/auth/v1/auth.proto`, `fraud/v1/fraud.proto`, `ledger/v1/ledger.proto`, `ml/v1/score.proto`, `common/v1/common.proto`
- [x] `buf.gen.yaml` + `buf lint` + `buf breaking` in CI
- [x] Codegen targets for Go, Java, Python wired into `make proto`

#### 0.3 Local stack
- [x] `docker-compose.yml`: Postgres, Redis, LocalStack, Jaeger, Prometheus, Grafana, Loki, OTel collector
- [x] LocalStack init script: SQS queues, SNS topics, Dynamo table, KMS keys, Secrets Manager entries
- [x] `make smoke` returns `APPROVE` end-to-end in < 60 s on a warm machine

#### 0.4 CI bones
- [x] GitHub Actions matrix: per-service lint + unit + build
- [x] `cosign` sign + `syft` SBOM on every image
- [x] Branch protection + CODEOWNERS

### Phase 1 — Hot path MVP  *(Week 1, days 4–7)*

#### 1.1 auth-service skeleton (Go)
- [x] gRPC + HTTP servers, graceful shutdown, `/healthz` + `/readyz`
- [x] OTel SDK wired, `traceparent` propagation verified end-to-end
- [x] Structured `zap` logger with `trace_id` correlation

#### 1.2 Idempotency
- [x] `Idempotency-Key` header required; Dynamo conditional `PutItem` with 24 h TTL
- [x] Returns cached response on duplicate; race-tested with goroutine fan-out
- [x] ADR-0005: idempotency strategy

#### 1.3 balance-service (Go)
- [x] Redis hot path (`GET account:{id}`)
- [ ] Postgres write-through on cache miss
- [ ] `HOLD` / `RELEASE` / `CAPTURE` primitives — atomic via Redis Lua script

#### 1.4 ledger-service (Go) — DynamoDB single-table
- [ ] Key design: `PK=ACCT#{id}` / `SK=TXN#{ulid}`
- [ ] GSI1: merchant view; GSI2: idempotency lookup
- [ ] Conditional write with `version` attr for optimistic lock
- [ ] ADR-0003: single-table design

#### 1.5 End-to-end APPROVE
- [ ] `curl`-able happy path: edge → auth → balance → ledger → 200 APPROVE
- [ ] Trace visible in Jaeger with all 4 spans
- [ ] One integration test exercising the full path against `docker-compose`

### Phase 2 — Fraud + ML  *(Week 2, days 1–5)*

#### 2.1 fraud-service (Java 21 + Spring Boot 3 + WebFlux)
- [ ] gRPC server on Netty; reactive `Mono<RiskScore>` pipeline
- [ ] Pluggable `RiskRule` interface (rules registered as beans in `RuleRegistry`)
- [ ] Rules implemented: velocity (count/sum per window), geo-jump, MCC tier, device mismatch, time-of-day
- [ ] Sliding window over Redis ZSET; window length configurable per rule

#### 2.2 ml-scorer (Python)
- [ ] Synthetic data generator (10 M rows, SMOTE-balanced labels)
- [~] Reproducible `train.py` producing versioned XGBoost model
- [ ] gRPC server, model loaded at boot, < 5 ms inference per call
- [ ] Model version exposed as Prometheus label

#### 2.3 Wire fraud into auth
- [ ] auth-service calls fraud-service **in parallel** with balance check (`errgroup`)
- [ ] Score aggregation: rules + ML → 0–1000 → bucket `{APPROVE, REVIEW, DECLINE}`
- [ ] Decline reason codes (ISO-8583 inspired) flow back to caller

#### 2.4 Property tests
- [ ] `gopter`: idempotency invariants (N concurrent same-key → exactly one ledger row)
- [ ] `jqwik`: rule engine monotonicity (higher risk inputs ⇒ never lower score)

### Phase 3 — Saga & compensation  *(Week 2 day 6 → Week 3 day 2)*

- [ ] Saga state machine in auth-service: `RESERVE → FRAUD → COMMIT` (or `COMPENSATE`)
- [ ] Persist saga state in Dynamo (resume on restart)
- [ ] Compensation paths: `ReleaseHold`, `EmitReversal`, `RetryDecision`
- [ ] Failure-injection test: kill ledger-service mid-saga, restart auth, expect compensation completes within 5 s
- [ ] ADR-0004: saga vs 2-phase commit

### Phase 4 — Resilience  *(Week 3, days 3–5)*

- [ ] `gobreaker` + Resilience4j circuit breakers on every cross-service call
- [ ] Per-dependency bulkhead (separate gRPC channels + pools)
- [ ] Deadline / context propagation with per-hop budget split
- [ ] Retry-with-jitter middleware (3 tries, exponential, capped)
- [ ] Hedged requests for fraud-service (fire 2nd after p99 deadline, cancel slower)
- [ ] Rate limit: Redis sliding window in Envoy ext-authz (per-IP + per-merchant)

### Phase 5 — Observability  *(Week 3, days 6–7)*

- [ ] OTel collector pipeline: metrics → Prometheus, logs → Loki, traces → Tempo
- [ ] RED metrics for every gRPC + HTTP handler
- [ ] USE metrics for Redis + Dynamo (saturation, errors, utilization)
- [ ] Grafana dashboard: hot-path latency heat-map, APPROVE rate, decline reason breakdown, saga state distribution, circuit-breaker open events
- [ ] Prometheus SLO rules + multi-window burn-rate alerts (1h / 6h windows)
- [ ] One runbook per alert under `observability/runbooks/`

### Phase 6 — Security  *(Week 4, days 1–3)*

- [ ] PAN tokenization at edge: HMAC + KMS envelope encryption
- [ ] mTLS between all services (cert-manager + Envoy SDS, or Linkerd)
- [ ] JWT RS256 with JWKS endpoint; key-rotation runbook
- [ ] Secrets via AWS Secrets Manager (LocalStack in dev)
- [ ] Threat model written (`docs/threat-model.md`) — STRIDE per service boundary
- [ ] SAST in CI (`semgrep`), SCA (`osv-scanner`, Dependabot)
- [ ] Container scan (`trivy`), image signing (`cosign` keyless via OIDC)

### Phase 7 — Async backbone & settlement  *(Week 4, days 4–5)*

- [ ] Outbox table in Dynamo + relay process emitting to SNS
- [ ] SNS topics: `txn-authorized`, `txn-declined`, `txn-captured`, `txn-reversed`
- [ ] SQS subscriptions: settlement (FIFO), notification (Standard), analytics
- [ ] settlement-service: T+1 batch close, reconciliation against ledger
- [ ] notification-service: signed webhooks to merchants with exponential backoff + DLQ

### Phase 8 — Infrastructure as code  *(Week 4, days 6–7)*

- [ ] Terraform modules: `vpc`, `eks`, `rds`, `dynamo`, `sqs-sns`, `redis`, `kms`, `iam`, `secrets`
- [ ] Three envs: `local` (LocalStack via `tflocal`), `staging` (real AWS, scaled-down), `prod` (scale baseline, not deployed)
- [ ] State in S3 + DynamoDB lock table; OIDC for GitHub Actions (no static creds)
- [ ] `tfsec` + `checkov` in CI
- [ ] `infracost` cost estimate posted to PRs

### Phase 9 — Deploy & chaos  *(Week 5, days 1–3)*

- [ ] Helm umbrella chart + per-service subchart
- [ ] ArgoCD app-of-apps with sync waves: infra → data → services → ingress
- [ ] Argo Rollouts blue/green with auto-rollback on SLO breach
- [ ] Chaos Mesh experiments: pod-kill, network-delay, partition between auth ↔ fraud
- [ ] Soak test (k6): 2k RPS for 30 min in staging — record p50/p95/p99
- [ ] Breakpoint test: ramp until first error — document the cliff

### Phase 10 — Polish & story  *(Week 5, days 4–7)*

- [ ] `README.md` with architecture diagram, 90-second pitch, screenshots
- [ ] Public read-only Grafana snapshot link
- [ ] Loom-style walkthrough video (optional)
- [ ] Resume bullets drafted (see §14)
- [ ] Interview-ready talking points: 5 deepest topics with whiteboard answers

---

## 10. Stretch goals

- [ ] Kafka swap — replace SNS/SQS with replayable event sourcing (ADR-0010)
- [ ] WASM rule engine — hot-reload fraud rules without redeploy
- [ ] eBPF-based latency probe (Pixie / Parca)
- [ ] Multi-region active-active via DynamoDB Global Tables
- [ ] 3-D Secure step-up challenge flow (HOTP)
- [ ] Real-time decision-stream dashboard via WebSocket push
- [ ] Shadow traffic mirroring (production prod → shadow eval cluster)

---

## 11. Out of scope

Explicitly **not** building:
- Real card-network connectivity (Visa / Mastercard). Simulated POS only.
- Real KYC / AML. Stubbed.
- Real chargeback adjudication. Schema present, workflow stubbed.
- Mobile SDK. POS calls via `curl` / `k6`.
- Multi-currency FX. USD only; schema supports more.

---

## 12. Risks & mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Scope creep blows past 5 weeks | High | Phases gate; stretch goals labeled |
| LocalStack drifts from real AWS | Medium | Hybrid model — staging on real AWS, validated weekly |
| ML model underperforms on synthetic data | Medium | XGBoost is forgiving; if AUC < 0.85, fall back to rules + log "model demo" honestly |
| EKS cost surprise | Medium | `infracost` in PRs; staging cluster auto-shuts-down after 4 h idle |
| Burnout at Phase 6 (security) | Medium | Phase 6 can ship partial — mTLS + tokenization are must-haves; rest is stretch |
| Proto breaking change cascades | Medium | `buf breaking` enforced in CI; consumer-driven Pact tests |

---

## 13. Verification matrix

| Phase | How we'll know it's real |
|---|---|
| 0 | `make up && make smoke` returns 200 APPROVE in < 60 s on cold machine |
| 1 | k6 smoke: 100 RPS for 60 s, 100 % success, p99 < 100 ms locally |
| 2 | Synthetic txn with known-bad signal returns DECLINE with reason code; trace shows fraud + ML spans |
| 3 | `make chaos-saga` kills ledger mid-flow; saga state in Dynamo resolves to `COMPENSATED` within 5 s; no orphan holds |
| 4 | Forced 50 % error rate on fraud-service → auth p99 stays bounded (graceful degrade), CB opens in Grafana |
| 5 | All RED panels populated; SLO burn-rate alert fires correctly in a synthetic regression |
| 6 | `gitleaks`, `trivy`, `semgrep` all green; `make threat-model` renders STRIDE diagram |
| 7 | Outbox replay test: stop SNS publisher, accumulate, restart → all events delivered, none lost |
| 8 | `terraform plan` in staging diff-clean after apply; `infracost` diff posted to PR |
| 9 | Argo Rollouts auto-rollback triggered by injected SLO breach in staging |
| 10 | Public Grafana snapshot link works; README walks a stranger to first APPROVE in < 5 min |

---

## 14. Resume talking points

(Baked into the North Star section so progress is measured against them, not vanity metrics.)

- Designed and built a **Visa-style distributed payment authorization engine** in Go and Java with a **< 100 ms p99 hot path** under 2k RPS, validated by k6 soak tests.
- Implemented **saga orchestration with compensating transactions**; verified correctness by killing services mid-flow under Chaos Mesh — zero stuck reservations across 1 000+ injected failures.
- Designed a **DynamoDB single-table schema** with optimistic locking and an **outbox pattern**; idempotency-key flow proven race-safe via property-based tests (10 k concurrent duplicates → exactly one ledger row).
- Provisioned multi-environment AWS infrastructure via **Terraform modules**; deployed via **Helm + ArgoCD** to EKS with blue/green rollouts and automated SLO-based rollback.
- Real-time fraud scoring combining a **Java rule engine** and an **XGBoost ML sidecar in Python via gRPC**; rules and model versions independently deployable.
- Defined **SLOs with multi-window burn-rate alerts** and authored **runbooks** for every alert.
