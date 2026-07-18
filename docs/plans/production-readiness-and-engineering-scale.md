# Highland Production Readiness and Engineering Scale Plan

Status: **Proposed**

Owner: Highland maintainers
Created: 2026-07-18
Target release line: post-0.3.x
Source review baseline: `58e6572` (`main` on 2026-07-18)

This is the authoritative execution plan for the five highest-impact improvements identified in the
whole-project review performed after Highland 0.3.0:

1. finish the multi-replica durability and high-availability model;
2. harden the build, release, and runtime software supply chain;
3. turn the documented compatibility matrix into an enforced certification matrix;
4. make OpenAPI the executable source of truth for the public Highland API; and
5. reduce critical-module concentration while introducing risk-based coverage gates.

The plan deliberately treats these as one production-readiness program. They share release gates,
test environments, migration constraints, evidence requirements, and operational documentation.
No single workstream is considered complete merely because its code has merged.

---

## 1. Executive outcome

At completion, an operator must be able to install a Highland release and establish, from published
evidence, all of the following:

- two or more API replicas provide consistent authentication protection, identity behavior,
  operation execution, and audit visibility;
- privileged mutations never silently lose their required audit record;
- voluntary disruption or a single-node failure does not unnecessarily remove the whole Highland
  control plane;
- every shipped container, chart, and runtime helper image is immutable, scanned, inventoried,
  signed, and traceable to source;
- every provider/version combination claimed as tested has a recent, machine-readable qualification
  result tied to the release commit;
- every public API route is represented by a versioned contract and protected against accidental
  backend/frontend drift;
- critical security and mutation code has substantially stronger tests than low-risk presentation or
  mapping code;
- provider work can be extended without repeatedly changing thousand-line planners, inventory
  modules, pages, or the process bootstrap; and
- rollback from each migration stage is documented, tested, and does not discard audit, identity, or
  storage-operation evidence.

Success is measured by production behavior and reproducible evidence, not checklist completion
alone.

## 2. Baseline and motivating evidence

### 2.1 Current strengths to preserve

The current project already provides a strong base:

- guarded, durable `StorageOperation` workflows with leader election;
- fail-closed storage policy and provider authorization;
- local and OIDC authentication, TOTP, session versioning, CSRF, RBAC, and login throttling;
- informer-backed Kubernetes storage inventory and bounded provider adapters;
- secure container defaults, NetworkPolicy, readiness/liveness endpoints, Prometheus metrics, and
  alerts;
- frontend type checking, linting, unit tests, Playwright coverage, Storybook, accessibility checks,
  lazy loading, and bundle budgets;
- Go unit tests, race-sensitive nightly tests, Helm render tests, OpenAPI linting, and provider
  contract fixtures; and
- unusually detailed architecture, security, provider, compatibility, validation, and runbook
  documentation.

This plan must not weaken those properties while restructuring the implementation.

### 2.2 Verified baseline on 2026-07-18

The review baseline produced the following results:

| Check | Baseline result |
|---|---|
| `go test ./... -count=1` | pass |
| `go vet ./...` | pass |
| Go statement coverage | 55.5% overall |
| `cmd/highland-api` coverage | 0.0% |
| frontend typecheck | pass |
| frontend lint | pass |
| frontend unit tests | 20 files, 87 tests, all pass |
| frontend production build | pass |
| initial frontend gzip budget | 177,340 bytes of 256,000 bytes |
| Git worktree before this plan | clean |

These figures are a point-in-time baseline, not permanent acceptance thresholds.

### 2.3 Current-state findings

#### HA and durable security state

- `chart/values.yaml` defaults both API and web to two replicas.
- `internal/audit.Store` maintains a process-local ring and optionally appends JSONL to a file.
- audit append, encode, and close failures are currently ignored by `Store.Append`.
- a process mutex cannot coordinate concurrent append behavior across API replicas sharing RWX
  storage.
- the audit list endpoint observes only the local replica's in-memory ring.
- local-login throttling explicitly stores counters per process.
- Redis is optional and currently focused on sessions, not all shared ephemeral security state.
- the chart has no PodDisruptionBudget or default topology-spread behavior.
- storage-operation execution already uses Kubernetes Lease leader election and must retain that
  behavior.

#### Supply chain

- release workflows build and push images and an OCI chart but do not publish signatures,
  provenance, SBOMs, or vulnerability reports.
- container bases and runtime benchmark images are tag-addressed rather than digest-addressed.
- `benchmark.fioImage` defaults to a floating `latest` tag.
- GitHub Actions are version-tagged rather than commit-SHA pinned.
- CI does not currently run an explicit Go vulnerability scan, container scan, chart scan, or secret
  scan.

#### Compatibility qualification

- `docs/compatibility.yaml` contains detailed Kubernetes and provider claims.
- the nightly workflow always runs contracts/scale tests but live Rook profiles are conditional on a
  repository variable and self-hosted runners.
- OpenEBS and LINSTOR live gates are documented but not represented as equivalent scheduled jobs.
- compatibility claims and workflow matrices are maintained independently.
- release automation does not consume a qualification manifest or enforce evidence freshness.

#### API contract

- `docs/openapi/storage-v1.yaml` covers the storage control-plane surface and is linted in CI.
- identity, account, audit, status, benchmark, compatibility, and some provider routes are not part
  of the same complete public contract.
- TypeScript DTOs and request functions are hand-maintained and frequently rely on JSON casts.
- Go handlers and request/response structs are not generated from, or validated at runtime against,
  the OpenAPI contract.
- CI has no breaking-contract diff gate.

#### Module concentration and coverage

- `internal/storage/operations/planner.go` is approximately 1,712 lines.
- `internal/storage/inventory.go` is approximately 1,190 lines.
- `internal/storage/context_engine.go` is approximately 1,183 lines.
- `cmd/highland-api/main.go` is approximately 550 lines and has no direct test coverage.
- several React feature pages range from roughly 600 to 1,285 lines.
- the current test suite is useful, but global coverage does not distinguish security-critical code
  from generated code or low-risk view composition.

## 3. Scope

### 3.1 In scope

- API and web high availability within one Kubernetes cluster.
- Shared security-state semantics across replicas.
- Durable, queryable, failure-aware audit storage and migration.
- Chart disruption, scheduling, rollout, and readiness behavior.
- Artifact integrity, SBOM, provenance, signing, vulnerability policy, and digest support.
- Runtime helper image policy, especially `fio` Jobs.
- A machine-readable provider qualification specification and results format.
- Scheduled, pull-request, and release qualification orchestration.
- Full public API contract coverage and generated client/server artifacts.
- Contract compatibility policy and deprecation rules.
- Decomposition of the bootstrap, operation planner, inventory, provider adapters, and large UI
  feature modules.
- Risk-based automated coverage policy.
- Documentation, migration, rollback, validation reports, and release evidence.

### 3.2 Explicit non-goals

- Multi-cluster management.
- Replacing Kubernetes as the storage-operation source of truth.
- Replacing provider data planes or provider-native controllers.
- Adding new storage providers solely to exercise the new architecture.
- Implementing OpenEBS-native or LINSTOR-native mutations as part of this program.
- Rewriting the frontend framework or API language.
- Replacing all existing unit tests with generated tests.
- Requiring a relational database for installations that use no durable audit feature, unless the
  final architecture decision establishes that production mode requires one.
- Treating raw statement coverage as proof of correctness.
- Promoting current preview providers to production support without their independent functional and
  operational gates passing.

### 3.3 Compatibility promise during the program

- Existing 0.3.x values must either continue to render or fail with a precise migration message.
- Existing local identities, password hashes, MFA material, session versions, policies, and
  `StorageOperation` objects must survive upgrades.
- Existing signed-cookie sessions should remain valid when the signing key and session policy are
  unchanged.
- Redis remains optional until an explicitly documented production/HA profile requires it.
- Existing public JSON fields are not removed or retyped without the contract deprecation process.
- Existing provider IDs and resource IDs remain stable.
- Storage writes remain disabled by default.
- A rollback must never delete audit data or nonterminal `StorageOperation` objects.

## 4. Program invariants

These are non-negotiable constraints across all phases.

1. **Fail closed for authority and audit.** If Highland requires a durable audit record for a
   privileged mutation and cannot persist it, the mutation is not admitted.
2. **Kubernetes remains authoritative for storage operations.** Shared databases or Redis do not
   replace `StorageOperation` CRs, Kubernetes resource versions, or controller leader election.
3. **One mutation, one durable identity.** Retries and replica failover must not create duplicate
   logical operations or duplicate external mutations.
4. **No secrets in telemetry or build evidence.** Audit metadata may contain approved operational
   identifiers; metrics, SBOMs, provenance, workflow artifacts, and logs must not contain credentials
   or recovery material.
5. **Immutable execution inputs.** Release images and runtime helper images used by privileged
   workflows must be addressable by digest.
6. **Claims require evidence.** A compatibility row cannot say `tested` or `release-gate: passed`
   without a recent result for the exact release commit or an explicitly allowed ancestor.
7. **The contract describes reality.** A route cannot be considered public and supported while
   absent from the canonical API contract.
8. **Generated code is reproducible.** Generation must be deterministic, committed or verified in
   CI, and require no network access after pinned tools are installed.
9. **Refactors preserve behavior first.** Package/component extraction lands with equivalence tests
   before functional expansion.
10. **Readiness remains bounded.** Readiness checks must not synchronously fan out to every provider
    or external system.
11. **Default installs remain safe.** New HA and security controls must have safe, usable defaults;
    optional integrations must remain explicit.
12. **No big-bang migration.** Each workstream must be independently deployable, observable, and
    reversible.

## 5. Target architecture

### 5.1 Runtime state ownership

| State | Authoritative owner | Replica behavior | Durability expectation |
|---|---|---|---|
| local identities and MFA | Kubernetes Secret or successor identity store | optimistic concurrency and replica sync | durable |
| signed-cookie session claims | browser cookie + signing key | stateless validation on all replicas | TTL-bound |
| centrally revocable sessions | Redis session backend | shared | optional durable/HA according to Redis deployment |
| login throttle counters | shared limiter backend in HA mode | atomic increments/expiry | ephemeral but replica-consistent |
| storage policy | `HighlandPolicy` CR | shared informer/cache | durable |
| storage operations | `StorageOperation` CRs | one elected reconciler | durable |
| audit records | `AuditSink` abstraction with durable backend | shared query semantics | append-only and failure-aware |
| provider/inventory caches | process-local informer-derived caches | eventual convergence | rebuildable |
| benchmark history | existing Kubernetes persistence, later contract-cleaned | shared Kubernetes source | durable when enabled |
| metrics | per process, aggregated by Prometheus | replica labels where required | external retention |

### 5.2 Audit abstraction

Introduce an interface that makes failure semantics explicit:

```go
type AuditSink interface {
    Append(ctx context.Context, event Event) error
    List(ctx context.Context, query Query) (Page, error)
    Health(ctx context.Context) Health
    Durable() bool
    Close(ctx context.Context) error
}
```

Required implementations:

- `MemoryAuditSink` for local development and tests; explicitly non-durable;
- one production durable sink selected by an ADR;
- an optional fan-out wrapper for transition periods, supporting primary/secondary writes with
  precisely defined failure policy; and
- a compatibility importer for existing JSONL files if JSONL is not retained as the production
  backend.

