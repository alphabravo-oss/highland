# Highland Kubernetes Storage Control Plane: Implementation Plan for Roadmap Phases 1–5

- Status: **Phases 1–4 implemented in preview; Phase 5 proposed for review**
- Owners: **TBD**
- Last updated: **2026-07-16**
- Target: **Highland 0.x → first multi-provider beta**

Implementation evidence and the remaining environment-dependent release gates are recorded in
[`../validation/storage-control-plane-implementation.md`](../validation/storage-control-plane-implementation.md).

## 1. Executive decision

Highland will evolve from a Longhorn-specific UI into a **Kubernetes storage control plane** with:

1. a provider-neutral Kubernetes/CSI inventory and capability layer;
2. Longhorn retained as the first fully managed provider;
3. a read-only Rook/Ceph provider using Rook CRDs, the Ceph Dashboard API, and Prometheus;
4. safe, typed, auditable Kubernetes and Rook/Ceph workflows;
5. a Kubernetes-to-backend context, impact, and policy layer with secure handoff to native provider
   administration surfaces such as the Ceph Dashboard.

Highland will **not call arbitrary CSI driver sockets directly**. CSI is the contract between
Kubernetes and a storage plugin; Kubernetes resources remain the source of truth for claims,
volumes, attachment, topology, capacity, snapshots, and workload ownership. Provider adapters add
the backend concepts CSI does not standardize, such as Longhorn replicas or Ceph pools and OSDs.

The first multi-provider release supports one Kubernetes cluster per Highland installation. The
internal provider identity and API contracts must not prevent later multi-cluster support, but
multi-cluster credentials, tenancy, aggregation, and failover are outside this plan.

Highland is not intended to become a second implementation of the Ceph Dashboard. The native Ceph
Dashboard remains the deep Ceph administration surface for daemon management, placement, recovery,
configuration, and backend-specific lifecycle operations. Highland adds the Kubernetes ownership
graph, Rook desired-state context, workload impact, provider comparison, safety policy, and durable
workflow history that a backend-only dashboard cannot provide.

## 2. Outcomes

At completion:

- Highland automatically discovers installed CSI drivers and presents a common inventory of
  drivers, storage classes, PVCs, PVs, snapshots, attachments, capacity, events, and workloads.
- The existing Longhorn UI, API proxy, actions, metrics, watch behavior, embedded-chart mode, and
  bolt-on deployment remain supported while Longhorn is represented as a managed provider.
- A Rook-managed Ceph cluster can be added without enabling writes. Highland shows Kubernetes and
  backend health for RBD and CephFS and correlates Kubernetes storage objects with Ceph resources
  when the provider supplies authoritative identifiers.
- Operators can move from a Highland Ceph overview or resource detail to an explicitly configured
  native Ceph Dashboard without Highland embedding the dashboard, forwarding credentials, or
  broadening its backend API permissions.
- Highland explains Kubernetes-to-Ceph ownership and impact: workload → Pod → PVC → PV →
  StorageClass/CSI driver → RBD image or CephFS subvolume → pool/filesystem and, where authoritative,
  relevant topology and runtime health.
- Highland identifies Rook desired-state versus Ceph runtime drift, correlates Kubernetes/Rook/Ceph
  events, and attributes capacity and health impact to namespaces and workloads without claiming
  precision the underlying APIs do not provide.
- Administrators can opt into a bounded set of safe storage workflows. Every write is authorized,
  validated, auditable, idempotent, recoverable after an API restart, and protected by dependency
  checks and explicit confirmation where destructive.
- A documented compatibility matrix and repeatable test environments gate releases.

## 3. Scope

### 3.1 Roadmap items covered

This plan covers these five roadmap items:

1. **Universal CSI core**
2. **Preserve Longhorn as the first managed adapter**
3. **Add a read-only Rook/Ceph adapter**
4. **Add safe Ceph workflows**
5. **Add native-dashboard handoff and the Kubernetes-to-backend context layer**

### 3.2 Support levels

Highland will report a support level per provider/driver rather than treating support as binary:

| Level | Meaning | User expectation |
|---|---|---|
| `detected` | Driver is represented using standard Kubernetes resources | Read-only common inventory; unsupported capability is hidden, not guessed |
| `verified` | Common workflows are continuously tested against the driver | Snapshot, restore, clone, expansion, capacity, health, or benchmark support is advertised only when tested and available |
| `managed` | A provider adapter supplies backend health and typed operations | Provider-specific pages, health, metrics, and explicitly supported actions |

Longhorn and Rook/Ceph are `managed`. All conforming CSI drivers begin as `detected`. A later plan
can promote OpenEBS, Piraeus/LINSTOR, TopoLVM, NFS, SMB, or democratic-csi through the same
interfaces without changing the common UI or API.

### 3.3 Non-goals

- Implementing or replacing a CSI driver.
- Connecting to arbitrary CSI Unix sockets from Highland.
- Reimplementing the entire Ceph Dashboard.
- Embedding the Ceph Dashboard in an iframe, proxying its browser session through Highland, sharing
  its cookies, or forwarding Highland's server-side Dashboard credentials to a browser.
- Managing more than one Kubernetes cluster from one Highland deployment.
- Managing Ceph RGW/object storage in these phases. Object storage is not ordinary CSI storage and
  should become a separate COSI/RGW module.
- Providing a raw Ceph command console, toolbox shell, or browser-visible Ceph administrative proxy.
- Automated OSD replacement, MON/MGR topology changes, PG repair, cluster purge, mirroring changes,
  erasure-code profile management, CRUSH-map editing, or Ceph upgrades.
- Migrating data between storage providers.
- Claiming direct workload impact from an OSD, PG, pool, or image when Ceph/Rook/CSI identifiers do
  not provide an authoritative relationship.
- Guaranteeing backend volume correlation when a driver does not expose an authoritative mapping.

## 4. Current-state constraints

The implementation must account for these existing design choices:

- `HIGHLAND_MANAGER_URL` and `managerUrl` configure one Longhorn manager.
- API startup always constructs a Longhorn reverse proxy, stream proxy, and Longhorn metrics scraper.
- `/readyz` currently fails when the Longhorn manager cannot answer `/v1`.
- `/api/v1/lh/*` transparently proxies Rancher-style Longhorn resources and actions.
- the SSE hub watches a fixed set of `longhorn.io/v1beta2` resources and emits unscoped frontend
  query keys.
- frontend domain models, hooks, navigation, and most feature pages are Longhorn-shaped.
- benchmark execution and persistence are reusable, but the default storage class and comments are
  Longhorn-specific.
- chart RBAC grants benchmark writes in Highland's namespace and Longhorn reads/watches in the
  Longhorn namespace; it does not grant cluster-wide storage discovery.
- Highland normally runs two API replicas, so any asynchronous write controller must handle leader
  election and replay safely.

The migration must preserve existing behavior while introducing provider-neutral paths. A big-bang
rewrite of the Longhorn UI is explicitly rejected.

## 5. Architecture decisions and invariants

### 5.1 Kubernetes remains the orchestration source of truth

Use Kubernetes APIs for:

- `CSIDriver`, `CSINode`, `StorageClass`, `CSIStorageCapacity`, `VolumeAttachment`, and, when
  available, `VolumeAttributesClass`;
- PVCs, PVs, Pods, workload owners, Namespaces, and Events;
- `VolumeSnapshotClass`, `VolumeSnapshot`, and `VolumeSnapshotContent`;
- Rook CRDs representing desired Ceph state.

Do not create, publish, attach, expand, snapshot, or delete a Kubernetes-owned volume by calling a
CSI controller socket. Doing so would bypass Kubernetes reconciliation, ownership, idempotency, and
admission controls.

### 5.2 Common summaries, provider-specific detail

Do not force every backend into a universal Longhorn-shaped `Volume`. Common records contain only
portable facts and references. Provider adapters attach typed detail and capability flags.

Example common records:

- `ProviderDescriptor`
- `DriverSummary`
- `StorageClassSummary`
- `ClaimSummary`
- `PersistentVolumeSummary`
- `SnapshotSummary`
- `AttachmentSummary`
- `CapacitySummary`
- `StorageEvent`
- `StorageOperation`

Every common resource includes stable identity and provenance:

```json
{
  "id": "cluster/default/pvc/data",
  "clusterId": "local",
  "providerId": "rook-ceph",
  "driver": "rook-ceph.rbd.csi.ceph.com",
  "namespace": "default",
  "name": "data",
  "kubernetesUid": "...",
  "pvName": "pvc-...",
  "volumeHandle": "...",
  "providerRef": {
    "kind": "rbd-image",
    "id": "authoritative-provider-id"
  },
  "capabilities": ["snapshot", "expand", "clone"]
}
```

`providerRef` is absent when Highland cannot prove the mapping. The UI must say “backend mapping
unavailable” rather than infer an image from a PVC name.

### 5.3 Capabilities control behavior

Buttons and mutations are never enabled because a provider name is recognized. The backend returns
capabilities based on:

1. installed Kubernetes API resources;
2. `CSIDriver`, StorageClass, SnapshotClass, and other discoverable configuration;
3. provider adapter/version support;
4. configured Highland write policy;
5. current user authorization;
6. current resource state and operation preconditions.

Example capability identifiers:

```text
inventory.claims.read
inventory.attachments.read
volume.create
volume.expand
volume.delete
snapshot.create
snapshot.delete
snapshot.restore
volume.clone
provider.health.read
ceph.pool.create
ceph.pool.delete
ceph.storageclass.create
```

Unknown capability is treated as unsupported. No optimistic fallback is allowed for writes.

### 5.4 Provider APIs remain server-side

- Longhorn's legacy proxy remains authenticated and authorized as it is today.
- New adapters expose typed Highland responses and actions.
- Ceph credentials, JWTs, CAs, and upstream URLs never reach the browser.
- Ceph Dashboard requests use TLS verification by default, bounded timeouts, response-size limits,
  token refresh, and version-aware content negotiation.
- User input cannot select an arbitrary upstream URL; provider endpoints come only from trusted
  configuration or constrained in-cluster discovery.

### 5.5 Readiness is not global provider health

Separate these concepts:

- `/healthz`: process liveness only.
- `/readyz`: configuration loaded, auth initialized, required caches synced, and the Kubernetes API
  available when storage core is enabled.
- provider health: reported per provider under `/api/v1/storage/providers` and `/api/v1/status`.

For backward compatibility, a legacy configuration containing only required Longhorn may retain the
current “Longhorn must be reachable” readiness behavior during the migration. Multi-provider mode
must not remove all Highland endpoints because one optional provider is unavailable.

### 5.6 Writes are introduced only after durable operation safety exists

Phase 1 defines provider-neutral write contracts but exposes the new common API as read-only.
Existing Longhorn actions remain available in Phase 2. Phase 3 is explicitly read-only for Ceph.
Phase 4 introduces a durable operation controller and enables the approved common and Ceph write
matrix.

## 6. Target package and component layout

The exact filenames may change during implementation, but responsibilities must remain separated:

