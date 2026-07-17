# ADR 0003: Provider-scoped portable storage authorization

Status: Accepted
Date: 2026-07-17

## Context

Highland originally used one `portableKubernetesWrites` switch for PVC and
VolumeSnapshot workflows. That switch described the Kubernetes API family, but
not the CSI backend affected by a request. Enabling it for Longhorn therefore
also authorized a Rook/Ceph, OpenEBS, or newly detected CSI StorageClass.

Provider-native actions already had independent Longhorn and Rook/Ceph policy
gates. Portable workflows needed the same isolation without duplicating the
Kubernetes implementation for every provider.

## Decision

`HighlandPolicy.spec.storage.portableKubernetesProviderIds` is the allowlist for
portable PVC and snapshot workflows. `acceptNewOperations` remains the global
admission gate and `portableKubernetesWrites` remains the workflow-family parent.

The planner derives the provider from authoritative Kubernetes evidence:

| Workflow | Provider evidence |
| --- | --- |
| Create, clone, or restore PVC | Target `StorageClass.provisioner`; source driver must match |
| Expand PVC | PVC StorageClass provisioner |
| Delete bound PVC | Bound PV `spec.csi.driver` |
| Delete unbound PVC | PVC StorageClass provisioner |
| Create snapshot | Bound source PV CSI driver, matching VolumeSnapshotClass driver |
| Delete snapshot | Referenced VolumeSnapshotClass driver |

A request `providerId` is only a consistency hint. A mismatch returns
`PROVIDER_MISMATCH`; missing evidence returns `PROVIDER_ATTRIBUTION_UNKNOWN`.
The resolved provider is included in the plan hash, signed challenge, durable
operation, audit event, metrics, history filter, and UI link.

After planning, the API checks `AllowsPortableProvider(plan.ProviderID)` before
returning a challenge and repeats the check after fresh replanning during
submission. Provider-native actions continue to use their existing independent
gates. The operation controller may finish work that was already approved when
an administrator later narrows admission.

## Compatibility

An older policy with portable writes enabled and no provider list is interpreted
as `[*]`. The wildcard is accepted only by itself and retains existing behavior
through upgrade. The Admin UI warns about it and writes explicit provider IDs;
fresh disabled installations use an empty list. Prometheus exposes a dedicated
legacy-wildcard gauge and alert.

Unknown CSI drivers receive a stable, bounded generic provider ID. They are
denied unless that exact ID is explicitly selected (or a legacy wildcard still
exists). Raw driver names are not placed in metric labels.

## Authorization matrix

Every successful mutation requires all cells in its row to pass:

| Workflow | Minimum role | Global gate | Family/provider gate | Installed RBAC ceiling |
| --- | --- | --- | --- | --- |
| Portable create/resize/clone/restore/snapshot | Operator | `acceptNewOperations` | `portableKubernetesWrites` and resolved provider ID allowed | Portable writer |
| Portable PVC/snapshot delete | Admin or action-defined role | `acceptNewOperations` | Same resolved-provider check | Portable writer |
| Longhorn-native | Operator/admin by action | `acceptNewOperations` | `longhornWrites` | Longhorn writer |
| Rook/Ceph create | Admin | `acceptNewOperations` | `rookCephWrites` | Ceph writer |
| Ceph StorageClass delete | Admin | `acceptNewOperations` | Rook/Ceph gate plus delete gate | Ceph StorageClass writer |
| Ceph pool delete | Admin | `acceptNewOperations` | Rook/Ceph gate plus pool-delete gate | Ceph pool writer |
| OpenEBS-native | N/A | N/A | Not implemented; read-only | None |

Viewer requests are always denied. Runtime policy cannot grant Kubernetes
permissions absent from the Helm-installed ceiling. Provider health is a
preflight condition, not authorization.

## Consequences

- “Longhorn + PVC lifecycle” truly affects only Longhorn.
- One portable implementation remains reusable across CSI providers.
- The provider inventory becomes an authorization input in the Admin UI, while
  Kubernetes objects remain the server-side source of truth.
- Missing or changed attribution fails closed and may require an operator to
  repair an object before Highland can mutate it.
- The additive v1alpha1 field preserves API compatibility, but legacy wildcard
  installations should be migrated promptly.
