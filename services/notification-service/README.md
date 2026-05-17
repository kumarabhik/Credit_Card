# notification-service

Go merchant webhook delivery service skeleton.

Expected layout:

- `cmd/server/` for process startup
- `internal/delivery/` for HTTP webhook dispatch
- `internal/queue/` for SQS consumption
- `internal/signing/` for webhook HMAC generation
- `internal/config/` for configuration loading
- `internal/obs/` for metrics, traces, and logging
- `test/integration/` for queue and webhook retry coverage