The durable backend decision must compare at least:

- PostgreSQL or another transactional database;
- a dedicated append-only log service or webhook receiver;
- Kubernetes custom resources with bounded partitioning and retention; and
- a single-writer JSONL service/sidecar with externally managed durable storage.

The decision must account for atomic append, query/pagination, tamper evidence, backups, retention,
multi-replica access, operational burden, and failure behavior. A shared file directly opened by all
API replicas is not an acceptable final HA design.

### 5.3 Shared rate limiter

Define a backend-neutral limiter contract:

```go
type LoginLimiter interface {
    Allow(ctx context.Context, username, clientKey string) (Decision, error)
    RecordFailure(ctx context.Context, username, clientKey string) error
    RecordSuccess(ctx context.Context, username, clientKey string) error
    Health(ctx context.Context) Health
    Close() error
}
```

The Redis implementation must use atomic scripts or transactions so IP and `user@IP` windows cannot
partially update. The in-memory implementation remains valid for single-replica development. HA mode
must surface and enforce the selected fail-open/fail-closed policy; authentication security defaults
must not silently weaken when Redis is unavailable.

### 5.4 Application composition

Move process construction from `cmd/highland-api/main.go` into an injectable application package:

```text
cmd/highland-api/main.go
  -> config load
  -> signal context
  -> app.Build(ctx, Dependencies)
  -> app.Run(ctx)

internal/app/
  config_validation.go
  dependencies.go
  providers.go
  storage.go
  auth.go
  server.go
  lifecycle.go
```

`Build` returns an application or a typed startup error. It must not call `os.Exit`. `main` owns exit
codes. Tests can inject fake Kubernetes clients, clocks, HTTP clients, audit sinks, limiters, and
provider factories.

### 5.5 API contract architecture

The canonical contract will be a root OpenAPI 3.1 document split into bounded files:

```text
docs/openapi/
  highland-v1.yaml
  paths/
    auth.yaml
    account.yaml
    admin.yaml
    audit.yaml
    benchmarks.yaml
    compatibility.yaml
    storage.yaml
    providers.yaml
    status.yaml
  schemas/
    common.yaml
    errors.yaml
    identity.yaml
    operations.yaml
    providers.yaml
    storage.yaml
```

Generated artifacts should live in explicit generated directories with headers and reproducibility
checks. Handwritten domain types may remain where generation would distort the domain model, but
wire types and clients must have one authoritative schema.

### 5.6 Qualification architecture

`docs/compatibility.yaml` remains the human-facing compatibility declaration. A schema-validated
qualification manifest links claims to runnable profiles:

```text
qualification/
  schema.json
  profiles.yaml
  results.schema.json
  scripts/
  fixtures/
  results/                 # release evidence or links, not secrets/raw kubeconfigs
```

Each profile declares:

- stable profile ID;
- provider and exact version selectors;
- Kubernetes distribution/version/architecture;
- environment class (`kind`, hosted runner, self-hosted lab, disposable VM);
- prerequisites and destructive-risk class;
- test suites and required assertions;
- cleanup assertions;
- timeout and retry policy;
- evidence retention and freshness window; and
- whether it gates pull requests, nightly health, preview releases, or production promotion.

## 6. Workstream A — Multi-replica durability and HA

### A0 — Decisions, threat model, and failure policy

- [ ] **HA-A0.1** Write an ADR defining the supported deployment profiles:
  single-replica development, default multi-replica, and production HA.
- [ ] **HA-A0.2** Inventory every mutable or cached state holder in the API and classify it as
  authoritative, durable, shared ephemeral, or replica-local rebuildable state.
- [ ] **HA-A0.3** Document consistency expectations and maximum convergence time for each state class.
- [ ] **HA-A0.4** Decide the production audit backend using the comparison in section 5.2.
- [ ] **HA-A0.5** Decide whether durable auditing is mandatory for all production profiles or only
  when storage/admin writes are enabled.
- [ ] **HA-A0.6** Define audit failure policy separately for read-only requests, login events,
  denied requests, user administration, policy changes, and storage mutations.
- [ ] **HA-A0.7** Define shared-limiter failure behavior and operator override semantics.
- [ ] **HA-A0.8** Extend the security threat model with replica hopping, partial backend outage,
  split audit visibility, concurrent append corruption, stale identity cache, and disruption cases.
- [ ] **HA-A0.9** Define recovery point and recovery time objectives for identity, audit, policy, and
  operations.
- [ ] **HA-A0.10** Record data retention, privacy, redaction, and deletion obligations for audit
  events.

#### A0 definition of done

- [ ] Every stateful subsystem has one named authority and explicit failure behavior.
- [ ] The audit backend and migration direction are approved before implementation begins.
- [ ] No later phase depends on an unresolved fail-open/fail-closed decision.

### A1 — Audit interface and error propagation

- [ ] **HA-A1.1** Introduce context-aware `AuditSink`, query, page, health, and typed error models.
- [ ] **HA-A1.2** Convert `MemoryAuditSink` from the current ring implementation without changing
  local-development behavior.
- [ ] **HA-A1.3** Make append failures observable to callers rather than silently ignored.
- [ ] **HA-A1.4** Update every audit call site to classify whether append is precondition, best effort,
  or post-action evidence.
- [ ] **HA-A1.5** For privileged mutations, persist an admission/request audit event before the
  mutation and a result event after reconciliation.
- [ ] **HA-A1.6** Ensure failure to append a required pre-mutation event prevents operation creation.
- [ ] **HA-A1.7** Ensure a post-mutation audit outage never causes an unsafe mutation retry; retain the
  `StorageOperation` and mark audit delivery pending.
- [ ] **HA-A1.8** Add an audit-delivery status/condition to durable operations if required by the ADR.
- [ ] **HA-A1.9** Add bounded retry with jitter for retryable audit failures.
- [ ] **HA-A1.10** Add dead-letter or operator-visible reconciliation for records that cannot be
  delivered after the normal retry budget.
- [ ] **HA-A1.11** Preserve request ID, operation ID, action ID, provider, target identity, user, role,
  source address, outcome, and timestamp fields.
- [ ] **HA-A1.12** Version the audit event schema and define forward/backward decoding behavior.
- [ ] **HA-A1.13** Reject secrets, passwords, tokens, recovery codes, raw request bodies, and provider
  credentials through structured event construction.
- [ ] **HA-A1.14** Add redaction tests for every sensitive API family.
- [ ] **HA-A1.15** Add sink health and queue-depth metrics without user/resource label cardinality.

### A2 — Durable audit backend and migration

- [ ] **HA-A2.1** Implement the selected durable audit sink with atomic append semantics.
- [ ] **HA-A2.2** Implement stable cursor pagination and filters for time, result, action, provider,
  operation ID, and bounded user lookup.
- [ ] **HA-A2.3** Add unique event IDs that remain collision-safe across replicas and restarts.
- [ ] **HA-A2.4** Preserve total ordering where the backend guarantees it; otherwise define stable
  ordering by timestamp plus event ID.
- [ ] **HA-A2.5** Add retention configuration with safe lower bounds for privileged-operation audit.
- [ ] **HA-A2.6** Add backup and restore documentation and a restoration verification command.
- [ ] **HA-A2.7** Add an importer for existing JSONL records with idempotent checkpoints.
- [ ] **HA-A2.8** Validate every imported line, quarantine malformed records, and report exact counts.
- [ ] **HA-A2.9** Provide dry-run import, resumable import, and duplicate detection.
- [ ] **HA-A2.10** Provide a dual-write migration mode only if its ordering and failure semantics are
  explicitly safe.
- [ ] **HA-A2.11** Make the audit API query the shared backend so all replicas return equivalent data.
- [ ] **HA-A2.12** Remove direct shared-RWX append from the production HA path.
- [ ] **HA-A2.13** Retain read-only JSONL export for portability if useful.
- [ ] **HA-A2.14** Add integrity verification, such as hash chaining or signed batches, if selected by
  the ADR.
- [ ] **HA-A2.15** Add corruption, disk-full, permission-loss, network-partition, timeout, and backend
  restart tests.

### A3 — Shared login throttling and session profile

- [ ] **HA-A3.1** Extract the current in-memory limiter behind the shared interface.
- [ ] **HA-A3.2** Implement Redis atomic counters for IP and normalized `username@IP` keys.
- [ ] **HA-A3.3** Preserve IPv4 `/32` and IPv6 `/64` normalization behavior.
- [ ] **HA-A3.4** Preserve exponential backoff, maximum lockout, failure-window expiry, and success
  reset semantics.
- [ ] **HA-A3.5** Namespace keys by cluster identity and Highland installation.
- [ ] **HA-A3.6** Never store plaintext passwords, session cookies, MFA codes, or recovery codes in
  limiter keys/values.
- [ ] **HA-A3.7** Hash or otherwise minimize usernames in Redis keys while retaining collision-safe
  behavior.
- [ ] **HA-A3.8** Add Redis TLS, CA, authentication, connection timeout, and health configuration.
- [ ] **HA-A3.9** Validate Redis configuration at startup for profiles that require shared limiting.
- [ ] **HA-A3.10** Add rate-limit backend identity and degraded status to the status API without
  exposing connection details.
- [ ] **HA-A3.11** Exercise alternating login attempts across replicas and prove one shared threshold.
- [ ] **HA-A3.12** Document signed-cookie and Redis-session tradeoffs separately from rate limiting.
- [ ] **HA-A3.13** Confirm local-account session-version revocation remains consistent across
  replicas during identity Secret updates.
- [ ] **HA-A3.14** Decide and document OIDC session revocation expectations.

### A4 — Kubernetes availability controls

- [ ] **HA-A4.1** Add configurable API and web PodDisruptionBudgets.
- [ ] **HA-A4.2** Default PDB behavior safely for replica counts greater than one.
- [ ] **HA-A4.3** Avoid rendering an impossible PDB for a one-replica development install.
- [ ] **HA-A4.4** Add `topologySpreadConstraints` values for API and web separately.
- [ ] **HA-A4.5** Provide a soft hostname anti-affinity default that does not make small clusters
  unschedulable.
- [ ] **HA-A4.6** Allow strict zone/node spreading in a production values profile.
- [ ] **HA-A4.7** Add configurable deployment rolling strategies with safe `maxUnavailable` and
  `maxSurge` defaults.
- [ ] **HA-A4.8** Add startup probes if informer initialization can exceed liveness timing on large
  clusters.
- [ ] **HA-A4.9** Keep readiness bounded and distinguish dependency degradation from process
  liveness.
- [ ] **HA-A4.10** Add termination grace configuration aligned with SSE closure, leader release,
  audit flush, and HTTP shutdown deadlines.
- [ ] **HA-A4.11** Add lifecycle hooks only where they improve deterministic drain behavior.
- [ ] **HA-A4.12** Ensure web readiness checks the local server and optionally API reachability without
  causing cascading restart loops.
- [ ] **HA-A4.13** Add chart tests for 1, 2, and 3 replicas, with and without audit and Redis.
- [ ] **HA-A4.14** Add a production example values file covering PDB, spread, Redis, durable audit,
  TLS ingress, and monitoring.

### A5 — HA observability and live validation

- [ ] **HA-A5.1** Expose per-replica build/version/start-time information.
- [ ] **HA-A5.2** Add metrics for audit append success/failure/latency, audit backlog, limiter backend
  errors, identity sync age, elected operation leader, and graceful shutdown failures.