```text
apps/api/internal/storage/
  model/             canonical API/domain types and error codes
  registry/          provider registration, discovery, support level, health
  inventory/         Kubernetes informers, indexes, correlation, pagination
  capabilities/      capability resolution and policy intersection
  operations/        durable operation API, reconciler, preflight, confirmation
  transport/         common HTTP handlers and DTO mapping

apps/api/internal/providers/
  longhorn/          adapter around the existing proxy, stream, metrics, and watches
  rookceph/          Rook CRD reader, Ceph Dashboard client, correlation, metrics

apps/web/src/api/storage/
  types.ts           generated or contract-checked common DTOs
  client.ts          common storage endpoints
  hooks.ts           provider/cluster/namespace-scoped query keys

apps/web/src/features/storage/
  providers/
  classes/
  claims/
  snapshots/
  attachments/
  capacity/

apps/web/src/features/providers/
  longhorn/          existing pages, migrated incrementally
  rook-ceph/         Ceph summary, pools, OSDs, RBD, CephFS
```

### 6.1 Provider contract

The provider interface must be narrow. Optional behavior is represented by smaller interfaces rather
than one interface where every provider returns “not implemented.”

```go
type Provider interface {
    Descriptor(context.Context) (ProviderDescriptor, error)
    Health(context.Context) ProviderHealth
    Capabilities(context.Context) []Capability
}

type InventoryEnricher interface {
    EnrichClaims(context.Context, []ClaimSummary) error
    EnrichVolumes(context.Context, []PersistentVolumeSummary) error
}

type ProviderResourceReader interface {
    ResourceKinds(context.Context) []ProviderResourceKind
    ListProviderResources(context.Context, kind string, page PageRequest) (Page, error)
    GetProviderResource(context.Context, kind, id string) (any, error)
}

type OperationPlanner interface {
    Plan(context.Context, OperationRequest) (OperationPlan, error)
}

type OperationExecutor interface {
    Reconcile(context.Context, *StorageOperation) error
}
```

Provider IDs are stable configuration identities, not display names. One Ceph provider may own both
RBD and CephFS driver names.

### 6.2 Common HTTP API

All list endpoints support `limit`, `continue`, provider, driver, namespace, status, and search
filters where applicable. List implementations must use informer indexes or Kubernetes pagination;
they must not issue one backend call per row.

```text
GET    /api/v1/storage/providers
GET    /api/v1/storage/providers/{providerId}
GET    /api/v1/storage/drivers
GET    /api/v1/storage/classes
GET    /api/v1/storage/claims
GET    /api/v1/storage/claims/{namespace}/{name}
GET    /api/v1/storage/volumes
GET    /api/v1/storage/volumes/{name}
GET    /api/v1/storage/snapshots
GET    /api/v1/storage/attachments
GET    /api/v1/storage/capacity
GET    /api/v1/storage/events

GET    /api/v1/providers/{providerId}/summary
GET    /api/v1/providers/{providerId}/health
GET    /api/v1/providers/{providerId}/resources/{kind}
GET    /api/v1/providers/{providerId}/resources/{kind}/{id}
```

Phase 4 adds only typed mutation routes documented in an OpenAPI contract. Examples:

```text
GET    /api/v1/storage/operations
GET    /api/v1/storage/operations/{operationId}
POST   /api/v1/storage/claims
PATCH  /api/v1/storage/claims/{namespace}/{name}/size
DELETE /api/v1/storage/claims/{namespace}/{name}
POST   /api/v1/storage/snapshots
DELETE /api/v1/storage/snapshots/{namespace}/{name}
POST   /api/v1/storage/restores
POST   /api/v1/storage/clones

POST   /api/v1/providers/{providerId}/ceph/block-pools
DELETE /api/v1/providers/{providerId}/ceph/block-pools/{namespace}/{name}
POST   /api/v1/providers/{providerId}/ceph/storage-classes
DELETE /api/v1/providers/{providerId}/ceph/storage-classes/{name}
```

Errors use one envelope with stable machine-readable codes:

```json
{
  "error": {
    "code": "DEPENDENCIES_EXIST",
    "message": "pool is referenced by 2 storage classes",
    "details": {},
    "retryable": false,
    "requestId": "..."
  }
}
```

### 6.3 UI information architecture

Common navigation:

- Overview
- Providers
- Storage Classes
- Claims & Volumes
- Snapshots
- Attachments
- Capacity
- Events
- Benchmarks

Provider-specific navigation appears under the selected provider and is capability-driven:

- Longhorn: existing Nodes & Disks, Backups, Recurring Jobs, Images, Instance Managers, Orphans,
  Settings, and support tools.
- Ceph: Health, RBD, CephFS, Pools, OSDs, MON/MGR status, and mirroring status when read support is
  present.

The default view is cluster-wide with provider and namespace filters. Selecting a provider changes
the contextual sidebar and resource vocabulary while retaining the same application shell, account,
administration area, and common Kubernetes inventory. URLs retain provider/filter state for sharing
and refresh.

When a trusted operator-facing URL is configured, Ceph overview and relevant resource detail pages
show an **Open Ceph Dashboard** action. The native dashboard opens in a new tab and remains a separate
security boundary. Highland may provide version-aware deep links only when covered by the declared
compatibility matrix; it must fall back to the configured dashboard root when a stable resource URL
cannot be guaranteed.

### 6.4 Configuration shape

Maintain `managerUrl` compatibility. When `providers.longhorn` is absent, synthesize the Longhorn
provider from the legacy setting.

Proposed Helm/config shape:

```yaml
storage:
  enabled: true
  scope:
    mode: cluster                 # cluster | namespaces
    namespaces: []
  writes:
    enabled: false               # enabled in Phase 4 only
  requiredProviders: [longhorn]

providers:
  longhorn:
    enabled: true
    managerUrl: ""               # defaults from legacy longhorn settings
    namespace: longhorn-system
  rookCeph:
    enabled: false
    namespace: rook-ceph
    cephClusterName: rook-ceph
    dashboard:
      serviceName: rook-ceph-mgr-dashboard
      url: ""
      publicUrl: ""              # operator-facing URL; never used for server-side API requests
      existingSecret: ""
      caSecret: ""
      insecureSkipVerify: false
    prometheus:
      url: ""
    writes:
      enabled: false
      allowStorageClassDelete: false
      allowPoolDelete: false
```

The Ceph Dashboard secret must contain a dedicated least-privilege account. Highland must not
automatically consume Rook's generated dashboard administrator password.

`dashboard.url` and `dashboard.publicUrl` have separate trust purposes. The private URL is used only
by the Highland API with a dedicated reader and pinned TLS policy. The public URL is rendered only as
an outbound browser link. Highland never copies the private reader username, password, token, CA
material, or authenticated session into that link.

## 7. Phase 1 — Universal CSI core

### 7.1 Objective and reasoning

Build the provider-neutral, read-only storage foundation before adding a second backend. This phase
proves that Highland can inventory arbitrary CSI drivers without weakening existing Longhorn
behavior or granting write permissions prematurely.

### 7.2 Phase 1 tasks

#### P1.1 — Record contracts and compatibility policy

- [ ] Add an architecture decision record stating that Highland uses Kubernetes APIs rather than
  direct CSI sockets.
- [ ] Define the support levels (`detected`, `verified`, `managed`) and capability vocabulary.
- [ ] Define the one-cluster scope and namespace access modes.
- [ ] Define the canonical identities, API error envelope, pagination contract, timestamp format,
  quantity format, and condition severity model.
- [ ] Add an OpenAPI document for the read-only common endpoints.
- [ ] Add `docs/compatibility.yaml` containing supported Kubernetes API versions and optional API
  requirements. Pin actual tested versions at implementation time; do not use floating “latest.”
- [ ] Decide and document whether namespace-scoped installations may see PV names and cluster-scoped
  driver metadata. Default to least privilege and show partial inventory when permissions are absent.

#### P1.2 — Create Kubernetes client and discovery foundation

- [ ] Move REST config/client creation out of the benchmark runner into a reusable Kubernetes client
  factory. Benchmarks consume that factory instead of owning cluster connectivity.
- [ ] Construct typed clients for core/storage APIs and a dynamic/discovery client for optional APIs.
- [ ] Add a compatible, pinned external snapshotter client dependency for
  `snapshot.storage.k8s.io/v1`, or encapsulate dynamic access behind typed internal DTOs.
- [ ] Detect optional APIs at startup and refresh discovery periodically so installing snapshot CRDs
  does not require a Highland restart.
- [ ] Expose discovery errors as provider/core conditions without panicking or disabling unrelated
  endpoints.
- [ ] Add configurable client QPS/burst, request timeouts, and a bounded user agent.

#### P1.3 — Build shared informer inventory

- [ ] Create informer factories for cluster-scoped resources: PVs, StorageClasses, CSIDrivers,
  CSINodes, VolumeAttachments, CSIStorageCapacity, and VolumeAttributesClass when served.
- [ ] Create namespace-aware informers for PVCs, Pods, Events, VolumeSnapshots, and permitted
  workload resources.
- [ ] Add indexes for:
  - PVC → PV;
  - PV → CSI driver and volume handle;
  - PV/PVC → StorageClass;
  - PVC → Pods and controller owners;
  - PV → VolumeAttachments;
  - StorageClass → SnapshotClasses;
  - driver → nodes, capacity objects, classes, volumes, and attachments.
- [ ] Track informer synchronization and last successful event time.
- [ ] Bound memory by storing required fields in normalized summaries rather than copying all objects
  into a second unbounded cache.
- [ ] Preserve watch reconnect behavior and continue periodic safety refreshes.

#### P1.4 — Implement provider registry and generic provider

- [ ] Build a provider registry keyed by stable provider ID.
- [ ] Register a generic Kubernetes/CSI provider that groups unknown drivers without pretending they
  share one backend.
- [ ] Allow a configured managed provider to claim one or more driver names.
- [ ] Detect ambiguous claims and report a configuration error rather than selecting an adapter by
  map iteration order.
- [ ] Return provider descriptor, support level, driver names, version facts, health conditions,
  capabilities, and last-observed timestamps.
- [ ] Preserve unknown fields needed for debugging in a bounded `metadata` section; do not expose
  Secrets or full annotations by default.

#### P1.5 — Implement common inventory and correlation

- [ ] Implement drivers, classes, claims, PVs, attachments, capacity, snapshots, and events readers.
- [ ] Normalize Kubernetes quantities without converting large byte values through JavaScript
  floating-point numbers. Use quantity strings plus optional decimal byte strings.
- [ ] Distinguish requested, provisioned, used, and backend-allocated capacity.
- [ ] Represent `Retain` reclaim policy and released PVs as explicit orphan-risk conditions.
- [ ] Correlate PVCs with workloads using Pod volumes and owner references.
- [ ] Treat `spec.csi.driver` and `spec.csi.volumeHandle` as authoritative CSI identity.
- [ ] Do not parse undocumented provider handle formats in the common layer.
- [ ] Report attachment state and node topology separately from backend replica placement.
- [ ] Report snapshot API absence, no matching SnapshotClass, and provider capability absence as
  different states.
- [ ] Add server-side filtering and pagination; prevent one API response from returning an unbounded
  cluster inventory.

#### P1.6 — Generalize live invalidation

- [ ] Refactor `watch.Hub` so resource-to-query-key mappings are registered by the storage core and
  providers rather than hard-coded globally.
- [ ] Scope invalidation frames by cluster, provider, namespace, resource kind, and optional name.
- [ ] Version the SSE event format while accepting the existing Longhorn key-only frame in the web
  client during migration.
- [ ] Coalesce bursts, retain bounded client buffers, and fall back to invalidate-all after drops.
- [ ] Emit cache-sync and watch-error metrics per resource/provider using bounded labels.

