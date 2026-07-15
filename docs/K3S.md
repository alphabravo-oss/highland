# Running Highland on k3s (later)

This project is fully developable **without** a cluster using the mock Longhorn manager.
When you move to a Linux node with **k3s**, use this path.

## 1. Install Longhorn on k3s

Follow upstream Longhorn install for k3s (open-iscsi, multipath blacklist as required).

```bash
helm repo add longhorn https://charts.longhorn.io
helm repo update
helm upgrade --install longhorn longhorn/longhorn \
  -n longhorn-system --create-namespace \
  --wait --timeout 15m
```

## 2. Port-forward manager (dev) or in-cluster deploy

**Dev laptop / same node:**

```bash
kubectl -n longhorn-system port-forward svc/longhorn-backend 9500:9500
```

```bash
cd apps/api
export HIGHLAND_MANAGER_URL=http://127.0.0.1:9500
export HIGHLAND_ADMIN_USER=admin
export HIGHLAND_ADMIN_PASSWORD='change-me'
export HIGHLAND_DEV_ROLES=0          # production: no extra accounts
export HIGHLAND_OIDC_MOCK=0          # enable real OIDC env vars instead
export HIGHLAND_AUDIT_FILE=/var/log/highland-audit.jsonl
go run ./cmd/highland-api
```

```bash
cd apps/web
export VITE_API_PROXY=http://127.0.0.1:8080
npm run dev
```

**In-cluster:** build images, set chart `longhorn.managerService=longhorn-backend`, `auth.local.existingSecret`, NetworkPolicy on.

```bash
./hack/kind-up.sh   # similar pattern; adapt for k3s context
helm dependency build ./chart
helm upgrade --install highland ./chart -n highland-system --create-namespace \
  --set longhorn.enabled=false \
  --set longhorn.namespace=longhorn-system
```

## 3. What becomes “real” on k3s

| Offline (now) | On k3s |
|---------------|--------|
| `mock-longhorn-manager` | `longhorn-backend:9500` |
| Synthetic benchmarks | Same API → future fio Job controller |
| Metrics scrape of mock `/metrics` | Manager `/metrics` live series |
| Preflight `skip` node checks | Run real node preflight / Longhorn jobs |
| OIDC mock | Real IdP via `HIGHLAND_OIDC_*` |

## 4. Offline CI stays the gate

```bash
./hack/run-ci-local.sh
```

Never requires k3s. Live validation is additive later.
