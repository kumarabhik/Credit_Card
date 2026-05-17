# Services

The monorepo is organized around eight services plus shared protocol contracts.

- `edge-gateway`: Envoy plus Go ext-authz for JWT, rate limiting, and PAN tokenization
- `auth-service`: Go hot-path orchestrator and saga coordinator
- `balance-service`: Go account and hold service backed by Redis and Postgres
- `fraud-service`: Java 21 Spring Boot WebFlux rules engine and ML client
- `ml-scorer`: Python gRPC scorer serving a versioned XGBoost model
- `ledger-service`: Go DynamoDB ledger and outbox writer
- `settlement-service`: Java async settlement and reconciliation worker
- `notification-service`: Go signed webhook delivery worker