- [ ] **HA-A5.3** Add alerts for required audit sink unavailable, sustained audit backlog, no operation
  leader, multiple observed leaders, stale identity sync, and shared limiter unavailable.
- [ ] **HA-A5.4** Extend the Grafana dashboard with HA and security-state panels.
- [ ] **HA-A5.5** Add a runbook for each new alert.
- [ ] **HA-A5.6** Validate two replicas receive alternating requests without audit/list divergence.
- [ ] **HA-A5.7** Delete the elected operation-controller pod during a safe disposable operation and
  prove recovery without duplicate mutation.
- [ ] **HA-A5.8** Drain a node and prove the PDB/topology rules preserve service where cluster
  capacity permits.
- [ ] **HA-A5.9** Restart the durable audit backend during reads and during a planned privileged
  mutation; verify the defined failure policy.
- [ ] **HA-A5.10** Restart Redis during login attempts and verify the defined failure policy.
- [ ] **HA-A5.11** Perform an identity role/disable/session-revoke update while requests alternate
  across replicas.
- [ ] **HA-A5.12** Capture exact recovery times and compare them with the approved objectives.

#### Workstream A completion gate

- [ ] Required audit events cannot be silently lost.
- [ ] Audit queries are replica-independent.
- [ ] HA login throttling is shared and atomic.
- [ ] Identity and session-revocation behavior is proven across replicas.
- [ ] One API pod failure and one voluntary disruption scenario pass without duplicate operations.
- [ ] The production HA profile has complete install, backup, recovery, alert, and rollback docs.

## 7. Workstream B — Software supply-chain hardening

### B0 — Policy and inventory

- [ ] **SC-B0.1** Write a supply-chain policy defining trusted registries, digest requirements,
  supported architectures, signature policy, SBOM format, and vulnerability severity thresholds.
- [ ] **SC-B0.2** Inventory container bases, Helm dependencies, Go modules, npm packages, GitHub
  Actions, downloaded CI tools, and runtime helper images.
- [ ] **SC-B0.3** Classify dependencies by execution privilege and customer-cluster impact.
- [ ] **SC-B0.4** Mark the `fio` image and embedded Longhorn chart as high-impact runtime inputs.
- [ ] **SC-B0.5** Define exception fields: identifier, affected artifact/CVE, justification, owner,
  compensating control, expiry, and review link.
- [ ] **SC-B0.6** Define maximum exception lifetime and prevent unbounded allowlists.
- [ ] **SC-B0.7** Decide whether release builds must use GitHub-hosted isolated runners.
- [ ] **SC-B0.8** Document artifact retention and public verification expectations.

### B1 — Immutable dependencies and images

- [ ] **SC-B1.1** Pin API builder and runtime base images by digest.
- [ ] **SC-B1.2** Pin web builder and nginx runtime images by digest.
- [ ] **SC-B1.3** Preserve human-readable version comments next to digests for maintainability.
- [ ] **SC-B1.4** Replace the floating `benchmark.fioImage` default with a reviewed version and digest.
- [ ] **SC-B1.5** Add separate `repository`, `tag`, and `digest` fields for runtime helper images.
- [ ] **SC-B1.6** Render `repository@sha256:...` when a digest is supplied and reject malformed
  digests.
- [ ] **SC-B1.7** Decide whether production benchmark execution rejects tag-only images.
- [ ] **SC-B1.8** Expose the resolved helper image digest in benchmark evidence and audit metadata.
- [ ] **SC-B1.9** Add admission-policy examples for restricting Highland-created Jobs to approved
  image digests.
- [ ] **SC-B1.10** Pin every GitHub Action to a full commit SHA with a version comment.
- [ ] **SC-B1.11** Pin downloaded CLI tools and verify checksums rather than using unbounded
  `npx --yes` resolution.
- [ ] **SC-B1.12** Verify `Chart.lock`, Go sums, and npm lockfile consistency in CI.
- [ ] **SC-B1.13** Add Renovate or Dependabot rules that update digest pins while preserving review
  visibility.

### B2 — Static and dependency security gates

- [ ] **SC-B2.1** Run `govulncheck ./...` on pull requests and main.
- [ ] **SC-B2.2** Run an npm production-dependency audit with a documented severity policy.
- [ ] **SC-B2.3** Add secret scanning for committed and generated artifacts.
- [ ] **SC-B2.4** Add Go and TypeScript security/static-analysis rules focused on command execution,
  path handling, TLS, SSRF, weak randomness, unsafe deserialization, and ignored security errors.
- [ ] **SC-B2.5** Scan rendered Helm manifests for privileged containers, missing security contexts,
  dangerous capabilities, host access, broad RBAC, and mutable images.
- [ ] **SC-B2.6** Scan final API and web images, not just source dependency graphs.
- [ ] **SC-B2.7** Scan the runtime `fio` image and embedded Longhorn dependency under the same policy.
- [ ] **SC-B2.8** Fail for fixable critical vulnerabilities unless a non-expired exception exists.
- [ ] **SC-B2.9** Define the high-severity gate based on reachability, exploitability, and fix
  availability rather than an undocumented blanket ignore.
- [ ] **SC-B2.10** Upload machine-readable SARIF or equivalent reports with bounded retention.
- [ ] **SC-B2.11** Ensure forked pull requests cannot access publishing credentials.
- [ ] **SC-B2.12** Add a scheduled scan of the latest released digests so newly disclosed issues are
  detected after release.

### B3 — SBOM and provenance

- [ ] **SC-B3.1** Generate SPDX JSON or CycloneDX SBOMs for API and web final images.
- [ ] **SC-B3.2** Generate a chart/component inventory including the embedded Longhorn dependency.
- [ ] **SC-B3.3** Include OS packages, Go modules, npm production dependencies, licenses, and source
  commit identifiers.
- [ ] **SC-B3.4** Verify SBOMs correspond to the final pushed digest.
- [ ] **SC-B3.5** Attach SBOM attestations to OCI artifacts rather than publishing only mutable CI
  files.
- [ ] **SC-B3.6** Generate build provenance using GitHub OIDC and the selected SLSA-compatible
  mechanism.
- [ ] **SC-B3.7** Include repository, workflow, commit, builder identity, invocation, platforms, and
  output digests in provenance.
- [ ] **SC-B3.8** Prevent untrusted workflow inputs from altering release tags or artifact names.
- [ ] **SC-B3.9** Verify attestations in a separate job before chart publication.
- [ ] **SC-B3.10** Publish verification commands in installation and release documentation.

### B4 — Artifact signing and release flow

- [ ] **SC-B4.1** Sign API and web image digests with keyless Cosign or the approved equivalent.
- [ ] **SC-B4.2** Sign the OCI Helm chart and preserve linkage to the exact image digests it deploys.
- [ ] **SC-B4.3** Add release metadata containing image digests, chart digest, source commit,
  qualification result, SBOM references, and signature verification identities.
- [ ] **SC-B4.4** Build once and promote by digest; do not independently rebuild the same release
  version in separate jobs.
- [ ] **SC-B4.5** Make chart publication depend on successful tests, scans, attestations, signatures,
  and required qualification.
- [ ] **SC-B4.6** Prevent overwriting existing immutable semantic-version artifacts.
- [ ] **SC-B4.7** Add a release-candidate channel if qualification needs to operate on final bits
  before promotion.
- [ ] **SC-B4.8** Verify both amd64 and arm64 manifests and their per-platform SBOM/provenance.
- [ ] **SC-B4.9** Test public verification from an unauthenticated clean environment.
- [ ] **SC-B4.10** Add compromise/revocation instructions for a bad artifact or workflow identity.

### B5 — Runtime verification and chart UX

- [ ] **SC-B5.1** Add chart values for API/web digest pinning without breaking existing tag users.
- [ ] **SC-B5.2** Document tag mode as convenience and digest mode as the production recommendation.
- [ ] **SC-B5.3** Surface running image IDs/digests in status/preflight without leaking registry
  credentials.
- [ ] **SC-B5.4** Warn when benchmark execution uses a mutable tag in a production profile.
- [ ] **SC-B5.5** Provide Kyverno and/or Gatekeeper examples verifying approved Highland signatures
  and helper image digests.
- [ ] **SC-B5.6** Add chart tests for tag-only, digest-only, tag-plus-digest, invalid digest, and
  private-registry pull-secret scenarios.
- [ ] **SC-B5.7** Verify upgrades preserve explicit customer digest pins.
- [ ] **SC-B5.8** Verify rollback uses known prior digests rather than moving tags.

#### Workstream B completion gate

- [ ] Every shipped or launched image is represented in the artifact inventory.
- [ ] Release artifacts have verified SBOMs, provenance, and signatures.
- [ ] Critical vulnerability and secret gates run before publication.
- [ ] The default `fio` image is immutable and reviewed.
- [ ] A clean machine can verify and install a release entirely by immutable identifiers.

## 8. Workstream C — Executable compatibility and certification matrix

### C0 — Schema and claim semantics

- [ ] **QA-C0.1** Define JSON Schema validation for `docs/compatibility.yaml`.
- [ ] **QA-C0.2** Define stable meanings for `detected`, `contract-tested`, `live-tested`, `managed`,
  `preview`, `production`, `required`, and `release-gate`.
- [ ] **QA-C0.3** Remove or migrate ambiguous claim terminology.
- [ ] **QA-C0.4** Define evidence freshness windows by gate type.
- [ ] **QA-C0.5** Define whether a result for an ancestor commit may certify a descendant that changed
  only documentation, and make the rule machine-checkable.
- [ ] **QA-C0.6** Define qualification invalidation paths based on changed files/components.
- [ ] **QA-C0.7** Define accepted retry policy and distinguish environmental flakes from product
  failures.
- [ ] **QA-C0.8** Require cleanup success as part of a passing live profile.
- [ ] **QA-C0.9** Define explicit `not-run`, `skipped`, `blocked`, `failed`, `flaky`, and `passed`
  states.
- [ ] **QA-C0.10** Ensure a conditional/skipped workflow can never be interpreted as a passing gate.

### C1 — Profile manifest and runner

- [ ] **QA-C1.1** Add `qualification/profiles.yaml` linked to compatibility provider/version rows.
- [ ] **QA-C1.2** Add profile and result schemas.
- [ ] **QA-C1.3** Implement a runner that selects profiles by gate, provider, changed component, or
  explicit ID.
- [ ] **QA-C1.4** Produce a normalized result containing profile, source commit, artifact digests,
  environment versions, start/end timestamps, assertions, cleanup, and outcome.
- [ ] **QA-C1.5** Capture Kubernetes server version and provider-reported versions from the live
  environment rather than trusting only input variables.
- [ ] **QA-C1.6** Reject results when observed versions do not match the selected profile.
- [ ] **QA-C1.7** Redact credentials, kubeconfigs, tokens, Secrets, and sensitive provider output.
- [ ] **QA-C1.8** Add deterministic run IDs and evidence directories.
- [ ] **QA-C1.9** Add timeouts per setup, test, cleanup, and total profile.
- [ ] **QA-C1.10** Always attempt cleanup in a protected finalization step.
- [ ] **QA-C1.11** Mark cleanup failure as a failed gate and emit a recovery instruction.
- [ ] **QA-C1.12** Upload concise evidence plus full logs with appropriate access and retention.

