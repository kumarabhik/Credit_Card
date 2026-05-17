# fraud-service

Java 21 Spring Boot WebFlux fraud scoring service skeleton.

Expected layout:

- `src/main/java/com/cc/fraud/grpc/` for gRPC transport
- `src/main/java/com/cc/fraud/rules/` for pluggable `RiskRule` implementations
- `src/main/java/com/cc/fraud/scoring/` for aggregation and orchestration
- `src/main/java/com/cc/fraud/config/` for application wiring
- `src/main/resources/` for configuration assets
- `src/test/java/com/cc/fraud/` for unit and integration tests
