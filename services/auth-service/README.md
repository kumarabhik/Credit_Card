# auth-service

Go hot-path orchestrator skeleton.

Implemented today:

- gRPC and HTTP servers with graceful shutdown
- `/healthz` and `/readyz` endpoints
- OTel trace propagation from `traceparent`
- structured `zap` logging with `trace_id`
- DynamoDB-backed idempotency with duplicate replay semantics

Expected layout:

- `cmd/server/` for the service entrypoint
- `internal/orchestrator/` for parallel fan-out and aggregation
- `internal/saga/` for compensation state transitions
- `internal/idempotency/` for request deduplication
- `internal/decision/` for score to decision mapping
- `internal/circuitbreaker/` for downstream guards
- `internal/clients/` for gRPC clients
- `internal/config/` for configuration loading
- `internal/obs/` for logging and OpenTelemetry setup
- `test/integration/` for testcontainers-based integration coverage