### C2 — Fast pull-request profiles

- [ ] **QA-C2.1** Keep Go provider/storage contracts, race-sensitive focused tests, frontend unit
  tests, and Playwright mock-manager tests on pull requests.
- [ ] **QA-C2.2** Add a lightweight kind profile for provider-neutral CSI API discovery and graceful
  absence of optional APIs.
- [ ] **QA-C2.3** Exercise CRD installation, server-side schema validation, RBAC, NetworkPolicy render,
  readiness, and API/web startup in the kind profile.
- [ ] **QA-C2.4** Test with snapshot CRDs present and absent.
- [ ] **QA-C2.5** Test cluster and namespace-allowlist storage scopes.
- [ ] **QA-C2.6** Add disabled-provider and mixed-provider startup matrix cases.
- [ ] **QA-C2.7** Run generated-code and OpenAPI contract drift checks.
- [ ] **QA-C2.8** Use path-based selection only to add focused jobs; retain a minimum smoke suite for
  every change.
- [ ] **QA-C2.9** Publish a PR summary mapping executed profiles to compatibility claims.

### C3 — Scheduled provider profiles

- [ ] **QA-C3.1** Convert current/previous Rook profiles into manifest-driven jobs.
- [ ] **QA-C3.2** Add a supported Longhorn current profile and previous-minor profile.
- [ ] **QA-C3.3** Add the documented OpenEBS HostPath live profile.
- [ ] **QA-C3.4** Add an OpenEBS Mayastor discovery/three-node profile when infrastructure is
  available.
- [ ] **QA-C3.5** Add the documented LINSTOR single-node file-thin profile.
- [ ] **QA-C3.6** Add the LINSTOR three-node DRBD profile before production promotion.
- [ ] **QA-C3.7** Add generic unknown-CSI discovery using a small disposable CSI implementation.
- [ ] **QA-C3.8** Run current and previous supported Kubernetes minors where claimed.
- [ ] **QA-C3.9** Add amd64 as required and arm64 qualification where releases publish arm64 images.
- [ ] **QA-C3.10** Test provider unavailable, degraded, slow, unauthorized, version-unknown, and CRD
  partially installed states.
- [ ] **QA-C3.11** Test API rolling upgrade and rollback against each release-gated provider profile.
- [ ] **QA-C3.12** Track duration and flake rate per profile.

### C4 — Required assertion catalogue

Every managed-provider profile must select applicable assertions from this catalogue:

- [ ] **QA-C4.1** Highland deploys using the candidate chart and immutable candidate image digests.
- [ ] **QA-C4.2** API and web become ready within the profile objective.
- [ ] **QA-C4.3** No unexpected restarts, panics, fatal logs, or sustained readiness flapping occur.
- [ ] **QA-C4.4** Provider identity, version, drivers, namespace, support level, capabilities, and
  health are accurate.
- [ ] **QA-C4.5** Inventory correlation covers StorageClass, PVC, PV, workload, attachment, snapshot,
  topology, and provider resource where supported.
- [ ] **QA-C4.6** Pagination, filtering, conditional requests, SSE invalidation, and stale-cache
  behavior work at live scale.
- [ ] **QA-C4.7** Viewer/operator/admin authorization matches policy.
- [ ] **QA-C4.8** Disabled write ceilings prevent operation creation.
- [ ] **QA-C4.9** Enabled disposable workflows generate plans, confirmation, operation records, audit
  events, and final provider state.
- [ ] **QA-C4.10** Destructive provider gates remain disabled unless the specific destructive profile
  is selected.
- [ ] **QA-C4.11** Provider outage degrades only the appropriate surface and does not incorrectly
  remove unrelated providers.
- [ ] **QA-C4.12** Upgrade preserves identities, policy, operations, audit configuration, and provider
  navigation.
- [ ] **QA-C4.13** Rollback remains operational and does not corrupt newer durable objects.
- [ ] **QA-C4.14** All disposable PVCs, snapshots, pools, classes, Jobs, Secrets, and test namespaces
  are removed or deliberately retained with an incident record.

### C5 — Release enforcement and evidence publication

- [ ] **QA-C5.1** Add a qualification aggregation job that validates result signatures/schema,
  commit ancestry, artifact digests, freshness, and required profile coverage.
- [ ] **QA-C5.2** Block preview release publication on missing/failed preview release gates.
- [ ] **QA-C5.3** Block production promotion on every required production profile.
- [ ] **QA-C5.4** Allow an emergency override only through a documented, audited, expiring exception.
- [ ] **QA-C5.5** Publish a release qualification summary alongside changelog/release artifacts.
- [ ] **QA-C5.6** Link each compatibility row to its latest public evidence where safe.
- [ ] **QA-C5.7** Display unqualified or stale profiles honestly rather than retaining an old tested
  claim.
- [ ] **QA-C5.8** Add a scheduled issue/notification when evidence becomes stale.
- [ ] **QA-C5.9** Preserve historical results for supported release lines.
- [ ] **QA-C5.10** Verify evidence contains no reusable credentials or sensitive cluster data.

#### Workstream C completion gate

- [ ] Every compatibility claim maps to one or more profile IDs.
- [ ] Every required profile has fresh evidence for the release artifacts.
- [ ] Skipped live infrastructure is visible as not-run and cannot satisfy a gate.
- [ ] OpenEBS, LINSTOR, Longhorn, Rook/Ceph, and generic CSI have appropriate scheduled coverage.
- [ ] Release qualification is reproducible and published without secrets.

## 9. Workstream D — OpenAPI as executable source of truth

### D0 — Contract governance

- [ ] **API-D0.1** Write an ADR choosing the canonical OpenAPI layout and code-generation tools.
- [ ] **API-D0.2** Pin generator versions and checksums.
- [ ] **API-D0.3** Define the public API boundary, including `/auth`, `/api/v1`, health/readiness,
  metrics exclusions, SSE, downloads, and Ceph dashboard handoff behavior.
- [ ] **API-D0.4** Define versioning and deprecation rules for paths, fields, enums, error codes, and
  semantics.
- [ ] **API-D0.5** Define what constitutes a breaking change.
- [ ] **API-D0.6** Define nullable versus optional semantics and prohibit ambiguous schemas.
- [ ] **API-D0.7** Define common timestamp, quantity, identifier, pagination, ETag, correlation ID,
  idempotency, confirmation, and error-envelope components.
- [ ] **API-D0.8** Decide how Longhorn compatibility proxy endpoints are documented without claiming
  Highland ownership of the upstream schema.
- [ ] **API-D0.9** Define schema review ownership and required reviewers for security-sensitive
  mutations.

### D1 — Complete route inventory and schema coverage

- [ ] **API-D1.1** Generate a route inventory from Chi registration and compare it with OpenAPI paths.
- [ ] **API-D1.2** Add auth provider, login, MFA challenge, logout, OIDC start/callback, and current
  user contracts.
- [ ] **API-D1.3** Add local user administration and account self-service contracts.
- [ ] **API-D1.4** Add security policy and OIDC runtime configuration contracts with secret write-only
  fields correctly represented.
- [ ] **API-D1.5** Add audit query and pagination contracts.
- [ ] **API-D1.6** Add compatibility, status, preflight, health narrative, dashboard, capacity, and
  metrics-summary contracts.
- [ ] **API-D1.7** Add benchmark create/list/get/delete and result contracts.
- [ ] **API-D1.8** Add backup credential setup contract without exposing Secret data.
- [ ] **API-D1.9** Preserve and modularize all existing storage/provider/action/operation/policy
  contracts.
- [ ] **API-D1.10** Document SSE event names, payload envelopes, reconnection, and cache invalidation
  semantics.
- [ ] **API-D1.11** Document binary/download endpoints with correct media types and size limits.
- [ ] **API-D1.12** Document standard 400, 401, 403, 404, 409, 412, 422, 429, 500, 502, and 503
  responses where applicable.
- [ ] **API-D1.13** Add examples for safe and failing mutation flows.
- [ ] **API-D1.14** Ensure every schema has bounded strings, arrays, maps, and object rules where the
  server enforces bounds.
- [ ] **API-D1.15** Ensure secret-bearing input fields never appear in response schemas/examples.

### D2 — Generated TypeScript client and DTOs

- [ ] **API-D2.1** Generate TypeScript wire DTOs into a clearly marked directory.
- [ ] **API-D2.2** Generate or wrap request functions using the existing same-origin credential model.
- [ ] **API-D2.3** Keep CSRF, bootstrap-response reuse, request ID extraction, and Highland/Longhorn
  URL normalization in one handwritten transport wrapper.
- [ ] **API-D2.4** Preserve React Query hooks as handwritten feature policy, consuming generated
  client methods and types.
- [ ] **API-D2.5** Replace manual storage DTOs incrementally, one endpoint family at a time.
- [ ] **API-D2.6** Replace unsafe response casts at migrated call sites.
- [ ] **API-D2.7** Model discriminated unions for MFA, operation states, provider support, and error
  variants accurately.
- [ ] **API-D2.8** Generate enum values without widening them to arbitrary strings unless forward
  compatibility explicitly requires it.
- [ ] **API-D2.9** Add adapters between generated wire types and UI-specific view models where useful.
- [ ] **API-D2.10** Add transport tests for CSRF, empty responses, non-JSON responses, request IDs,
  cancellation, and error envelopes.
- [ ] **API-D2.11** Prevent generated files from being linted/formatted inconsistently with their
  generator.
- [ ] **API-D2.12** Delete superseded manual DTOs only after call-site migration is complete.

### D3 — Go wire types and server conformance

- [ ] **API-D3.1** Generate Go request/response wire types or an equivalent typed server interface.
- [ ] **API-D3.2** Keep domain models separate where generated wire concerns would pollute domain
  logic.
- [ ] **API-D3.3** Add explicit mapping functions with tests for domain-to-wire and wire-to-domain
  conversions.
- [ ] **API-D3.4** Migrate read-only endpoint families before privileged mutations.
- [ ] **API-D3.5** Migrate mutation requests only after confirmation headers and error codes are fully
  represented.
- [ ] **API-D3.6** Enforce request body size, strict JSON decoding, unknown-field policy, and content
  types consistently.
- [ ] **API-D3.7** Return the canonical error envelope from every covered handler.
- [ ] **API-D3.8** Preserve request/correlation IDs across generated middleware and handlers.
- [ ] **API-D3.9** Add response validation in tests and optional non-production diagnostics, not as an
  unbounded production hot-path cost.
- [ ] **API-D3.10** Ensure health/readiness remain dependency-light after integration.

### D4 — Contract CI gates

- [ ] **API-D4.1** Bundle and lint the modular OpenAPI document in CI.
- [ ] **API-D4.2** Validate examples against schemas.
- [ ] **API-D4.3** Verify every public route/method is documented or explicitly allowlisted as
  internal/proxy infrastructure.
- [ ] **API-D4.4** Verify documented operation IDs are unique and stable.
- [ ] **API-D4.5** Regenerate Go and TypeScript artifacts and fail on a dirty diff.
- [ ] **API-D4.6** Run a breaking-change comparison against the target branch and latest supported
  release contract.
- [ ] **API-D4.7** Require an explicit version/deprecation decision for accepted breaking changes.
- [ ] **API-D4.8** Run server response conformance tests for representative success and error paths.
- [ ] **API-D4.9** Run generated TypeScript compile tests and a small consumer fixture.
- [ ] **API-D4.10** Publish the bundled OpenAPI document as a release artifact and serve it through
  documentation tooling if desired.

