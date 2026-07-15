<div align="center">

<img src="apps/web/public/favicon.svg?v=3" alt="Highland" width="88" height="88" />

# Highland

### An enterprise-grade alternative UI & management plane for [Longhorn](https://longhorn.io/)

Highland is a modern, secure console for Longhorn distributed storage — enterprise login (SSO/RBAC),
live I/O metrics, guided backups, and fio benchmarks — layered on top of your existing Longhorn
**without changing the data plane**.

[![CI](https://github.com/alphabravo-oss/highland/actions/workflows/ci.yaml/badge.svg)](https://github.com/alphabravo-oss/highland/actions/workflows/ci.yaml)
[![Publish images](https://github.com/alphabravo-oss/highland/actions/workflows/release.yaml/badge.svg)](https://github.com/alphabravo-oss/highland/actions/workflows/release.yaml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![GHCR](https://img.shields.io/badge/images-ghcr.io-2496ED?logo=github)](https://github.com/orgs/alphabravo-oss/packages?repo_name=highland)
[![Status: Alpha](https://img.shields.io/badge/status-alpha-orange.svg)](https://github.com/alphabravo-oss/highland)

**Built by [AlphaBravo](https://alphabravo.io)**

</div>

---

> **Alpha software.** Highland is under active development. APIs, Helm chart
> values, and UI are subject to change without notice, and it is **not yet
> recommended for production use**. Feedback and issues are very welcome.

![Highland dashboard in dark mode](docs/highland-dashboard-dark.png)

## Why Highland

Longhorn ships a capable UI — Highland is what you reach for when you need to run it like a **product**:
a hardened access model, an operator-grade console, and workflows that make day-2 storage administration
boringly easy.

- **Enterprise access, built in** — local admin, OIDC/SSO, RBAC (admin vs. viewer), and an audit log.
  The browser **never** talks to the Longhorn manager directly; everything flows through an authenticated
  backend-for-frontend (BFF).
- **Live insight** — real-time volume/node/disk dashboards, per-volume I/O throughput & IOPS charts,
  and on-demand **fio benchmarks** you can run from the UI.
- **Operator-grade console** — enterprise data tables everywhere (sort, search, pagination, CSV export,
  bulk actions), consolidated kebab/action menus, guided wizards, and confirmation modals on every
  destructive action. Light / dark / system themes. English & Spanish.
- **Guided backups** — a step-by-step wizard for S3 / NFS / Azure backup targets and credentials, plus
  full snapshot, backup, restore, DR-standby, and recurring-job management.
- **Full Longhorn parity — and beyond** — volumes, nodes & disks, backing images, engine images,
  instance managers, orphans, system backups, support bundles, and every manager setting (grouped, with
  a danger zone and inline docs).
- **Kubernetes-native** — Helm chart, stateless signed-cookie sessions (no Redis), Kubernetes Secrets
  for credentials, and ConfigMap-persisted benchmark history. Nothing to babysit.

---

## Quick start

### Bolt on to an existing Longhorn installation (default)

> **Prerequisites:** a Kubernetes cluster with **Longhorn already installed** (its own UI can be disabled),
> plus `helm` and `kubectl`. Highland connects to the in-cluster `longhorn-backend` service.

Install straight from GitHub Container Registry — no cloning, no image builds:

```bash
helm install highland oci://ghcr.io/alphabravo-oss/charts/highland \
  --version 0.2.0 \
  --namespace highland-system --create-namespace \
  --set auth.local.createSecret=true \
  --set auth.local.password='change-me' \
  --set longhorn.namespace=longhorn-system
```

Then open the UI:

```bash
kubectl -n highland-system port-forward svc/highland-web 8080:80
# → http://127.0.0.1:8080     log in with  admin / change-me
```

### All-in-one Highland + Longhorn (opt-in alpha)

Embedded mode installs the pinned Longhorn backend in the same Helm release and scales the stock
Longhorn UI to zero. Use it only on a cluster that does **not** already have Longhorn, prepare every
storage node using the [Longhorn prerequisites](docs/INSTALL.md#embedded-longhorn-node-prerequisites),
and install the release in `longhorn-system`:

```bash
helm install highland oci://ghcr.io/alphabravo-oss/charts/highland \
  --version 0.2.0 \
  --namespace longhorn-system --create-namespace \
  --set embeddedLonghorn.enabled=true \
  --set auth.local.createSecret=true \
  --set auth.local.password='change-me' \
  --wait --timeout 10m

kubectl -n longhorn-system port-forward svc/highland-web 8080:80
# → http://127.0.0.1:8080
```

> **Data-loss warning:** an embedded release owns the storage backend as well as Highland. Do not run
> `helm uninstall` until Longhorn-backed workloads and volumes are safely removed or backed up, and
> follow the [embedded uninstall runbook](docs/INSTALL.md#11-uninstall).

That's it. To expose it beyond your laptop, enable the Ingress (`--set ingress.enabled=true`) or a
NodePort/LoadBalancer service — see **[docs/INSTALL.md](docs/INSTALL.md)** (and **[docs/K3S.md](docs/K3S.md)**
for k3s notes).

### Container images

Prebuilt images are published to GHCR on every release:

| Image | Pull |
|-------|------|
| API (BFF) | `ghcr.io/alphabravo-oss/highland-api` |
| Web (console) | `ghcr.io/alphabravo-oss/highland-web` |

Tags: `latest`, `edge` (main), `<version>` (e.g. `0.1.0`), and `sha-<commit>`.

### Try it locally (Docker Compose)

Spin up the whole topology — including a mock Longhorn manager — on your machine:

```bash
docker compose -f deploy/docker-compose.yaml up --build
# → http://127.0.0.1:8088     admin / highland
```

---

## Architecture

```text
Browser ──▶ highland-web (nginx)  ──▶ highland-api (BFF)  ──▶ longhorn-backend:9500
            static console            authn/z · RBAC · audit    (Longhorn manager REST)
                                       stateless signed cookie
```

Highland never proxies the Longhorn manager to the browser. The **BFF** terminates auth, enforces RBAC,
rewrites manager links, negotiates content types, and audits every mutation — so the manager API is never
directly exposed.

```text
highland/
├── apps/
│   ├── api/    # Go BFF (chi): /auth, /healthz, /api/v1/lh/*, status, benchmarks
│   └── web/    # React 19 + Vite + TypeScript + Tailwind 4 console
├── chart/      # Helm chart (api + web + RBAC + NetworkPolicy)
└── docs/       # install, k3s, UX, parity matrix
```

---

## Configuration highlights

| Area | How |
|------|-----|
| **Local admin** | `auth.local.createSecret=true` + `auth.local.password`, or point at an existing Secret with `auth.local.existingSecret`. |
| **SSO / OIDC** | Configure `auth.oidc.*`; users map to admin/viewer roles. |
| **Backups** | Use the in-app **backup setup wizard** (S3 / NFS / Azure) — it provisions the credential Secret and backup target for you. |
| **Sessions** | Stateless HMAC-signed cookies — no Redis, no external session store. |
| **Longhorn namespace** | `longhorn.namespace` (default `longhorn-system`). |
| **Embedded Longhorn** | `embeddedLonghorn.enabled=true` installs pinned Longhorn 1.12.0 in the release namespace; default is `false`. |
| **Embedded tuning** | Pass Longhorn chart values under `embeddedLonghorn.*`; the stock UI remains off by default. |

Full reference: **[docs/INSTALL.md](docs/INSTALL.md)**.

---

## Development

```bash
# API (Go)                      # Web (React + Vite + TS)
cd apps/api && go test ./...    cd apps/web && npm ci && npm run dev
```

CI runs Go build/test, web typecheck/unit/build/Storybook, a Playwright smoke + a11y suite, Helm lint
and both chart deployment-mode renders, plus a parity gate on every push. Images and the Helm chart publish to GHCR on tagged releases. See
`.github/workflows/`.

---

## License

[Apache 2.0](LICENSE) © [AlphaBravo](https://alphabravo.io)

<div align="center"><sub>Built with care by <a href="https://alphabravo.io">AlphaBravo</a> — enterprise Kubernetes, security, and platform engineering.</sub></div>
