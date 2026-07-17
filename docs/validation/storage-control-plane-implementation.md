# Storage control-plane implementation evidence

- Branch: `feat/storage-control-plane`
- Plan: [`../plans/storage-control-plane-phases-1-5.md`](../plans/storage-control-plane-phases-1-5.md)
- Evidence date: 2026-07-16
- Release stage: preview; live qualification is mandatory before promotion

## Implemented surfaces

| Phase | Implementation evidence |
|---|---|
| Universal CSI core | reusable Kubernetes client factory; dynamic optional-API discovery; pinned upstream snapshot v1 contract types; scoped informer inventory; collision-resistant generic providers; exact quantity strings; correlation, conditions, filters, opaque pagination; scoped SSE v2; common API/UI; 10k warm-list and bounded-browser tests; cluster/namespace RBAC |
| Longhorn adapter | synthesized legacy config, optional startup, managed descriptor/version downgrade, existing proxy/stream/scraper/actions/watches retained, common enrichment, legacy SSE and routes retained, bolt-on/embedded chart matrix |
| Rook/Ceph read | served-CRD detection, version-tolerant readers and fixtures, desired/runtime state merge, cluster/MON/MGR/OSD/pool/filesystem health, typed allowlisted Dashboard GET client with TLS/auth/logout/limits/redirect blocking/retry/circuit/15-minute maximum stale cache, allowlisted Prometheus queries with partial results and query telemetry, conservative correlation, curated API/UI, opt-in RBAC/Secret mounts/egress, current/previous lab profiles |
| Durable workflows | machine-readable action registry; typed UI forms; feature/role/capability and supported Rook/Ceph-version intersection; strict parameter schemas; server dry-run; HMAC challenge and separate warning/typed-name confirmation; durable immutable StorageOperation; Kubernetes-enforced idempotency; leader election, restart/takeover replay, retry classification, recovery and retention; fresh preflight; SSA and UID/resourceVersion preconditions; fail-closed Ceph pool policy; durable audit correlation and fail-safe GC; metrics, alerts, dashboard, and split write RBAC |
| Context and native handoff | HTTPS-only Ceph Dashboard browser handoff with explicit disposable-lab override; exact canonical relationship graph with provenance/confidence/freshness; Rook desired/runtime drift with generated CephFS/internal-pool handling; confirmed/potential/unknown impact; shared destructive preflight dependency engine; provider-attributed timeline; separate capacity measures and honest forecast unavailability; explicit provider comparison without scores; read-only remediation surfaces; provider/resource context UI |

## Work-package audit

`Implemented` means the repository surface and its automated evidence exist. `Release pending` means
the implementation is present but the plan explicitly requires a real storage cluster, destructive
scratch environment, independent reviewer, or external scan before promotion. A pending release
gate is never inferred from fixtures.

