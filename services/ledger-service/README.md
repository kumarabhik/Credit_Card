# ledger-service

Go ledger and outbox service skeleton.

Implemented today:

- DynamoDB single-table ledger write path with `PK=ACCT#{id}` and `SK=TXN#{ulid}`
- `GSI1` merchant projection and `GSI2` idempotency lookup projection
- optimistic-lock `version` state item updated with conditional Dynamo transactions
- gRPC `Write` / `Get` service plus HTTP `/healthz` and `/readyz`

Expected layout:

- `cmd/server/` for service startup
- `internal/ledger/` for ledger write orchestration
- `internal/outbox/` for relay logic
- `internal/store/` for DynamoDB access patterns
- `internal/config/` for configuration loading
- `internal/obs/` for metrics, traces, and structured logging
- `test/integration/` for Dynamo or LocalStack backed coverage