### D5 — Contract migration and documentation

- [ ] **API-D5.1** Migrate common error and pagination primitives first.
- [ ] **API-D5.2** Migrate provider-neutral read APIs.
- [ ] **API-D5.3** Migrate provider-specific read APIs.
- [ ] **API-D5.4** Migrate identity/account/admin APIs.
- [ ] **API-D5.5** Migrate benchmark and backup setup APIs.
- [ ] **API-D5.6** Migrate storage operations and policy mutation APIs last.
- [ ] **API-D5.7** Maintain a temporary endpoint/type migration ledger.
- [ ] **API-D5.8** Document client generation and consumption for external automation users.
- [ ] **API-D5.9** Add changelog notes for deprecated or clarified semantics.
- [ ] **API-D5.10** Remove the old partial contract entry point or redirect it to the canonical bundle
  only after all consumers migrate.

#### Workstream D completion gate

- [ ] Every supported public route is present in the canonical contract.
- [ ] Go and TypeScript generated artifacts are deterministic and drift-gated.
- [ ] Representative runtime responses conform to the schema.
- [ ] Breaking API changes cannot merge without explicit version/deprecation handling.
- [ ] The web app no longer maintains duplicate wire DTOs for migrated APIs.

## 10. Workstream E — Modularization and risk-based quality gates

### E0 — Boundaries and baseline characterization

- [ ] **ENG-E0.1** Record current package dependencies and identify cycles/undesired directions.
- [ ] **ENG-E0.2** Record current line counts, test counts, coverage by package, build time, test time,
  bundle sizes, and key endpoint latency.
- [ ] **ENG-E0.3** Add characterization tests around bootstrap combinations before extracting code.
- [ ] **ENG-E0.4** Add characterization tests for every operation action family before splitting the
  planner.
- [ ] **ENG-E0.5** Add inventory golden/contract tests before splitting discovery and normalization.
- [ ] **ENG-E0.6** Add UI behavior tests around large pages before component extraction.
- [ ] **ENG-E0.7** Define desired dependency direction: transport -> application -> domain -> ports,
  with infrastructure implementing ports.
- [ ] **ENG-E0.8** Define code-size review triggers as signals, not rigid correctness rules.
- [ ] **ENG-E0.9** Prohibit refactor phases from silently changing API or authorization semantics.

### E1 — Testable application bootstrap

- [ ] **ENG-E1.1** Introduce `internal/app` lifecycle and dependency types.
- [ ] **ENG-E1.2** Move config validation out of environment parsing where necessary so config structs
  can be tested directly.
- [ ] **ENG-E1.3** Introduce provider factories accepting explicit configs and dependencies.
- [ ] **ENG-E1.4** Inject Kubernetes clients, dynamic/discovery clients, HTTP clients, clock, random
  source, audit sink, limiter, session backend, and logger.
- [ ] **ENG-E1.5** Return typed startup errors identifying configuration, required dependency,
  optional provider, or transient initialization failures.
- [ ] **ENG-E1.6** Keep optional provider failures isolated according to documented readiness policy.
- [ ] **ENG-E1.7** Centralize lifecycle start/stop ordering.
- [ ] **ENG-E1.8** Ensure partially constructed applications close all started goroutines and clients.
- [ ] **ENG-E1.9** Move `os.Exit` decisions exclusively into `main`.
- [ ] **ENG-E1.10** Test shutdown with SSE clients, provider watchers, Redis, audit sink, leader
  election, and HTTP requests in flight.
- [ ] **ENG-E1.11** Add bootstrap matrix tests for storage on/off and each managed provider on/off.
- [ ] **ENG-E1.12** Add required/optional dependency failure tests.
- [ ] **ENG-E1.13** Add a process-level smoke that exercises real signal shutdown and exit codes.

### E2 — Operation planner decomposition

- [ ] **ENG-E2.1** Extract shared plan primitives: target validation, parameter decoding, provider
  attribution, authorization, namespace scope, dependency capture, dry-run, impact, confirmation,
  hash, and error construction.
- [ ] **ENG-E2.2** Define an action-planner interface keyed by stable action ID.
- [ ] **ENG-E2.3** Extract PVC create, expand, delete, clone, and restore planners.
- [ ] **ENG-E2.4** Extract snapshot create/delete planners.
- [ ] **ENG-E2.5** Extract Longhorn-native planners by bounded action family.
- [ ] **ENG-E2.6** Extract Rook/Ceph pool and StorageClass planners.
- [ ] **ENG-E2.7** Preserve one authoritative authorization/preflight pipeline around action-specific
  logic.
- [ ] **ENG-E2.8** Prevent action planners from bypassing provider resolution or policy checks.
- [ ] **ENG-E2.9** Keep action definitions, plan generation, execution, and inspection mappings
  exhaustively checked.
- [ ] **ENG-E2.10** Add a test failing when an action is registered without planner/executor/
  inspector/audit metadata.
- [ ] **ENG-E2.11** Preserve deterministic plan hashes across a behavior-preserving extraction.
- [ ] **ENG-E2.12** Add golden compatibility fixtures for old/new plan equivalence.
- [ ] **ENG-E2.13** Fuzz parameter decoding, target identifiers, quantity values, and confirmation
  bindings.

### E3 — Inventory and context decomposition

- [ ] **ENG-E3.1** Separate Kubernetes discovery from informer lifecycle.
- [ ] **ENG-E3.2** Separate raw-object observation from normalized storage DTO construction.
- [ ] **ENG-E3.3** Separate indexing/correlation from HTTP filtering and pagination.
- [ ] **ENG-E3.4** Define immutable cache snapshots or clear locking ownership.
- [ ] **ENG-E3.5** Preserve bounded memory and last-sync/staleness evidence.
- [ ] **ENG-E3.6** Extract relationships, impact, drift, timeline, capacity, comparison, and remediation
  services behind explicit inputs.
- [ ] **ENG-E3.7** Avoid provider network calls while holding inventory locks.
- [ ] **ENG-E3.8** Add scale tests for 10k and a documented higher stress tier.
- [ ] **ENG-E3.9** Add watch reconnect, relist, optional-CRD add/remove, and partial-sync tests.
- [ ] **ENG-E3.10** Measure allocations and latency for common list/context queries before and after
  extraction.
- [ ] **ENG-E3.11** Preserve stable IDs, continuation tokens, attribution, freshness, and confidence
  semantics.

### E4 — Provider adapter structure

- [ ] **ENG-E4.1** Define shared conventions for discovery, normalization, health, capabilities,
  context resources, and optional runtime clients.
- [ ] **ENG-E4.2** Split each large adapter into bounded files/packages by concern without forcing a
  lowest-common-denominator abstraction.
- [ ] **ENG-E4.3** Keep provider-specific CRD schemas and handle parsing isolated.
- [ ] **ENG-E4.4** Centralize timeout, TLS, user-agent, response-size, and error-sanitization helpers
  for outbound provider clients.
- [ ] **ENG-E4.5** Preserve provider fixture contracts across extraction.
- [ ] **ENG-E4.6** Add compile-time interface assertions and capability consistency tests.
- [ ] **ENG-E4.7** Add negative fixtures for malformed, missing, newer, and partially populated CRDs.
- [ ] **ENG-E4.8** Avoid changing provider support claims solely because code was rearranged.

### E5 — Frontend feature decomposition

- [ ] **ENG-E5.1** Separate generated wire DTOs, feature query hooks, view models, and presentation.
- [ ] **ENG-E5.2** Extract large table column definitions into tested feature modules.
- [ ] **ENG-E5.3** Extract action dialogs/wizards from list pages while retaining one mutation policy
  path.
- [ ] **ENG-E5.4** Extract provider-neutral storage page shells from provider-specific panels.
- [ ] **ENG-E5.5** Keep URL/query state and provider selection explicit and testable.
- [ ] **ENG-E5.6** Use route-level lazy boundaries without adding avoidable request waterfalls.
- [ ] **ENG-E5.7** Preserve loading, empty, partial, stale, forbidden, unavailable, and error states.
- [ ] **ENG-E5.8** Preserve table keyboard behavior, focus management, accessible names, mobile
  layouts, CSV export, sorting, pagination, and bulk-action semantics.
- [ ] **ENG-E5.9** Add Storybook stories for extracted states rather than only happy paths.
- [ ] **ENG-E5.10** Add visual coverage for high-risk dialogs and provider/status variations.
- [ ] **ENG-E5.11** Track per-route chunk and initial bundle budgets after extraction.
- [ ] **ENG-E5.12** Remove dead components, types, hooks, and translations after migration using a
  pinned dead-code analysis tool.

### E6 — Risk-based coverage and test policy

- [ ] **ENG-E6.1** Classify packages/features into critical, high, standard, generated, and excluded
  coverage tiers.
- [ ] **ENG-E6.2** Put auth, middleware authorization/CSRF, audit, policy, operation planning/
  execution, identity persistence, and app bootstrap in the critical tier.
- [ ] **ENG-E6.3** Put provider clients, inventory correlation, readiness, and chart security/RBAC in
  the high tier.
- [ ] **ENG-E6.4** Exclude generated code from handwritten coverage targets while compiling/testing
  it separately.
- [ ] **ENG-E6.5** Establish initial thresholds from measured baselines rather than selecting
  aspirational numbers without tests.
- [ ] **ENG-E6.6** Ratchet changed-line and package thresholds upward; do not allow unexplained
  regressions.
- [ ] **ENG-E6.7** Suggested target after migration: at least 80% for critical packages, 70% for high
  packages, and no reduction in overall meaningful handwritten coverage. Final numbers require a
  baseline decision.
- [ ] **ENG-E6.8** Require branch/behavior tests for fail-closed paths, not just statement execution.
- [ ] **ENG-E6.9** Add race tests for shared-state packages on pull requests where runtime permits,
  with the broader matrix nightly.
- [ ] **ENG-E6.10** Add fuzz seed corpora for API decoding, policy, operation planning, provider
  identifiers, and durable audit decoding.
- [ ] **ENG-E6.11** Add mutation or fault-injection testing selectively for authorization and
  confirmation invariants if maintainable.
- [ ] **ENG-E6.12** Publish coverage by package/tier and changed lines without exposing repository
  tokens to untrusted reports.
- [ ] **ENG-E6.13** Treat flaky tests as defects with ownership, quarantine expiry, and visibility.

#### Workstream E completion gate

- [ ] Process construction is testable without `os.Exit` or real provider infrastructure.
- [ ] Critical planning and inventory responsibilities have explicit module boundaries.
- [ ] Large UI pages are decomposed without UX or performance regression.
- [ ] Coverage gates reflect risk and cannot be satisfied by generated code.
- [ ] Provider additions no longer require editing central monoliths for unrelated concerns.

## 11. Integrated delivery phases

Workstreams may execute in parallel only where their interfaces are stable. These phases define the
recommended merge order and program gates.

### Phase 0 — Baseline, ADRs, and program scaffolding

- [ ] Approve this plan and assign workstream owners/reviewers.
- [ ] Capture the baseline commands, coverage, bundle, chart renders, and live environment inventory.
- [ ] Complete the HA state inventory and audit backend ADR.
- [ ] Complete supply-chain policy and tool selection.
- [ ] Complete contract/code-generation ADR.
- [ ] Complete qualification schemas and claim terminology.
- [ ] Complete target package dependency rules and risk tiers.
- [ ] Add a validation-report template and evidence directory conventions.
- [ ] Record all accepted decisions in the decision ledger.

