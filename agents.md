# agents.md — Contributor Guide for AI Coding Agents

> This file tells AI coding agents (Claude Code, Cursor, Codex, Aider, …) how to work in this repo without breaking it. Humans should read it too — but it's *written for* agents.

---

## 0. Read order — do this first, every session

1. **`agents.md`** (this file)
2. **`system.md`** — developer-environment ground truth. Verify a tool is listed there **before** invoking it. If you install / upgrade / remove anything or change PATH or any env var, you **must** update `system.md` in the same change. Also, if you make a material repo, runtime, debugging, or process change, you **must** refresh the `system.md` checkpoint before ending the session so the next agent inherits facts instead of guesses. This file exists specifically to prevent agents from hallucinating "X is installed" when it isn't.
3. **`roadmap.md`** — find the next `[ ]` or `[~]` item; that is the work
4. **`docs/adr/`** — read all Architecture Decision Records before touching architecture
5. **`services/<name>/AGENTS.md`** (if present) — local override for that service

If any of the above contradicts a vague user request, the docs win. Surface the conflict; do not silently resolve it.

---

## 1. Hard rules — do not violate

These are not preferences. Violating them fails CI or fails review.

- **No real PAN in code, fixtures, logs, or tests.** Use the tokenizer. Anything matching `/\b(?:\d[ -]*?){13,19}\b/` fails CI via a pre-commit hook.
- **No secrets in repo.** Use `.env.example` + AWS Secrets Manager. `gitleaks` runs pre-commit.
- **No breaking proto changes.** `buf breaking` is enforced in CI. Add new fields with new tag numbers; never repurpose or remove tags.
- **Hot path = Go.** Do not introduce a JVM dependency on the synchronous authorize path (edge → auth → balance → ledger).
- **Every cross-service call needs**: timeout, circuit breaker, retry policy, OTel span. No exceptions.
- **Every Dynamo write that mutates state needs**: idempotency key OR optimistic-lock version. Plain `PutItem` without conditions is a review blocker.
- **No `interface{}` / raw `Object` at service boundaries.** Protobuf or typed DTOs only.
- **No `.block()` in WebFlux code.** Use `Mono` / `Flux` end to end.
- **No `time.Sleep` in test code.** Use real synchronization (channels, `eventually`, `Awaitility`).
- **Verify before invoking.** Don't run a tool that isn't listed in `system.md`. Don't trust a memory, a comment, or another agent's claim that "X is installed" — run `<tool> --version` and check the row. If a row is wrong, fix `system.md` before doing anything else. If a tool is missing, install it (with the user's permission) and update `system.md` + the "Recent changes" log in the same change.
- **Leave a factual checkpoint before you yield.** If you make any material code, runtime, roadmap, debugging, or process change, update `system.md` before ending the session. At minimum record what changed, what you verified, what is still broken (if anything), and the exact next step. This applies even when the host toolchain did not change.

---

## 2. Local dev — the 60-second loop

```
make up        # full stack via docker-compose + LocalStack
make smoke     # one synthetic APPROVE end-to-end (must return APPROVE in < 60s)
make test      # unit + integration per service
make load      # k6 smoke (30s, 100 RPS)
make chaos     # injects pod-kill on one random service for 30s
make down      # tear down
```

If `make up` takes > 60 s on a warm machine, file an issue — the inner loop is the product.

If a tool you need is missing locally, prefer adding it to `docker-compose.yml` over installing it on the host.

---

## 3. Per-language conventions

### Go  *(services 2, 3, 6, 8)*

- **Version:** Go 1.22+. `go.work` at repo root.
- **Lint:** `golangci-lint` with shared `.golangci.yml`. CI rejects warnings.
- **Errors:** Wrap with `fmt.Errorf("op-context: %w", err)`. Define sentinel errors per package; never `errors.New` at boundaries.
- **Logging:** `go.uber.org/zap` only. Always log `trace_id`. Never log PAN, CVV, full card token, JWT, or PII.
- **HTTP:** `net/http` + `chi`. **gRPC:** `google.golang.org/grpc`.
- **Concurrency:** `errgroup` for fan-out. Always pass `ctx`. Do not spawn bare goroutines from request handlers — they outlive the request and leak.
- **Tests:** `testify/require` for assertions. Table-driven. `testcontainers-go` for Redis / Postgres / Dynamo in integration tests. `gopter` for property tests.
- **Layout:** `cmd/<binary>/main.go`, `internal/...` (private), `pkg/...` (only if genuinely reusable across services).

### Java  *(services 4, 7)*

- **Version:** Java 21. **Framework:** Spring Boot 3.x, WebFlux (Reactor). Virtual threads opt-in per service.
- **Build:** Gradle (Kotlin DSL). Spotless + Checkstyle + ErrorProne.
- **Logging:** SLF4J + Logback JSON layout. MDC carries `trace_id`.
- **Reactive rule:** No blocking I/O on the event loop. **No `.block()` in production code.** Use R2DBC for SQL; for unavoidable blocking, schedule on `boundedElastic`.
- **Tests:** JUnit 5 + AssertJ + Testcontainers. Reactor `StepVerifier` for reactive flows. `jqwik` for property tests.
- **Records over POJOs** for DTOs. Constructor injection only (no field `@Autowired`).

### Python  *(service 5)*

- **Version:** Python 3.12. **Tooling:** `uv` for deps, `ruff` for lint+format, `mypy --strict`.
- **gRPC:** `grpcio` async server. Inference path stays sync (CPU-bound; release the GIL via numpy).
- **Models:** Artifacts in `model/`, versioned by content hash. **Never** load a model from network at request time.
- **No `print()`** — use `structlog` with JSON output.

### Terraform

- **Version:** 1.7+. Module-per-resource-family in `modules/`. Envs compose modules — no resources inline in env dirs.
- **CI gates:** `terraform fmt`, `tflint`, `tfsec`, `checkov` — all green or PR blocked.
- **No hardcoded ARNs.** Use data sources or module outputs.
- **State:** S3 backend + DynamoDB lock table. Never local state.

### Helm

- Umbrella chart in `infra/helm/umbrella/`, per-service subcharts.
- `helm lint` + `helm template | kubeconform` in CI.
- Values: `values.yaml` (defaults) + `values-{env}.yaml` (override). **Never** put secrets in values — use ExternalSecrets pointing at AWS Secrets Manager.

---

## 4. Where to put new things

| Adding... | Goes in... |
|---|---|
| A new fraud rule | `services/fraud-service/src/main/java/com/cc/fraud/rules/` — implement `RiskRule`, register in `RuleRegistry`, add `RuleTest` |
| A new gRPC method | Edit `proto/<svc>/v1/*.proto` → run `make proto` → implement server side, regenerate clients in same PR |
| A new Dynamo access pattern | First update `docs/adr/0003-dynamo-single-table.md` with the new PK/SK or GSI, then code |
| A new SLO | `observability/prometheus/rules/slo.yaml` + runbook in `observability/runbooks/<alert-name>.md` |
| A new env var | `.env.example` + service's `config.go` / `application.yaml` + Helm `values.yaml` + secret if sensitive |
| A new infra resource | Terraform module under `infra/terraform/modules/` first, never inline in an env dir |
| A new chaos experiment | `infra/k8s/chaos/<name>.yaml` + `make chaos-<name>` target |
| A new ADR | `docs/adr/NNNN-short-slug.md` — next available number, follow the template in `docs/adr/0001` |

---

## 5. Roadmap workflow

- Pick the next `[ ]` from `roadmap.md`. Flip to `[~]` in the same PR that starts the work.
- Keep one `[~]` at a time per agent session. If you must yield, leave a comment in the PR body explaining state.
- Flip to `[x]` **only** when all four are true:
  1. Code merged to `main`
  2. Tests added (unit + at least one integration where applicable)
  3. Docs / ADR updated if behavior or contract changed
  4. Observability hook added (metric, log field, or trace attribute)
- **Do not delete `[x]` items** — they are the audit trail. Strikethrough or annotate "(reverted in #PR)" if needed.

---

## 6. Testing strategy by ring

```
       ┌──────────────────────────────┐
       │  Chaos (Chaos Mesh, weekly)  │
       ├──────────────────────────────┤
       │  Load / soak (k6, nightly)   │
       ├──────────────────────────────┤
       │  E2E (compose, per PR)       │
       ├──────────────────────────────┤
       │  Contract (Pact, per PR)     │
       ├──────────────────────────────┤
       │  Integration (testcontainers)│
       ├──────────────────────────────┤
       │  Unit (per file change)      │
       └──────────────────────────────┘
```

A PR cannot ship without green **Unit + Integration + Contract**. **E2E** is nightly. **Load / Chaos** run on a schedule.

**No mocking infrastructure in integration tests.** Use `testcontainers` for Redis, Postgres, Dynamo (or LocalStack). Mocking your dependencies hides exactly the bugs integration tests exist to catch.

---

## 7. Common pitfalls (learned the hard way)

- **Dynamo conditional write returns success even on no-op.** Check `Attributes` returned to confirm intent (e.g. that the version actually moved).
- **WebFlux + blocking JDBC = deadlock under load.** Use R2DBC, or explicitly schedule blocking work on `boundedElastic`.
- **gRPC deadline propagation in Go:** must pass the request `ctx`, not derive a fresh `context.Background()`. Otherwise the downstream sees no deadline and your timeout budget evaporates.
- **Redis Lua scripts are atomic per shard.** Don't span keys across hash slots — use hash-tags `{account:123}:balance`, `{account:123}:holds`.
- **LocalStack ≠ AWS for IAM edge cases.** Validate IAM policies against real AWS at least once per env. LocalStack will happily accept things real IAM rejects.
- **`time.Now()` in tests = flaky tests.** Inject a clock (`clockwork.Clock` in Go, `java.time.Clock` in Java).
- **Idempotency key + body change = ambiguity.** Hash the body into the idempotency record; on a key reuse with different body, return `409 Conflict`.
- **OTel `SpanKind`** matters — server vs client vs internal changes how traces are aggregated. Set it explicitly.

---

## 8. Hot-path discipline

The hot path is `edge → auth → (balance ∥ fraud) → ledger`. Treat it like a third rail.

- **Latency budget is § 4 in `roadmap.md`.** If you add a hop, you must subtract from another hop's budget, and explain how in the PR.
- **Synchronous fan-out is fine, sequential fan-out is not.** `errgroup` in Go, parallel `Mono` in Java.
- **Anything that can be async, must be.** Settlement, notification, analytics, fraud-feedback — all off the hot path.
- **No new dependency added to a hot-path service without an ADR** that documents the latency cost.

---

## 9. Observability is non-negotiable

Every code change touches at least one of these:

- A **metric** (Prometheus counter / histogram / gauge)
- A **log field** (structured, with `trace_id`)
- A **trace attribute** (OTel span attribute on the active span)

If a PR doesn't add or change observability, ask: *would I be able to debug this at 3 a.m. from a dashboard alone?* If no — add the hook before merging.

**Naming:**

- Metric names: `<service>_<resource>_<action>_<unit>` — e.g. `auth_authorize_duration_seconds`, `ledger_writes_total`.
- Span names: lowercase, dotted — `auth.orchestrate`, `fraud.score.rules`, `ledger.put_item`.
- Log fields: `snake_case`, always include `trace_id`, never include PAN/CVV/JWT.

---

## 10. Asking for help

- For ambiguous design choices → write an **ADR draft** and open a PR with it. Don't decide silently.
- For unclear roadmap items → leave a comment on the item with the question; do not start work until clarified.
- For broken local dev → check `docs/troubleshooting.md` first, then file an issue.
- If you are an AI agent and you genuinely cannot proceed without user input → stop and ask. Do not fabricate a plausible-looking solution.

---

## 11. Anti-patterns — PR will be rejected

- Adding a **feature flag** without a removal date in the same PR
- **Sleep-based tests** (`time.Sleep` in test code, `Thread.sleep`)
- **Catching `Exception` / `error` and continuing silently** — log + propagate, or handle a specific subclass
- Adding a new dependency to the hot path **without an ADR**
- **Cross-service shared databases** — services own their data; cross-service reads go through the owner's API
- **`TODO` comments without an issue link**
- **Mocking infrastructure** in integration tests (use testcontainers)
- **`@Autowired` field injection** in Java — constructor injection only
- **Bare goroutines** spawned from request handlers (use `errgroup` with context cancellation)
- **`SELECT *`** in SQL or `Scan` without a projection in Dynamo — list the attributes you actually need
- **Hardcoded ARNs, account IDs, or region names** in code or Terraform
- **Changing the dev environment without updating `system.md`** — installing / upgrading / removing tooling, modifying PATH, or changing any host env var must include a `system.md` row update **and** a "Recent changes" log entry in the same logical change. (`system.md` itself is gitignored and per-machine; the update is local but mandatory before invoking anything new.)
- **Ending a significant session without updating the `system.md` checkpoint** — if the work changed repo behavior, runtime state, debugging truth, or the resume plan, document it locally before you stop.

---

## 12. PR checklist (copy into every PR description)

```
## What
<one paragraph>

## Why
<link to roadmap.md item, ADR, or issue>

## Roadmap impact
- [ ] Flipped roadmap.md item from [ ] → [~] or [~] → [x]
- [ ] ADR added/updated if architecture changed

## Tests
- [ ] Unit
- [ ] Integration (testcontainers)
- [ ] Contract (Pact) — if proto changed
- [ ] Property-based — if invariants changed

## Observability
- [ ] Metric / log field / trace attribute added or updated
- [ ] Runbook updated if alert changed

## Security
- [ ] No secrets, no PAN, no PII added to repo or logs
- [ ] gitleaks / trivy / semgrep green
```

---

## 13. Glossary (so we speak the same language)

| Term | Meaning |
|---|---|
| **Authorization** | The synchronous "can this card pay this merchant this amount right now" decision |
| **Capture** | Later step that converts a hold into a real charge (often T+0 or T+1) |
| **Hold** | Temporary reservation of funds against an account |
| **Reversal** | Cancellation of an authorization before capture |
| **Refund** | Return of funds after capture |
| **Chargeback** | Dispute-initiated reversal, weeks/months later |
| **Idempotency key** | Client-supplied unique ID that makes a request safely retryable |
| **Saga** | Sequence of local transactions with compensations, used instead of distributed 2PC |
| **Outbox** | Pattern where events are written transactionally with state, then relayed |
| **MCC** | Merchant Category Code (4-digit, e.g. `5411` Grocery) |
| **PAN** | Primary Account Number — the full card number; high sensitivity |
| **BIN** | Bank Identification Number — first 6–8 digits of PAN; identifies issuer |
| **PCI DSS** | Payment Card Industry Data Security Standard |
| **3DS** | 3-D Secure — step-up auth challenge (e.g. SMS code, biometric) |
| **ISO-8583** | The dominant card-network message format; we use its reason codes |
| **RED / USE** | Metric methodologies — Rate/Errors/Duration for requests, Utilization/Saturation/Errors for resources |
| **SLO / SLI / error budget** | Service Level Objective / Indicator / how much unreliability you can spend |

---

## 14. When this file is wrong

This document codifies what we know today. If you (agent or human) find that reality has diverged — a hard rule blocks legitimate work, a convention no longer fits, a pitfall has been engineered away — **update this file** in the same PR that proves the change. Don't work around it silently.

The roadmap is the work; this file is the contract.
