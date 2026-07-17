# OpenEBS provider

OpenEBS remains native read-only. Provider ID `openebs` may be selected for
portable Kubernetes PVC/snapshot workflows supported by its detected CSI
driver; this does not expose an OpenEBS-native mutation API.

The OpenEBS provider is an opt-in, read-only managed provider. It discovers installed engines from
documented provisioners, CSI drivers, controller workloads, and OpenEBS CRDs.

```yaml
providers:
  openebs:
    enabled: true
    namespace: openebs
```

Highland does not install OpenEBS when this value is enabled. Install and lifecycle-manage OpenEBS
separately, then enable the provider to grant bounded read access.

## Supported engine surfaces

- Dynamic LocalPV HostPath: controller health, StorageClasses, PVs, PVCs, node placement, workload
  relationships, and explicit non-replicated risk.
- LocalPV LVM: LVM nodes, volumes, snapshots, volume-group placement, capacity, and exact CSI
  correlation when the CRD identity matches the CSI handle.
- LocalPV ZFS: ZFS nodes, volumes, snapshots, backups, restores, pool placement, capacity, and exact
  CSI correlation.
- Replicated PV Mayastor: controller/driver detection, Kubernetes inventory, and DiskPool CRs.
- RawFile LocalPV: detection and common Kubernetes inventory only in the initial preview.

Mayastor replica, target/nexus, rebuild, and runtime-volume detail is planned through a fixed,
versioned, server-side Mayastor REST client. Highland does not infer these facts from Kubernetes
names.

## Lab installation

The single-node Highland lab uses HostPath only:

```sh
helm upgrade --install openebs openebs \
  --repo https://openebs.github.io/openebs \
  --version 4.5.1 \
  --namespace openebs \
  --create-namespace \
  --set engines.replicated.mayastor.enabled=false \
  --set engines.local.lvm.enabled=false \
  --set engines.local.zfs.enabled=false \
  --set engines.local.rawfile.enabled=false \
  --set engines.local.hostpath.enabled=true \
  --set loki.enabled=false \
  --set alloy.enabled=false
```

Do not use that profile as a production resilience design. HostPath data remains on one node and
does not fail over.

Mayastor must not be enabled until every target node satisfies OpenEBS prerequisites, including the
required kernel, NVMe-TCP support, hugepages, CPU/memory reservation, labels, networking, and
dedicated block devices.

## RBAC and security

When enabled, Highland receives:

- read/list/watch for allowlisted `openebs.io`, `local.openebs.io`, and `zfs.openebs.io` resources;
- read/list/watch for Deployments and DaemonSets in the configured OpenEBS namespace;
- the existing provider-neutral Kubernetes storage reads.

The chart adds no OpenEBS write verbs and no OpenEBS Secret access. Provider responses remove
secret-, password-, token-, credential-, CHAP-, and key-like fields and bound nested structures.

## Degradation

Missing optional engine CRDs produce empty engine-specific resource pages. They do not make the API
unready. An installed controller with unavailable replicas is a provider health error. A disabled
engine is informational.

Disabling the provider removes Highland's registration and OpenEBS-specific RBAC after rollout. It
does not delete OpenEBS, StorageClasses, PVCs, PVs, snapshots, or data.

See [`../plans/openebs-provider.md`](../plans/openebs-provider.md) for the complete roadmap,
definitions of done, write gates, tests, and rollback policy.
