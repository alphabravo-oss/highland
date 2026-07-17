# Plan: Optional embedded Longhorn subchart (one-shot deploy)

Status: **Proposed** · Owner: TBD · Target chart version: 0.2.0

## 1. Goal

Let an operator deploy **Highland + a Longhorn backend in a single `helm install`**, with
Longhorn's own UI disabled, so the only console is Highland. This must be **strictly opt-in**:
the default and the existing "bolt-on to an already-installed Longhorn" flow are unchanged.

```bash
# Today (bolt-on): Longhorn must already exist in the cluster.
helm install highland oci://ghcr.io/alphabravo-oss/charts/highland -n longhorn-system \
  --set auth.local.password=…

# After this plan (all-in-one): Highland brings Longhorn with it, Longhorn UI off.
helm install highland oci://ghcr.io/alphabravo-oss/charts/highland -n longhorn-system \
  --create-namespace \
  --set embeddedLonghorn.enabled=true \
  --set auth.local.password=…
```

## 2. Background — how Highland finds Longhorn today

Highland is a bolt-on. The BFF reaches the Longhorn **manager** Service (not the UI) at a URL
built by a template helper:

- `chart/templates/_helpers.tpl` → `highland.managerUrl`:
  `http://{{ .Values.longhorn.managerService }}.{{ .Values.longhorn.namespace }}.svc.cluster.local:{{ .Values.longhorn.managerPort }}`
- `chart/templates/configmap.yaml` writes that into `config.json` as `managerUrl`, consumed by the BFF.
- `chart/values.yaml` defaults:
  ```yaml
  longhorn:
    enabled: false            # comment already says "if true, chart may declare dependency later"
    namespace: longhorn-system
    managerService: longhorn-backend
    managerPort: 9500
  ```
- `chart/templates/networkpolicy.yaml` egress allows the api pods to reach `.Values.longhorn.namespace`
  on `.Values.longhorn.managerPort`, plus DNS + 443/6443.
- The BFF `/readyz` probe actively GETs `managerUrl/v1` — so readiness already reflects backend reachability.

**Implication:** to embed Longhorn we don't touch the BFF at all. We only (a) add a conditional
subchart, and (b) make `managerUrl` point at the in-release Longhorn backend when embedded.

## 3. Reasoning / motivation

- **One-command trials & demos.** New users can stand up the full stack without first learning
  Longhorn's install. Lowers time-to-first-volume dramatically.
- **Air-gapped / edge.** A single chart + a single image set is easier to mirror and version.
- **Highland-as-the-UI story.** Disabling the Longhorn UI makes Highland the canonical console,
  which is the product's whole thesis.
- **No regression risk to existing users.** Gated behind `embeddedLonghorn.enabled=false` by default;
  the bolt-on path is the tested, supported default.

## 4. Design

### 4.1 The `longhorn:` values-key collision (the crux)

Highland already owns a top-level `longhorn:` values block for **connection config**
(`managerService`, `namespace`, `managerPort`). If we add a dependency literally named `longhorn`,
Helm maps the subchart's values under the same `longhorn:` key → collision and confusion.

**Decision: alias the dependency to `embeddedLonghorn`.**

```yaml
# chart/Chart.yaml
dependencies:
  - name: longhorn
    alias: embeddedLonghorn          # subchart values live under .Values.embeddedLonghorn
    version: "1.9.x"                 # PIN to a supported Longhorn chart release (see §4.6)
    repository: https://charts.longhorn.io
    condition: embeddedLonghorn.enabled
```

- `embeddedLonghorn.enabled` (parent value) both **toggles the dependency** (`condition:`) and is
  passed down to Longhorn (which ignores an unknown `enabled` key — harmless).
- Highland's existing `longhorn:` connection block is **kept as-is** → backward compatible.
- Rejected alternative: rename Highland's connection block (e.g. to `backend:`). This is a breaking
  values change for every existing user for no functional gain. Aliasing avoids it.

