# ADR 0001: Kubernetes APIs are the storage orchestration source of truth

- Status: Accepted
- Date: 2026-07-15
- Scope: Highland storage control plane

## Decision

Highland discovers and changes Kubernetes-owned storage through the Kubernetes API. It does not
connect to CSI Unix sockets or issue CSI controller RPCs directly.

The common inventory uses `CSIDriver`, `CSINode`, `StorageClass`, `CSIStorageCapacity`,
`VolumeAttachment`, PV, PVC, Pod, Event, and the optional `snapshot.storage.k8s.io/v1` APIs. Managed
provider adapters may add backend facts, but they cannot replace an authoritative Kubernetes
driver, `volumeHandle`, UID, or `resourceVersion` with a guessed identity.

Rook/Ceph writes create or change typed Kubernetes and Rook resources. The Ceph Dashboard remains a
read-only runtime and postflight data source. Longhorn's existing authenticated legacy proxy is
preserved as a compatibility surface while new common workflows use Kubernetes objects.

## Consequences

- Kubernetes reconciliation, admission, quota, ownership, and idempotency remain in force.
- An unknown conforming CSI driver can appear in the common read-only inventory without an adapter.
- Backend correlation is omitted when an adapter cannot prove it from exact provider identifiers.
- Highland cannot expose backend-only operations as generic CSI operations.
- A durable `StorageOperation` and fresh server-side preflight protect every new write.

## Invariants

1. One Highland installation controls one Kubernetes cluster.
2. Provider IDs are stable configured identities; driver names are owned by at most one managed
   provider.
3. Quantities cross the API as Kubernetes quantity strings, never JavaScript numbers.
4. Unknown capability means unsupported, especially for writes.
5. Namespace scope uses per-namespace informers and intentionally omits cluster-scoped metadata.
6. No browser request selects a provider upstream URL or receives provider credentials.
7. No raw Ceph proxy, toolbox command runner, or CSI socket client is part of Highland.

## Rejected alternatives

- Direct CSI calls: bypass Kubernetes ownership and admission and require privileged socket access.
- A universal Longhorn-shaped volume model: loses portable semantics and encourages guessed fields.
- Direct Ceph mutation: creates a second desired-state authority alongside Rook.
- In-memory asynchronous writes: cannot safely survive replicas, restarts, or leader changes.
