# Piraeus / LINSTOR provider validation

Validation date: **2026-07-17**

Highland target: **0.3.0**
Labs: single-node K3s integration cluster plus a disposable three-node K3s/DRBD qualification
cluster, both with independently installed Piraeus Operator and LINSTOR

## Tested versions

| Component | Version |
|---|---|
| Kubernetes | K3s `1.36.2+k3s1` |
| Piraeus Operator | `2.10.8` |
| LINSTOR Controller/Satellite | `1.33.3` (REST API `1.27.0`) |
| Piraeus/LINSTOR CSI | `1.11.3` |

Piraeus was installed independently from its upstream manifest. Highland's Helm release contains no
Piraeus, LINSTOR, CSI, or DRBD resource and has no owner reference on those objects.

## Automated evidence

- `go test ./...`, `go vet ./...`, and `go build ./cmd/highland-api` pass.
- `go test -race ./internal/providers/linstor` passes.
- LINSTOR tests cover URL rejection, fixed GET paths, bearer authentication, redirect refusal,
  response limits, caching, unauthorized-response redaction, wrapped REST payloads, CRD health,
  exact CSI correlation, stable IDs, and degraded node/replica health.
- Web typecheck, lint, 87 unit tests, production build, and bundle budgets pass. The lazy LINSTOR
  chunk is 4.9 KiB gzip and does not increase the initial bundle.
- The permanent live qualification test covers every LINSTOR route as separate admin and viewer
  users in light and dark themes at 1440×1000 and 390×844. It fails on API errors, request
  failures, console errors, horizontal overflow, role leakage, or serious/critical accessibility
  findings.
- `helm lint`, `hack/test-chart.sh`, and parity checks pass. Enabled rendering proves cluster-scoped
  read-only Piraeus CRD RBAC, namespaced workload RBAC, named token/CA Secret references, fixed
  NetworkPolicy egress, no wildcard access, and no LINSTOR mutation verbs.

## Live storage and API evidence

- `highland-linstor-validation` StorageClass provisioned a 128 MiB PVC through
  `linstor.csi.linbit.com`; the mounted BusyBox workload wrote and read durable test data.
- The provider reports health `ok`, LINSTOR `1.33.3`, one online node, two pools, one resource
  definition, one UpToDate replica placement, and all Piraeus components ready.
- All allowlisted list APIs returned `200`: components, clusters, satellites,
  satellite-configurations, node-connections, nodes, storage-pools, resource-groups,
  resource-definitions, resources, snapshots, remotes, schedules, and error-reports.
- Representative detail APIs for a UUID-backed pool, replica, and error report returned `200`.
- Common StorageClass, PVC, PV, attachment, event, provider health, drift, and capacity-forecast
  surfaces returned their documented bounded result. The test PV/PVC correlated exactly to the
  observed LINSTOR resource definition; the relationship graph reports fresh backend evidence.
- A deliberately unreachable controller changed only LINSTOR health to warning. Longhorn remained
  available and the LINSTOR workload kept reading data. Restoring the endpoint returned health to
  `ok` without data-plane intervention.
- Disabling the Highland provider returned `404` only for LINSTOR provider routes. CSI I/O continued;
  re-enabling restored inventory automatically.
- Scaling Highland API and web to zero did not stop the CSI workload. A new value was written and
  read from the mounted LINSTOR PVC while Highland was absent, then inventory recovered after both
  deployments returned.
- After all snapshot, security, and failover tests were cleaned up, the original PVC remained
  bound, its LINSTOR replica was `UpToDate`, and a fresh reader pod recovered the original proof
  plus both writes made while Highland workflows or deployments were disabled.

## Snapshot and restore evidence

- Kubernetes external-snapshotter CRDs and the v8.6.0 release manifest were installed as an
  independent cluster prerequisite; its published controller manifest used image v8.5.0.
  Highland does not own either resource.
