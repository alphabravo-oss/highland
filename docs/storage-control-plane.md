# Highland storage control plane

Highland presents Kubernetes storage as a common inventory and adds managed-provider detail without
pretending every backend has the same model. Kubernetes remains authoritative for claims, volumes,
attachments, snapshots, topology, and storage operations.

## Concepts

- A **driver** is the CSI provisioner recorded by Kubernetes.
- A **provider** is Highland's stable grouping and optional backend adapter. Unknown drivers each
  receive their own stable, path-safe `csi-<normalized-driver>-<short-sha256>` provider identity;
  the digest prevents punctuation-normalization collisions.
- A **claim** is a PVC. A **persistent volume** is the Kubernetes PV. A backend volume, Longhorn
  replica, Ceph image, or pool is provider-specific.
- A **provider reference** exists only when Highland proves an exact mapping. The UI reports
  unavailable/ambiguous mapping instead of parsing undocumented names.

Support levels are `detected` (portable read-only inventory), `verified` (continuously tested common
workflows), and `managed` (backend health/resources and explicitly supported actions).

## API and UI

The common API is under `/api/v1/storage`; managed resources use
`/api/v1/providers/{providerId}`. The normative contract is
[`openapi/storage-v1.yaml`](openapi/storage-v1.yaml). Lists are bounded to 500 objects and use opaque
continuation tokens. Filters include provider, driver, namespace, status, and search.

Pages include Providers, Storage Classes, Claims & Workloads, PVs, Snapshots, Attachments, Capacity,
Events, Operations, and Benchmarks. Filters remain in the URL. Provider-specific Longhorn routes are
unchanged; Ceph, OpenEBS, and Piraeus/LINSTOR pages are curated and provider-scoped. LINSTOR native
resources are read-only; its independently installed CSI continues running without Highland.

## Scope and partial data

Cluster scope reads common cluster and namespaced resources. Namespace scope creates independent
informers for each allowlisted namespace and does not request PVs, StorageClasses, CSIDrivers,
CSINodes, VolumeAttachments, VolumeSnapshotClasses, or VolumeSnapshotContents. This is a deliberate
least-privilege mode, not an empty cluster. Every response contains a `ClusterScopedInventory`
condition explaining the omitted relationships.

```yaml
storage:
  enabled: true
  scope:
    mode: namespaces
    namespaces: [team-a, team-b]
```

Missing snapshot CRDs, discovery denial, a stale provider, and an empty inventory are distinct
states. `/readyz` requires the configured storage cache and Kubernetes API, but only providers listed
in `storage.requiredProviders`. Optional provider outages appear in provider health and do not remove
unrelated endpoints.

## Writes

All new writes are off by default:

```yaml
storage:
  writes:
    enabled: false
    recoveryEnabled: false
providers:
  rookCeph:
    writes:
      enabled: false
      allowPoolDelete: false
```

Real fio benchmark Jobs are also opt-in with `benchmark.kubernetesJobEnabled=true`; this is kept
separate because that subsystem creates a scratch PVC, Job, and ConfigMap even when common storage
workflows are read-only. The default chart therefore grants no Kubernetes storage mutation verbs.

When enabled, clients first request a plan. The plan contains resources, dependencies, warnings,
blast radius, and a five-minute HMAC challenge bound to the user, action, provider, target UID and
resourceVersion, and plan hash. Destructive actions also require the exact resource name. Approval
creates an immutable `StorageOperation`; one leader-elected reconciler repeats preflight, performs a
server-side dry-run, uses server-side apply or UID/resourceVersion delete preconditions, and watches
the result to a terminal status.

Approved actions are the output of `GET /api/v1/storage/actions`. No generic mutation proxy exists.
Pool deletion is a second, default-off gate and fails closed whenever fresh Rook and Ceph data cannot
prove emptiness.

For recovery and emergency procedures, see
[`runbooks/storage-operations.md`](runbooks/storage-operations.md). For permissions and the threat
model, see [`security/storage-threat-model.md`](security/storage-threat-model.md).

## Performance and freshness

Inventory reads are served from shared informer caches and paginated in memory without per-row
backend calls. Provider readers batch/list upstream data and bound payloads. SSE v2 invalidations are
scoped by cluster, provider, namespace, resource, and name; legacy Longhorn key frames remain
accepted. Polling is retained as a safety refresh.

The release gate is a warm paginated-list p95 below 500 ms with 10,000 claims. Provider requests,
queues, response bodies, retries, and Prometheus labels are bounded.
