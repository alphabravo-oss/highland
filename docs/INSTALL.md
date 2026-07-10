# Install Highland (Kubernetes-native)

Highland is installed **the same way as Longhorn**: Helm chart + container images + Secrets.
You do **not** need shell `export` for production.

## Prerequisites

- Kubernetes (k3s, kind, RKE2, …)
- Helm 3
- Longhorn already installed (bolt-on) **or** install Longhorn first
- Container images built/pushed (or use compose for local smoke)

## 1. Build & push images (once)

```bash
# from highland/
docker build -t your-registry/highland-api:0.1.0 apps/api
docker build -t your-registry/highland-web:0.1.0 apps/web
docker push your-registry/highland-api:0.1.0
docker push your-registry/highland-web:0.1.0
```

## 2. Helm install (local admin, no OIDC)

```bash
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
    password: "change-me-strong"   # prefer sealed-secrets / external-secrets in real prod
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
  className: traefik   # or nginx
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
helm upgrade --install highland ./chart -n highland-system --create-namespace -f values-prod.yaml
```

## 3. What the chart creates

| Resource | Purpose |
|----------|---------|
| Deployment `*-api` | Go BFF; mounts ConfigMap; env from Secret |
| Deployment `*-web` | nginx SPA; proxies `/auth` `/api` → api Service |
| ConfigMap `*-config` | Non-secret settings (`config.json`) |
| Secret `*-admin` | Local admin username/password |
| Service api + web | ClusterIP |
| NetworkPolicy | API egress to Longhorn manager |
| Ingress (optional) | External HTTPS |

Browser → **highland-web** → **highland-api** → **longhorn-backend** (ClusterIP only).

## 4. Existing Secret (recommended for GitOps)

```bash
kubectl -n highland-system create secret generic highland-admin \
  --from-literal=username=admin \
  --from-literal=password='change-me'
```

```bash
helm upgrade --install highland ./chart -n highland-system \
  --set auth.local.createSecret=false \
  --set auth.local.existingSecret=highland-admin \
  ...
```

## 5. Optional OIDC (still keeps local admin if `localAlways`)

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

## 6. Local Docker (no cluster)

```bash
docker compose -f deploy/docker-compose.yaml up --build
# http://127.0.0.1:8088  admin / highland
```

## 7. Optional Redis (multi-replica session HA)

```yaml
replicaCount:
  api: 2
redis:
  enabled: true
  addr: my-redis:6379
  passwordSecret: highland-redis
```

Without Redis, use `replicaCount.api: 1` or sticky sessions at the Ingress.

## 8. OIDC (optional; local admin remains)

Local username/password from Secret always works when `config.localAlways: true` (default).

OIDC is fully implemented (`/auth/oidc/start` + `/auth/oidc/callback`). Set issuer, client ID, redirect, client secret via values/Secret. Map role via claim `highland_role` (or groups containing admin/operator).

## 9. Benchmarks on cluster

When the API pod has a ServiceAccount that can create Jobs, benchmarks run as real fio Jobs.
Offline / no kube API → same API returns synthetic results so UI keeps working.

## 10. Dev on laptop without Docker

Still supported for contributors (`go run` / `npm run dev` / compose).
**Production path is Helm + Secrets**, not exports.
