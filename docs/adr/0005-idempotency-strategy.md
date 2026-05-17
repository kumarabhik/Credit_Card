# ADR-0005: Idempotency Strategy for Authorization Requests

- Status: Accepted
- Date: 2026-05-18

## Context

The authorization entrypoint is explicitly retryable. Merchants, gateways, and client SDKs will resend the same
authorization request when they see a timeout, a broken connection, or an ambiguous network failure. Without a
repository-wide idempotency contract, those retries would double-spend the same balance, create duplicate ledger
side effects, and make it impossible to distinguish a safe replay from a genuinely new purchase attempt.

The repo contract adds two hard constraints that matter here:

- every state-changing DynamoDB write must be protected by idempotency or optimistic locking
- the hot path must stay in Go and remain safe under concurrent fan-out and retry storms

We also need the implementation to work in local development against LocalStack while staying faithful to the
production behavior we want in AWS.

## Decision

We will make `Idempotency-Key` mandatory for `POST /v1/authorize` and for the gRPC authorize request payload.

The auth service will enforce idempotency with the following rules:

1. Every request body is hashed deterministically before work begins.
2. A DynamoDB item is claimed first with a conditional `PutItem` against `PK=IDEMP#{key}` and `SK=META`.
3. Claimed records carry a 24-hour TTL so duplicate protection automatically expires.
4. If the same key is retried with the same request hash after the response is stored, the cached authorize response is replayed.
5. If the same key is retried with a different request hash, the service returns `409 Conflict` / `ALREADY_EXISTS`.
6. Local development may fall back to an in-memory store, but production semantics are defined by the DynamoDB flow above.

Observability is part of the decision:

- spans record the `idempotency_key` attribute on `auth.orchestrate`
- structured logs include `trace_id`, `merchant_id`, `txn_id`, and whether the response was replayed

## Consequences

### Positive

- Safe client retries no longer risk duplicate authorization side effects.
- Concurrent requests racing on the same key converge on one stored response.
- The policy is explicit enough to test with goroutine fan-out and to reason about in incident response.
- TTL keeps the idempotency table bounded without a manual cleanup job.

### Negative

- Clients now must generate and preserve an idempotency key for every authorize call.
- The authorize hot path adds one DynamoDB write before business logic and one update after success.
- Reusing a key with a different body becomes a user-visible conflict instead of a silent overwrite.

## Follow-up

- Add the same conflict semantics to capture, reverse, and refund once those endpoints exist.
- Tie downstream ledger writes to the same request identity so replay analysis stays end-to-end.