- The initial `FILE_THIN` qualification pool advertised snapshot capability in inventory but
  rejected an actual snapshot. This is retained as an important runtime-validation finding: UI
  capability hints do not replace a real backend exercise.
- A disposable loop-backed `LVM_THIN` storage pool then provisioned a source PVC. A CSI
  `VolumeSnapshot` reached `readyToUse: true`, LINSTOR reported the snapshot successful, and a new
  PVC restored from it with the exact source checksum and accepted a subsequent write.
- The qualification PVCs, snapshot, StorageClasses, VolumeSnapshotClass, LINSTOR pool, LVM
  objects, loop device, and backing image were removed. The integration cluster retained only its
  original validation volume and intended `highland-file-thin` pool.

## HTTPS and bearer-token evidence

- A controlled TLS reverse proxy presented a private-CA certificate with the LINSTOR service DNS
  name and required a fixed bearer token while proxying the real controller.
- A trusted client with the correct token received `200`; an incorrect token received `401`; a
  client without the CA rejected the connection.
- Highland connected with `insecureSkipVerify=false` using named CA and token Secrets. An invalid
  token changed only LINSTOR health to warning with a bounded `401` condition and did not expose
  the credential in API responses. Rotating both proxy and Secret to a new token restored health
  to `ok`.
- The temporary proxy, certificates, and Secrets were removed, and the shared integration lab was
  returned to its explicitly allowed in-cluster HTTP controller endpoint.

## Three-node DRBD evidence

- Three disposable Ubuntu/K3s nodes each supplied an independent LVM-thin data disk and a Piraeus
  satellite. A StorageClass with `placementCount: "3"` created three `UpToDate` DRBD replicas.
- Destroying one replica node left the other two `UpToDate` and connected while the mounted writer
  advanced. Restarting the node resynchronized it to `UpToDate`.
- The workload was moved from its original node to another replica node; pre-failover data was
  present and new writes continued with all three replicas healthy.
- With two of three nodes stopped, the remaining primary explicitly reported
  `suspended:quorum`, `quorum:no`, and `blocked:upper`; its writer stopped advancing. Restoring one
  peer re-established 2/3 quorum and writes resumed at the next sequence without a gap. Restoring
  the final peer returned all replicas to `UpToDate`.
- The workload, LINSTOR cluster, DHCP reservations, VM disks, and three qualification VMs were
  removed. Unrelated host VMs were untouched.

## Browser evidence

Authenticated Chromium exercised 24 LINSTOR and common-storage routes for both admin and viewer
roles, both light and dark themes, and desktop/mobile viewports: 192 route states total. There were
no console errors, failed requests, API responses at or above 400, serious/critical accessibility
findings, or horizontal overflow. Viewer navigation exposed no Administration link. Every page
rendered a focused heading and provider-specific navigation; empty optional resources rendered
useful empty states. Qualification found and fixed missing accessible names on icon-only mobile
pagination controls and keyboard access to the shared scrollable main region.

## Qualification conclusions and operating constraints

- Snapshot support depends on the Kubernetes snapshot CRDs/controller and a snapshot-capable
  LINSTOR storage backend. `FILE_THIN` was not sufficient in this tested deployment; `LVM_THIN`
  completed the full CSI snapshot/restore path.
- Keep LVM volume-group and thin-pool names concise. Device-mapper names combine these values with
  long Kubernetes IDs and snapshot suffixes, so unusually long backend names can exceed the kernel
  device-name limit.
- Production controller connections must use verified HTTPS and narrowly scoped, rotated
  credentials. Plain HTTP remains an explicit lab-only exception.
- The three-node exercise proves replicated placement, single-node continuity, resynchronization,
  failover, quorum suspension, and safe quorum recovery. It does not turn Highland into a LINSTOR
  reconciler: provider-native repair and lifecycle remain Piraeus/LINSTOR responsibilities.

Every live failure/removal test preserved the core lifecycle boundary: Highland is observational,
and LINSTOR CSI and volume I/O remain independent.
