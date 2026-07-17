# Piraeus / LINSTOR provider validation

Validation date: **2026-07-17**

Highland target: **0.3.0 preview**
Lab: single-node K3s with independently installed Piraeus Operator and LINSTOR

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

## Browser evidence

Authenticated Chromium visited the dashboard, all fourteen resource pages, context, operations,
provider-filtered PVs, and provider-filtered claims in dark mode at 1440×900. The dashboard was also
checked at 390×844. There were no console/page errors, failed HTTP responses, or horizontal overflow.
Every page rendered a focused heading and provider-specific navigation; empty optional resources
rendered useful empty states.

## Explicit lab limits and deferred production gates

- The cluster does not install the Kubernetes VolumeSnapshot CRDs, so a Kubernetes
  VolumeSnapshot/restore test is unavailable. Highland reports snapshot support as partial; the
  backend LINSTOR snapshot endpoint was still probed and returned a valid empty list.
- This single-node file-thin lab cannot safely simulate node loss, DRBD quorum, multi-replica
  placement, or a satellite degradation without disrupting the shared test cluster. Unit fixtures
  prove the degraded health contract. A disposable three-node DRBD qualification remains the gate
  for promoting the provider beyond preview.
- The live controller used trusted in-cluster HTTP with the explicit lab flag. HTTPS CA, bearer
  rejection, token redaction, and rotation behavior are covered by client/config/chart tests; a
  production installation must use verified HTTPS.
- No separate viewer account exists in this lab. Shared authentication/authorization tests cover
  viewer read access and admin-only controls; the LINSTOR workspace itself exposes no native writes.

These limits do not weaken the core lifecycle boundary: Highland is observational, and every live
failure/removal test demonstrated that LINSTOR CSI and volume I/O remain independent.
