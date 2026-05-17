# auth-service

Go hot-path orchestrator skeleton.

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