### 4.2 Wiring `managerUrl` when embedded

Longhorn's chart creates the manager Service `longhorn-backend:9500` in the **release namespace**.
When embedded, Highland must point there instead of at `.Values.longhorn.namespace`.

Make the helper namespace-aware:

```gotemplate
{{- define "highland.managerUrl" -}}
{{- $ns := .Values.longhorn.namespace -}}
{{- if .Values.embeddedLonghorn.enabled -}}
{{-   $ns = .Release.Namespace -}}   {{/* subchart installs into the release namespace */}}
{{- end -}}
http://{{ .Values.longhorn.managerService }}.{{ $ns }}.svc.cluster.local:{{ .Values.longhorn.managerPort }}
{{- end }}
```

The service name (`longhorn-backend`) and port (`9500`) are Longhorn defaults and stay in
`.Values.longhorn.*`, so a user who renames them still has one place to do it. Apply the same
`$ns` override in `networkpolicy.yaml`'s egress selector so the api pods may reach the co-located
backend.

### 4.3 Namespace strategy

Helm installs a subchart into the **same release namespace** as the parent — you cannot place the
subchart in a different namespace via standard Helm. Longhorn strongly expects `longhorn-system`
(instance-manager affinity, docs, support tooling).

**Decision:** when `embeddedLonghorn.enabled=true`, **recommend installing the whole release into
`longhorn-system`** (`-n longhorn-system --create-namespace`). Highland co-locates there. Document
this prominently; add a `NOTES.txt` hint and a `fail`-fast guard is optional (see §4.7).

### 4.4 Disabling the Longhorn UI

Longhorn chart knobs, set under the alias:

```yaml
embeddedLonghorn:
  enabled: false
  longhornUI:
    replicas: 0            # no Longhorn UI pods
  ingress:
    enabled: false         # no Longhorn UI ingress (Highland's ingress is separate)
```

`longhornUI.replicas: 0` is the supported switch to remove the UI deployment; the `longhorn-frontend`
Service may still be templated but has no endpoints. That's acceptable (Highland never references it).
Confirm against the pinned Longhorn version during implementation — value paths occasionally move.

### 4.5 Sensible embedded defaults we ship

Provide a curated `embeddedLonghorn:` block in `values.yaml` (all overridable):

```yaml
embeddedLonghorn:
  enabled: false
  longhornUI:
    replicas: 0
  ingress:
    enabled: false
  # Leave persistence/default-settings to Longhorn defaults; call out the ones
  # operators most often set (defaultDataPath, replica count, etc.) in docs.
  # defaultSettings:
  #   defaultDataPath: /var/lib/longhorn
```

### 4.6 Version pinning & vendoring

- **Pin** the Longhorn chart `version` (e.g. `1.9.x` → resolve to an exact release at implementation
  time; the target Longhorn line the team supports). Never float to `*`.
- `helm dependency update ./chart` writes `chart/charts/longhorn-<ver>.tgz` and a `Chart.lock`.
- **Commit `Chart.lock`**; **gitignore `chart/charts/*.tgz`** and run `helm dependency build` in CI/release
  (reproducible from the lock). Alternatively vendor the `.tgz` — decide in §13.

### 4.7 CRD lifecycle, ordering, teardown

- Longhorn ships its CRDs inside `templates/` (so `helm upgrade` updates them — good).
- **Ordering:** Highland api may start before the Longhorn manager is Ready; `/readyz` will report
  not-ready until the backend answers, which is correct behavior. Document `--wait --timeout 10m` for
  a clean one-shot install.
- **Teardown is dangerous:** a naive `helm uninstall` can strip the manager while volumes still exist,
  and Longhorn requires the `deleting-confirmation-flag` + uninstaller Job to remove data safely.
  Embedding does **not** change Longhorn's uninstall semantics — **document loudly** that
  `helm uninstall` of an embedded release can orphan CRDs/volumes and risks data loss. Provide the
  documented Longhorn uninstall runbook.