Phase 0 gate:

- [ ] No implementation-blocking architecture decision remains unresolved.
- [ ] Baselines are reproducible from the recorded commit.
- [ ] Owners and reviewers understand security-sensitive approval boundaries.

### Phase 1 — Fast, low-migration safety wins

- [ ] Pin the `fio` helper image and add digest-capable chart values.
- [ ] Add `govulncheck`, secret scanning, final-image scanning, and Helm security scanning.
- [ ] Pin GitHub Actions and CI tools.
- [ ] Add PDB/topology/rolling-strategy chart templates and render tests.
- [ ] Add compatibility/profile schemas and validate existing declarations.
- [ ] Add the public route inventory check.
- [ ] Add baseline package coverage reporting without failing thresholds yet.
- [ ] Add characterization tests for bootstrap, planner, inventory, and large UI flows.

Phase 1 gate:

- [ ] Existing installations render compatibly or receive actionable validation errors.
- [ ] No mutable `latest` helper image remains in defaults.
- [ ] New CI checks are stable and have documented exception behavior.

### Phase 2 — Interfaces before migrations

- [ ] Introduce `AuditSink` and convert current behavior into `MemoryAuditSink`.
- [ ] Introduce the backend-neutral login limiter.
- [ ] Introduce `internal/app` and move lifecycle construction behind injectable dependencies.
- [ ] Establish modular OpenAPI root and common schemas.
- [ ] Establish generator jobs and drift checks on a small read-only endpoint family.
- [ ] Establish qualification runner and normalized result format.
- [ ] Establish action-planner registration and coverage checks without moving all actions yet.

Phase 2 gate:

- [ ] Interfaces have contract tests and no silent behavior changes.
- [ ] Main can build/run through the new app layer.
- [ ] Generation is deterministic in local and CI environments.

### Phase 3 — Shared security state and durable audit

- [ ] Implement the selected durable audit backend.
- [ ] Implement JSONL import/dry-run/resume and backup/restore procedures.
- [ ] Add required pre-mutation audit admission behavior.
- [ ] Add shared Redis login throttling.
- [ ] Add backend health/status/metrics/alerts.
- [ ] Add multi-replica integration tests and failure injection.
- [ ] Add production HA example values and operational runbooks.

Phase 3 gate:

- [ ] Required audit failure paths are proven fail closed.
- [ ] Audit/list and limiter behavior are consistent across replicas.
- [ ] Migration from existing JSONL has been rehearsed with corrupted and duplicate inputs.

### Phase 4 — Complete API contract migration

- [ ] Cover every public route in the canonical OpenAPI document.
- [ ] Generate complete TypeScript wire DTOs/client surface.
- [ ] Generate Go wire types/server conformance surface.
- [ ] Migrate endpoint families in the sequence from section D5.
- [ ] Add breaking-diff and route-coverage gates.
- [ ] Publish the bundled contract as a release artifact.

Phase 4 gate:

- [ ] Route inventory and OpenAPI match.
- [ ] Generated artifacts are clean after regeneration.
- [ ] Web and server conformance suites pass without duplicate manual wire types for migrated routes.

### Phase 5 — Provider and domain modularization

- [ ] Complete planner extraction with deterministic plan equivalence.
- [ ] Complete inventory/context service extraction with scale equivalence.
- [ ] Restructure provider adapters by bounded concern.
- [ ] Decompose the largest frontend pages and API hooks.
- [ ] Remove superseded compatibility layers and dead code.
- [ ] Activate agreed risk-tier coverage thresholds.

Phase 5 gate:

- [ ] Authorization, confirmation, plan hashes, IDs, API payloads, and UX workflows remain compatible.
- [ ] Critical-tier thresholds pass.
- [ ] Latency, allocations, bundle sizes, and accessibility do not materially regress.

### Phase 6 — Full live certification matrix

- [ ] Run generic CSI and chart profiles on hosted/disposable infrastructure.
- [ ] Run Longhorn current/previous profiles.
- [ ] Run Rook current/previous profiles.
- [ ] Run OpenEBS required profiles.
- [ ] Run LINSTOR required profiles.
- [ ] Run HA disruption and backend outage profiles.
- [ ] Run upgrade and rollback from the last supported release.
- [ ] Publish signed/validated result summaries.

Phase 6 gate:

- [ ] Every release-gated compatibility row has fresh passing evidence.
- [ ] Cleanup succeeds and no untracked disposable infrastructure remains.
- [ ] Candidate artifact digests exactly match the artifacts tested.

### Phase 7 — Signed release and post-release observation

- [ ] Build candidate artifacts once.
- [ ] Generate/attach SBOMs and provenance.
- [ ] Scan and sign candidate digests.
- [ ] Qualify those exact digests.
- [ ] Promote/publish the chart and images without rebuilding.
- [ ] Publish verification instructions and qualification summary.
- [ ] Monitor audit, auth, operation leader, provider, and error metrics through the defined soak
  period.
- [ ] Exercise documented rollback in a non-production environment using published prior digests.
- [ ] Close or schedule every accepted exception with an expiry.

Phase 7 gate:

- [ ] Public artifacts verify successfully from a clean environment.
- [ ] No critical unresolved production-readiness alert remains.
- [ ] The release evidence ledger is complete and immutable enough for independent review.

## 12. Detailed test strategy

### 12.1 Unit tests

#### Audit

- [ ] append success and typed failure;
- [ ] deterministic event validation and redaction;
- [ ] pagination/cursor stability;
- [ ] duplicate event ID handling;
- [ ] retention boundary behavior;
- [ ] required versus best-effort event policy;
- [ ] retry classification and backoff;
- [ ] malformed JSONL import and quarantine;
- [ ] idempotent resumable import;
- [ ] close/flush behavior;
- [ ] integrity-chain verification if adopted.

#### Rate limiting and sessions

- [ ] IP and `user@IP` threshold behavior;
- [ ] IPv4/IPv6 normalization;
- [ ] atomic dual-key updates;
- [ ] window expiry and backoff ceiling;
- [ ] success reset;
- [ ] replica-alternating attempts;
- [ ] Redis timeout/unavailability policy;
- [ ] cluster/installation key isolation;
- [ ] no sensitive key material;
- [ ] local identity session-version revocation.

#### App bootstrap

- [ ] every provider enable/disable combination;
- [ ] required versus optional provider initialization failure;
- [ ] Kubernetes unavailable with storage enabled/disabled;
- [ ] Redis configured/unconfigured/unavailable;
- [ ] audit sink configured/unconfigured/unavailable;
- [ ] OIDC initialization failure with allowed local recovery;
- [ ] lifecycle cleanup after partial construction;
- [ ] signal-driven graceful shutdown;
- [ ] stable exit/error classification.

#### Planner and operations

- [ ] action registration exhaustiveness;
- [ ] provider attribution and scope;
- [ ] role and write-ceiling enforcement;
- [ ] dry-run and impact requirements;
- [ ] plan hash equivalence before/after extraction;
- [ ] stale resource/policy/confirmation rejection;
- [ ] failover observation before mutation retry;
- [ ] audit admission failure;
- [ ] post-mutation audit delivery recovery;
- [ ] terminal operation retention under audit outage.

#### Inventory and providers

- [ ] optional API discovery transitions;
- [ ] informer sync/reconnect/relist;
- [ ] stable normalized IDs and pagination;
- [ ] malformed/partial/newer CRDs;
- [ ] stale provider runtime data;
- [ ] provider timeout and response-size limits;
- [ ] relationship/impact/drift/capacity equivalence;
- [ ] 10k inventory and higher stress fixture.

#### OpenAPI and mapping

- [ ] every operation example validates;
- [ ] domain/wire mapping round trips;
- [ ] nullable/optional distinctions;
- [ ] enum and error-code coverage;
- [ ] unknown-field policy;
- [ ] quantity and large-integer preservation;
- [ ] pagination and timestamp formats;
- [ ] secret fields are write-only/absent in responses.

#### Frontend

- [ ] generated transport and canonical errors;
- [ ] CSRF on all unsafe methods;
- [ ] request cancellation and stale response behavior;
- [ ] loading/empty/partial/stale/forbidden/unavailable/error states;
- [ ] extracted table sorting/filtering/pagination/export;
- [ ] action-dialog confirmation and focus behavior;
- [ ] provider navigation and route state;
- [ ] responsive and theme variants;
- [ ] translation-key parity after extraction.

### 12.2 Integration tests

- [ ] Two API instances behind a round-robin proxy using one identity store, audit backend, Redis,
  and fake Kubernetes API.
- [ ] Concurrent audit appends and equivalent cross-replica queries.
- [ ] Concurrent identity updates with optimistic conflict retry.
- [ ] Alternating failed logins across replicas reaching one lockout threshold.
- [ ] Storage operation creation on one replica and reconciliation by another elected replica.
- [ ] Audit backend unavailable before operation submission.
- [ ] Audit backend unavailable after provider mutation but before terminal event delivery.
- [ ] Redis unavailable under each configured limiter failure policy.
- [ ] OpenAPI response validation for every endpoint family.
- [ ] Generated TypeScript client against the Go integration server.
- [ ] Upgrade schema compatibility using persisted identity, policy, operations, and audit fixtures.
- [ ] Chart deployment with 1/2/3 replicas and production security settings.

### 12.3 Concurrency, fuzz, and fault injection

- [ ] Run `go test -race` for auth, audit, app lifecycle, policy, storage operations, inventory,
  watch, and provider clients.
- [ ] Fuzz audit decoding/import, API JSON decoding, identifiers, pagination tokens, quantities,
  operation parameters, plan confirmation, provider handles, and policy documents.
- [ ] Inject network timeouts, connection resets, slow responses, partial writes, disk full,
  permission denied, Kubernetes conflicts, Lease loss, watch closure, and process termination.
- [ ] Verify bounded retry and no goroutine/resource leaks.
- [ ] Verify no duplicate external mutation after leader loss at every meaningful execution boundary.

### 12.4 Chart tests

Render and validate at least:

- [ ] default install;
- [ ] one API/web replica;
- [ ] two API/web replicas;
- [ ] production HA profile;
- [ ] Redis sessions only;
- [ ] Redis limiter only if separable;
- [ ] durable audit enabled;
- [ ] legacy audit values migration;
- [ ] PDB disabled/enabled and invalid replica combinations;
- [ ] default and strict topology spread;
- [ ] tag-only and digest-pinned API/web/helper images;
- [ ] malformed digest rejection;
- [ ] every provider individually and all providers together;
- [ ] storage writes/admin policy combinations;
- [ ] namespace-allowlist scope;
- [ ] ServiceMonitor/PrometheusRule/Grafana enabled;
- [ ] ingress TLS and NetworkPolicy;
- [ ] embedded Longhorn enabled/disabled;
- [ ] server-side CRD dry-run against supported Kubernetes versions.

### 12.5 Browser and accessibility tests

