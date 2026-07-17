# Rook/Ceph provider

Portable PVC and VolumeSnapshot workflows for Rook/Ceph require provider ID
`rook-ceph`. They do not require or imply Ceph infrastructure management.
`rookCephWrites`, StorageClass deletion, and pool deletion remain separate native
gates; routine validation keeps both destructive gates disabled.

The Rook/Ceph provider is opt-in. Rook CRDs supply desired state and operator status, the Ceph
Dashboard supplies bounded runtime facts, and Prometheus supplies an allowlisted time-series
snapshot. Dashboard mutation is never used.

## Prerequisites

- One explicitly named `CephCluster` using `ceph.rook.io/v1`.
- Read/list/watch access to `CephCluster`, `CephBlockPool`, `CephFilesystem`, and `CephRBDMirror` in
  the configured Rook namespace.
- A dedicated read-only Dashboard user and an optional CA Secret.
- An optional operator-controlled Prometheus query URL.

The preview matrix is pinned in [`../compatibility.yaml`](../compatibility.yaml): Rook 1.20.2 and
1.19.6 with Kubernetes 1.34/1.35, Ceph Squid 19.2.3 or Tentacle 20.2.1, and Ceph CSI 3.16.2. Ceph
20.2.0 is deliberately excluded. Rook's maintained-version and Ceph support policy is authoritative:
[maintenance/support](https://rook.io/docs/rook/latest-release/Getting-Started/maintenance-and-support/)
and [Ceph upgrade support](https://rook.io/docs/rook/v1.20/Upgrade/ceph-upgrade/).

```yaml
providers:
  rookCeph:
    enabled: true
    namespace: rook-ceph
    clusterName: rook-ceph
    dashboard:
      url: https://rook-ceph-mgr-dashboard.rook-ceph.svc:8443
      publicUrl: https://ceph.example.com
      allowHttp: false
      existingSecret: highland-ceph-dashboard-reader
      caSecret: rook-ceph-dashboard-ca
      insecureSkipVerify: false
    prometheus:
      url: http://prometheus-operated.monitoring.svc:9090
```

When the private `url` is configured, Highland exposes the native Dashboard at the authenticated,
same-origin `/ceph-dashboard/` path. The Dashboard Service can remain `ClusterIP`; Highland's web
proxy checks the existing Highland session before forwarding traffic, validates the configured TLS
endpoint, scopes Ceph's login cookie to that path, and does not inject the private reader credential.
Configure Ceph's `mgr/dashboard/url_prefix` as `/ceph-dashboard` so SPA assets, API calls, and
redirects stay under the gateway. Users still authenticate to Ceph with a separate human identity.

`publicUrl` remains an optional fallback for deployments that deliberately expose the Dashboard as
a separate application. It must be an absolute HTTPS URL without userinfo, query parameters, or a
fragment. Plain HTTP is rejected unless `allowHttp: true` is explicitly set for a disposable lab;
the UI then displays a credential-safety warning. Verified Ceph resource areas may receive a
versioned, identifier-free deep link; unknown versions and unstable routes open the root.

Create the credential Secret yourself; do not copy the generated Dashboard admin password:

```sh
kubectl -n rook-ceph create secret generic highland-ceph-dashboard-reader \
  --from-literal=username=highland-reader \
  --from-literal=password='replace-me'
```

Assign only the Ceph Dashboard read permissions required for health, pools, OSDs, RBD images, and
quorum using the supported Ceph release's role tooling. Credential creation is intentionally not
automated because Ceph role names and organizational policy vary.

## Data and degradation

Rook CRD absence, permission denial, configured-cluster absence, operator status, Dashboard outage,
Prometheus outage, and stale cached data are independent conditions. Dashboard/Prometheus failure
does not stop common Kubernetes inventory. Backend links appear only for exact identifiers.
Dashboard last-known data may be served as explicitly stale for at most 15 minutes; older cache
entries fail closed. Stale data is never accepted by a pool create/delete safety check.

The API exposes typed provider summary/health and paginated `pools`, `filesystems`, `mirroring`,
`osds`, `rbd-images`, and `quorum` resources. It does not expose raw JSON, credentials, commands, or
arbitrary Ceph endpoints. Every provider resource includes the stable provider ID, provider kind,
detected Rook operator version, served Rook API version, source, observation time, and stale/partial
state. Pool records keep Rook desired state and Ceph runtime usage in separate fields. Native
Dashboard availability shown by Highland comes from its private read client and provider health;
the public browser URL is intentionally not probed.

## Write preview

`providers.rookCeph.writes.enabled` permits the approved StorageClass and replicated-pool workflows
only when global storage writes are also enabled. `allowPoolDelete` is separate and false by default.
For installations using admin policy control, these startup flags are replaced by the effective
runtime gates **Rook / Ceph native workflows**, **Ceph StorageClass deletion**, and **Ceph pool
deletion**. The child deletion gates cannot be enabled unless the parent Ceph gate and matching
Helm-installed ceilings are enabled. The Admin UI still cannot bypass version, health, runtime
verification, dependency, role, or per-resource confirmation checks.
Pool creation requires a ready cluster, safe replica count, enough failure domains, allowlisted
options, server-side dry-run, Rook Ready, and fresh Ceph runtime presence. Pool deletion requires
fresh health and proof that no class, PV/PVC path, image, filesystem, or mirroring dependency exists.
Every new Ceph workflow is withheld unless Highland can read the fixed `rook-ceph-operator`
Deployment and both its stable image tag and the `CephCluster` Ceph image tag are in the declared
matrix. Stable Ceph 19.2.x and 20.2.1+ are accepted; 20.2.0 and unparseable/digest-only image
references remain read-only. Read-only inventory remains available on an unknown or untested
version, and already-approved operations can still recover.

Health policy is intentionally simple and fail-safe in this preview. `HEALTH_ERR` blocks every Ceph
infrastructure plan. `HEALTH_WARN` is a server-generated warning that must be acknowledged for pool
or StorageClass creation, and health is checked again by the operation controller. Pool deletion is
stricter: it requires a fresh `HEALTH_OK`; every warning code blocks deletion because Highland cannot
prove that an unrelated recovery or degraded state makes removal safe. No browser-side override or
force path exists.

Unsupported operations include filesystem creation/deletion, erasure-coded pool creation, OSD/MON/
MGR changes, repair, mirroring mutation, upgrades, raw commands, purge, and direct RBD deletion.

## Upgrade, disable, and uninstall

Upgrade Rook and Ceph using their release-specific guides before changing Highland's declared
provider profile. Keep Highland read-only, rotate the Dashboard account if required, then verify CRD,
Dashboard, and Prometheus freshness independently. Highland never upgrades Rook, Ceph, or Ceph CSI.

To disable the adapter, set `providers.rookCeph.enabled=false` and roll out Highland. This removes its
Rook RBAC, credential mounts, egress, and provider routes; it does not delete a CephCluster, pool,
filesystem, Class, PVC, PV, snapshot, or image. Re-enabling with the same stable provider ID restores
correlation after caches synchronize.

Uninstalling Highland likewise leaves a separately managed Rook/Ceph installation untouched. Before
removing Highland, stop new writes, let operations finish, export required audit history, remove its
read-only Dashboard account/Secret, and confirm no Highland-owned Pending/Running operation remains.
Use Rook's own [Ceph teardown procedure](https://rook.io/docs/rook/latest/Getting-Started/ceph-teardown/)
only when intentionally destroying the backend. Never use Highland's lab cleanup tooling on a
shared or production cluster.
