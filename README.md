# Highland

**Highland** is a bolt-on management plane for [Longhorn](https://longhorn.io/): a modern console, native auth, and BFF — without forking Longhorn’s data plane.

> Longhorn is the cattle; Highland is the ranch that manages them.

## Install (Kubernetes — like Longhorn)

Production is **Helm + images + Secrets**, not shell exports.

```bash
# Build images, then:
helm upgrade --install highland ./chart \
  -n highland-system --create-namespace \
  --set image.api.repository=YOUR_REG/highland-api \
  --set image.web.repository=YOUR_REG/highland-web \
  --set image.api.tag=0.1.0 --set image.web.tag=0.1.0 \
  --set auth.local.createSecret=true \
  --set auth.local.password='change-me' \
  --set longhorn.namespace=longhorn-system
```

Full guide: **[docs/INSTALL.md](docs/INSTALL.md)** · k3s notes: **[docs/K3S.md](docs/K3S.md)**

```text
Browser → highland-web (nginx) → highland-api → longhorn-backend:9500
         ↑ Ingress / Service      ↑ Secret (admin) + ConfigMap
```

Local Docker (same topology):

```bash
docker compose -f deploy/docker-compose.yaml up --build
# http://127.0.0.1:8088  admin / highland
```

## Status

**Phase 0** foundations + **Phase 1 parity core** wired through the authenticated BFF proxy.

| Area | Status |
|------|--------|
| Auth (local login, session cookie, `/auth/me`) | Done |
| Transparent proxy `/api/v1/lh/*` → manager `/v1/*` + link rewrite | Done |
| AppShell, theme light/dark/system, Lucide nav | Done |
| Dashboard (collections + optional `/dashboard`) | Done |
| Volumes list/create/delete/detail + manager actions map | Done |
| Nodes & disks (scheduling toggle, disk capacity) | Done |
| Backups, backup targets, recurring jobs | Done |
| Settings (grouped, danger zone) | Done |
| Support bundles | Done |
| Engine images, backing images, instance managers, orphans, system backups | Done (list/CRUD as applicable) |
| Live I/O metrics / fio benchmarks | Phase 3 (shell pages only) |
| OIDC, multi-user audit productization | Phase 2 |

The browser **never** talks to the Longhorn manager directly — only Highland API.

## Layout

```text
highland/
├── README.md
├── apps/
│   ├── api/          # Go BFF (chi): /auth, /healthz, /api/v1/lh/*
│   └── web/          # React 19 + Vite + TS + Tailwind 4 SPA
├── chart/            # Helm chart (api + web + NetworkPolicy)
└── docs/
```

Upstream Longhorn lives in the sibling `../longhorn/` tree and is **not** modified.

## Prerequisites

- Go 1.22+
- Node 20+ / npm
- Optional: Helm 3, kind/k3d, a running Longhorn manager

## Tests & CI

```bash
# Full local CI parity (parity matrix + unit + e2e + helm)
./hack/run-ci-local.sh

# Pieces
./hack/check-parity.sh            # P0 gate on docs/parity-matrix.yaml
cd apps/api && go test ./...
cd apps/web && npm test && npm run typecheck && npm run build
cd apps/web && npm run test:e2e   # starts mock manager + API + Vite via hack/e2e-stack.sh
```

Plan / status: see `../HIGHLAND_PLAN.md` (v0.2) and `docs/parity-matrix.yaml`.

**k3s later:** `docs/K3S.md` — offline mock remains the default; point `HIGHLAND_MANAGER_URL` at `longhorn-backend` when ready.

## Auth: local admin (primary)

**Local username/password login is the default and does not need OIDC.**

```bash
export HIGHLAND_AUTH_MODE=local          # default
export HIGHLAND_LOCAL_ALWAYS=true        # default — break-glass local even if OIDC added later
export HIGHLAND_ADMIN_USER=admin
export HIGHLAND_ADMIN_PASSWORD='change-me'
# Optional multi-user JSON:
# export HIGHLAND_USERS='[{"username":"admin","password":"...","role":"admin"}]'
```

| Env | Default | Meaning |
|-----|---------|---------|
| `HIGHLAND_AUTH_MODE` | `local` | `local` \| `oidc` \| `local+oidc` |
| `HIGHLAND_LOCAL_ALWAYS` | `true` | Keep `POST /auth/login` for admin even in OIDC-heavy setups |
| `HIGHLAND_OIDC_*` | empty | Optional IdP; not required |
| `HIGHLAND_OIDC_MOCK` | `false` | Dev-only mock IdP |

Public discovery: `GET /auth/providers` → `{ "local": true, "oidc": false, ... }`.

**Dev roles** (enabled by default via `HIGHLAND_DEV_ROLES=1`):

| User | Password | Role |
|------|----------|------|
| admin | highland | admin |
| operator | operator | operator (no settings writes) |
| viewer | viewer | viewer (GET only) |

**E2E (T1.12):** Playwright `e2e/parity.spec.ts` — login → create volume → snapshot against
`cmd/mock-longhorn-manager` (not a reimplementation of Longhorn; fixture for CI).

**kind (best-effort real cluster):**

```bash
./hack/kind-up.sh
# then port-forward longhorn-backend and run API/web, or:
USE_MOCK=0 HIGHLAND_MANAGER_URL=http://127.0.0.1:9500 ./hack/install-dev.sh
```

GitHub Actions: `.github/workflows/ci.yaml` (api, web, e2e, helm).

## Run API (local)

```bash
cd apps/api

export HIGHLAND_MANAGER_URL=http://127.0.0.1:9500   # longhorn-backend
export HIGHLAND_ADMIN_USER=admin
export HIGHLAND_ADMIN_PASSWORD=highland
export HIGHLAND_LISTEN_ADDR=:8080

go run ./cmd/highland-api
```

Smoke:

```bash
curl -s http://127.0.0.1:8080/healthz
curl -s -c /tmp/hc.jar -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"highland"}' \
  http://127.0.0.1:8080/auth/login
curl -s -b /tmp/hc.jar http://127.0.0.1:8080/auth/me
curl -s -b /tmp/hc.jar http://127.0.0.1:8080/api/v1/lh/volumes
curl -s -b /tmp/hc.jar http://127.0.0.1:8080/api/v1/lh/nodes
curl -s -b /tmp/hc.jar http://127.0.0.1:8080/api/v1/lh/settings
```

Tests:

```bash
cd apps/api && go test ./...
```

## Run web (local)

```bash
cd apps/web
npm install
npm run dev
```

Vite proxies `/auth` and `/api` to `http://127.0.0.1:8080` (override with `VITE_API_PROXY`). Open http://localhost:5173.

**One-shot dev stack (mock manager + API + web):**

```bash
./hack/install-dev.sh
```

```bash
npm run typecheck
npm run build
npm test
npm run test:e2e
```

## How UI actions work

1. UI loads collections via `GET /api/v1/lh/<type>` (TanStack Query).
2. Destructive/mutating ops use **manager-provided `actions` / `links`** (stock Longhorn UI pattern), posted through the same proxy.
3. Create uses `POST /api/v1/lh/<collection>` with Rancher-style JSON bodies.

## Helm

```bash
helm lint ./chart
helm template highland ./chart --namespace highland-system
```

```bash
kubectl create secret generic highland-admin \
  --from-literal=username=admin \
  --from-literal=password='change-me'
```

## Design system

- React 19 + TypeScript strict + Vite
- Tailwind CSS v4 + Lucide icons
- Light / Dark / System theme (`highland-theme`, FOUC-safe)
- AppShell: collapsible sidebar + top menu bar

## Spec

See `../HIGHLAND_PLAN.md` for the full parity matrix and later phases.
