# Admin storage write policy validation

Validated: 2026-07-16 UTC

## Source and contract evidence

- Runtime model, watcher, fail-closed snapshot, optimistic update, challenge verification, status,
  SSE invalidation, and policy API: `apps/api/internal/policy/`.
- Dedicated bounded authorization-failure metric and critical alert identify active operations that
  lose an installed Kubernetes permission without placing resource names or error text in labels.
- Action/role impact and policy-version-bound workflow challenges:
  `apps/api/internal/storage/operations/`.
- CRD and fresh-install ordering: `chart/crds/highlandpolicies.highland.io.yaml` and
  `chart/templates/highlandpolicy.yaml`.
- Permission ceiling and named-singleton RBAC: `chart/templates/rbac.yaml`.
- Admin UI and confirmation: `apps/web/src/features/admin/StoragePolicyPage.tsx`.
- OpenAPI and examples: `docs/openapi/storage-v1.yaml` and
  `docs/examples/highland-policy/`.
- Architecture/security: ADR 0002, storage threat model, storage RBAC reference, install guide,
  provider guides, and storage-operation runbook.

## Automated validation

All passed:

```text
go test ./...
go vet ./...
go test -race ./internal/policy ./internal/storage/operations ./internal/storage \
  ./internal/watch ./internal/auth ./internal/middleware

npm run typecheck
npm run lint
npm test -- --run
npm run build

helm lint ./chart ...
SKIP_DEPENDENCY_BUILD=1 ./hack/test-chart.sh
kubectl apply --server-side --dry-run=server \
  -f chart/crds/highlandpolicies.highland.io.yaml
git diff --check
```

Results:

- 19 frontend test files / 79 tests passed.
- Production initial bundle: 178,713 gzip bytes, below the 256,000-byte budget.
- Lint completed with 17 pre-existing warnings and no errors; the policy page/dialog introduced no
  new warning.
- Chart matrix passed default read-only, runtime control without ceiling, cluster and namespace
  portable ceilings, Longhorn ceiling, independent Ceph ceilings, invalid combinations, embedded
  Longhorn, no wildcard RBAC, and fresh CRD-before-singleton ordering.
- Strict server decoding rejected an unknown `spec.storage.unknownCapability`.

Security tests cover unauthenticated access, viewer/operator denial, CSRF, missing/incorrect/expired/
tampered/cross-user/cross-cluster challenges, exact Ceph deletion phrase, ceiling violations, stale
versions, concurrent conflict, idempotent retry, malformed policy, coherent concurrent snapshots,
and action policy assignments.

## Live environment

```text
URL:                 http://100.116.90.61:30284
Kubernetes:          v1.36.2+k3s1
Namespace/release:   longhorn-system/highland
Helm revision:       55
API image:           highland-api:admin-policy5-20260716
Web image:           highland-web:storage-policy-scopes-20260716
Longhorn:            1.12.x embedded
Rook/Ceph:           configured, supported read integration
OpenEBS:             configured, native actions remain read-only
Cluster identity:    beastnode1/highland-lab
```

The final QA-only image patches were rolled directly onto the disposable Deployment after Helm
revision 55 to avoid changing CRD ownership while moving the source chart to Helm's `crds/`
bootstrap mechanism. The release's stored values still name the immediately preceding QA image
tags; a normal fresh install from the final chart has no such transition.

## Live policy and RBAC evidence

- Installed ceiling: portable, Longhorn, Rook/Ceph, Ceph StorageClass delete, and Ceph pool delete
  permissions installed.
- Final requested/effective policy: all gates false.
- Final policy generation/observed generation: 9/9.
- Final nonterminal operation count: zero.
- ServiceAccount:
  - get/update named `HighlandPolicy/highland`: yes;
  - update another policy: no;
  - delete the singleton: no;
  - create Kubernetes RBAC Role: no;
  - create PVC and use explicitly installed Ceph pool permissions: yes.
- Temporary live viewer and operator accounts each received:
  - `200` for effective policy read;
  - `403` for policy history;
  - `403` for policy planning/mutation.
  They were deleted after validation.
- Two simultaneous admin sessions planned from one resourceVersion. The first update reached
  generation 8; the second returned `409 POLICY_STALE`. Policy was narrowed to disabled at
  generation 9.

## Live transition and durable-operation evidence

1. Enabled portable + Longhorn policy without restart; the action catalogue immediately exposed
   `create-pvc` and `longhorn-volume-attach`.
2. Narrowed back to disabled; the catalogue updated without reload.
3. Created a disposable 64 MiB Longhorn PVC through a signed, policy-version-bound durable
   `StorageOperation`; it reached `Succeeded` and the PVC reached `Bound`.
4. Highland correctly blocked destructive cleanup with `IMPACT_ANALYSIS_INCOMPLETE`. The disposable
   PVC was removed directly; its PVC, PV, and Longhorn volume all disappeared.
5. Submitted a second disposable 32 MiB PVC while enabled. Its operation was `Pending` when policy
   was disabled at generation 7.
6. A new plan immediately returned `403 ACTION_FORBIDDEN`, while the already-approved operation
   continued to `Succeeded` and the PVC reached `Bound`.
7. The second disposable PVC was removed directly; PVC, PV, and Longhorn volume cleanup was
   confirmed.
8. API restarts and Helm revisions 52 through 55 preserved the policy singleton and its runtime
   state.

## Live browser, accessibility, and observability evidence

- Desktop dark mode: all six controls, requested/effective/ceiling distinctions, audit facts, and
  disabled state visible with no console errors.
- The final scope-oriented refinement separates the global admission gate, cross-provider
  Kubernetes workflows, Longhorn-only native workflows, and Rook/Ceph-only native workflows. It
  includes safe draft presets for disabled, Longhorn-native-only, and Longhorn plus PVC lifecycle.
- Mobile 390×844: zero horizontal page overflow.
- Confirmation modal: current/requested/effective/ceiling matrix, actor, request ID, workflows,
  roles, active operations, cluster identity, and typed phrases visible; header/footer remain pinned
  within a 1,000-pixel viewport.
- Light and dark rendering exercised.
- Axe: zero serious/critical findings on the policy page and open confirmation modal.
- Metrics reported generation 5 during the captured check, zero effective capabilities, zero
  ceiling mismatch, and bounded update-result labels. Later live transitions reached generation 9.
- `/status` reports runtime source, effective policy, generation, observed generation, stale/partial
  state, and conditions without credentials.

## Accepted validation boundaries

- Destructive Ceph execution was not attempted. Its independent ceiling, runtime gate, admin role,
  exact phrase, version/runtime verification, and dependency checks are covered by render/unit/API
  tests. Ordinary live smoke intentionally kept Ceph runtime gates disabled.
- Provider-native Longhorn destructive workflows were not executed against persistent data. Their
  complete action registry, role/risk/confirmation mapping, planner/controller contracts, and
  policy gate are covered by tests; live validation used disposable portable Longhorn PVCs.
- The existing cluster's impact graph was partial for PVC deletion, so Highland correctly failed
  closed. Cleanup used direct Kubernetes deletion only for the two disposable smoke resources.
- The complete Playwright visual suite and a human screen-reader pass remain release-qualification
  work. Focused authenticated desktop/mobile, light/dark, console, keyboard semantics, dialog
  accessibility, and Axe checks passed.