#### P1.7 — Add common read-only API handlers

- [ ] Implement the common GET routes from section 6.2.
- [ ] Validate filter lengths, page size, continuation tokens, namespaces, and provider IDs.
- [ ] Return partial results with explicit conditions when optional permissions or APIs are absent;
  do not silently return an empty healthy list.
- [ ] Add request IDs and the common error envelope.
- [ ] Publish and contract-test JSON examples.
- [ ] Add API deprecation headers/process before changing common response fields.

#### P1.8 — Add common UI

- [ ] Introduce common storage DTOs separate from `LHResource` and rename ambiguous Longhorn types
  such as frontend `Volume` to `LonghornVolume` during touched-file migrations.
- [ ] Add provider-, namespace-, and cluster-scoped TanStack Query keys.
- [ ] Build Providers, Storage Classes, Claims & Volumes, Snapshots, Attachments, Capacity, and Events
  pages.
- [ ] Add capability-aware empty states explaining why an action or data source is unavailable.
- [ ] Add filters to the URL and preserve them across reloads.
- [ ] Add accessible loading, partial-data, stale-data, degraded-provider, and permission-denied states.
- [ ] Use virtualized or paginated tables for large inventories.
- [ ] Keep existing Longhorn navigation visible until Phase 2 explicitly moves it.

#### P1.9 — Generalize benchmarks

- [ ] Remove Longhorn-specific comments and defaults from benchmark internals.
- [ ] Require or select a StorageClass per run; display its provisioner/provider before confirmation.
- [ ] Validate requested access mode and volume mode against the selected class/profile.
- [ ] Preserve ownership labels and cleanup behavior.
- [ ] Record provider ID, CSI driver, StorageClass, PVC/PV, node, and topology in benchmark results.
- [ ] Add an option to retain failed test PVCs only for administrators and only with an explicit
  confirmation; default remains cleanup.

#### P1.10 — Helm, RBAC, and NetworkPolicy

- [ ] Add `storage.enabled`, scope, and provider configuration values.
- [ ] Add a read-only ClusterRole for the selected common storage resources.
- [ ] Support namespace allowlists using Roles/RoleBindings for namespaced inventory where feasible.
- [ ] Keep Secret access narrowly named/resource-scoped; the universal inventory never lists Secret
  contents.
- [ ] Add chart assertions for cluster and namespace modes.
- [ ] Update NetworkPolicy for Kubernetes API access without adding broad internet egress.
- [ ] Document exactly which pages degrade when cluster-scoped permission is omitted.

#### P1.11 — Observability and status

- [ ] Add provider/core metrics:
  - `highland_storage_provider_up{provider,kind}`;
  - `highland_storage_inventory_objects{kind,provider}`;
  - `highland_storage_sync_timestamp_seconds{source}`;
  - `highland_storage_watch_errors_total{source}`;
  - `highland_storage_provider_request_duration_seconds{provider,operation}`;
  - `highland_storage_provider_errors_total{provider,reason}`.
- [ ] Keep label values bounded; never label by volume/PVC name.
- [ ] Extend status output with Kubernetes storage-core health and per-provider health.
- [ ] Split readiness from optional provider health as described in section 5.5.
- [ ] Add dashboards/alerts for cache not synced, provider unavailable, watch failures, and stale data.

### 7.3 Phase 1 definition of done

- [ ] An installed but otherwise unknown CSI driver appears as `detected` without code changes.
- [ ] Common pages correctly show drivers, classes, claims/PVs, workloads, attachments, snapshots,
  capacity, and events from an ephemeral cluster.
- [ ] Missing snapshot CRDs and restricted RBAC produce explicit partial-support conditions.
- [ ] No new common mutation endpoint is enabled.
- [ ] Longhorn legacy pages and `/api/v1/lh/*` still pass their existing tests.
- [ ] Common OpenAPI and frontend DTO contract tests pass.
- [ ] Warm list API p95 is below 500 ms for a synthetic inventory of 10,000 PVCs on the CI reference
  hardware, with bounded response pages; cold-cache targets are documented separately.
- [ ] UI remains usable and accessible with 10,000 synthetic claims through server pagination or
  virtualization.
- [ ] Default Helm install renders the required read-only RBAC and no storage-write permissions.
- [ ] Documentation explains support levels, scope, permissions, and partial-data behavior.

### 7.4 Phase 1 tests and validation

#### Unit and property tests

- identity generation and collision resistance;
- Kubernetes quantity normalization, including values above JavaScript's safe integer range;
- pagination/continuation validation;
- capability intersection;
- PVC/PV/Pod/attachment/snapshot correlation;
- reclaim/orphan-risk conditions;
- provider-driver claim conflicts;
- SSE registration, coalescing, scoping, reconnect, and slow-client overflow;
- RBAC error classification versus empty inventory.

#### API/integration tests

- fake-client handler tests for every common endpoint and error code;
- discovery tests with snapshot and VolumeAttributesClass APIs present and absent;
- informer integration tests using an API-server-backed environment where watch semantics matter;
- OpenAPI response conformance and backward-compatible fixture tests;
- authorization tests for viewer, operator, and admin GET access.

#### Browser tests

- login → provider list → class → claim → PV/workload/attachment drill-down;
- provider and namespace filters survive refresh and deep links;
- partial permissions and missing snapshot APIs show an explanation rather than an empty success;
- keyboard navigation, landmarks, focus order, contrast, and screen-reader labels;
- 10,000-row fixture remains responsive.

#### Live validation

Use an ephemeral kind/k3d cluster with the CSI host-path test driver and external snapshot controller:

1. discover the driver and StorageClass;
2. create fixtures using kubectl: PVC, consuming Pod, snapshot, restored PVC, clone, and attachment;
3. verify every relationship in Highland;
4. expand a fixture outside Highland and verify live invalidation;
5. remove snapshot CRDs in a scratch cluster and verify graceful degradation;
6. revoke one RBAC permission and verify explicit partial status and metrics;
7. interrupt the watch connection and verify reconnect plus safety polling.

## 8. Phase 2 — Preserve Longhorn as the first managed provider

### 8.1 Objective and reasoning

Move Longhorn behind the provider boundary without rewriting its mature functionality. The phase is
complete only when existing Longhorn behavior remains at parity and Longhorn also enriches the new
common storage views.

### 8.2 Phase 2 tasks

#### P2.1 — Wrap existing Longhorn components in an adapter

- [ ] Implement a Longhorn provider descriptor, health source, capability source, version support
  check, and owned CSI driver names.
- [ ] Move construction of the reverse proxy, stream proxy, scraper, and Longhorn watch registrations
  into the adapter lifecycle.
- [ ] Allow the application to start without Longhorn when the adapter is disabled.
- [ ] Preserve `HIGHLAND_MANAGER_URL`, `managerUrl`, `HIGHLAND_LONGHORN_NAMESPACE`, and existing Helm
  values as compatibility inputs.
- [ ] Fail fast on malformed URLs, but report unreachable optional Longhorn as provider health rather
  than process failure.
- [ ] Keep connection pools, timeouts, streaming semantics, link rewriting, and proxy observability.

#### P2.2 — Preserve the legacy API contract

- [ ] Keep `/api/v1/lh`, `/api/v1/lh/*`, backing-image streaming, and the synthetic Longhorn
  dashboard endpoint unchanged.
- [ ] Keep current authentication, CSRF, method authorization, settings-admin policy, audit behavior,
  and response/link rewriting.
- [ ] Add deprecation metadata only after the common/provider-specific replacement is complete; do
  not announce removal in this phase.
- [ ] Version and retain mock Longhorn manager fixtures.
- [ ] Ensure a nil/disabled Longhorn adapter never leaves handlers that can dereference a nil proxy.

#### P2.3 — Enrich common storage inventory

- [ ] Map Longhorn Kubernetes status to common claims/PVs using authoritative PV/PVC data.
- [ ] Enrich common volume summaries with robustness, frontend, engine, replica summary, backup
  summary, and backend allocation without overwriting Kubernetes truth.
- [ ] Surface Longhorn nodes/disks as provider resources, distinct from Kubernetes CSINodes.
- [ ] Register Longhorn CRD watches with the generalized SSE hub.
- [ ] Scope Longhorn query invalidations by provider/resource while continuing legacy key invalidation.
- [ ] Generalize metrics storage so common volume metrics carry provider ID while legacy metric routes
  remain functional.

#### P2.4 — Integrate Longhorn into provider-aware UI

- [ ] Show Longhorn on Providers with version, namespace, manager reachability, support level, and
  health.
- [ ] Link common claim/volume details to Longhorn-specific detail when an authoritative mapping exists.
- [ ] Move existing Longhorn navigation under a provider-aware grouping without changing route URLs
  in this phase.
- [ ] Preserve every existing action and visibility rule.
- [ ] Ensure generic “Create claim” and Longhorn-native “Create volume” are labeled distinctly until
  their semantics are intentionally unified.
- [ ] Preserve dark/light themes, i18n keys, command palette, saved views, and accessibility behavior.

#### P2.5 — Readiness, status, and compatibility

- [ ] Implement compatibility behavior for legacy Longhorn-only readiness.
- [ ] Report Longhorn provider health separately in multi-provider mode.
- [ ] Keep the supported Longhorn version matrix in a machine-readable file and render it in Status.
- [ ] Warn on an untested Longhorn version but do not silently enable version-sensitive actions.
- [ ] Add provider capability downgrades for API/version differences.

#### P2.6 — Helm and deployment compatibility

- [ ] Preserve bolt-on Longhorn configuration.
- [ ] Preserve optional embedded Longhorn subchart behavior and UI-disabled defaults.
- [ ] Render a synthesized provider configuration from legacy values.
- [ ] Update NetworkPolicy using provider configuration while retaining Longhorn namespace/port rules.
- [ ] Verify upgrades from the last Longhorn-only release with no required values migration.
- [ ] Document rollback: disabling storage core returns users to legacy pages without modifying
  Longhorn resources.

### 8.3 Phase 2 definition of done

- [ ] Every `done` P0 item in `docs/parity-matrix.yaml` remains passing.
- [ ] Existing `/api/v1/lh/*` contract fixtures are unchanged except intentional additive headers.
- [ ] Longhorn appears as `managed` in the common Providers view.
- [ ] Common claims/volumes link to correct Longhorn details without name-based guessing.
- [ ] Longhorn can be disabled and Highland still starts with the universal core.
- [ ] Bolt-on and embedded chart modes both pass live validation.
- [ ] A Longhorn outage degrades only the configured required readiness/provider paths.
- [ ] No existing Longhorn action loses CSRF, RBAC, audit, or streaming protection.
- [ ] Upgrade and rollback procedures are documented and tested.

### 8.4 Phase 2 tests and validation

#### Regression tests

- run all existing Go, React, mock-manager, Playwright, visual, accessibility, parity, and chart tests;
- golden-test Longhorn proxy link/action rewriting and streamed upload/download behavior;
- test legacy config synthesis and environment-over-file precedence;
- test disabled, optional-unreachable, required-unreachable, and healthy adapter states;
- test old SSE frames and new scoped frames against the web client;
- compare common enrichment against Kubernetes/PV truth fixtures.

#### Live Longhorn matrix

On an isolated VM-backed k3s/kind environment suitable for Longhorn prerequisites:

1. validate the pinned supported Longhorn minor and the previous supported minor;
2. test bolt-on and embedded deployments separately;
3. create a Kubernetes PVC and a Longhorn-native volume;
4. attach/detach, snapshot, restore/clone, expand, backup, and delete test resources;
5. verify nodes/disks, recurring jobs, images, instance managers, orphans, and settings;
6. run fio against a selected Longhorn StorageClass;
7. restart Highland API replicas and verify sessions, watches, and UI recovery;
8. stop the Longhorn manager and verify readiness/provider-health behavior;
9. perform a Highland upgrade and rollback without changing Longhorn data.

## 9. Phase 3 — Read-only Rook/Ceph adapter

### 9.1 Objective and reasoning

Add useful Ceph visibility without introducing a second mutation path. Rook CRDs provide desired
state and operator status; the Ceph Dashboard API provides runtime backend detail; Prometheus
provides time-series metrics. Highland should curate Kubernetes-relevant Ceph information rather than
copy every Ceph Dashboard page.

### 9.2 Supported read-only Ceph scope

- Rook `CephCluster` health and conditions;
- MON and MGR quorum/availability summary;
- OSD inventory, up/in state, utilization, and host placement;
- `CephBlockPool` desired state/status and Ceph pool runtime summary;
- `CephFilesystem` desired state/status and filesystem summary;
- RBD and CephFS CSI drivers and StorageClasses;
- RBD image inventory when available through the supported Ceph API;
- PVC/PV/RBD or CephFS correlation when authoritative;
- PG/placement health summary, not a full PG administration console;
- `CephRBDMirror` status when present;
- capacity, performance, alerts, tasks, and health messages relevant to Kubernetes users.

### 9.3 Phase 3 tasks

#### P3.1 — Establish the Ceph compatibility laboratory

- [ ] Select and record supported Kubernetes, Rook, Ceph, and Ceph CSI version combinations.
- [ ] Test the current and previous supported Rook minor and the Ceph majors supported by those Rook
  versions. Pin images and charts in CI manifests.
- [ ] Build a repeatable three-storage-node Rook/Ceph test environment with disposable raw disks or
  loop devices. Do not install it over the shared Longhorn development cluster.
- [ ] Add fixture capture tooling that redacts FSIDs, hostnames, IPs, tokens, usernames, and secrets.
- [ ] Store sanitized Rook CRD and Ceph API fixtures by compatibility version.
- [ ] Define the minimum resources and expected runtime for PR, nightly, and release validation.

#### P3.2 — Implement Rook discovery and CRD readers

- [ ] Detect served `ceph.rook.io` API versions and supported resource kinds.
- [ ] Discover configured `CephCluster` by namespace/name; fail on ambiguity rather than selecting the
  first cluster.
- [ ] Read/watch `CephCluster`, `CephBlockPool`, `CephFilesystem`, `CephRBDMirror`, and only the
  additional read models explicitly used by the UI.
- [ ] Decode unstructured resources through version-tolerant internal models.
- [ ] Preserve unknown status conditions for diagnostics while mapping known conditions into common
  severity.
- [ ] Register Rook watches with provider-scoped SSE invalidation.
- [ ] Report CRD absence, Rook operator unavailable, Ceph cluster not ready, and permission denied as
  separate health conditions.

#### P3.3 — Implement the Ceph Dashboard client

- [ ] Require an explicitly configured least-privilege Ceph Dashboard credential Secret.
- [ ] Support service discovery or a fixed configured URL, but never an arbitrary request-supplied URL.
- [ ] Load a configured CA Secret; verify TLS by default; make insecure TLS an explicit warning-bearing
  development option.
- [ ] Authenticate server-side, cache JWTs only in memory, refresh before expiration, and retry once
  on authentication expiry.
- [ ] Apply per-request timeout, connection pool limits, response-size limits, and cancellation.
- [ ] Retry only idempotent GETs on narrowly classified transient failures with bounded backoff.
- [ ] Negotiate the documented Ceph API media type/version and reject unsupported responses cleanly.
- [ ] Redact authorization headers, credentials, tokens, and sensitive payload fields from logs/errors.
- [ ] Implement typed clients only for endpoints actually used by Highland.
- [ ] Add a circuit breaker/stale-cache policy so a failing dashboard cannot exhaust API goroutines.
- [ ] Mark data with `observedAt` and `stale` when serving a last-known read cache.

#### P3.4 — Implement Prometheus integration

- [ ] Reuse/generalize the current Prometheus parser only where direct scrape semantics are suitable.
- [ ] Prefer an operator-configured Prometheus query endpoint for Ceph time series; do not assume
  Highland can scrape every daemon directly.
- [ ] Use an allowlist of queries/metrics with bounded labels.
- [ ] Normalize read/write throughput, IOPS, latency, capacity, recovery/backfill, and health metrics.
- [ ] Handle absent Prometheus independently from CRD/API health; inventory must still work.
- [ ] Record query latency, failures, last success, and stale status.

#### P3.5 — Correlate Kubernetes and Ceph resources

- [ ] Claim known RBD and CephFS driver names through the provider registry.
- [ ] Read StorageClass `clusterID`, `pool`, `fsName`, and secret references without returning secret
  values.
- [ ] Use PV `volumeHandle` as the authoritative CSI-side identifier.
- [ ] Use documented provider metadata/API fields for RBD image correlation when available.
- [ ] Keep RBD and CephFS correlation strategies separate.
- [ ] Never query Ceph once per PVC; batch/list and index backend resources.
- [ ] Represent uncorrelated, ambiguous, stale, and deleted-backend states explicitly.
- [ ] Add reconciliation diagnostics that explain which correlation step failed without exposing
  credentials.

#### P3.6 — Build Ceph read-only API endpoints

- [ ] Implement provider summary and health.
- [ ] Implement paginated/read-only resource endpoints for pools, OSDs, filesystems, RBD images,
  quorum components, and mirroring status in the supported scope.
- [ ] Merge Rook desired state and Ceph runtime state without overwriting either source; include source
  and observation timestamps.
- [ ] Return `503` with retryable error metadata for live-only data when no safe stale result exists.
- [ ] Do not expose a generic pass-through endpoint to `/api` on the Ceph Dashboard.
- [ ] Add provider/version metadata to all Ceph responses for support diagnostics.

#### P3.7 — Build Ceph UI

- [ ] Add Rook/Ceph to Providers with a clear read-only badge.
- [ ] Add health overview: overall health, quorum, OSD up/in, pool capacity, PG summary, active warnings,
  and data-source freshness.
- [ ] Add OSD list/detail with host, device/class when available, state, utilization, and relevant
  health messages.
- [ ] Add pools list/detail combining `CephBlockPool` desired state with runtime usage/health.
- [ ] Add RBD and CephFS views tied to common StorageClasses and claims.
- [ ] Add read-only mirroring status when configured.
- [ ] Link to the native Ceph Dashboard for unsupported deep administration when an operator-provided
  safe URL is configured.
- [ ] Never render credentials, secret names unnecessarily, raw command output, or unbounded raw JSON.
- [ ] Make stale, partial, permission-denied, and unsupported-version states prominent.

#### P3.8 — Helm, RBAC, credentials, and networking

- [ ] Add opt-in Rook/Ceph provider values; default remains disabled.
- [ ] Add narrowly scoped read/list/watch RBAC in the configured Rook namespace.
- [ ] Add core storage read permissions already required by Phase 1.
- [ ] Mount or reference a named Ceph Dashboard credential Secret and optional CA Secret.
- [ ] Do not grant Highland permission to list every Secret in `rook-ceph`.
- [ ] Add NetworkPolicy egress only to configured dashboard/Prometheus Services and ports.
- [ ] Add `NOTES.txt` and installation documentation for creating a least-privilege dashboard account.
- [ ] Add startup/config validation for incomplete Ceph settings.

#### P3.9 — Security review

- [ ] Threat-model SSRF, credential exposure, excessive Ceph privileges, compromised dashboard
  responses, large payloads, stale health, and cross-namespace data visibility.
- [ ] Verify TLS and Secret handling with an independent checklist/reviewer.
- [ ] Fuzz or property-test Ceph JSON decoding and error-envelope redaction.
- [ ] Confirm no Phase 3 Ceph route accepts POST, PUT, PATCH, or DELETE.
- [ ] Add tests proving operator/viewer/admin all receive the same read-only Ceph capability set except
  for normal page visibility policy.

### 9.4 Phase 3 definition of done

- [ ] A supported Rook/Ceph cluster appears as one `managed`, read-only provider owning RBD and CephFS
  drivers.
- [ ] Highland shows accurate cluster health, quorum, OSDs, pools, RBD/CephFS inventory, capacity,
  and supported metrics with source freshness.
- [ ] Kubernetes claims link to Ceph backend resources only when correlation is authoritative.
- [ ] Ceph Dashboard or Prometheus outage does not break common Kubernetes inventory or Longhorn.
- [ ] No Ceph write capability or generic Ceph proxy is exposed.
- [ ] Credentials never appear in API responses, logs, audit entries, browser storage, or support
  fixtures.
- [ ] Supported-version contract suites pass for the declared matrix.
- [ ] Read-only install, upgrade, disable, and uninstall procedures are documented.

### 9.5 Phase 3 tests and validation

#### Unit/contract tests

- Rook CRD decoding across supported fixtures and unknown additive fields;
- health severity mapping and source-freshness rules;
- Dashboard JWT login, cache, refresh, expiration, 401 retry, and logout behavior;
- TLS validation, custom CA, timeout, oversized response, malformed JSON, and redaction;
- API media/version negotiation and unsupported version response;
- RBD and CephFS correlation, including ambiguous/no-match cases;
- batching guarantees that prevent N+1 backend requests;
- Prometheus absent, slow, malformed, and stale cases.

#### Live read-only validation

1. install the pinned Rook operator and Ceph cluster in the disposable three-node environment;
2. create an RBD pool/Class/PVC/Pod and a CephFS filesystem/Class/RWX PVC/Pods outside Highland;
3. create snapshots/restores outside Highland and verify common inventory;
4. compare Highland health/capacity/counts with Rook status and Ceph Dashboard;
5. stop/restore one OSD in the scratch cluster and verify degraded/recovery transitions;
6. expire/rotate the dashboard credential and verify token recovery and redaction;
7. block Dashboard and Prometheus separately and verify partial/stale behavior;
8. restart Rook operator and Highland API replicas and verify watch/cache recovery;
9. run with a read permission removed and verify explicit partial status;
10. confirm every Ceph mutation request receives `404`/`405` and no backend change occurs.

## 10. Phase 4 — Safe generic and Rook/Ceph workflows

### 10.1 Objective and reasoning

Introduce writes only through typed, policy-controlled workflows. For the first writable Ceph
release, Highland writes Kubernetes resources and Rook CRDs; the Ceph Dashboard API remains read-only
and is used for preflight/postflight verification. This preserves declarative ownership and avoids a
second imperative source of truth.

### 10.2 Initial action matrix

