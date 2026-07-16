# OpenEBS Provider Implementation Plan

- Status: **Phase 1 implemented in preview; later write phases gated**
- Owner: **Highland storage control plane**
- Last updated: **2026-07-16**
- Target: **Highland multi-provider preview**

## 1. Executive decision

Highland will add OpenEBS as a managed, engine-aware storage provider. Kubernetes resources and
OpenEBS custom resources remain the source of truth. Highland will not call CSI sockets and will not
pretend that OpenEBS HostPath, LocalPV LVM, LocalPV ZFS, and Replicated PV Mayastor have the same
operational model.

The initial implementation is read-only and supports:

- automatic detection of installed OpenEBS engines and their CSI/provisioner identities;
- component, driver, and engine health;
- normalized inventories for Mayastor DiskPools, LocalPV LVM, LocalPV ZFS, and HostPath;
- capacity, placement, phase, freshness, and Kubernetes ownership views;
- exact PVC/PV-to-provider correlation when an authoritative backend identity is present;
- engine-specific navigation, dashboards, resource tables, detail views, and empty states;
- the existing Highland context, impact, inventory, event, and capacity layers.

Writes remain disabled until the operation controller has provider-specific preflight, postflight,
idempotency, compatibility, and recovery coverage. Installing an OpenEBS engine does not implicitly
authorize Highland to mutate it.

## 2. Goals and user outcomes

An operator should be able to answer these questions without reading raw CRDs:

1. Which OpenEBS engines are installed, configured, and actually serving volumes?
2. Are the controller and node components healthy?
3. Which nodes, pools, volume groups, or ZFS pools provide capacity?
4. Which workloads depend on each OpenEBS volume?
5. Is a volume local or replicated, and what failure boundary protects it?
6. Are Mayastor DiskPools online and sufficiently distributed?
7. Are LocalPV volumes pinned to healthy nodes with enough remaining capacity?
8. Are snapshots, clones, expansion, and encryption available for the selected engine?
9. Is data incomplete because an engine, CRD, permission, or runtime API is unavailable?
10. What must be fixed before an operation or upgrade is safe?

## 3. Scope

### 3.1 Engines

| Engine | Provisioner/driver | Initial support |
|---|---|---|
| Dynamic LocalPV HostPath | `openebs.io/local` | component health, StorageClasses, PV/PVC/workload ownership, node placement |
| LocalPV LVM | `local.csi.openebs.io` | LVM nodes, volumes, snapshots, capacity and topology |
| LocalPV ZFS | `zfs.csi.openebs.io` | ZFS nodes, volumes, snapshots, backups/restores, capacity and topology |
| Replicated PV Mayastor | `io.openebs.csi-mayastor` | component health, DiskPools, Kubernetes volumes, topology and replication policy |
| RawFile LocalPV | `rawfile.csi.openebs.io` | detection and common Kubernetes inventory; promoted after compatibility testing |

Mayastor replica, nexus/target, rebuild, and live I/O details require its versioned control-plane
REST API. They are a separate phase so Highland can enforce fixed upstream configuration, bounded
responses, timeouts, compatibility checks, and fail-closed behavior.

### 3.2 Non-goals for the first implementation

- Direct CSI socket calls.
- Raw Kubernetes object or arbitrary OpenEBS REST proxying.
- Creating or deleting pools, volume groups, ZFS pools, disks, or volumes.
- Formatting disks or constructing LVM/ZFS host configuration.
- Force deletion, data migration, rebuild manipulation, or upgrade execution.
- Treating node-local storage as highly available.
- Inferring backend identity from PVC display names.
- Installing Mayastor on nodes that do not satisfy its kernel, NVMe-TCP, hugepage, CPU, and memory
  prerequisites.

## 4. Architecture

### 4.1 Provider identity

The provider has stable ID `openebs`, kind `openebs`, and managed support level. It owns documented
OpenEBS CSI/provisioner names. Engine availability is reported separately in descriptor metadata and
the provider summary.

### 4.2 Sources and authority

| Source | Authority |
|---|---|
| Kubernetes `CSIDriver`, `StorageClass`, PVC, PV, snapshots, attachments | Kubernetes lifecycle and workload ownership |
| Deployments, DaemonSets, Pods | installed component and rollout health |
| Mayastor `DiskPool` CRs | desired pool membership and reported pool status |
| `local.openebs.io` CRs | LocalPV LVM desired/runtime records |
| `zfs.openebs.io` CRs | LocalPV ZFS desired/runtime records |
| OpenEBS labels and image tags | installed component/version evidence |
| Optional Mayastor REST client | replicated-volume runtime details in a later phase |