### 4.8 What does NOT change

- BFF code, images, RBAC templates, auth/CSRF/observability — all untouched.
- Default install (`embeddedLonghorn.enabled=false`) renders byte-for-byte as today (verify in tests).

## 5. Non-goals

- Managing Longhorn upgrades/migrations on the user's behalf beyond what the pinned subchart does.
- Multi-namespace splits (Highland in ns A, Longhorn in ns B) via one release — not supported by Helm.
- Replacing the bolt-on flow. Embedded is an alternative, not the default.

## 6. Task breakdown

### Phase A — Chart mechanics
1. Add the aliased `dependencies:` block to `chart/Chart.yaml` (§4.1). Bump chart `version` → `0.2.0`.
2. `helm dependency update ./chart`; commit `Chart.lock`; add `chart/charts/` to `.gitignore`.
3. Make `highland.managerUrl` namespace-aware (§4.2).
4. Apply the same namespace override to `networkpolicy.yaml` egress.
5. Add the `embeddedLonghorn:` block to `chart/values.yaml` with `enabled: false` + UI-off defaults (§4.5).
6. `NOTES.txt`: when embedded, print backend status + the "install into longhorn-system" and uninstall caveats.

### Phase B — CI / release
7. `ci.yaml` Helm job: add `helm dependency build ./chart` before `helm lint`/`helm template`.
   Add a second `helm template … --set embeddedLonghorn.enabled=true` render to the matrix.
8. `release.yaml` chart job: add `helm dependency build ./chart` before `helm package`. Confirm the
   packaged `.tgz` bundles the subchart (or documents pulling it at install time).

### Phase C — Docs
9. `README.md`: add a short "All-in-one (Highland + Longhorn)" quickstart alongside the bolt-on one,
   with the namespace + uninstall caveats. Keep it clearly labeled opt-in / alpha.
10. `docs/INSTALL.md`: full embedded install/upgrade/uninstall runbook, node prerequisites
    (open-iscsi, nfs client utils, kernel modules), and the data-loss warning.
11. Values reference: document `embeddedLonghorn.*` and the `managerUrl` override behavior.

### Phase D — Tests & validation (see §7–§9)

## 7. Testing strategy

### 7.1 Template / render tests (no cluster)
- `helm dependency build ./chart` succeeds from `Chart.lock`.
- `helm lint ./chart` passes.
- **Disabled (default) render** `helm template highland ./chart -n highland-system`:
  - No `longhorn.io` / Longhorn resources present.
  - `config.json` `managerUrl` == `http://longhorn-backend.longhorn-system.svc.cluster.local:9500`.
  - Output is **unchanged vs. current main** (golden-file / `diff` gate — proves zero regression).
- **Enabled render** `helm template highland ./chart -n longhorn-system --set embeddedLonghorn.enabled=true`:
  - Longhorn manager DaemonSet + `longhorn-backend` Service present.
  - Longhorn UI deployment has **0 replicas** (or is absent); no Longhorn UI ingress.
  - `config.json` `managerUrl` namespace == release namespace (`longhorn-system`).
  - NetworkPolicy egress namespace selector == release namespace.
  - All docs render as valid YAML (`yaml.safe_load_all`).
- Optional: adopt `helm unittest` for the assertions above so they run in CI.

### 7.2 Live validation (ephemeral cluster or isolated namespace)
> ⚠️ The shared dev k3s (`beastnode1`) **already runs standalone Longhorn 1.12.0 in longhorn-system**.
> Do **not** embed-install over it. Use a throwaway `kind`/k3d cluster (or a dedicated node pool) so the
> embedded Longhorn doesn't fight the existing one for disks/CRDs.