| Work package | Disposition | Evidence or remaining gate |
|---|---|---|
| P1.1 contracts and compatibility | Implemented | ADR, product contract, capability matrix, pinned compatibility YAML, OpenAPI, scope/partial-data docs |
| P1.2 Kubernetes client/discovery | Implemented | shared typed/dynamic/discovery client, bounded QPS/burst/timeout/user-agent, five-minute optional-API rediscovery, pinned snapshot v1 types |
| P1.3 informer inventory | Implemented; release pending | scoped core/storage/dynamic informers, indexes, sync/error telemetry and periodic resync; API-server watch fault qualification remains live |
| P1.4 registry/generic provider | Implemented | deterministic IDs, driver claims, collision failure, one detected provider per unclaimed driver |
| P1.5 inventory/correlation | Implemented | exact quantities, PVC/PV/workload/attachment/snapshot/capacity/event correlation and bounded pagination tests |
| P1.6 live invalidation | Implemented | scoped SSE v2, legacy Longhorn frames, coalescing, overflow-to-resync and reconnect tests |
| P1.7 common read API | Implemented | authenticated GET routes, validation, request IDs, error envelope, partial conditions, OpenAPI route conformance |
| P1.8 common UI | Implemented | typed DTOs/query keys, provider/common pages, URL filters, drill-down, partial/stale/permission states and bounded 10k browser fixture |
| P1.9 benchmarks | Implemented | explicit StorageClass/profile validation, provider/driver/PVC/PV/node/topology result identity, guarded failed-fixture retention |
| P1.10 chart/RBAC/network | Implemented | cluster and namespace scopes, read-only default, named Secret access, Kubernetes API egress validation and render matrix |
| P1.11 observability/status | Implemented | bounded provider/inventory/watch/request metrics, provider-aware status, PrometheusRule and Grafana dashboard |
| P2.1 Longhorn adapter | Implemented | optional adapter lifecycle, descriptor/health/capabilities/version behavior, malformed URL failure and optional outage isolation |
| P2.2 legacy API | Implemented | proxy/stream/dashboard/auth/CSRF/audit/link behavior retained; no removal metadata because no route is deprecated in this release |
| P2.3 common enrichment | Implemented | authoritative Kubernetes correlation, Longhorn detail, provider-scoped plus legacy invalidation, provider-aware metrics |
| P2.4 provider-aware UI | Implemented | managed-provider summary, authoritative links and provider-aware navigation without legacy route changes |
| P2.5 readiness/compatibility | Implemented | optional-provider readiness separation, pinned version matrix, untested-version warning and capability downgrade |
| P2.6 deployment compatibility | Implemented; release pending | legacy synthesis and bolt-on/embedded render matrix pass; live current/previous upgrade/rollback rows remain pending |
| P3.1 Ceph lab/matrix | Implemented; release pending | pinned current/previous profiles, sanitized fixtures, guarded three-node install/validate/cleanup scripts; physical runs remain pending |
| P3.2 Rook discovery/readers | Implemented | configured-cluster-only discovery, served CRDs, version-tolerant bounded models, optional mirror handling, scoped events |
| P3.3 Dashboard client | Implemented | fixed typed allowlist, verified TLS/custom CA, in-memory JWT, refresh/logout, timeout/size/redirect/redaction/circuit/stale-cache tests |
| P3.4 Prometheus | Implemented | fixed query allowlist, bounded labels, partial snapshots, redirect block and per-query freshness/error/latency telemetry |
| P3.5 Ceph correlation | Implemented | driver ownership, safe StorageClass metadata, authoritative handle policy, distinct RBD/CephFS strategies and bounded indexes |
| P3.6 Ceph read API | Implemented | curated typed summaries/resources, provenance/freshness, desired/runtime merge and retryable unavailable responses; no raw proxy |
| P3.7 Ceph UI | Implemented | health/quorum/daemon/pool/filesystem/mirroring/capacity/throughput views with read-only, stale and partial states |
| P3.8 chart/security plumbing | Implemented | disabled default, named credentials/CA keys, narrow namespace reads and dashboard/Prometheus egress, config validation/docs |
| P3.9 security review | Implemented; release pending | SSRF/redaction/read-only/fuzz/property tests and threat model pass; independent checklist reviewer remains mandatory |
| P4.1 action policy | Implemented | machine-readable typed matrix, action authorization, role/scope gates and confused-deputy/replay/takeover threat model |
| P4.2 durable operations | Implemented | v1alpha1 CRD, immutable request, Kubernetes idempotency, Lease leader election, SSA/preconditions, replay/retry/retention tests |
| P4.3 plan/confirmation | Implemented | server admission dry-run, dependency/blast-radius plan, five-minute user/target/version/plan-bound HMAC challenge and fresh preflight |
| P4.4 generic lifecycle | Implemented; release pending | typed PVC/expand/snapshot/restore/clone/delete planners and reconciler/postflight logic; driver checksum lifecycle matrix remains live |
| P4.5 Ceph StorageClasses | Implemented; release pending | RBD/CephFS ownership/readiness/schema/default/dependency checks; supported-matrix lifecycle remains live |
| P4.6 replicated pools | Implemented; release pending | constrained schema, health/failure-domain safety, CRD dry-run/apply/watch and runtime postflight; live Ceph reconciliation remains pending |
| P4.7 guarded pool deletion | Implemented; release pending | default-off admin gate, fresh health, exact ownership, all dependency sources fail closed, UID/version delete and runtime-absence postflight; destructive lab row remains pending |
| P4.8 operations UI | Implemented | typed plan/review/confirm flow, warnings separate from typed name, immediate ID, durable timeline/history/filter/audit links |
| P4.9 audit/metrics | Implemented | correlated lifecycle events, allowlisted details, exact durable-terminal evidence before GC, operation metrics/alerts/dashboard |
| P4.10 chart/feature gates | Implemented | read-only defaults, split opt-in writer roles, no wildcard writes, operation/Lease permissions, emergency-disable and recovery runbook |
| P5.1 secure Ceph Dashboard handoff | Implemented; release pending | HTTPS-only by default, strict URL validation, no server fetch of the public URL, safe new-tab links, reviewed identifier-free deep-link allowlist and root fallback; production public TLS endpoint remains an operator deployment choice |
| P5.2 relationship graph | Implemented | versioned canonical IDs, exact evidence, bounded provider/namespace/kind/depth/page expansion, freshness/confidence, Kubernetes/CSI/Rook/Ceph nodes, compact accessible UI table |
| P5.3 desired/runtime drift | Implemented; release pending | separate authorities, grace tracking, generation/readiness/stale/runtime-only/missing categories, expected Rook-managed CephFS and internal pool suppression, live healthy-current profile reports in sync; deliberate drift injection matrix remains pending |
| P5.4 workload impact | Implemented | confirmed/potential/unknown separation, no OSD placement overclaim, freshness/incomplete conditions, bounded endpoint, UI and shared destructive-preflight analyzer |
| P5.5 incident timeline | Implemented | exact UID provider attribution for Kubernetes events, provider records, dedupe/count/clock-skew/retention/filter/limit semantics and timeline UI |
| P5.6 capacity ownership/forecast | Implemented; release pending | requested/provisioned/backend/raw measures remain non-additive, ownership dimensions/evidence/bounds, fixed allowlisted Prometheus range history, explicit unavailable forecast when Prometheus or sample/window gates are absent; long-duration forecast qualification remains pending |
| P5.7 provider comparison | Implemented | explicit candidate facts and tested profiles, eligible/ineligible/unknown criteria, no opaque score, stale/missing/unverified facts become unknown, non-comparable benchmarks remain separate |
| P5.8 guided remediation | Implemented | evidence-required read-only recommendations, Highland/Rook/native/specialist/observe boundaries, prerequisites/risks/escalation, unsafe URL and executable/destructive guidance rejection, no execution controls |
| P5.9 context UI | Implemented | all-storage and provider context routes, relationship/impact/drift/timeline/capacity/comparison/guidance panels, detail-page links, partial/stale/empty/error states and live browser smoke |
| P5.10 context security/operations | Implemented; release pending | query/result bounds, exact identifiers, fail-closed destructive impact, namespace-aware source inventory, safe external links and route contracts; independent tenant-leakage review and sustained-load telemetry qualification remain pending |

