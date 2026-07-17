# Storage RBAC reference

Runtime authorization is also provider-scoped. The portable writer role is an
installation-time ceiling only: a request must resolve its CSI provider from the
selected StorageClass, PV, or VolumeSnapshotClass, and that provider ID must be
present in the effective HighlandPolicy allowlist. A Longhorn provider scope
cannot authorize Rook/Ceph or OpenEBS even though the Kubernetes RBAC verbs are
shared. See [ADR 0003](../adr/0003-provider-scoped-portable-storage-authorization.md).

The chart renders independent roles so enabling one workflow does not grant every storage write.
`hack/test-chart.sh` rejects wildcard resources/verbs and proves read-only defaults contain no
mutation verbs.

| Rendered role | Scope | Resources | Verbs | Gate |
|---|---|---|---|---|
| `highland-storage-read` | cluster | PV, StorageClass, CSI driver/node/capacity, VolumeAttachment, optional snapshot metadata | get/list/watch | `storage.enabled`, cluster mode |
| `highland-storage-read-<namespace>` | allowlisted namespace | PVC, Pod, Event, VolumeSnapshot | get/list/watch | namespace mode |
| `highland-longhorn-read` | Longhorn namespace | allowlisted Longhorn CRDs | get/list/watch | Longhorn provider |
| `highland-rook-ceph-read` | Rook namespace | CephCluster, CephBlockPool, CephFilesystem, CephRBDMirror; fixed `rook-ceph-operator` Deployment | get/list/watch on Rook CRDs; get only on the named Deployment | Rook/Ceph provider |
| `highland-linstor-read` | cluster | LinstorCluster, LinstorSatellite, LinstorSatelliteConfiguration, LinstorNodeConnection | get/list/watch | LINSTOR provider |
| `highland-linstor-workloads-read` | Piraeus namespace | Deployment, StatefulSet, DaemonSet | get/list/watch | LINSTOR provider |
| `highland-storage-operations-read` | Highland namespace | StorageOperation | get/list/watch | always |
| `highland-storage-operation-controller` | Highland namespace | StorageOperation status and Lease | get/list/watch/create/update/patch; terminal-object delete for retention | writes or recovery |
| `highland-namespaced-storage-writer` | configured namespaces | PVC and VolumeSnapshot | bounded create/update/patch/delete | writes or recovery |
| `highland-storageclass-writer` | cluster | StorageClass | bounded create/patch/delete | writes or recovery |
| `highland-ceph-storageclass-writer` | Rook/cluster | only resources required by typed Ceph Class workflows | bounded verbs | Ceph writes |
| `highland-ceph-pool-writer` | Rook namespace | CephBlockPool | get/create/patch/delete | Ceph writes plus pool-delete policy where applicable |
| `highland-benchmark` | benchmark namespace | scratch PVC, Job, ConfigMap | bounded lifecycle verbs | `benchmark.kubernetesJobEnabled` |
| `highland-policy` | Highland namespace | named `HighlandPolicy/highland` | get/list/watch; update/patch and status only on the singleton | `adminPolicyControl.enabled` |

The Deployment receives named Secret keys through `secretKeyRef`; no role grants Secret `list`.
Dashboard credentials and CA material are never part of universal inventory. Namespace mode omits
cluster PV/driver metadata by design, producing an explicit partial-inventory condition.
LINSTOR bearer tokens and CA material are injected only through named Secret key references; the
RBAC above grants no Secret reads or provider mutations.

Application authorization is additional to Kubernetes RBAC. Viewer has read access, operator may
use approved namespaced lifecycle actions, and admin is required for destructive or
infrastructure-scoped actions. Session authentication, CSRF, namespace policy, action policy,
planning, confirmation, and fresh controller preflight all run before the service account mutates a
resource.

## Runtime policy ceiling

`adminPolicyControl.installStorageWriterPermissions=true` is an installation-time acknowledgement,
not a runtime enable switch. Individual ceiling values determine which bounded writer roles render.
The Admin UI can request only a subset of those capabilities. It cannot create Roles, ClusterRoles,
RoleBindings, or ClusterRoleBindings, and the Highland ServiceAccount has no verbs on those resource
types.

Highland RBAC remains a second boundary: viewers may inspect effective policy, operators may execute
only actions whose `minimumRole` is operator, and only admins may plan/apply runtime policy or execute
admin-only workflows. Those checks run in the API for both planning and submission.

Terminal `StorageOperation` CRs are retained indefinitely when append-only audit persistence is
disabled. When `persistence.audit.enabled=true`, the JSONL audit stream is durable and terminal
operation CRs may be garbage-collected after 30 days; garbage collection never runs without that
durable audit sink. A multi-replica API requires a `ReadWriteMany` audit volume; use one API replica
when the selected StorageClass only supports `ReadWriteOnce`.
