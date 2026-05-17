# ADR-0001: Baseline Architecture for the Authorization Platform

- Status: Accepted
- Date: 2026-05-18

## Context

This repository is building a Visa-style distributed payment authorization engine.
The roadmap and design document require a polyglot architecture with:

- a Go hot path for low-latency authorization orchestration
- Java services for reactive fraud and settlement workflows
- a Python ML sidecar for real-time scoring
- Redis, DynamoDB, and Postgres split by access pattern
- SNS and SQS for asynchronous fan-out
- Terraform, Helm, and GitOps deployment targets
- OpenTelemetry-backed observability across every service hop

The project also has hard constraints:

- no real PAN may be committed or logged
- no JVM dependency may be introduced on the synchronous authorize hot path
- every cross-service call must carry timeouts, retries, circuit breakers, and traces
- every state-changing DynamoDB write must be idempotent or version-guarded

## Decision

We will use the following baseline architecture:

1. `edge-gateway` terminates ingress traffic and performs request mediation at the boundary.
2. `auth-service` is the Go hot-path orchestrator for authorization, idempotency, and saga coordination.
3. `balance-service` is a Go service backed by Redis for hot account state with Postgres recovery storage.
4. `fraud-service` is a Java 21 Spring Boot WebFlux service that executes rules and calls the ML scorer.
5. `ml-scorer` is a Python gRPC service that loads a versioned XGBoost model at boot.
6. `ledger-service` is a Go service backed by a DynamoDB single-table ledger with an outbox pattern.
7. `settlement-service` and `notification-service` consume asynchronous events off the hot path.
8. Observability is provided through OpenTelemetry, Prometheus, Loki, Tempo or Jaeger, and Grafana.
9. Infrastructure is defined in Terraform, packaged with Helm, and intended for GitOps rollout patterns.

## Consequences

### Positive

- The hot path stays in Go, aligned with the latency budget and repo rules.
- Fraud and settlement concerns can evolve independently without polluting the synchronous path.
- Typed protobuf contracts remain the boundary between services and languages.
- The storage model matches domain access patterns instead of forcing one database to do every job.
- The architecture is faithful to the portfolio story captured in the roadmap and design doc.

### Negative

- The platform becomes operationally more complex than a single-language monolith.
- Local development requires containers and service emulation to stay ergonomic.
- Code generation, linting, and CI must coordinate three language ecosystems from one repository.

## Follow-up ADRs

- ADR-0003: DynamoDB single-table design
- ADR-0004: saga orchestration versus two-phase commit
- ADR-0005: idempotency strategy
- ADR-0007: fraud score aggregation model