Intentional plan dispositions:

- Snapshot CRDs remain dynamically discovered so they can appear without restart, but every decoded
  v1 object is converted through the pinned upstream external-snapshotter types.
- No cancel API is exposed because none of the initial workflows has a proven safe cancellation
  point. This is the required fail-closed outcome for P4.2.
- The Ceph Dashboard integration has no write or request-supplied endpoint path; all Ceph mutations
  go through Kubernetes/Rook declarative resources.
- Generic CSI hostpath and Longhorn live profiles require suitable runners and are retained as
  explicit release rows rather than represented by a mock nightly pass.

## Automated evidence

The following commands are PR gates and passed locally on the evidence date:

```text
cd apps/api && go test ./... -count=1 && go vet ./...
cd apps/api && go build -o /tmp/highland-api ./cmd/highland-api && go build -o /tmp/mock-longhorn-manager ./cmd/mock-longhorn-manager
cd apps/api && go test -race ./internal/handlers ./internal/storage ./internal/providers/... ./internal/watch
cd apps/api && go test ./internal/providers/rookceph -run=^$ -fuzz=FuzzDashboardFixtureNormalizationIsBounded -fuzztime=5s
cd apps/web && npm run typecheck && npm run lint && npm test -- --run && npm run build && npm run build-storybook
cd apps/web && npm run test:e2e
SKIP_DEPENDENCY_BUILD=1 ./hack/test-chart.sh
npx --yes @redocly/cli@2.18.0 lint docs/openapi/storage-v1.yaml --config redocly.yaml
./hack/check-parity.sh
./hack/test-api-disabled-providers.sh
rg --files hack -g '*.sh' | xargs bash -n
git diff --check
```