1. `helm install hl ./chart -n longhorn-system --create-namespace --set embeddedLonghorn.enabled=true
   --set auth.local.password=… --wait --timeout 10m`.
2. Longhorn manager pods Ready; `longhorn-backend` Service has endpoints.
3. `longhorn-ui` / `longhorn-frontend` deployment: **0 replicas**.
4. Highland api `/readyz` → 200 (`managerUrl` reachable), `/healthz` → ok.
5. Highland UI lists nodes/disks; create a small volume through Highland → it appears via
   `kubectl get volumes.longhorn.io -n longhorn-system`.
6. `/metrics` still scrapes (observability unaffected).
7. `helm upgrade` (no-op values) is stable; CRDs unchanged.
8. Teardown per the documented Longhorn uninstall runbook (confirm no dangling namespaces on a scratch cluster).

## 8. Definition of Done

- [ ] `embeddedLonghorn.enabled=false` (default): render is **identical** to pre-change main (golden diff clean); bolt-on install works as before.
- [ ] `embeddedLonghorn.enabled=true`: single `helm install` brings up Longhorn backend **with UI at 0 replicas**, and Highland becomes Ready and drives volumes end-to-end on an ephemeral cluster.
- [ ] `managerUrl` + NetworkPolicy egress correctly target the in-release backend when embedded, the external one when not.
- [ ] Longhorn chart version is **pinned**; `Chart.lock` committed; `helm dependency build` reproducible.
- [ ] CI passes with the new `dependency build` step and renders **both** enabled/disabled variants.
- [ ] Release job packages/publishes the chart (subchart bundled or documented) to OCI.
- [ ] README + INSTALL document the opt-in, the `longhorn-system` namespace requirement, node prerequisites, and the uninstall/data-loss caveats.
- [ ] Chart `version` bumped (0.2.0); CHANGELOG/notes updated.

## 9. Risks & mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| `longhorn:` vs subchart key collision | Confusing/wrong values | Alias to `embeddedLonghorn` (§4.1); keep existing key |
| Longhorn expects `longhorn-system` | Instance-manager/support quirks | Require/recommend release ns = `longhorn-system`; NOTES + docs; optional `fail` guard |
| Heavy backend + node prereqs | Install fails on unprepared nodes | Document open-iscsi/nfs/kernel prereqs; `--wait` surfaces failures early |
| Naive `helm uninstall` orphans CRDs/volumes | **Data loss / stuck namespace** | Loud docs + Longhorn uninstall runbook; never auto-delete data |
| Subchart version drift | Surprise upgrades | Pin exact version + `Chart.lock`; bump deliberately |
| CI/release forget `dependency build` | Broken lint/package | Add the step in both workflows; covered by DoD |
| Embedding over an existing Longhorn | CRD/disk conflicts | Docs: one Longhorn per cluster; live tests only on ephemeral clusters |
| Chart size / OCI packaging | Larger artifact, mirror cost | Acceptable; note in release docs; keep bolt-on as the lean default |

## 10. Rollback

- Feature is additive and gated. To back out: set/keep `embeddedLonghorn.enabled=false` (or revert the
  `dependencies` block + helper change). No BFF or data-plane changes to undo.

## 11. Open decisions (resolve before implementation)

1. **Vendor the subchart `.tgz`** (committed, air-gap-friendly) **or** pull via `helm dependency build`
   in CI/release (smaller repo)? Recommendation: `Chart.lock` + build-in-CI, gitignore the tgz.
2. **Exact Longhorn version to pin** — match the team's supported Longhorn line; validate UI-disable
   value paths against it.
3. **Hard guard** on namespace: `fail` the template when `embeddedLonghorn.enabled` and
   `.Release.Namespace != "longhorn-system"`, or just warn in NOTES? Recommendation: warn, don't block
   (advanced users may knowingly deviate).
4. Whether to also expose a minimal `embeddedLonghorn.defaultSettings` passthrough in our values or let
   users set it under the alias directly.