| Action | API mechanism | Minimum Highland role | Confirmation | Default |
|---|---|---:|---|---|
| Create PVC | Kubernetes PVC | operator | summary | enabled when storage writes opt in |
| Expand PVC | Kubernetes PVC patch | operator | old/new size | enabled when supported |
| Create snapshot | VolumeSnapshot | operator | summary | enabled when supported |
| Restore/clone to new PVC | PVC dataSource | operator | summary | enabled when supported |
| Delete snapshot | VolumeSnapshot delete | operator | typed name if retained backend risk | enabled when supported |
| Delete PVC | PVC delete | admin | typed namespace/name + reclaim warning | enabled when storage writes opt in |
| Create Ceph RBD StorageClass | StorageClass | admin | rendered plan | Ceph writes opt in |
| Create CephFS StorageClass for existing filesystem | StorageClass | admin | rendered plan | Ceph writes opt in |
| Create replicated `CephBlockPool` | Rook CRD | admin | rendered plan + health checks | Ceph writes opt in |
| Delete StorageClass | StorageClass delete | admin | typed name + dependency list | disabled unless explicitly enabled |
| Delete empty `CephBlockPool` | Rook CRD delete | admin | typed name + two-stage checks | disabled unless explicitly enabled |

Not included: erasure-coded pool creation, CephFilesystem creation/deletion, OSD/MON/MGR actions,
PG repair, mirroring mutations, raw commands, upgrades, purge, or direct RBD image deletion.

### 10.3 Phase 4 tasks

#### P4.1 — Define action policy and threat model

- [ ] Add a machine-readable action registry containing capability, role, provider, risk level,
  required confirmation, feature flag, preflight checks, and audit action name.
- [ ] Replace method-only authorization for new storage routes with action-level authorization.
- [ ] Retain method-level viewer protection as defense in depth.
- [ ] Define operator versus admin permissions and document why destructive/infrastructure actions are
  admin-only.
- [ ] Define namespace allowlist enforcement for namespaced writes.
- [ ] Define resource quota, LimitRange, admission-policy, and Pod Security failure handling.
- [ ] Threat-model confused-deputy access, replay, stale preflight, concurrent mutation, CSRF,
  credential leakage, and operation takeover across API replicas.

#### P4.2 — Add durable `StorageOperation` state

- [ ] Add a versioned `storageoperations.highland.io/v1alpha1` CRD, or document an equivalently durable
  Kubernetes-backed operation record before implementation starts. An in-memory-only store is not
  acceptable.
- [ ] Store immutable requested action/target/parameter hash/requester metadata and mutable status,
  conditions, timestamps, target UIDs, retries, and result references.
- [ ] Never store provider credentials, Secret values, CSRF tokens, or reusable confirmation secrets.
- [ ] Return `202 Accepted` with an operation ID for asynchronous workflows.
- [ ] Implement one leader-elected operation reconciler across API replicas using Kubernetes Leases.
- [ ] Make every reconciliation step idempotent and safe under at-least-once execution.
- [ ] Use Kubernetes server-side apply with a Highland field manager for created/managed resources.
- [ ] Use `resourceVersion`/UID preconditions for updates and deletes.
- [ ] Persist terminal failure reasons and bounded sanitized diagnostics.
- [ ] Add operation retention/garbage collection that never removes audit records.
- [ ] Add cancel only for operations with a defined safe cancellation point.
- [ ] Document CRD installation/upgrade policy and rollback compatibility before shipping the chart.

#### P4.3 — Implement planning, dry-run, and confirmation

- [ ] Add a plan endpoint or `dryRun=true` mode that returns intended resources, dependency checks,
  warnings, capability decisions, and estimated blast radius without mutation.
- [ ] Use Kubernetes server-side dry-run for Kubernetes/Rook resources where supported.
- [ ] Generate a short-lived confirmation challenge bound to user, action, provider, target UID,
  resourceVersion, and plan hash.
- [ ] Require a typed resource name for destructive actions in addition to the challenge.
- [ ] Expire the challenge after a short documented interval and invalidate it when dependencies or
  resourceVersion change.
- [ ] Re-run all preflight checks inside the reconciler immediately before mutation; browser-side
  checks are advisory only.

#### P4.4 — Implement common Kubernetes lifecycle workflows

- [ ] Create PVC from an existing StorageClass with validated quantity, access mode, volume mode, and
  optional allowed data source.
- [ ] Expand PVC only when the StorageClass permits expansion, requested size increases, quota permits,
  and no incompatible operation is active.
- [ ] Create VolumeSnapshot only with a matching SnapshotClass and source-state checks.
- [ ] Restore/clone into a new PVC using the standard data source fields and driver/provider match.
- [ ] Delete snapshot with deletion-policy/retained-backend warnings.
- [ ] Delete PVC only after showing workloads, StatefulSet ownership, attachments, reclaim policy,
  finalizers, snapshots, and backend correlation.
- [ ] Refuse deletion of a PVC still referenced by a live workload unless a separately designed force
  workflow is added later.
- [ ] Watch the resulting Kubernetes resource to terminal success/failure and update StorageOperation.
- [ ] Do not treat HTTP request completion as storage operation completion.

#### P4.5 — Implement safe Ceph StorageClass workflows

- [ ] Create RBD StorageClasses only for a discovered ready `CephBlockPool` in the configured provider.
- [ ] Create CephFS StorageClasses only for a discovered ready existing `CephFilesystem` and valid
  subvolume-group configuration.
- [ ] Discover/reference the Rook-managed CSI provisioner and node-stage Secret names without exposing
  values.
- [ ] Validate reclaim policy, binding mode, expansion setting, mount options, filesystem, encryption
  options, and topology against an allowlist.
- [ ] Block duplicate class names and conflicting default-class annotations.
- [ ] Limit default-class changes to an explicit separate plan showing the existing defaults.
- [ ] On StorageClass deletion, block when PVC/PV dependencies exist. A StorageClass with `Retain` PVs
  requires a stronger warning even when no active PVC remains.

#### P4.6 — Implement replicated CephBlockPool creation

- [ ] Support a deliberately constrained schema: namespace, name, replicated size, failure domain,
  device class when validated, compression mode from an allowlist, and safe deletion policy options.
- [ ] Require the target `CephCluster` to be ready and reject creation during `HEALTH_ERR`; define and
  document which `HEALTH_WARN` codes block or merely warn.
- [ ] Validate replica size against available failure domains and configured minimum safety policy.
- [ ] Default to a replica count and failure domain recommended by the supported Rook/Ceph profile;
  never silently downgrade redundancy to make a request fit.
- [ ] Use server-side dry-run, then create the Rook CRD and watch Rook conditions.
- [ ] Verify the pool through read-only Ceph runtime data before marking the operation succeeded.
- [ ] If creation fails before the pool becomes usable and no dependent resource exists, offer an
  explicit cleanup operation; do not automatically delete an uncertain backend resource.

#### P4.7 — Implement guarded Ceph deletion

- [ ] Keep pool deletion disabled by default behind `allowPoolDelete`.
- [ ] Require admin role, fresh provider health, typed name, confirmation challenge, and a second
  server-side preflight immediately before deletion.
- [ ] Block deletion when referenced by any StorageClass, PVC/PV, snapshot, RBD image, CephFS data pool,
  mirroring configuration, or other discovered Rook resource.
- [ ] Treat inability to prove emptiness as a hard block, not a warning.
- [ ] Use UID/resourceVersion preconditions and the Rook CRD deletion path.
- [ ] Track finalizers and deletion progress; surface blocked finalizers without recommending force
  removal automatically.
- [ ] Verify backend absence through read-only Ceph data before succeeding.
- [ ] Never expose or automate Rook cluster cleanup/purge confirmation in this phase.

#### P4.8 — Operation UI and UX safety

- [ ] Add a review screen showing provider, cluster, namespace, target, rendered changes, dependencies,
  reclaim/deletion policies, warnings, and required role.
- [ ] Separate warning acknowledgement from typed destructive confirmation.
- [ ] Show the operation ID immediately and a durable progress/timeline view.
- [ ] Allow navigation away without losing operation visibility.
- [ ] Disable duplicate submission while an equivalent nonterminal operation exists.
- [ ] Display authoritative server-side failure codes and remediation; do not replace them with generic
  toast-only errors.
- [ ] Add Operations history filtered by provider/user/action/state.
- [ ] Display audit linkage and target resource links without revealing sensitive request fields.
- [ ] Keep all unavailable/unsafe actions absent or disabled with an explanation derived from
  capabilities and preflight.

#### P4.9 — Audit and observability

- [ ] Extend audit records with operation ID, action ID, provider ID/kind, target kind/namespace/name/UID,
  plan hash, result, and HTTP/Kubernetes/Ceph correlation IDs.
- [ ] Record plan, approval, execution start, terminal result, and denial as distinct events.
- [ ] Sanitize all request/response details using allowlists.
- [ ] Add metrics:
  - `highland_storage_operations_total{provider,action,result}`;
  - `highland_storage_operation_duration_seconds{provider,action}`;
  - `highland_storage_operations_in_progress{provider,action}`;
  - `highland_storage_operation_retries_total{provider,reason}`;
  - `highland_storage_preflight_denials_total{provider,reason}`.
- [ ] Add alerts for stuck operations, repeated reconciliation failure, leader absence, and Ceph
  postflight mismatch.

#### P4.10 — Helm, RBAC, and feature gates

- [ ] Keep `storage.writes.enabled=false` and `providers.rookCeph.writes.enabled=false` by default for
  the first release containing write code.
- [ ] Keep StorageClass deletion behind a separate `allowStorageClassDelete` gate so enabling Ceph
  creation workflows does not implicitly grant cluster-scoped delete permission.
- [ ] Split chart RBAC into read core, namespaced storage writer, cluster storage-class writer, Rook
  pool writer, operation CRD writer, and leader-election permissions.
- [ ] Render only the roles needed for enabled actions.
- [ ] Do not grant wildcard writes to `ceph.rook.io`, core resources, or Secrets.
- [ ] Add chart tests proving read-only mode has no mutation verbs.
- [ ] Add NetworkPolicy only for existing read endpoints; Phase 4 does not need arbitrary Ceph API
  write egress because dashboard writes remain disabled.
- [ ] Add upgrade and emergency-disable runbooks. Disabling writes stops new submissions but must let
  an administrator observe existing operations and safely finish/recover them.

### 10.4 Phase 4 definition of done

- [ ] Only actions in the approved matrix are exposed.
- [ ] All new writes return durable operation IDs and survive API pod restart and leader change.
- [ ] Viewer, operator, and admin permissions match the action registry in both UI and backend tests.
- [ ] CSRF, action authorization, namespace scope, confirmation, fresh preflight, UID/resourceVersion
  preconditions, audit, and metrics protect every write path.
- [ ] Generic PVC create/expand/delete, snapshot/create/delete, restore, and clone pass against the
  generic CSI test driver and supported Longhorn/Ceph CSI drivers where capabilities exist.
- [ ] Safe RBD/CephFS StorageClass creation and replicated CephBlockPool creation pass on the supported
  Rook/Ceph matrix.
- [ ] Pool/StorageClass deletion is blocked whenever Highland cannot prove it is safe.
- [ ] The Ceph Dashboard integration remains read-only.
- [ ] Feature gates and Helm RBAC default to read-only.
- [ ] Operation recovery, audit export, rollback constraints, and emergency-disable procedures are
  documented and validated.

### 10.5 Phase 4 tests and validation

#### Authorization and security tests

- every action × role × namespace-scope combination;
- CSRF missing/invalid/replayed;
- expired confirmation, changed resourceVersion, changed dependency graph, and different user;
- forged provider/target IDs and path traversal;
- Secret/token redaction in logs, audit, operation status, metrics, and browser responses;
- chart RBAC assertions preventing wildcard mutation and read-only mode mutation;
- SSRF tests proving request bodies cannot override provider endpoints.