- [ ] Login throttling messaging and recovery without exposing account existence.
- [ ] Session revocation across replica-routed requests.
- [ ] Audit pagination/filtering during concurrent events.
- [ ] Durable audit degraded-state/admin mutation blocking UX.
- [ ] Provider qualification/status evidence presentation if exposed in UI.
- [ ] Generated-client errors preserve actionable messages and request IDs.
- [ ] All refactored routes at desktop/mobile widths and light/dark/system themes.
- [ ] Keyboard-only navigation, focus trapping/restoration, accessible names, live regions, and Axe.
- [ ] No console errors, failed unexpected requests, horizontal overflow, or hydration/bootstrap races.
- [ ] Visual comparison for dashboards, storage pages, operation dialogs, account/admin, and status.

### 12.6 Performance tests

- [ ] Preserve initial gzip budget or approve an evidence-based change.
- [ ] Set route-chunk budgets for large storage/admin/provider pages.
- [ ] Measure cold authenticated navigation and warm route switching.
- [ ] Measure audit query latency at representative retention volumes.
- [ ] Measure limiter latency and behavior under burst load.
- [ ] Measure API bootstrap/readiness for 1k, 10k, and stress-tier storage objects.
- [ ] Measure context graph, impact, timeline, and provider summary latency.
- [ ] Measure memory per API replica and informer/cache duplication implications.
- [ ] Measure graceful shutdown and operation-leader failover duration.
- [ ] Record p50/p95/p99 where meaningful and establish regression thresholds.

### 12.7 Security tests

- [ ] Attempt audit bypass through backend outage, malformed event fields, oversized values, and
  caller cancellation.
- [ ] Attempt login-limit bypass through replica hopping, IP/header spoofing, username casing, IPv6
  rotation, and Redis key collisions.
- [ ] Verify trusted-proxy behavior with direct, nginx, and ingress paths.
- [ ] Verify generated clients never log or serialize secret response fields.
- [ ] Verify OpenAPI examples and qualification evidence contain no credentials.
- [ ] Verify image signatures, provenance identities, SBOM attachment, and digest mismatch failure.
- [ ] Verify malicious/malformed helper image references are rejected.
- [ ] Verify chart security contexts, capabilities, root filesystem, service account, and RBAC remain
  least privilege.
- [ ] Re-run the storage and context threat-model abuse cases.

## 13. Live validation environments

### 13.1 Environment classes

| Class | Purpose | Minimum topology | Persistence |
|---|---|---|---|
| hosted kind | fast API/chart/CRD smoke | one worker sufficient | ephemeral |
| disposable k3s VM | realistic single-node/provider smoke | one node | ephemeral |
| HA k3s/kubeadm | disruption, spread, provider HA | three schedulable nodes | ephemeral or resettable |
| Rook lab | current/previous Rook/Ceph | three storage-capable nodes | resettable |
| OpenEBS lab | HostPath and Mayastor profiles | one and three nodes respectively | resettable |
| LINSTOR lab | file-thin and DRBD profiles | one and three nodes respectively | resettable |
| release verification host | signature/install verification | clean amd64 and arm64 where available | ephemeral |

### 13.2 Common environment requirements

- [ ] Isolated namespace/cluster identity per run.
- [ ] No production credentials or data.
- [ ] Exact candidate image digests.
- [ ] Time synchronization sufficient for signed tokens and evidence timestamps.
- [ ] Resource capacity checked before destructive/provider tests.
- [ ] Logs/metrics available for the run but scrubbed before publication.
- [ ] Cleanup script idempotent and safe when setup partially fails.
- [ ] Explicit ownership and recovery contact for self-hosted resources.
- [ ] Automatic expiration/cleanup for abandoned environments where possible.

### 13.3 HA live scenario

1. Install the candidate production HA values with two or three API/web replicas.
2. Confirm pods are spread when topology permits and PDBs are valid.
3. Confirm all replicas report the same version/config profile and become ready.
4. Alternate authenticated requests across replicas.
5. Generate audit events on multiple replicas and compare paginated results.
6. Alternate invalid logins and confirm one shared threshold.
7. Create a disposable safe storage operation.
8. Terminate the elected controller before, during, and after the external mutation boundary in
   separate runs.
9. Confirm exactly one logical mutation and eventual terminal state.
10. Drain a node and observe PDB, scheduling, readiness, SSE reconnection, and user-visible behavior.
11. Interrupt Redis and audit backends separately.
12. Verify documented failure behavior, alerts, recovery, and backlog convergence.
13. Upgrade, repeat a safe workflow, then roll back and verify durable state.
14. Remove all disposable resources and record evidence.

## 14. Migration strategy

### 14.1 Audit migration

Recommended staged sequence:

1. Ship the `AuditSink` abstraction with behavior-compatible memory/legacy adapters.
2. Ship the durable backend in opt-in mode and expose health without changing mutation admission.
3. Provide dry-run JSONL import and validation.
4. Rehearse backup, import, query equivalence, and rollback in a disposable environment.
5. Enable durable backend primary writes with legacy export/secondary behavior if approved.
6. Enable required-audit admission for privileged mutations.
7. Observe through a soak period.
8. Remove direct multi-writer JSONL support from the production HA profile while retaining export and
   explicit legacy read/import tooling.

Migration requirements:

- [ ] Existing JSONL is never modified in place.
- [ ] Import produces counts for read, accepted, duplicate, quarantined, and failed records.
- [ ] Import checkpoints are durable and resumable.
- [ ] Event IDs remain stable or retain a legacy ID mapping.
- [ ] Retention does not begin deleting migrated evidence until import verification completes.
- [ ] `StorageOperation` garbage collection remains fail closed throughout migration.
- [ ] Rollback retains both old and new evidence stores.

### 14.2 Rate limiter migration

- [ ] Default single-replica/local development can retain memory mode.
- [ ] HA profile selects shared mode explicitly or by validated profile defaults.
- [ ] Switching to shared mode starts with empty ephemeral counters unless a migration is explicitly
  required; document this temporary reset.
- [ ] Switching away from shared mode emits a security warning in HA deployments.
- [ ] Backend key prefixes include schema version for safe future migration.

### 14.3 API contract migration

- [ ] Preserve endpoint paths and JSON shapes during initial generation adoption.
- [ ] Introduce generated DTOs alongside manual DTOs temporarily.
- [ ] Migrate one endpoint family per coherent pull request.
- [ ] Add equivalence tests before removing old mappings.
- [ ] Deprecate rather than abruptly remove public fields.
- [ ] Preserve a bundled contract for every supported release line.

### 14.4 Code modularization migration

- [ ] Extract behavior without feature additions in the same commit where practical.
- [ ] Use characterization/golden tests to prove equivalence.
- [ ] Keep temporary forwarding wrappers short-lived and tracked in the migration ledger.
- [ ] Avoid package moves that make security review history unnecessarily opaque unless the benefit
  outweighs it.
- [ ] Delete old paths only after consumers and tests move.

## 15. Rollback strategy

### 15.1 General rollback rules

- Never use rollback to delete or downgrade authoritative CR data blindly.
- Never delete the new audit backend when reverting application code.
- Preserve old image and chart digests for the supported rollback window.
- Validate CRD stored/served version compatibility before rollback.
- Stop new privileged operations before a rollback that changes operation-controller code.
- Wait for or deliberately account for nonterminal operations.
- Export configuration and audit/backend health before rollback.
- Record the rollback as an audit/incident event in the available durable system.

### 15.2 Audit rollback

- [ ] Disable new mutation admission if the older release cannot safely write/read the selected
  audit backend.
- [ ] Retain the durable backend read-only rather than converting records backward destructively.
- [ ] Re-enable legacy sink only through explicit configuration and documented limitations.
- [ ] Keep `StorageOperation` retention disabled unless terminal durable evidence is proven.
- [ ] Provide a forward-recovery path that resumes pending audit deliveries.

### 15.3 Supply-chain rollback

- [ ] Roll back by known digest, never by assuming a mutable tag still identifies the old artifact.
- [ ] Verify the prior artifact signature/provenance before rollback.
- [ ] Document how admission policies temporarily allow the prior approved digest.
- [ ] If a signing identity is compromised, publish revocation/incident guidance and rotate workflow
  trust configuration.

### 15.4 Contract/code rollback

- [ ] Generated-code adoption must not require persisted data changes by itself.
- [ ] Retain wire compatibility so old web/new API and new web/old API combinations work during
  rolling deployment within the documented skew window.
- [ ] Test both skew directions for at least the current and previous supported release.
- [ ] Avoid rollback once a truly breaking API version is exposed without following the versioning
  policy.

## 16. Observability and operational readiness

### 16.1 Required metrics

- audit append attempts, success, failure, latency, retries, backlog, oldest pending age;
- audit query latency and result/error counts;
- limiter allow/deny/backend-error counts and backend latency without username/IP labels;
- identity sync success/failure/age;
- session backend type and health as bounded status, not credential-bearing labels;
- operation-controller leader state, handovers, reconcile latency, retries, and post-failover
  observations;
- qualification profile duration/outcome/flake counters in CI reporting, not application cardinality;
- provider/inventory cache sync age and object counts;
- API request metrics by normalized route/status;
- graceful shutdown duration and forced termination count.

### 16.2 Required alerts

- durable audit required but unavailable;
- audit backlog age exceeds objective;
- repeated audit delivery failure;
- shared limiter required but unavailable;
- identity sync stale or repeatedly conflicting;
- no operation controller leader;
- apparent multiple operation leaders;
- API ready replicas below desired availability;
- PDB blocks expected maintenance for an extended period;
- provider certification evidence stale before release;
- released artifact acquires a newly disclosed critical vulnerability.

### 16.3 Required runbooks

- durable audit unavailable;
- audit backlog/quarantine recovery;
- audit backup, restore, integrity check, and JSONL import;
- Redis limiter/session outage;
- identity Secret conflict/corruption/recovery;
- missing or stuck operation-controller leader;
- API rollout/readiness failure;
- PDB and topology scheduling failure;
- failed provider qualification and environment cleanup;
- vulnerable or compromised release artifact;
- signature/provenance verification failure;
- OpenAPI compatibility regression;
- safe rollback with nonterminal storage operations.

## 17. Documentation deliverables

- [ ] Update `README.md` production status and verified-artifact instructions.
- [ ] Update `docs/INSTALL.md` with single-replica, default, and production HA profiles.
- [ ] Add durable audit backend configuration, sizing, backup, restore, and migration sections.
- [ ] Add Redis shared-limiter guidance distinct from Redis sessions.
- [ ] Update storage RBAC and threat-model documents.
- [ ] Add supply-chain policy and artifact verification guide.
- [ ] Add qualification profile authoring and evidence interpretation guide.
- [ ] Update every provider guide with its automated/live qualification level.
- [ ] Add API contract generation and compatibility contributor guide.
- [ ] Publish the canonical OpenAPI bundle and external-client example.
- [ ] Add architecture diagrams for runtime state ownership and app composition.
- [ ] Update troubleshooting for replica inconsistency, audit outage, limiter outage, digest errors,
  and qualification failures.
- [ ] Update changelog and release notes with migrations, defaults, deprecations, and rollback caveats.
- [ ] Add validation reports under `docs/validation/` for each major workstream and the final release.

## 18. CI/CD target pipeline

The desired dependency graph is:

