# balance-service

Go balance and hold service skeleton.

Expected layout:

- `cmd/server/` for process startup
- `internal/account/` for account domain logic
- `internal/hold/` for HOLD, RELEASE, and CAPTURE primitives
- `internal/cache/` for Redis access
- `internal/store/` for Postgres recovery and write-through logic
- `internal/config/` for service configuration
- `internal/obs/` for metrics, traces, and structured logging
- `test/integration/` for Redis and Postgres backed tests