#### Operation-controller tests

- leader election with two API replicas;
- duplicate request/idempotency behavior;
- restart after each reconciliation step;
- transient Kubernetes API errors, watch closure, timeout, and conflict retry;
- terminal versus retryable classification;
- cancellation at allowed and disallowed points;
- operation retention and garbage collection;
- target deletion/recreation with the same name but different UID;
- safe behavior when the feature gate is disabled while operations exist.

#### Generic lifecycle live tests

For each verified CSI profile:

1. create PVC and mount it in a workload;
2. write a checksum fixture;
3. expand and verify filesystem/device size;
4. snapshot and restore to a new claim;
5. clone where supported;
6. mount restored/clone claims and verify data checksum;
7. prove delete is blocked while a live workload references the claim;
8. remove workload, delete claim, and verify reclaim-policy result;
9. verify operation timeline, audit, events, and metrics;
10. restart Highland during long-running steps and verify recovery.

#### Ceph workflow live tests

1. begin with a healthy supported Rook/Ceph cluster;
2. dry-run and create a replicated `CephBlockPool`;
3. watch Rook reconciliation and verify runtime pool existence;
4. create an RBD StorageClass, PVC, workload, snapshot, restore, expansion, and checksum validation;
5. create a CephFS StorageClass for an existing filesystem and verify RWX workload behavior;
6. attempt duplicate/default-class conflicts and verify hard failure;
7. attempt pool deletion while a StorageClass, PVC/PV, snapshot, or RBD image exists and verify block;
8. remove dependencies, enable the explicit deletion gate, delete the empty scratch pool, and verify
   Rook plus Ceph runtime absence;
9. introduce `HEALTH_WARN` and `HEALTH_ERR` scenarios and verify the documented policy;
10. stop the Ceph Dashboard after CRD submission and verify the operation waits/fails safely rather
    than reporting an unverified success;
11. rotate credentials and restart the elected Highland API during an operation;
12. confirm no native Ceph write endpoint was called.

## 11. Phase 5 — Kubernetes/Ceph context and native-dashboard integration

### 11.1 Objective and reasoning

Make Highland the best place to understand and safely operate Ceph-backed Kubernetes storage without
trying to replace the native Ceph Dashboard.

The Ceph Dashboard already owns deep Ceph administration: OSD/MON/MGR operations, pools and placement,
RBD image internals, CephFS administration, mirroring, configuration, recovery, logs, users/roles,
RGW, NFS, and backend-specific tuning. Highland's differentiated value is the context between
Kubernetes consumption, Rook desired state, CSI behavior, Ceph runtime state, and organization-level
policy.

Phase 5 therefore adds:

1. a secure, explicit handoff to the native Ceph Dashboard;
2. an authoritative Kubernetes-to-backend resource graph;
3. Rook desired-state versus Ceph runtime drift detection;
4. workload-aware impact analysis;
5. a unified Kubernetes/Rook/Ceph event and operation timeline;
6. capacity ownership and forecasting with clearly separated logical and physical measurements;
7. guided remediation and native-dashboard deep links;
8. provider-neutral comparison across Longhorn, Rook/Ceph, and verified CSI drivers.

Phase 5 does not add new direct Ceph mutation endpoints. Phase 4 remains the only Highland Ceph write
surface, and those writes continue to use Kubernetes resources and Rook CRDs. The Ceph Dashboard API
remains read-only from Highland.

### 11.2 Product boundary

| Concern | Highland owns | Native Ceph Dashboard owns |
|---|---|---|
| Kubernetes consumption | StorageClasses, PVCs, PVs, snapshots, attachments, Pods, workload owners | Not authoritative |
| Rook orchestration | Desired CRDs, reconciliation conditions, supported typed workflows | Runtime display where the Rook manager module exposes it |
| Ceph runtime | Curated read-only health, capacity, image/subvolume identity, and verification | Full Ceph inspection and administration |
| Safety | dependency graph, impact analysis, action policy, confirmation, audit, durable operations | Ceph-native authorization and operation safety |
| Multi-provider view | Longhorn, Ceph, and generic CSI comparison | Ceph only |
| Deep administration | outbound handoff and guided context | OSD/MON/MGR, PGs, CRUSH, repair, upgrades, mirroring, RGW, NFS, tuning |
| Identity | Highland roles for Highland workflows | separate Ceph Dashboard users, roles, and SSO |

Highland must never imply that a link to the native dashboard grants access. The browser authenticates
to Ceph independently, and Ceph's authorization remains authoritative for actions performed there.

### 11.3 Phase 5 tasks

#### P5.1 — Secure native Ceph Dashboard handoff

- [ ] Add and document `providers.rookCeph.dashboard.publicUrl` separately from the private
  server-to-server Dashboard API URL.
- [ ] Validate the public URL as an absolute `https` URL by default. Permit plain HTTP only behind an
  explicit disposable-lab setting and render a visible warning.
- [ ] Reject URL userinfo, `javascript:`, `data:`, protocol-relative values, malformed hosts, and
  fragments/query values that could carry credentials.
- [ ] Never make server-side requests to `publicUrl`; this prevents the browser-link setting from
  becoming an SSRF input.
- [ ] Document secure exposure using an operator-managed Ingress, Gateway, LoadBalancer, or NodePort
  with valid TLS and a stable operator-facing name.
- [ ] Keep the private Highland Dashboard reader and human Dashboard login identities separate.
  Never use Rook's generated administrator password as Highland's backend credential.
- [ ] Show **Open Ceph Dashboard** on the provider overview and supported Ceph resource detail pages.
- [ ] Open the dashboard in a new tab with `noopener`/`noreferrer`; do not embed it in an iframe or
  loosen Highland's CSP to accommodate embedding.
- [ ] Add a compatibility-versioned deep-link registry only for Ceph Dashboard routes verified in the
  declared matrix. Fall back to the configured root URL whenever a route is unknown or unstable.
- [ ] Do not place resource names, namespaces, IDs, tokens, or other sensitive context into a deep-link
  query unless the target route and encoding are explicitly tested and allowlisted.
- [ ] Display native-dashboard availability using the private read client/provider health, not by
  probing the public browser URL from the Highland API.
- [ ] Document Ceph Dashboard SSO as an operator option. Highland does not broker or mint Ceph
  Dashboard sessions in this phase.

#### P5.2 — Build the authoritative storage resource graph

- [ ] Define versioned graph node types for provider, CSI driver, StorageClass, PVC, PV,
  VolumeAttachment, VolumeSnapshot, Pod, workload owner, node, RBD image, CephFS subvolume,
  CephBlockPool, CephFilesystem, OSD/topology group, and Highland StorageOperation.
- [ ] Define versioned edge types such as `uses`, `binds`, `provisions`, `attaches`, `mounted-by`,
  `owned-by`, `backed-by`, `belongs-to-pool`, `belongs-to-filesystem`, `managed-by`, and
  `affected-by`.
- [ ] Include edge evidence, source, observation time, freshness, and confidence:
  - `authoritative`: exact Kubernetes UID, CSI handle, or documented backend identifier;
  - `derived`: deterministic relationship from authoritative objects;
  - `potential`: topology/health relationship that cannot prove direct data placement;
  - `unknown`: no safe correlation is available.
- [ ] Build informer and provider indexes once per observation cycle. Never perform a Ceph request per
  PVC, PV, Pod, or table row.
- [ ] Correlate RBD using documented CSI volume handles and Ceph image identifiers only. Do not infer
  identity from display names.
- [ ] Correlate CephFS claims using documented filesystem/subvolume metadata only; keep RBD and CephFS
  strategies separate.
- [ ] Correlate Pods to workload owners through owner references and expose unresolved/broken chains
  explicitly.
- [ ] Add bounded graph APIs:
  - `GET /api/v1/storage/relationships`;
  - `GET /api/v1/storage/resources/{kind}/{id}/relationships`;
  - `GET /api/v1/providers/{providerId}/relationships`.
- [ ] Require provider, namespace, kind, depth, and page-size bounds. Reject unrestricted whole-cluster
  graph expansion.
- [ ] Preserve cluster/namespace scope and role-based visibility on every node and edge.
- [ ] Surface ambiguous, stale, or partial relationships instead of silently omitting uncertainty.

#### P5.3 — Detect Rook desired-state versus Ceph runtime drift

- [ ] Define drift categories for missing runtime resource, unexpected runtime resource, Rook not
  Ready, spec/status divergence, reconciliation stalled, version unsupported, runtime stale, and
  post-operation verification mismatch.
- [ ] Compare each supported `CephBlockPool`, `CephFilesystem`, `CephRBDMirror`, and `CephCluster`
  desired/status view with bounded Ceph runtime evidence.
- [ ] Keep desired and runtime fields separate in API responses; never overwrite one authority with
  the other.
- [ ] Record first observed, last observed, duration, severity, and whether the drift is actionable in
  Highland or requires native Ceph/Rook administration.
- [ ] Suppress expected reconciliation windows using documented, bounded grace periods without
  hiding terminal or repeatedly failing conditions.
- [ ] Link drift records to relevant Kubernetes dependencies, Rook conditions, Ceph health evidence,
  StorageOperations, and audit events.
- [ ] Add `GET /api/v1/providers/{providerId}/drift` and a provider drift summary.
- [ ] Do not offer automatic repair for unsupported native Ceph operations.

#### P5.4 — Add workload-aware impact analysis

- [ ] Add **What depends on this?** and **What backs this?** views for common and Ceph resources.
- [ ] For a StorageClass, pool, filesystem, image/subvolume, PVC, PV, or attachment, list confirmed
  dependent namespaces, workloads, Pods, snapshots, and operations.
- [ ] Separate `confirmed`, `potential`, and `unknown` impact. An OSD or PG condition must not be
  presented as confirmed workload impact unless Ceph provides authoritative placement evidence.
- [ ] Summarize affected requested capacity, provisioned capacity, workload count, namespace count,
  access modes, attachment state, and reclaim policy.
- [ ] Add a read-only impact endpoint suitable for both incident response and Phase 4 preflight:
  `GET /api/v1/storage/impact?provider=...&kind=...&id=...`.
- [ ] Reuse the same dependency engine for UI impact views and destructive operation checks so the
  two surfaces cannot disagree.
- [ ] Include freshness and incomplete-source conditions in every impact result.
- [ ] Fail destructive plans closed when required impact sources are unavailable.

#### P5.5 — Create a unified storage timeline

- [ ] Normalize Kubernetes Events, Rook conditions/transitions, Ceph health changes, Highland provider
  errors, StorageOperation phases, audit entries, and credential/configuration changes into one
  bounded timeline model.
- [ ] Add provider, namespace, workload, resource, severity, source, action, and time filters.
- [ ] Correctly scope provider-filtered Kubernetes Events through authoritative involved-object
  correlation; do not show unrelated cluster events merely because they are storage-shaped.
- [ ] Deduplicate repeated Kubernetes Events while preserving count and first/last occurrence.
- [ ] Preserve source timestamps and annotate clock skew or unknown ordering.
- [ ] Link timeline entries back to the resource graph, operation detail, Rook object, or native Ceph
  Dashboard when appropriate.
