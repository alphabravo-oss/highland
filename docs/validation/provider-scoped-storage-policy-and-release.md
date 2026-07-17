# Provider-Scoped Storage Policy Validation

Date: 2026-07-17
Branch: `feat/storage-control-plane`
Cluster: k3s `v1.36.2+k3s1`, node `beastnode1`

## Outcome

Provider-scoped portable Kubernetes authorization is implemented and qualified for Longhorn,
Rook/Ceph, OpenEBS, and explicit generic CSI provider IDs. The live policy was returned to a fully
disabled state after qualification. No smoke namespace, disposable PVC/PV, or nonterminal
`StorageOperation` remains.

## Release candidate

- API image: `highland-api:provider-scope3-20260717`
- Web image: `highland-web:provider-scope-20260717`
- Highland Helm release: revision 59, chart `0.2.0`, app `0.1.0`
- Longhorn: `1.12.0`
- Rook/Ceph: Rook `1.20.2`, Ceph health `HEALTH_OK`
- OpenEBS: chart `4.5.1`

## Automated evidence

| Gate | Result |
| --- | --- |
| `go test ./...` | Pass |
| `go vet ./...` | Pass |
| Race tests for policy, observability, storage operations, storage, and watch | Pass |
| Frontend Vitest | 19 files, 83 tests passed |
| Frontend lint | Pass; warning-only pre-existing repository findings |
| Frontend typecheck | Pass |
| Production build and bundle budget | Pass; initial gzip 179,002 bytes / 256,000-byte budget |
| Helm lint and render matrix | Pass |
| Live Playwright policy qualification | 2 passed |
| Axe WCAG A/AA serious/critical findings | 0 |
| Live console and non-aborted request failures | 0 |
| Mobile dark-mode horizontal overflow | 0 across policy and all provider operations views |

The backend authorization matrix covers provider × workflow × Highland role × runtime policy,
including native-provider isolation, the global gate, generic CSI opt-in, authoritative provider
resolution, forged provider hints, signed provider attribution, provider-list normalization,
challenge tampering, replay, and ceiling intersection.

## Kubernetes admission evidence

Server-side dry-run accepted an enabled portable policy scoped to `[longhorn]`. Admission rejected:

- portable writes enabled with an empty provider list;
- portable writes disabled with a nonempty provider list;
- the legacy wildcard mixed with an explicit provider.

The CRD uses a set list, a bounded provider count, DNS-subdomain validation, parent/child CEL
invariants, explicit status schema, and wildcard isolation.

## Live provider qualification

### Longhorn-only

- Effective portable provider list: `[longhorn]`.
- A Longhorn StorageClass plan resolved `driver.longhorn.io` to `longhorn` from authoritative cluster
  data rather than the request hint.
- Rook/Ceph and OpenEBS plans returned `403 PROVIDER_POLICY_DISABLED`.
- Highland operation `storage-85dd…` created and bound a disposable Longhorn PVC.
- Highland refused an unsafe deletion when relationship evidence was partial/stale with
  `IMPACT_ANALYSIS_INCOMPLETE`; the disposable object was then removed through Kubernetes and its
  backend PV cleanup was verified.

### Rook/Ceph-only

- Effective portable provider list: `[rook-ceph]`; Longhorn and OpenEBS plans were denied.
- Highland operation `storage-1bc…` created and bound a disposable RBD PVC.
- A disposable pod mounted the claim and passed write/read verification.
- Ceph destructive pool and StorageClass gates remained disabled throughout.
- Snapshot validation was skipped because this lab does not install the Kubernetes
  `VolumeSnapshotClass` API. This is an environment prerequisite, not an authorization bypass.

### OpenEBS-only

- The plan resolved `openebs.io/local` to `openebs`.
- Highland operation `storage-e0b…` created a WaitForFirstConsumer claim.
- A disposable consumer bound the claim and passed write/read verification.
- OpenEBS exposes no provider-native mutation actions; only explicitly scoped Kubernetes workflows
  were available.

## Upgrade persistence

Live upgrade testing exposed a migration hazard: an early live revision tracked the policy CRD from
`templates/`, while the candidate initially moved it only to `crds/`. Helm interpreted that as
removal. The chart now retains both the fresh-install bootstrap CRD and an upgrade-tracked copy with
`helm.sh/resource-policy: keep`.

After one-time lab recovery, revisions 58 and 59 completed successfully. The revision 59 verification
preserved the policy UID, generation, and exact disabled spec across the upgrade. Chart tests assert
fresh-install ordering, the keep policy, and matching provider-scope validation in the rendered CRD.

## Final live state

- `highland-api` and `highland-web`: successfully rolled out, 1/1 Ready.
- `/healthz`: `status=ok`.
- `HighlandPolicy`: Ready, generation `1/1`, every write gate false, provider list `[]`.
- Rook/Ceph: `Ready/HEALTH_OK`.
- Nonterminal `StorageOperation` count: 0.
- Smoke namespaces and disposable storage objects: 0.

## Accepted boundaries

- The lab lacks the external snapshot CRDs/controller, so a Rook/Ceph VolumeSnapshot could not be
  created. Planner, provider attribution, policy, and admission behavior are covered automatically.
- The Longhorn relationship graph correctly blocked an inadequately evidenced delete. Qualification
  does not weaken that fail-closed behavior merely to make a smoke cleanup pass.
- OpenEBS native mutation is intentionally unavailable because this integration currently has no
  reviewed provider-native workflow.

## GitHub release evidence

- Release commit: [`e90121e`](https://github.com/alphabravo-oss/highland/commit/e90121e9c663ab8520c19cd2d787a3e68f038052)
- Pull request: [#13](https://github.com/alphabravo-oss/highland/pull/13)
- Merge commit: [`fd99885`](https://github.com/alphabravo-oss/highland/commit/fd998853af362dd0104008977939ec76afaaf0a6)
- Annotated release tag: [`v0.2.0`](https://github.com/alphabravo-oss/highland/tree/v0.2.0)
- Required GitHub checks: Go, web/Storybook/OpenAPI, Helm, parity, and Playwright all passed before merge.