Contract tests cover malformed/disallowed Dashboard responses, TLS trust, auth refresh, timeout,
size bounds, logout, redirect rejection, 15-minute stale expiry, redaction, Prometheus partial results,
supported Rook 1.19/1.20 and Ceph 19.2.x/20.2.1+ sanitized fixtures, explicit rejection of old,
prerelease, unknown, digest-only, and Ceph 20.2.0 versions, unknown additive fields, provider-ID
collisions, capability prerequisites, action roles, warning acknowledgement, forged/path-traversal
inputs, server dry-run admission denial, Ceph health and failure domains, fail-closed deletion,
transient versus authorization failure classification, operation restart/leader-takeover recovery,
exact-terminal-audit-gated retention, malformed stored-operation rejection, and concurrent
idempotency.

Browser coverage includes provider-to-class-to-claim-to-PV/workload/attachment drill-down, curated
Ceph detail without Secret rendering, URL filter persistence, accessible partial snapshot support,
typed operation forms without a raw JSON editor, a bounded 10,000-claim page, legacy parity, role
enforcement, CSRF, WCAG 2.1 AA smoke checks, visual smoke surfaces, and an opt-in live context test
covering relationship, drift, timeline, capacity, comparison, and guidance panels without post-login
browser errors.

The Helm matrix renders legacy bolt-on, embedded Longhorn, cluster and namespace storage scope,
Longhorn disabled, Ceph read-only, recovery-only, explicit writes, pool deletion, benchmarks,
Prometheus alerts, Grafana dashboard, and Dashboard/Prometheus-specific NetworkPolicy egress. It
rejects wildcard RBAC, default mutation permissions, an empty Kubernetes API NetworkPolicy allowlist,
and multi-replica durable audit backed by a non-RWX volume.

## Live qualification record

Live checks are intentionally not represented as passed by unit fixtures. Fill one row per clean
environment and attach logs/artifacts to the release:

On 2026-07-16 the current workspace deployed revision 14 to the local single-node k3s lab at the
configured NodePort. Highland ran from `highland-api:phase5g-20260716` and
`highland-web:phase5b-20260716` at Helm revision 19; embedded Longhorn 1.12.0 and
Rook/Ceph 1.20.2 with Ceph 20.2.1 were
Ready. Authenticated crawling returned HTTP 200 for every common storage GET surface, both provider
summaries/health/relationship routes, drift, timeline, ownership, forecast, comparison, remediation,
actions, operations, and every non-empty Rook/Ceph resource list/detail kind. Unauthenticated
storage access returned 401. The live Playwright context test passed with no post-login console/page
errors. Ceph reported `HEALTH_OK`; supported desired/runtime drift reported zero active records after
normal `.mgr` and Rook-managed CephFS pools were correctly classified.

This is meaningful integration evidence, but it is not the guarded three-node destructive,
failure-domain, upgrade/rollback, or previous-version qualification required for release.

| Profile | Cluster/run | Install + inventory | lifecycle/checksum | outage/recovery | upgrade/rollback/cleanup | Reviewer |
|---|---|---:|---:|---:|---:|---|
| generic CSI hostpath + snapshots | pending | ☐ | ☐ | ☐ | ☐ | — |
| Longhorn current, bolt-on | pending | ☐ | ☐ | ☐ | ☐ | — |
| Longhorn previous, bolt-on | pending | ☐ | ☐ | ☐ | ☐ | — |
| Longhorn current, embedded | local single-node partial | ☑ | read-only smoke only | ☐ | upgrade to revision 14 only; rollback/cleanup pending | implementing operator |
| Rook/Ceph current | local single-node partial | ☑ | CSI read/write smoke; destructive lifecycle pending | ☐ | ☐ | implementing operator |
| Rook/Ceph previous | pending | ☐ | ☐ | ☐ | ☐ | — |

Use [`../../hack/storage-lab/README.md`](../../hack/storage-lab/README.md) for the guarded three-node
Rook suite. Scheduled contract/scale checks and opt-in self-hosted current/previous Rook jobs are in
`.github/workflows/storage-nightly.yaml`. A failed, skipped, or unavailable live runner is not a pass.

## Release decision

Do not promote Rook/Ceph or write workflows beyond preview until every applicable live row is
complete, parity remains green, the security checklist has an independent reviewer, image scanning
has no unresolved high/critical finding, and an operator who did not implement the feature completes
the install/upgrade/disable/uninstall documentation dry run.