- [ ] Define retention separately for transient events, durable operations, and audit history.
- [ ] Never copy unbounded Ceph logs into Highland. Link to native log administration for deep
  investigation.

#### P5.6 — Add capacity ownership and forecasting

- [ ] Keep the following measurements distinct:
  - PVC requested capacity;
  - PV provisioned capacity;
  - backend logical image/subvolume size;
  - backend allocated/used bytes;
  - pool usable/raw capacity;
  - physical cluster raw capacity.
- [ ] Attribute requested/provisioned capacity by provider, driver, StorageClass, namespace, workload,
  reclaim policy, pool, and filesystem where relationships are authoritative.
- [ ] Explain thin provisioning, replica/erasure overhead, snapshots/clones, compression, metadata,
  and raw-versus-usable differences; never sum unlike measures into one misleading total.
- [ ] Add capacity pressure thresholds and headroom policy per provider/pool/StorageClass.
- [ ] Add forecasting only when a sufficiently fresh Prometheus history is available. Include sample
  window, confidence, missing-data conditions, and the distinction between trend and guarantee.
- [ ] Do not fabricate a forecast from a single Dashboard observation.
- [ ] Add `GET /api/v1/storage/capacity/ownership` and
  `GET /api/v1/providers/{providerId}/capacity/forecast`.
- [ ] Bound metric query ranges, resolution, cardinality, and cache size.

#### P5.7 — Add guided remediation and native handoff

- [ ] Define machine-readable remediation records with condition code, explanation, Highland-safe
  action, native-dashboard destination, runbook, prerequisites, and escalation level.
- [ ] Prefer safe Highland workflows when Phase 4 explicitly supports the action.
- [ ] For native Ceph actions, explain why Highland cannot perform the operation and link to the
  appropriate Ceph Dashboard area or runbook.
- [ ] Never provide a raw Ceph command shell or automatically execute copied commands.
- [ ] Avoid suggesting force deletion, finalizer removal, cluster purge, or destructive repair as an
  ordinary remediation.
- [ ] Require compatibility-specific review for deep links and remediation text that depends on Ceph
  or Rook version behavior.
- [ ] Show the evidence and freshness behind each recommendation.

#### P5.8 — Add provider-neutral comparison and placement guidance

- [ ] Compare providers and StorageClasses using capability, topology, access mode, expansion,
  snapshot/clone support, reclaim policy, observed health, capacity headroom, and benchmark context.
- [ ] Do not collapse provider health into one opaque score. Show the contributing facts and
  unavailable data.
- [ ] Keep backend-specific metrics semantically distinct when direct comparison is invalid.
- [ ] Allow administrators to define placement policies such as required access mode, topology,
  snapshot support, encryption requirement, minimum headroom, or verified support level.
- [ ] Produce recommendations as read-only guidance in Phase 5. Automatic workload/data migration
  remains outside this plan.
- [ ] Include the exact tested provider/driver/version profile behind a recommendation.

#### P5.9 — Build the context-first UI

- [ ] Add a provider overview section for:
  - Kubernetes consumers;
  - backend resources;
  - active drift;
  - impacted workloads;
  - capacity ownership/headroom;
  - recent timeline;
  - native-dashboard handoff.
- [ ] Add a relationship panel to PVC, PV, StorageClass, snapshot, pool, filesystem, OSD, RBD image,
  CephFS subvolume, and operation detail pages.
- [ ] Use a compact graph only when it clarifies relationships; always provide an accessible tabular
  representation.
- [ ] Add **What depends on this?** and **What backs this?** actions consistently.
- [ ] Display source, confidence, freshness, and partial-data conditions near relationships and
  impact results.
- [ ] Keep native-dashboard links visually distinct from Highland actions and label that a separate
  login/authorization boundary applies.
- [ ] Preserve provider workspace navigation while retaining common inventory and administration.
- [ ] Keep unsupported native Ceph capabilities discoverable through explanation/linking rather than
  adding disabled replicas of every Ceph Dashboard control.

#### P5.10 — Security, privacy, and operability

- [ ] Threat-model public links, deep-link injection, cross-origin navigation, compromised native
  dashboards, identity confusion, stale correlation, tenant information leakage, and impact-query
  amplification.
- [ ] Apply namespace scope before graph construction is returned; filtering only in the browser is
  unacceptable.
- [ ] Ensure graph/timeline/capacity labels cannot expose Secret names or values beyond existing
  allowlisted metadata policy.
- [ ] Add rate limits and query-cost bounds for graph expansion, timeline searches, impact analysis,
  and metric forecasts.
- [ ] Add metrics for graph build duration, unresolved correlations, drift duration, impact query
  failures, stale sources, deep-link fallbacks, and forecast data sufficiency.
- [ ] Add alerts for sustained authoritative drift, graph rebuild failure, high unresolved-correlation
  rates, and provider data freshness violations.
- [ ] Document behavior when Ceph Dashboard, Prometheus, Rook operator, Kubernetes Events, or snapshot
  APIs are unavailable independently.
- [ ] Confirm an optional native-dashboard outage never breaks common inventory, resource graph data
  already available from Kubernetes/Rook, Longhorn, or Phase 4 operation recovery.

### 11.4 Phase 5 definition of done

- [ ] A securely configured **Open Ceph Dashboard** action is available from the Ceph provider and
  supported resource details, with no credential/session forwarding or iframe embedding.
- [ ] Users can navigate the authoritative chain from workload/PVC/PV/StorageClass to RBD image or
  CephFS subvolume and its pool/filesystem, and back to Kubernetes consumers.
- [ ] Every relationship includes source, freshness, and confidence; ambiguous identity is never
  presented as authoritative.
- [ ] Highland reports Rook/Ceph desired-versus-runtime drift and links it to affected Kubernetes
  consumers when provable.
- [ ] Impact analysis answers **what depends on this** for supported resources and fails closed for
  destructive Phase 4 preflight when required evidence is missing.
- [ ] Provider-scoped timelines exclude unrelated storage events and combine Kubernetes, Rook, Ceph,
  Highland operation, and audit evidence.
- [ ] Capacity views distinguish requested, provisioned, logical, allocated, usable, and raw values.
- [ ] Forecasts appear only with adequate fresh time-series evidence and disclose confidence/window.
- [ ] Remediation consistently chooses a safe Highland workflow, native Ceph handoff, or documented
  no-action/escalation state.
- [ ] Ceph deep administration remains in the native dashboard; no new generic Ceph write proxy,
  command console, repair automation, or credential brokerage is introduced.
- [ ] Longhorn and generic CSI providers can use the same relationship, impact, timeline, capacity,
  and comparison contracts without becoming Ceph-shaped.

### 11.5 Phase 5 tests and validation

#### Contract and property tests

- graph node/edge schema versioning, pagination, depth limits, and deterministic IDs;
- authoritative/derived/potential/unknown confidence propagation;
- RBD and CephFS correlation fixtures across the declared version matrix;
- ambiguous, missing, malformed, duplicated, stale, and additive provider data;
- sensitive-field redaction in nodes, edges, events, capacity dimensions, remediation, and links;
- public URL and deep-link validation against scheme confusion, userinfo, control characters,
  traversal, encoded delimiters, and credential-bearing queries;
- proof that `publicUrl` is never used for a server-side request.

#### API and authorization tests

- every graph/impact/timeline/capacity endpoint across admin/operator/viewer and namespace scope;
- provider filter correctness, including unrelated Longhorn/Ceph/local-path events;
- query-cost, page-size, time-range, and graph-depth rejection;
- optional-source outage combinations and partial-condition envelopes;
- Phase 4 destructive preflight uses the same dependency result as the Phase 5 impact API;
- no Phase 5 endpoint accepts a provider mutation or arbitrary Ceph Dashboard path.

#### Browser and accessibility tests

- provider overview and all relationship-enabled detail pages;
- native-dashboard root link, verified deep link, and compatibility fallback;
- separate-security-boundary labeling and safe new-tab attributes;
- graph/table keyboard navigation, screen-reader labels, large inventories, empty/partial states, and
  reduced-motion behavior;
- no iframe, credential-bearing URL, token, raw Ceph command console, or generic backend action.

#### Live Rook/Ceph validation

1. expose the native Ceph Dashboard through a TLS-protected operator-facing URL;
2. verify Highland links to it without forwarding the backend reader identity;
3. create RBD and CephFS workloads in multiple namespaces and verify forward/backward graph traversal;
4. compare graph identifiers with Kubernetes objects, CSI volume handles, Rook CRDs, and Ceph runtime;
5. introduce a Rook reconciliation delay and a runtime-only/missing resource fixture and verify drift;
6. introduce pool/OSD/filesystem health scenarios and verify confirmed versus potential impact;
7. generate Kubernetes scheduling, CSI attachment, Rook, Ceph health, and Highland operation events
   and verify timeline attribution/deduplication;
8. compare requested, provisioned, image/subvolume, pool, and raw capacity without double counting;
9. collect sufficient Prometheus history and validate forecast window/confidence, then remove
   Prometheus and verify explicit unavailable state;
10. stop the public dashboard endpoint while keeping the private reader healthy, then invert the
    failure, and verify the two trust paths degrade independently;
11. rotate Ceph Dashboard human and Highland reader credentials independently;
12. verify unsupported native actions consistently link/explain rather than appearing as Highland
    mutations.

## 12. Cross-phase quality gates

### 12.1 Pull-request CI

Required on every PR:

- `go test ./... -count=1` and Go build;
- frontend typecheck, unit tests, build, and Storybook build;
- Playwright mock-manager Longhorn regression suite;
- common storage API contract tests;
- sanitized Rook/Ceph fixture contract tests;
- chart dependency build, lint, and render for:
  - legacy Longhorn-only;
  - universal read-only;
  - namespace-scoped read-only;
  - Longhorn disabled;
  - Ceph read-only;
  - write code present but gates disabled;
  - explicitly enabled write RBAC;
- parity-matrix gate;
- static checks for forbidden wildcard RBAC and accidental Secret rendering;
- accessibility smoke for every new top-level page.

### 12.2 Nightly CI

- ephemeral Kubernetes + CSI host-path + snapshot controller lifecycle suite;
- supported Longhorn live smoke on suitable VM-backed runners;
- supported Rook/Ceph live read suite on a disposable three-node storage environment;
- operation restart/leader-election suite after Phase 4;
- relationship/drift/impact fixture suite and provider-filter correctness after Phase 5;
- synthetic scale suite with 10,000 PVCs, 10,000 PVs, 1,000 attachments, 1,000 snapshots, and a
  representative event load;
- dependency/API compatibility scan against the declared matrix.

### 12.3 Release qualification

- full current/previous supported Longhorn matrix;
- full declared Rook/Ceph matrix;
- native Ceph Dashboard public-link/root/deep-link compatibility and security validation;
- Kubernetes-to-Ceph relationship, drift, impact, timeline, and capacity ownership validation;
- clean install, upgrade from the prior Highland release, rollback, disable-provider, credential
  rotation, and uninstall validation;
- backup/restore of Highland-owned operation/audit state where persistence is enabled;
- security checklist and threat-model delta review;
- no unresolved critical/high vulnerabilities in newly introduced runtime dependencies/images;
- documentation dry run by an operator who did not implement the feature.

### 12.4 Performance and resilience targets