Every response records `source`, `observedAt`, provider ID, engine, API version, and stale/partial
state where applicable. Secret-like keys and oversized nested structures are removed.

### 4.3 Discovery

The adapter discovers served resources rather than assuming one CRD version. It checks an
allowlisted set of API groups, plural resources, and versions, then selects the server-preferred
served version. Missing optional CRDs produce an unavailable engine/resource condition, not an API
process failure.

No request parameter can choose a GVR, namespace, or upstream endpoint.

### 4.4 Normalized resources

Provider resource kinds:

- `components`
- `engines`
- `disk-pools`
- `lvm-nodes`
- `lvm-volumes`
- `lvm-snapshots`
- `zfs-nodes`
- `zfs-volumes`
- `zfs-snapshots`
- `zfs-backups`
- `zfs-restores`
- `hostpath-volumes`

Normalized records retain bounded `spec` and `status` sections for forward compatibility, while
promoting high-value fields such as node, phase, health, capacity, used, available, storage pool,
volume group, replica count, filesystem, compression, encryption, and volume handle.

### 4.5 Health model

Provider health is the worst meaningful state from:

- no OpenEBS engine/component observed;
- unavailable required Kubernetes API;
- unavailable OpenEBS namespace;
- Deployment unavailable replicas;
- DaemonSet unavailable desired nodes;
- crash-looping or non-ready OpenEBS Pods;
- offline/faulted DiskPools;
- engine CRD served but controller absent;
- stale or partial resource observations.

An intentionally disabled engine is informational, not unhealthy. HostPath is explicitly labelled
non-replicated. Lack of the optional Mayastor runtime API does not make Kubernetes inventory fail.

### 4.6 Correlation rules

- The StorageClass provisioner establishes provider ownership.
- CSI volume handles are used as exact backend keys.
- LVM/ZFS/Mayastor provider references are attached only when the corresponding normalized resource
  has the same exact UID/name/volume handle.
- HostPath PVs use the authoritative PV CSI/local volume identity and node affinity; Highland does
  not claim a separate replicated backend object.
- Missing matches create an informational correlation condition.

## 5. API

Existing provider-neutral routes are reused:

```text
GET /api/v1/storage/providers
GET /api/v1/storage/providers/openebs
GET /api/v1/providers/openebs/summary
GET /api/v1/providers/openebs/resources/{kind}
GET /api/v1/providers/openebs/resources/{kind}/{id}
GET /api/v1/storage/relationships
GET /api/v1/storage/impact
```

All lists remain bounded and paginated. Unsupported kinds return a typed `404`, permission failures
remain distinguishable from absent CRDs, and partial summary failures appear as conditions.

## 6. UX and information architecture

```text
OpenEBS
├── Overview
├── Context & insights
├── Operations
├── Replicated PV / Mayastor
│   └── Disk pools
├── LocalPV LVM
│   ├── Nodes & volume groups
│   ├── Volumes
│   └── Snapshots
├── LocalPV ZFS
│   ├── Nodes & pools
│   ├── Volumes
│   ├── Snapshots
│   └── Backups & restores
├── HostPath
│   └── Local volumes
└── Kubernetes inventory
    ├── Storage classes
    ├── Claims & workloads
    ├── Persistent volumes
    ├── Snapshots
    ├── Attachments
    ├── Capacity
    └── Events
```

Navigation includes only installed/observable engines. The overview leads with actionable health,
then engine cards, component readiness, capacity/risk, and Kubernetes ownership. Raw capability IDs
are not the primary content.

Each resource page must explain:

- what the resource controls;
- what healthy looks like;
- whether it is local or replicated;
- which failure conditions matter;
- the observation source and freshness;
- the safe next diagnostic.

## 7. Security

- Read-only RBAC is restricted to allowlisted OpenEBS API groups/resources plus component workload
  reads in the configured namespace.
- Secret resources and Secret values are never read.
- Dynamic resource names are constants, not user input.
- Nested responses are depth, count, and string-length bounded.
- Search occurs only across the already bounded representation.
- OpenEBS writes are absent from capabilities and chart RBAC in the initial phase.
- Future REST clients must use fixed configuration, TLS where supported, response limits, and
  version allowlists.

## 8. Delivery phases

### Phase 0 — contracts and fixtures

Tasks:

- Document engine identities, resource kinds, health semantics, and non-goals.
- Capture sanitized fixtures for every supported CRD family.
- Add compatibility entries for tested OpenEBS and Kubernetes releases.
- Define exact correlation fixtures for each driver.

Definition of done:

- The plan and provider documentation are reviewed.
- No response shape depends on raw CRD passthrough.
- Fixtures contain no secrets, host identifiers, or customer data.

### Phase 1 — read-only provider foundation

Tasks:

