# ADR-0003: DynamoDB Single-Table Design for Ledger Entries

- Status: Accepted
- Date: 2026-05-18

## Context

The ledger service sits on the synchronous authorization hot path and needs predictable write latency while still
supporting multiple query patterns:

- account-centric transaction history
- merchant-centric views for operational analysis
- idempotency lookups for safe retries and duplicate detection

The repository contract also requires conditional writes or optimistic locking for every state mutation. That means
the ledger design cannot rely on blind `PutItem` operations even if each authorization only creates one new ledger
entry.

## Decision

We will store ledger state in the shared `cc-ledger-local` table using these item families:

1. **Account state item**
   - `PK=ACCT#{account_id}`
   - `SK=STATE`
   - Carries the optimistic-lock `version` attribute for the account ledger stream.

2. **Ledger entry item**
   - `PK=ACCT#{account_id}`
   - `SK=TXN#{ulid}`
   - Stores the immutable authorization / capture / reversal / refund event.

3. **Merchant query projection**
   - `GSI1PK=MCH#{merchant_id}`
   - `GSI1SK=TS#{created_at}#TXN#{ulid}`
   - Supports merchant-centric operational lookups.

4. **Idempotency query projection**
   - `GSI2PK=IDEMP#{idempotency_key}`
   - `GSI2SK=TXN#{ulid}`
   - Supports duplicate-write lookup without scanning an account partition.

Ledger writes are executed as a DynamoDB transaction:

- update the `STATE` item only if the current `version` matches the expected version
- put the immutable ledger entry only if that `(PK, SK)` pair does not already exist

`ledger_id` is encoded as `{account_id}|{ulid}` so the service can reconstruct the primary key pair for point reads
without an extra lookup index.

## Consequences

### Positive

- Account history stays clustered in one partition for the primary read path.
- Merchant and idempotency lookups become index queries instead of scans.
- Optimistic locking is explicit and observable through the `version` attribute.
- The design fits the repo rule that state-changing Dynamo writes must be guarded.

### Negative

- `ledger_id` becomes an encoded identifier rather than a random opaque token.
- Writes are slightly more expensive because each mutation touches both the state item and the immutable entry item.
- Query flexibility is bounded by the indexes we model up front.

## Follow-up

- Add outbox relay items in the same table when asynchronous settlement and notification flows are wired.
- Revisit whether merchant projections need day-bucketed partition keys once load testing data exists.
