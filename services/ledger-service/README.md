# ledger-service

Go ledger and outbox service skeleton.

Expected layout:

- `cmd/server/` for service startup
- `internal/ledger/` for ledger write orchestration
- `internal/outbox/` for relay logic
- `internal/store/` for DynamoDB access patterns
- `internal/config/` for configuration loading
- `internal/obs/` for metrics, traces, and structured logging
- `test/integration/` for Dynamo or LocalStack backed coverage
