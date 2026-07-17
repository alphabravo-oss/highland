# Storage capability matrix

Capabilities are derived from installed APIs, driver/Class configuration, provider support,
Highland feature policy, the signed-in role, and current resource state. An empty cell means
Highland does not advertise the capability; it is never an optimistic fallback.

| Capability | Generic detected CSI | Longhorn managed | Rook/Ceph managed |
|---|---:|---:|---:|
| Drivers, Classes, PVC/PV, workloads, attachments, events | detected | managed | managed |
| CSI capacity | when `CSIStorageCapacity` is served | managed | managed |
| Snapshot inventory | when snapshot v1 APIs are served | managed | managed |
| Backend health/resources | — | replicas, nodes/disks, backup and engine facts | pools, OSDs, RBD, CephFS, quorum, mirroring |
| Create/expand/delete PVC | preview; installed API/Class policy and write gate required | preview through common workflow; legacy actions retained | preview through common workflow |
| Snapshot/create/delete, restore, clone | preview; snapshot API/Class and driver match required | preview plus legacy native actions | preview when Ceph CSI advertises prerequisites |
| Create RBD/CephFS StorageClass | — | — | preview; admin, ready backend, explicit write gate |
| Create replicated block pool | — | — | preview; admin, healthy cluster, safe failure domains, fresh runtime verification |
| Delete StorageClass | — | — | preview; admin and zero PVC/PV dependencies |
| Delete block pool | — | — | separately gated; typed name and fail-closed emptiness proof |

Rook/Ceph mutations additionally require a stable Rook 1.19.x/1.20.x operator tag and a stable Ceph
19.2.x or 20.2.1+ image tag. Unknown versions, prerelease tags, digest-only tags, Ceph 20.2.0, and
versions outside that matrix retain managed read-only inventory but advertise no Ceph write
capability.

The exact tested versions are in [`compatibility.yaml`](compatibility.yaml). `detected` is a
portable read-only promise, not a claim that every driver supports snapshot, clone, expansion, or
topology. A workflow is listed by `GET /api/v1/storage/actions` with an availability explanation and
is still revalidated against its selected resource during planning and reconciliation.

Highland does not manage raw CSI sockets, Ceph commands, OSD replacement, MON/MGR topology, CephFS
creation, mirroring changes, erasure-code profiles, backend upgrades, or cross-provider migration.