- warm paginated list API p95 < 500 ms on the reference 10,000-object dataset;
- provider health endpoints p95 < 2 s when upstream is healthy and never wait indefinitely;
- no N+1 upstream call per displayed resource;
- bounded graph expansion and no per-node/per-edge provider request;
- bounded provider response bodies, caches, queues, retry counts, and label cardinality;
- browser interaction remains responsive on maximum tested page size;
- optional provider outage does not make unrelated providers or common cached inventory unavailable;
- watch disconnect, API restart, and elected-controller change recover without duplicate destructive
  effects.

## 13. Validation environments

| Environment | Purpose | Persistent data |
|---|---|---|
| Go fake clients / HTTP mocks | Fast handler, policy, client, and error tests | none |
| API-server-backed integration test | Informers, watches, discovery, status updates | ephemeral |
| kind/k3d + CSI host-path | Universal CSI lifecycle | ephemeral |
| VM-backed k3s + Longhorn | Longhorn kernel/iSCSI/NFS behavior | scratch only |
| Three-node Rook/Ceph lab | Ceph health, RBD, CephFS, failure, safe writes | scratch raw disks only |

Never run destructive Ceph/Longhorn qualification against the shared developer cluster or any
cluster containing user data. Test manifests must label all created resources and enforce a unique
run ID. Cleanup checks fail the job if labeled resources, PVs, RBD images, pools, snapshots, or
namespaces remain.

## 14. Rollout and migration strategy

### Release A — storage core preview

- Ship universal read-only inventory behind `storage.enabled`.
- Keep current Longhorn UI and readiness default.
- Collect performance and permission-degradation feedback.

### Release B — Longhorn managed-adapter default

- Enable the provider registry and synthesized Longhorn adapter by default.
- Keep legacy routes and pages.
- Make common storage navigation generally available.

### Release C — Ceph read-only preview

- Ship Ceph adapter opt-in and read-only.
- Support only the declared version matrix and least-privilege credential procedure.
- Collect correlation/API compatibility fixtures with explicit user consent and redaction.

### Release D — safe writes preview

- Ship operation controller and write routes with all write gates disabled by default.
- Enable on disposable/acceptance clusters first.
- Promote common actions individually based on live test evidence.
- Keep pool deletion separately disabled even when other Ceph writes are enabled.

### Release E — context and native-dashboard integration preview

- Configure a separate operator-facing Ceph Dashboard URL and secure external exposure.
- Ship Kubernetes-to-provider relationships, drift, impact, timeline, and capacity ownership as
  read-only capabilities.
- Enable version-aware deep links only for the declared compatibility matrix; otherwise use root
  handoff.
- Treat forecasting and provider guidance as evidence-labeled preview capabilities.
- Do not expand Ceph mutation scope beyond the Phase 4 action matrix.

### Rollback rules

- Disabling a provider never deletes its Kubernetes or backend resources.
- Disabling common UI leaves legacy Longhorn routes available during the compatibility window.
- Rolling back Highland must not roll back Rook, Ceph, Longhorn, CSI drivers, CRDs, PVCs, PVs, or
  snapshots.
- Operation CRD versions must remain readable by the immediately previous supported Highland release,
  or rollback is blocked with a documented migration procedure.
- No automatic rollback deletes a storage pool or volume after uncertain partial success.

## 15. Documentation deliverables

- [ ] Product concepts: provider, driver, backend, support level, claim versus backend volume.
- [ ] Universal inventory installation and cluster/namespace RBAC modes.
- [ ] Longhorn migration/compatibility and legacy-route policy.
- [ ] Rook/Ceph prerequisites, supported versions, least-privilege Dashboard user, TLS CA, NetworkPolicy,
  and Prometheus configuration.
- [ ] Native Ceph Dashboard exposure, public/private URL separation, human versus Highland reader
  identity, SSO options, deep-link compatibility, and separate authorization boundary.
- [ ] Kubernetes-to-backend relationship model, confidence semantics, impact analysis, drift,
  provider-scoped timeline, and capacity measurement definitions.
- [ ] Common capability matrix by verified driver/version.
- [ ] Read-only versus writable deployment modes.
- [ ] Every supported write workflow, preflight, confirmation, expected conditions, failure recovery,
  and non-goals.
- [ ] Emergency disable, credential rotation, API leader failure, stuck operation, and stale provider
  runbooks.
- [ ] Upgrade, rollback, uninstall, and cleanup validation.
- [ ] Security model and complete RBAC reference.
- [ ] Troubleshooting decision tree for Kubernetes API, CSI, Rook operator, Ceph Dashboard,
  Prometheus, and correlation failures.

## 16. Risks and mitigations

| Risk | Consequence | Mitigation |
|---|---|---|
| Lowest-common-denominator model | Provider features become unusable | Keep common summaries small and use provider-specific typed detail |
| Longhorn regression during extraction | Existing users lose mature behavior | Adapter wrapper first, legacy API golden tests, parity matrix, dual navigation |
| Excessive cluster RBAC | Highland compromise has broad impact | Read/write role split, namespace mode, feature-gated bindings, no wildcard Secret access |
| Ceph API version drift | Broken or unsafe UI behavior | Declared matrix, media negotiation, typed endpoint clients, fixtures and live contract tests |
| Incorrect PVC ↔ backend correlation | User acts on wrong resource | Authoritative IDs only; ambiguous/unavailable state; no name guessing |
| N+1 backend lookups | API/cluster overload | informer indexes, batch provider lists, pagination, explicit performance tests |
| One provider outage affects all UI | Multi-provider system becomes brittle | separate provider health, bounded clients, stale read cache, independent readiness |
| Duplicate writes across API replicas | Resource corruption or repeated actions | durable operations, leader election, idempotent reconciliation, UID/resourceVersion checks |
| Stale preflight permits destructive action | Data loss | plan-bound expiring confirmation and fresh reconciler-side dependency checks |
| Ceph credentials leak | Cluster compromise | named least-privilege Secret, TLS, server-side JWT, redaction allowlists, no raw proxy |
| Pool deletion misses hidden dependency | Data loss | disabled by default; inability to prove empty is a hard block; Rook and runtime verification |
| Highland duplicates the Ceph Dashboard | Split ownership, inconsistent behavior, excessive scope | Explicit product boundary; native handoff for deep administration; no iframe/raw proxy |
| Public dashboard link becomes SSRF or credential vector | Credential leakage or internal network access | Render-only validated URL; never fetched server-side; reject userinfo and unsafe schemes |
| Deep links break across Ceph versions | Broken operator workflow | Compatibility-versioned allowlist with root URL fallback |
| Graph presents inferred identity as fact | Operator acts on the wrong workload/backend | Evidence and confidence on every edge; authoritative IDs only; ambiguity is explicit |
| Provider-filtered events include unrelated workloads | Misdiagnosis and alert fatigue | involved-object/provider correlation before filtering; contract and live attribution tests |
| Capacity values are double-counted | Unsafe planning and misleading forecasts | Keep requested/provisioned/logical/allocated/usable/raw measures separate |
| Forecast appears authoritative with weak data | Premature capacity decisions | minimum history/freshness gates, confidence/window disclosure, unavailable rather than guessed |
| Helm CRD lifecycle blocks rollback | Operations unavailable | explicit CRD version/upgrade policy and previous-version readability gate |
| Test environment contaminates shared storage | Data loss or flaky validation | disposable clusters/disks, unique run labels, cleanup assertions, no shared-cluster destructive tests |

## 17. Milestones and estimated effort

These are engineering estimates, not commitments. They assume one senior engineer familiar with Go,
React, Kubernetes storage, and Helm, plus access to review and test infrastructure.

| Milestone | Estimate | Exit gate |
|---|---:|---|
| Phase 1 universal read-only core | 4–7 engineer-weeks | unknown CSI driver and common inventory validated live |
| Phase 2 Longhorn adapter preservation | 3–5 engineer-weeks | parity, upgrade, bolt-on, and embedded gates pass |
| Phase 3 Rook/Ceph read-only | 4–7 engineer-weeks | declared matrix and degraded-state tests pass |
| Phase 4 safe workflows | 8–16 engineer-weeks | durable operations and approved action matrix pass destructive scratch tests |
| Phase 5 context and native handoff | 6–10 engineer-weeks | relationship, drift, impact, timeline, capacity, and dashboard-handoff gates pass |

Security review, UX design, CI infrastructure, compatibility failures, and upstream API defects can
extend the calendar. Two or three engineers can parallelize backend, frontend, and lab work, but the
phase gates remain sequential because each phase depends on the preceding contracts and safety model.

## 18. Overall definition of done

- [ ] Roadmap phases 1–5 meet their individual definitions of done.
- [ ] Highland accurately describes itself as a Kubernetes storage control plane, not a CSI driver or
  direct CSI controller.
- [ ] Any CSI driver is detected through the universal read-only layer without provider code.
- [ ] Longhorn has no P0 parity regressions and remains supported through legacy and provider-aware
  surfaces.
- [ ] Rook/Ceph read support passes the declared live compatibility matrix.
- [ ] Only approved, typed, feature-gated Ceph/Kubernetes workflows can mutate state.
- [ ] Highland provides secure native Ceph Dashboard handoff without embedding, credential sharing,
  session brokerage, or public-URL server-side requests.
- [ ] Kubernetes-to-backend relationships, drift, impact, timelines, and capacity ownership are
  evidence-labeled, bounded, scope-aware, and validated against the declared live matrix.
- [ ] No raw Ceph administrative proxy, toolbox command execution, or direct CSI socket access exists.
- [ ] Read-only defaults, least-privilege RBAC, CSRF, action authorization, durable operations,
  dependency checks, confirmations, audit, metrics, and recovery are validated.
- [ ] Installation, upgrade, rollback, emergency disable, troubleshooting, and cleanup documentation
  has been executed successfully by someone other than the implementer.
- [ ] Release notes clearly identify preview/GA support levels and the exact tested provider versions.

## 19. Primary technical references

- [CSI specification](https://github.com/container-storage-interface/spec/blob/master/spec.md)
- [Kubernetes Storage API](https://kubernetes.io/docs/reference/kubernetes-api/storage/)
- [Kubernetes Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
- [Kubernetes Volume Snapshots](https://kubernetes.io/docs/concepts/storage/volume-snapshots/)
- [Kubernetes VolumeSnapshotClass](https://kubernetes.io/docs/concepts/storage/volume-snapshot-classes/)
- [CSI external health monitor](https://kubernetes-csi.github.io/docs/external-health-monitor-controller.html)
- [Rook CRD specification](https://rook.io/docs/rook/latest-release/CRDs/specification/)
- [Rook RBD storage](https://www.rook.io/docs/rook/latest-release/Storage-Configuration/Block-Storage-RBD/block-storage/)
- [Rook CephFS storage](https://www.rook.io/docs/rook/latest-release/Storage-Configuration/Shared-Filesystem-CephFS/filesystem-storage/)
- [Rook Ceph Dashboard integration](https://rook.io/docs/rook/latest/Storage-Configuration/Monitoring/ceph-dashboard/)
- [Rook Ceph monitoring](https://www.rook.io/docs/rook/latest-release/Storage-Configuration/Monitoring/ceph-monitoring/)
- [Ceph Dashboard](https://docs.ceph.com/en/latest/mgr/dashboard/)
- [Ceph Dashboard REST API](https://docs.ceph.com/en/latest/mgr/ceph_api/)
- [Ceph CSI](https://github.com/ceph/ceph-csi)