- Add config, chart values, RBAC, registration, descriptor, health, summary, discovery, normalized
  resources, pagination, search, and exact enrichment.
- Add engine-aware UI, routes, navigation, tables, detail pages, empty states, and context links.
- Install a minimal HostPath-only OpenEBS lab release and validate coexistence with Longhorn/Ceph.

Definition of done:

- OpenEBS appears as one managed provider.
- Installed engines and component health are correct.
- HostPath is clearly non-replicated.
- Missing LVM/ZFS/Mayastor engines are shown as not installed, not failed.
- Existing Longhorn, Ceph, and generic CSI behavior is unchanged.

### Phase 2 — Mayastor runtime depth

Tasks:

- Add a fixed, server-side, versioned Mayastor REST client.
- Inventory replicated volumes, replicas, targets/nexuses, rebuild state, snapshots, and nodes.
- Add topology safety and under-replication conditions.
- Validate kernel modules, hugepages, CPU/memory, and node labels.

Definition of done:

- Highland explains replica placement and degraded/rebuild state from authoritative runtime data.
- Runtime failure degrades only Mayastor-specific detail.
- Unsupported Mayastor versions remain read-only with an explicit condition.

### Phase 3 — safe common operations

Tasks:

- Add typed PVC create, expand, clone, snapshot, restore, and delete flows using Kubernetes APIs.
- Resolve capability per StorageClass/engine/version.
- Add impact analysis, typed confirmation, idempotency, durable reconciliation, and postflight.

Definition of done:

- Every operation is resumable, auditable, and fail-closed.
- LocalPV node loss and reclaim-policy risks are shown before confirmation.
- No engine infrastructure mutation exists.

### Phase 4 — guarded OpenEBS infrastructure operations

Candidate operations:

- Mayastor node cordon/uncordon and drain.
- Mayastor replica-count changes.
- DiskPool creation only from explicitly selected unused block devices.
- LVM/ZFS StorageClass creation.
- Snapshot/clone lifecycle where the installed engine supports it.

Required gates:

- explicit global and provider write flags;
- current/previous tested OpenEBS versions;
- fresh provider health;
- node/device identity proof;
- dependency and blast-radius analysis;
- server-side dry run where available;
- postflight verification and recovery tests.

### Phase 5 — lifecycle and ecosystem

- Upgrade preflight and compatibility reporting.
- Support bundle collection without secrets.
- Prometheus history and alert rules.
- Multi-cluster provider identities.
- Backup integrations such as Velero/VolSync.
- RawFile promotion after stability and compatibility validation.

## 9. Testing

### Backend unit tests

- discovery across API versions;
- missing optional CRDs;
- forbidden versus absent resources;
- engine detection;
- component rollout health;
- DiskPool/LVM/ZFS normalization;
- response bounding and sensitive-key removal;
- pagination and search;
- exact enrichment and non-correlation conditions;
- descriptor metadata and capabilities.

### Chart tests

- provider disabled by default;
- config rendering;
- namespace-scoped component Role;
- cluster-scoped OpenEBS CRD read Role;
- no write verbs;
- no Secret access;
- coexistence with Rook/Ceph and embedded Longhorn.

### Frontend tests

- workspace selection and engine-aware navigation;
- overview health/risk hierarchy;
- installed and absent engine cards;
- per-engine columns and detail highlights;
- partial/error/empty states;
- mobile overflow;
- keyboard navigation and accessible names.

### Live validation

- install OpenEBS with HostPath only on the single-node k3s lab;
- create a StorageClass/PVC/workload smoke fixture;
- verify PVC → PV → node → OpenEBS provider relationships;
- test every OpenEBS page and API;
- verify no post-login console/API errors;
- verify Longhorn and Ceph remain healthy;
- uninstall the fixture without deleting unrelated storage.

## 10. Release gates

- Go tests, frontend unit tests, production builds, chart tests, and storage E2E pass.
- Live OpenEBS pages have no serious/critical accessibility violations.
- No unbounded provider payloads or browser-visible credentials.
- No write verbs are added for OpenEBS Phase 1.
- Provider outage does not affect Highland liveness or unrelated providers.
- Documentation includes install, disable, upgrade, and uninstall behavior.

## 11. Rollback and uninstall

Disabling `providers.openebs.enabled` removes Highland's OpenEBS provider registration and
OpenEBS-specific RBAC on the next rollout. It does not modify OpenEBS, StorageClasses, PVCs, PVs,
snapshots, or data.

Uninstalling Highland never uninstalls OpenEBS. Uninstalling OpenEBS is a separate storage-platform
operation and must occur only after every dependent workload and retained PV has been migrated,
backed up, or intentionally destroyed.