```text
source checks
  ├─ Go unit/vet/race/fuzz-smoke/coverage/security
  ├─ web type/lint/unit/build/storybook/a11y/bundle/security
  ├─ OpenAPI lint/examples/route coverage/generation/breaking diff
  ├─ Helm lint/render/schema/security/CRD dry-run
  └─ qualification manifest/schema/fast profiles
          ↓
candidate build once
  ├─ API amd64/arm64 images
  ├─ web amd64/arm64 images
  └─ OCI chart referencing candidate digests
          ↓
scan + SBOM + provenance + sign candidate digests
          ↓
live qualification of exact candidate digests
          ↓
qualification aggregation and release gate
          ↓
immutable promotion/publication without rebuild
          ↓
post-release scheduled vulnerability + qualification monitoring
```

Pipeline requirements:

- [ ] concurrency cancellation must not cancel final cleanup for self-hosted provider labs;
- [ ] untrusted pull requests receive no publish, signing, or lab credentials;
- [ ] artifacts passed between jobs are verified by digest/checksum;
- [ ] release jobs use minimal permissions and environment protection;
- [ ] OIDC token permissions exist only in jobs that create attestations/signatures;
- [ ] workflow logs avoid printing secrets and kubeconfigs;
- [ ] required checks have stable names suitable for branch protection;
- [ ] flaky live profiles cannot be silently rerun until green without preserving attempts;
- [ ] release promotion consumes a validated qualification summary.

## 19. Risk register

| Risk | Impact | Mitigation | Evidence required |
|---|---|---|---|
| audit backend adds operational burden | adoption friction/outage risk | support clear profiles, health, backup, sizing, optional dev sink | install and recovery rehearsal |
| fail-closed audit blocks urgent operations | operational delay | explicit break-glass policy with auditable external procedure; no silent bypass | outage tabletop/live exercise |
| dual-write migration diverges | incomplete/conflicting history | avoid if possible; define primary authority and reconciliation | mismatch injection test |
| Redis outage weakens login protection | brute-force exposure or login outage | explicit failure policy, HA Redis guidance, alerts | outage test |
| PDB/topology defaults make small clusters unschedulable | install/upgrade failure | soft defaults, replica-aware rendering, production strict profile | 1/2/3-node chart/live tests |
| digest pinning slows dependency updates | stale vulnerable components | automated digest update PRs and scheduled scans | update rehearsal |
| security scans create noisy gates | ignored findings or blocked releases | severity/reachability policy and expiring exceptions | exception audit |
| live labs are flaky or unavailable | false confidence or delayed releases | result states, retries with preserved attempts, multiple environment classes | flake-rate report |
| compatibility claims drift from profiles | misleading support promise | schema links and release aggregation | claim-to-profile check |
| generated types distort domain design | maintenance complexity | separate wire and domain models with mappings | code review + mapping tests |
| codegen creates large diffs | review fatigue | pinned deterministic tools, modular schemas, generated directories | clean regeneration |
| refactor changes plan hashes or authorization | rejected work or unsafe mutation | equivalence fixtures and security tests before extraction | old/new comparison |
| frontend extraction regresses UX | operator errors | characterization, visual, accessibility, and E2E coverage | browser evidence |
| higher coverage becomes vanity metric | effort without risk reduction | tiered behavior-focused gates, exclude generated code | package-tier report |
| arm64 behavior differs from amd64 | broken published platform | per-platform build, scan, smoke, and selected live qualification | architecture evidence |

## 20. Decision ledger

Record decisions here or link the resulting ADRs before checking them complete.

| ID | Decision | Status | Record |
|---|---|---|---|
| DEC-1 | Production durable audit backend | **accepted** | [ADR-0004](../adr/0004-production-durable-audit-and-ha-profiles.md) (PostgreSQL) |
| DEC-2 | When durable audit is mandatory | **accepted** | ADR-0004 (writes/admin production HA) |
| DEC-3 | Required-audit failure policy by action class | **accepted** | ADR-0004 |
| DEC-4 | Shared limiter backend outage policy | **accepted** | [ADR-0005](../adr/0005-shared-login-limiter-outage-policy.md) |
| DEC-5 | Production Redis topology/support boundary | **accepted** | ADR-0004 / ADR-0005 |
| DEC-6 | Default PDB and topology behavior | **accepted** | ADR-0004 |
| DEC-7 | SBOM format and scanners | **accepted** | [ADR-0006](../adr/0006-software-supply-chain-policy.md) |
| DEC-8 | Signing/provenance implementation | **accepted** | ADR-0006 (Cosign keyless + SLSA; env-gated) |
| DEC-9 | Vulnerability thresholds and exceptions | **accepted** | ADR-0006 + supply-chain-policy.md |
| DEC-10 | Compatibility evidence freshness/ancestry | **accepted** | [ADR-0007](../adr/0007-openapi-qualification-coverage.md) |
| DEC-11 | OpenAPI generator toolchain | **accepted** | ADR-0007 |
| DEC-12 | Public API/deprecation policy | **accepted** | ADR-0007 |
| DEC-13 | Coverage tier thresholds | **accepted** | ADR-0007 + coverage-tiers.md |
| DEC-14 | Maximum supported rolling-version skew | **accepted** | ADR-0007 (N / N-1) |

## 21. Traceability matrix

| Original finding | Primary tasks | Primary validation |
|---|---|---|
| multi-replica durability and HA | HA-A0 through HA-A5, Phases 1–3 | cross-replica audit/limiter, leader failover, node drain, backend outages |
| software supply chain | SC-B0 through SC-B5, Phases 1 and 7 | digest verification, scans, SBOM/provenance/signature verification, clean install |
| executable certification matrix | QA-C0 through QA-C5, Phase 6 | manifest/schema gates, live provider profiles, cleanup, release aggregation |
| OpenAPI source of truth | API-D0 through API-D5, Phase 4 | route coverage, regeneration, breaking diff, Go/TS conformance |
| module concentration and coverage | ENG-E0 through ENG-E6, Phases 2 and 5 | equivalence tests, risk-tier coverage, performance/a11y/browser regression |

## 22. Pull request and review strategy

Prefer small, coherent pull requests with explicit migration state. Suggested series:

1. baseline reports, ADRs, schemas, and CI scaffolding;
2. immutable helper image and digest chart support;
3. initial security scans and pinned Actions/tools;
4. PDB/topology/rollout chart controls;
5. audit interface with legacy behavior adapter;
6. durable audit backend and importer;
7. audit admission/delivery reconciliation;
8. limiter interface and Redis implementation;
9. app composition extraction and bootstrap tests;
10. modular OpenAPI root/common types and first generated read API;
11. remaining read API contract migrations;
12. identity/admin API contract migration;
13. mutation API contract migration and breaking-diff gate;
14. qualification manifest/runner and fast profile;
15. provider live profiles one provider family at a time;
16. planner extraction by action family;
17. inventory/context extraction;
18. provider adapter restructuring;
19. frontend feature decomposition by route;
20. SBOM/provenance/signing and build-once promotion;
21. final HA/live/release validation and documentation.

Every security-sensitive PR must include:

- threat/failure behavior summary;
- changed authority or trust boundaries;
- tests for positive, denied, unavailable, stale, replay, concurrent, and rollback paths as
  applicable;
- metrics/log/audit changes;
- migration and compatibility notes;
- generated and handwritten diff separation where practical; and
- exact validation commands/results.

## 23. Global definition of done

The program is complete only when all of the following are true:

- [ ] All workstream completion gates are checked.
- [ ] All architecture/security decisions are recorded in ADRs or this decision ledger.
- [ ] No required task is silently dropped; deferred work has an owner, reason, target, and impact.
- [ ] Default, development, and production HA profiles are documented and tested.
- [ ] Privileged mutation audit behavior is fail-closed and proven under outage.
- [ ] Cross-replica audit, identity, throttling, session, and operation behavior is validated.
- [ ] PDB, topology, rollout, readiness, and shutdown behavior is validated on a multi-node cluster.
- [ ] API/web/helper images and the chart use immutable release identities.
- [ ] Release images/chart have SBOMs, provenance, signatures, and passing security policy.
- [ ] Every compatibility claim maps to fresh qualification evidence.
- [ ] OpenEBS, LINSTOR, Longhorn, Rook/Ceph, and generic CSI required profiles pass or are honestly
  marked unsupported/not promoted.
- [ ] Every supported public route is represented in the canonical OpenAPI contract.
- [ ] Go and TypeScript generated artifacts are reproducible and drift-gated.
- [ ] Breaking API changes are policy-gated.
- [ ] Critical modules are decomposed with behavioral equivalence evidence.
- [ ] Risk-based coverage gates pass and generated code does not inflate them.
- [ ] Go test/vet/race/fuzz-smoke, frontend type/lint/unit/build/Storybook/Playwright/a11y/visual,
  OpenAPI, Helm, security, performance, and live qualification gates pass.
- [ ] Upgrade from the last supported release and rollback to it are validated with durable state.
- [ ] Audit backup/restore/import, Redis outage, leader loss, node drain, artifact compromise, and
  provider qualification runbooks are exercised.
- [ ] The exact artifacts tested are the artifacts published; no post-qualification rebuild occurs.
- [ ] Public verification works from a clean environment.
- [ ] Final source, validation report, qualification result, artifact digests, and release tag are
  traceable to one another.

## 24. Evidence ledger template

Create/update `docs/validation/production-readiness-and-engineering-scale.md` during execution with
the following evidence.

### 24.1 Source baseline

- commit and branch;
- dirty-worktree state;
- toolchain versions;
- Go package coverage and durations;
- frontend test/build/bundle results;
- chart render inventory;
- current compatibility/profile mapping.

### 24.2 Architecture and security

- ADR links;
- threat-model review;
- audit and limiter failure-policy decisions;
- API compatibility policy;
- supply-chain policy and exceptions.

### 24.3 Automated validation

- exact commands and exit results;
- coverage by risk tier;
- race/fuzz/fault-injection summaries;
- OpenAPI route/generation/breaking-diff result;
- Helm matrix and server-side CRD validation;
- scan reports and accepted exceptions;
- SBOM/provenance/signature verification.

### 24.4 Live validation

- profile IDs and result documents;
- observed Kubernetes/provider versions;
- candidate artifact digests;
- HA replica placement and PDB state;
- leader failover timing and exactly-once evidence;
- audit/Redis outage behavior;
- upgrade/rollback evidence;
- cleanup proof and remaining resources.

### 24.5 Release evidence

- source commit;
- pull request and review links;
- API/web per-platform and manifest digests;
- chart digest;
- SBOM/provenance/signature references;
- qualification aggregation result;
- release/tag link;
- clean-machine verification output;
- post-release soak result.

## 25. First implementation checkpoint

Before beginning broad refactoring, the first checkpoint should deliver a useful, independently
releasable safety increment:

- [ ] approved HA/audit, supply-chain, OpenAPI, and qualification ADRs;
- [ ] pinned `fio` image with digest-capable values;
- [ ] PDB/topology/rolling-strategy chart support;
- [ ] initial vulnerability, secret, image, and chart scanning;
- [ ] pinned GitHub Actions and downloaded CI tools;
- [ ] compatibility/profile schemas with current claims validated;
- [ ] generated route inventory with an explicit contract-coverage report;
- [ ] package/risk-tier coverage report in non-blocking mode;
- [ ] bootstrap/planner/inventory/UI characterization tests; and
- [ ] a completed validation report for this checkpoint.

That checkpoint reduces immediate production risk while establishing the evidence and interfaces
needed for the larger durable-state and modularization work.
