# Install Highland (Kubernetes-native)

Highland supports two Helm deployment modes:

- **Bolt-on (default):** install Highland in any namespace and connect it to an existing Longhorn installation.
- **Embedded (opt-in alpha):** install Highland and pinned Longhorn 1.12.0 together in one release in
  `longhorn-system`. The stock Longhorn UI has zero replicas, leaving Highland as the only console.

The embedded mode is intended for new clusters, trials, edge deployments, and air-gapped packaging.
Never enable it in a cluster that already has Longhorn installed.

## Prerequisites

- Helm 3 and `kubectl`
- A supported Kubernetes cluster
- Bolt-on mode: an existing Longhorn installation and its manager Service address
- Embedded mode: Kubernetes 1.25 or newer and nodes prepared as described below
- Highland images in a registry reachable by the cluster (or the published GHCR images)

### Embedded Longhorn node prerequisites

Every node that may run Longhorn must meet the
[Longhorn 1.12 installation requirements](https://longhorn.io/docs/1.12.0/deploy/install/):

- Install and start `open-iscsi`/`iscsid` for the default V1 data engine.
- Install NFSv4 client utilities (`nfs-common`, `nfs-utils`, or the distribution equivalent) for RWX
  volumes and NFS backups, and ensure the kernel supports NFSv4.1 or newer.
- Ensure mount propagation is enabled and Longhorn workloads may run as root/privileged.
- Provide `bash`, `curl`, `findmnt`, `grep`, `awk`, `blkid`, and `lsblk` on the host.
- Load the extra kernel modules and configure huge pages/IOMMU only if deliberately enabling the
  experimental V2/SPDK data engine. The shipped defaults use V1.

Use Longhorn's matching CLI release to verify the nodes before installation:

```bash
# Download the correct longhornctl binary for your architecture first.
./longhornctl check preflight

# Optional: let longhornctl install the V1 prerequisites on supported hosts.
./longhornctl --kubeconfig ~/.kube/config \
  --image longhornio/longhorn-cli:v1.12.0 install preflight
./longhornctl check preflight
```

See the official guide for supported host distributions, SELinux notes, and V2 engine preparation.

## 1. Build and push images (source installs only)

Skip this section when installing a released chart that uses the published GHCR images.

```bash
# From the Highland repository.
docker build -t your-registry/highland-api:0.1.0 apps/api
docker build -t your-registry/highland-web:0.1.0 apps/web
docker push your-registry/highland-api:0.1.0
docker push your-registry/highland-web:0.1.0
```

## 2. Bolt-on install (default)

This mode creates only Highland resources and leaves the existing Longhorn lifecycle independent.
For a source checkout, restore the declared (disabled-by-default) dependency first; Helm validates
dependency availability before evaluating its condition.

```bash
helm dependency build ./chart

helm upgrade --install highland ./chart \
  --namespace highland-system \
  --create-namespace \
  --set image.api.repository=your-registry/highland-api \
  --set image.api.tag=0.1.0 \
  --set image.web.repository=your-registry/highland-web \
  --set image.web.tag=0.1.0 \
  --set auth.local.createSecret=true \
  --set auth.local.username=admin \
  --set auth.local.password='change-me-strong' \
  --set longhorn.enabled=true \
  --set longhorn.namespace=longhorn-system \
  --set longhorn.managerService=longhorn-backend \
  --set longhorn.managerPort=9500 \
  --set config.authMode=local \
  --set config.localAlways=true
```

Or use a values file (GitOps-friendly):

```yaml
# values-prod.yaml
image:
  api:
    repository: your-registry/highland-api
    tag: "0.1.0"
  web:
    repository: your-registry/highland-web
    tag: "0.1.0"
auth:
  local:
    createSecret: true
    username: admin
    password: "change-me-strong" # Prefer sealed/external secrets in production.
config:
  authMode: local
  localAlways: true
  cookieSecure: true
longhorn:
  namespace: longhorn-system
  managerService: longhorn-backend
  managerPort: 9500
ingress:
  enabled: true
  className: traefik
  hosts:
    - host: highland.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: highland-tls
      hosts: [highland.example.com]
```

```bash
helm upgrade --install highland ./chart \
  --namespace highland-system --create-namespace \
  -f values-prod.yaml
```

## 3. Embedded Highland + Longhorn install (opt-in alpha)

Helm installs a subchart into its parent release namespace. Use `longhorn-system` for the combined
release so Longhorn's component assumptions and standard support tooling remain valid. There must be
only one Longhorn installation in the cluster.

For a source checkout, restore the pinned chart dependency from `Chart.lock`, then install:

```bash
helm dependency build ./chart

helm upgrade --install highland ./chart \
  --namespace longhorn-system \
  --create-namespace \
  --set embeddedLonghorn.enabled=true \
  --set auth.local.createSecret=true \
  --set auth.local.password='change-me-strong' \
  --wait --timeout 10m
```

For the released OCI chart, the Longhorn subchart is already bundled:

```bash
helm install highland oci://ghcr.io/alphabravo-oss/charts/highland \
  --version 0.2.0 \
  --namespace longhorn-system --create-namespace \
  --set embeddedLonghorn.enabled=true \
  --set auth.local.password='change-me-strong' \
  --wait --timeout 10m
```

Embedded Longhorn settings are passed through under the alias. For example:

```yaml
embeddedLonghorn:
  enabled: true
  longhornUI:
    replicas: 0
  ingress:
    enabled: false
  persistence:
    defaultClassReplicaCount: 3
  defaultSettings:
    defaultDataPath: /var/lib/longhorn
```

Keep `longhornUI.replicas: 0` and `ingress.enabled: false` if Highland should remain the only console.
The `longhorn-frontend` Service may exist with no endpoints; Highland never uses it.

Verify the combined release:

```bash
kubectl -n longhorn-system rollout status daemonset/longhorn-manager --timeout=10m
kubectl -n longhorn-system get endpoints longhorn-backend
kubectl -n longhorn-system get deployment longhorn-ui \
  -o jsonpath='{.spec.replicas}{"\n"}' # must print 0
kubectl -n longhorn-system rollout status deployment/highland-api --timeout=10m
kubectl -n longhorn-system get configmap highland-config \
  -o jsonpath='{.data.config\.json}{"\n"}'
```

The manager URL in the last command must be
`http://longhorn-backend.longhorn-system.svc.cluster.local:9500`. Highland's `/readyz` remains not
ready until the embedded manager answers, which is expected during startup.

## 4. What the chart creates

| Resource | Purpose |
|----------|---------|
| Deployment `*-api` | Go BFF; mounts ConfigMap; env from Secret |
| Deployment `*-web` | nginx SPA; proxies `/auth` and `/api` to the API Service |
| ConfigMap `*-config` | Non-secret settings, including the manager URL |
| Secret `*-admin` | Local admin username/password |
| Service api + web | ClusterIP access for Highland |
| RBAC | Benchmarks plus read/watch access in the effective Longhorn namespace |
| NetworkPolicy | API egress to the effective Longhorn manager namespace |
| Ingress (optional) | External HTTPS for Highland |
| Longhorn subchart resources (embedded only) | Manager, CSI, CRDs, StorageClass, and data-plane controllers; UI replicas are zero |

Browser → **highland-web** → **highland-api** → **longhorn-backend** (ClusterIP only).

## 5. Existing Secret (recommended for GitOps)

```bash
kubectl -n highland-system create secret generic highland-admin \
  --from-literal=username=admin \
  --from-literal=password='change-me'

helm upgrade --install highland ./chart -n highland-system \
  --set auth.local.createSecret=false \
  --set auth.local.existingSecret=highland-admin
```

Create the Secret in `longhorn-system` instead when using embedded mode.

## 6. Optional OIDC

Local username/password remains available when `config.localAlways: true` (the default). OIDC users
map to admin/viewer roles through the `highland_role` claim or configured groups.

```yaml
config:
  authMode: local+oidc
  localAlways: true
auth:
  local:
    createSecret: true
    password: "break-glass-password"
  oidc:
    issuerURL: https://login.example.com
    clientID: highland
    redirectURL: https://highland.example.com/auth/oidc/callback
    createSecret: true
    clientSecret: "..."
```

## 7. Local Docker (no cluster)

```bash
docker compose -f deploy/docker-compose.yaml up --build
# http://127.0.0.1:8088  admin / highland
```

## 8. Optional Redis (multi-replica session HA)

```yaml
replicaCount:
  api: 2
redis:
  enabled: true
  addr: my-redis:6379
  passwordSecret: highland-redis
```

Without Redis, use one API replica or sticky sessions at the Ingress.

## 9. Benchmarks on cluster

Real in-cluster benchmarks are opt-in so the default chart has no storage mutation permissions:

```yaml
benchmark:
  kubernetesJobEnabled: true
```

When enabled, benchmarks run as fio Jobs and provision a PVC using the StorageClass selected in the
UI. `benchmark.storageClass` may set a default; blank requires an explicit selection. Results retain
provider, CSI driver, class, PVC/PV, node, and topology identity. Failed PVC retention is admin-only
and requires typing `RETAIN FAILED PVC`; cleanup is the default. When disabled, the offline synthetic
mode remains available but creates no Kubernetes resources.

## 10. Upgrade

### Bolt-on mode

Upgrade Highland normally. The separately installed Longhorn release is not changed:

```bash
helm dependency build ./chart
helm upgrade highland ./chart \
  --namespace highland-system \
  -f values-prod.yaml \
  --wait --timeout 10m
```

### Embedded mode

An embedded Highland chart upgrade can also upgrade Longhorn when its pinned subchart version changes.
Before upgrading:

1. Read the Highland release notes and the matching
   [Longhorn upgrade guide](https://longhorn.io/docs/1.12.0/deploy/upgrade/).
2. Confirm all volumes are healthy, resolve failed resources, and create a Longhorn system backup.
3. Respect Longhorn's supported upgrade path: one minor version at a time. Downgrades are unsupported.
4. Rebuild from `Chart.lock` and inspect the dependency version before applying the release.

```bash
helm dependency build ./chart
helm dependency list ./chart

helm upgrade highland ./chart \
  --namespace longhorn-system \
  -f values-embedded.yaml \
  --wait --timeout 10m

kubectl -n longhorn-system rollout status daemonset/longhorn-manager --timeout=10m
kubectl -n longhorn-system rollout status deployment/highland-api --timeout=10m
```

Do not change `embeddedLonghorn.enabled` from true to false on a live combined release; that removes
the subchart resources without following the storage uninstall workflow.

## 11. Uninstall

### Bolt-on mode

Removing Highland does not remove the separate Longhorn release:

```bash
helm uninstall highland --namespace highland-system --wait
```

### Embedded mode — destructive storage operation

> **Data-loss warning:** `helm uninstall highland -n longhorn-system` removes both Highland and the
> Longhorn storage control plane. A careless uninstall can delete data or leave volumes, CRDs, and the
> namespace stuck. Never use `--no-hooks`, force-delete the namespace, or manually delete Longhorn CRDs
> to bypass the safety mechanism.

Follow the official [Longhorn 1.12 uninstall runbook](https://longhorn.io/docs/1.12.0/deploy/uninstall/)
and adapt the Helm release name to `highland`:

1. Back up required data and verify the backups can be restored elsewhere.
2. Delete or migrate every workload using Longhorn PVs, then remove the corresponding PVCs, PVs, and
   Longhorn StorageClasses. Confirm no Longhorn volumes remain.
3. Explicitly enable Longhorn deletion only after the inventory is empty:

   ```bash
   kubectl -n longhorn-system get volumes.longhorn.io
   kubectl get pv,pvc -A
   kubectl -n longhorn-system patch settings.longhorn.io deleting-confirmation-flag \
     --type=merge -p '{"value":"true"}'
   ```

4. Uninstall the combined release and allow the Longhorn pre-delete hook to finish:

   ```bash
   helm uninstall highland --namespace longhorn-system --wait --timeout 15m
   ```

5. Inspect the namespace and cluster-scoped Longhorn resources before deleting anything else. If the
   hook fails, inspect the `longhorn-uninstall` Job/pod logs and continue with the matching official
   runbook. Do not improvise CRD cleanup while data may still exist.

## 12. Values reference for Longhorn modes

| Value | Default | Behavior |
|-------|---------|----------|
| `embeddedLonghorn.enabled` | `false` | Enables the aliased, pinned Longhorn 1.12.0 dependency. |
| `embeddedLonghorn.longhornUI.replicas` | `0` | Keeps the stock Longhorn UI Deployment scaled down. |
| `embeddedLonghorn.ingress.enabled` | `false` | Prevents a Longhorn UI Ingress; Highland ingress is separate. |
| `embeddedLonghorn.persistence.defaultClassReplicaCount` | Longhorn default (`3`) | Overrides replicas in the default Longhorn StorageClass. |
| `embeddedLonghorn.defaultSettings.defaultDataPath` | Longhorn default (`/var/lib/longhorn`) | Overrides the host data path for new Longhorn nodes. |
| `longhorn.namespace` | `longhorn-system` | Longhorn namespace in bolt-on mode. In embedded mode, the release namespace always wins. |
| `longhorn.enabled` | `false` | Legacy bolt-on provider switch. Set true when connecting to an existing Longhorn release. |
| `providers.longhorn.enabled` | `null` | Explicit provider-native override; null synthesizes from the legacy/embedded switch. |
| `longhorn.managerService` | `longhorn-backend` | Manager Service name in both modes. |
| `longhorn.managerPort` | `9500` | Manager REST port in both modes. |

All other Longhorn chart values can be passed under `embeddedLonghorn.*`. Consult the values for the
exact pinned chart before overriding them:

```bash
helm show values longhorn/longhorn --version 1.12.0
```

The computed manager URL is:

- Bolt-on: `http://<managerService>.<longhorn.namespace>.svc.cluster.local:<managerPort>`
- Embedded: `http://<managerService>.<release namespace>.svc.cluster.local:<managerPort>`

The API namespace environment variable, RBAC, and manager NetworkPolicy use the same effective
namespace, so custom bolt-on namespaces remain supported while embedded mode stays co-located.

## 13. Universal storage and Rook/Ceph preview

The common read-only CSI inventory is enabled by default. Cluster scope renders read-only
ClusterRole access; least-privilege namespace scope renders Roles only in the allowlist and visibly
omits cluster-scoped PV/driver/attachment metadata:

```yaml
storage:
  enabled: true
  scope:
    mode: namespaces
    namespaces: [team-a, team-b]
  writes:
    enabled: false
```

Rook/Ceph is separately opt-in and requires a dedicated read-only Dashboard credential. See the
[Rook/Ceph provider guide](providers/rook-ceph.md). New storage and Ceph writes are off by default;
enabling them installs narrowly split roles and the durable operation controller. The pool-delete
gate remains separate. Review the [storage concepts](storage-control-plane.md),
[capability matrix](storage-capability-matrix.md), [complete RBAC reference](security/storage-rbac.md),
[threat model](security/storage-threat-model.md), and [operation runbook](runbooks/storage-operations.md)
before enabling any write gate.

## 14. Development without Docker

Contributors can still use `go run`, `npm run dev`, or Compose. The production path is Helm plus
Kubernetes Secrets; shell exports are not required for a cluster install.
