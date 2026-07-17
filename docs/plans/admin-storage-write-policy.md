# Admin-Controlled Storage Write Policy Plan

Status: **Implemented and validated — 2026-07-16**

This document is the authoritative, checkable implementation plan for allowing
Highland administrators to enable and disable supported storage mutations from
the Admin UI. A task may be checked only when its stated evidence exists.

### Task tracking rules

- Leave every implementation checkbox unchecked until the task's source change
  and its proportionate test or runtime evidence both exist.
- Complete phases in order unless a task explicitly has no dependency on the
  preceding phase.
- Record evidence in section 8 as work is completed.
- If implementation changes an architectural decision, update this plan and
  the relevant ADR before checking dependent tasks.
- A passing narrow unit test cannot prove a chart, RBAC, browser, upgrade, or
  live-cluster requirement.
- Do not check a phase definition of done until every task in that phase is
  either complete or recorded as an explicitly accepted limitation.

Completion evidence is consolidated in
[`docs/validation/admin-storage-write-policy.md`](../validation/admin-storage-write-policy.md).
Checked live-provider items may be satisfied by a documented, reviewed
validation boundary where executing a destructive or environment-dependent
workflow would create more risk than evidence.

## 1. Outcome

An administrator can safely change Highland's runtime storage-write policy from
an admin-only page without editing Helm values or restarting Highland.

The feature must preserve all of the following:

- Highland user RBAC remains enforced by the API.
- Kubernetes RBAC remains an installation-time permission ceiling.
- The UI cannot grant the Highland ServiceAccount permissions that were not
  explicitly installed by the cluster operator.
- Disabling writes blocks new plans and submissions immediately.
- Already-approved operations continue reconciling safely.
- Longhorn, portable Kubernetes, Rook/Ceph, and destructive Ceph permissions
  remain independently understandable and gated.
- Every policy change is planned, confirmed, persisted, audited, observable,
  reversible, and resistant to concurrent updates.
- Helm upgrades do not silently overwrite the runtime policy.

## 2. Why this needs a policy subsystem

Today `storage.writes.enabled` is rendered into Highland's Helm-owned
configuration and read at API startup.

It also affects:

- whether the operation controller starts;
- whether Highland can create `StorageOperation` resources;
- whether PVC and VolumeSnapshot writer RBAC is rendered;
- whether Ceph writer RBAC is rendered;
- whether write workflow cards and submission routes are enabled.

A simple UI checkbox is therefore insufficient. Updating the current ConfigMap
would require a restart, create Helm drift, and may claim writes are enabled
while the ServiceAccount still lacks writer permissions.

The implementation will explicitly separate:

1. **Installed permission ceiling** — immutable at runtime and controlled by
   Helm/cluster operators.
2. **Runtime storage policy** — mutable by Highland administrators, but never
   able to exceed the installed ceiling.
3. **Per-action Highland authorization** — the existing viewer/operator/admin
   role checks, evaluated for every plan and submission.

## 3. Decisions

### 3.1 Installation-time opt-in

Add:

```yaml
adminPolicyControl:
  enabled: false
  installStorageWriterPermissions: false
  ceiling:
    portableKubernetesWrites: false
    longhornWrites: false
    rookCephWrites: false
    allowCephStorageClassDelete: false
    allowCephPoolDelete: false
```

Rules:

- The default installation remains read-only and receives no additional writer
  RBAC.
- `adminPolicyControl.enabled=true` enables the policy API, controller, and UI.
- `installStorageWriterPermissions=true` installs the bounded Kubernetes
  permissions needed by supported workflows.
- Runtime policy may enable a capability only when its installed ceiling is
  true.
- Highland never creates or modifies Roles, ClusterRoles, RoleBindings, or
  ClusterRoleBindings through the Admin API.

Reasoning: this keeps privilege escalation in Helm/GitOps, where cluster
operators can review it, while permitting authorized runtime activation.

### 3.2 Runtime source of truth

Add a namespaced singleton custom resource:

```yaml
apiVersion: highland.io/v1alpha1
kind: HighlandPolicy
metadata:
  name: highland
spec:
  storage:
    acceptNewOperations: false
    portableKubernetesWrites: false
    longhornWrites: false
    rookCephWrites: false
    allowCephStorageClassDelete: false
    allowCephPoolDelete: false
status:
  effective:
    # Computed intersection of requested policy and installed ceiling.
  observedGeneration: 1
  conditions: []
```

Rules:

- Helm creates the singleton object.
- Highland may `get`, `list`, `watch`, `update`, and `patch` only the named
  singleton.
- Highland receives no permission to delete the object.
- Runtime state is the intersection of `spec` and installed capability ceiling.
- Status explains every requested-but-unavailable capability.
- The resource version is used for optimistic concurrency.
- Helm must preserve an existing runtime `spec` during ordinary upgrades.

Reasoning: a typed CR provides validation, watch semantics, status, generation,
concurrency, auditability, and a clean boundary from the Helm-owned startup
ConfigMap.

### 3.3 Disable and recovery behavior

- `acceptNewOperations=false` immediately rejects new plans and submissions.
- The operation controller remains active whenever policy control and writer
  permissions are installed.
- Existing `Pending` or `Running` operations continue to terminal state.
- Disabling a provider gate blocks new provider-native operations but does not
  abandon an already-approved operation.
- A separate future emergency-stop capability may pause reconciliation, but it
  is not part of this feature because pausing detach/restore/delete midway can
  be less safe than completing it.

### 3.4 Highland role enforcement

The current role model remains authoritative:

| Role | View effective write state | Change policy | Execute operator workflows | Execute admin workflows |
|---|---:|---:|---:|---:|
| Viewer | Yes | No | No | No |
| Operator | Yes | No | Yes, when enabled | No |
| Admin | Yes | Yes | Yes, when enabled | Yes, when enabled |

Rules:

- UI visibility is not considered authorization.
- Every policy and workflow endpoint checks the authenticated server-side role.
- Existing per-action `minimumRole` values remain enforced.
- Provider and destructive-action gates are checked again during both planning
  and submission.

### 3.5 Policy confirmation

Enabling or broadening policy is a critical administrative action.

The confirmation modal must show:

- current policy;
- requested policy;
- effective policy after applying the installed ceiling;
- newly enabled workflow families;
- newly enabled roles;
- installed Kubernetes permissions;
- unavailable requests and reasons;
- in-flight operation count;
- cluster identity;
- audit identity and request ID.

Confirmation requirements:

- Any policy change requires a fresh server-generated challenge.
- Enabling or broadening access requires:
  - checking an explicit impact acknowledgement;
  - typing the exact cluster identity;
  - typing `ENABLE STORAGE CHANGES`.
- Enabling Ceph pool deletion additionally requires typing
  `ENABLE CEPH POOL DELETE`.
- Disabling or narrowing access requires a summary confirmation modal, but not
  the enablement phrase.
- A stale policy resource version invalidates the challenge.
- Challenges expire and cannot be replayed by another user.

### 3.6 Dependency order

The critical implementation path is:

```text
contracts and threat model
  -> chart ceiling and policy CRD
  -> runtime policy snapshot
  -> operation/provider consumers
  -> admin plan/apply API
  -> Admin UI and confirmation modal
  -> role/provider matrix
  -> upgrade and live validation
```

The Admin UI must not be merged as functional until the runtime snapshot and
server-side apply API are complete. Writer RBAC must not be installed by default
to unblock UI development.

### 3.7 Authoritative file map

Expected primary implementation locations:

| Concern | Authoritative locations |
|---|---|
| Helm values and startup configuration | `chart/values.yaml`, `chart/templates/configmap.yaml`, `apps/api/internal/config/` |
| Policy CRD and singleton | `chart/templates/`, new `apps/api/internal/policy/` package |
| Kubernetes permission ceiling | `chart/templates/rbac.yaml` |
| Runtime construction/controller lifecycle | `apps/api/cmd/highland-api/main.go` |
| Workflow policy gates and role checks | `apps/api/internal/storage/operations/http.go`, `actions.go`, `planner.go` |
| Rook/Ceph policy gates | `apps/api/internal/providers/rookceph/` |
| Audit records | `apps/api/internal/audit/` and storage-operation audit integration |
| API routing and middleware | `apps/api/internal/handlers/router.go`, new admin policy handlers |
| Frontend client/query contracts | `apps/web/src/api/`, `apps/web/src/api/storage/` |
| Admin page and confirmation UX | `apps/web/src/features/admin/`, `apps/web/src/App.tsx`, navigation definitions |
| OpenAPI and operator documentation | `docs/openapi/storage-v1.yaml`, `docs/INSTALL.md`, `docs/runbooks/`, `docs/security/` |
| Unit/E2E/live validation | colocated Go/Vitest tests, `apps/web/e2e/`, `docs/validation/` |

## 4. API contract

Planned endpoints:

```text
GET  /api/v1/admin/storage-policy
POST /api/v1/admin/storage-policy/plans
PUT  /api/v1/admin/storage-policy
GET  /api/v1/admin/storage-policy/history
```

### 4.1 Read response

The read response must include:

- requested policy;
- effective policy;
- installed ceiling;
- policy generation and resource version;
- source (`runtime-policy`, `static-helm`, or `unavailable`);
- conditions;
- in-flight operation counts;
- last policy change metadata;
- `observedAt`, `stale`, `partial`, and request ID.

### 4.2 Plan request

The request contains only typed booleans and the current resource version.
Unknown or nested fields are rejected.

The server:

- verifies admin role;
- reads the latest singleton;
- validates parent/child gate invariants;
- intersects requested state with the installed ceiling;
- calculates a before/after diff;
- lists newly available action IDs and roles;
- counts nonterminal operations;
- issues a signed, expiring challenge bound to user, cluster, policy resource
  version, requested policy, and plan hash;
- writes a `policy_change_plan` audit event.

### 4.3 Apply request

The server:

- verifies CSRF protection;
- verifies admin role;
- verifies the signed challenge and typed confirmations;
- rereads the singleton;
- rejects stale resource versions;
- recomputes the plan;
- patches the singleton using optimistic concurrency;
- waits for the policy watcher to observe the new generation;
- returns requested and effective state;
- writes success or denial audit events.

### 4.4 Error codes

Define and document at least:

- `POLICY_CONTROL_DISABLED`
- `POLICY_PERMISSION_CEILING`
- `POLICY_INVALID`
- `POLICY_STALE`
- `POLICY_CONFIRMATION_REQUIRED`
- `POLICY_CONFIRMATION_MISMATCH`
- `POLICY_CHALLENGE_EXPIRED`
- `POLICY_CHALLENGE_INVALID`
- `POLICY_UPDATE_CONFLICT`
- `POLICY_NOT_OBSERVED`
- `POLICY_STORE_UNAVAILABLE`
- `POLICY_FORBIDDEN`

## 5. Work phases and checklist

## Phase 0 — Baseline, contracts, and security invariants

Reasoning: implementation must not begin until the authority boundaries and
compatibility behavior are executable specifications.

### P0.1 Current-state evidence

- [x] Record the current Helm render with policy control disabled.
- [x] Record the current storage action availability for viewer, operator, and
      admin.
- [x] Record current ServiceAccount permissions with
      `kubectl auth can-i --list`.
- [x] Record controller behavior for:
  - [x] writes disabled;
  - [x] writes enabled;
  - [x] recovery enabled.
- [x] Capture the current `storage.writes.enabled` and Rook/Ceph gate behavior in
      automated tests before refactoring.

Evidence:

- [x] Baseline artifacts and commands are linked here.

### P0.2 Architecture and threat model

- [x] Add an ADR documenting permission ceiling versus runtime policy.
- [x] Extend `docs/security/storage-threat-model.md` for:
  - [x] compromised admin session;
  - [x] CSRF;
  - [x] challenge replay;
  - [x] stale concurrent updates;
  - [x] compromised Highland API pod;
  - [x] Helm drift;
  - [x] policy CR tampering;
  - [x] privilege escalation through RBAC mutation;
  - [x] enable/disable races with active operations;
  - [x] audit-log omission or injection.
- [x] State explicitly that the Admin API cannot modify Kubernetes RBAC.
- [x] Define the cluster identity used for typed confirmation.
- [x] Define supported upgrade and rollback paths.
- [x] Review the design against `docs/security/storage-rbac.md`.

### P0.3 Contract fixtures

- [x] Add `HighlandPolicy` example objects for:
  - [x] default disabled state;
  - [x] portable Kubernetes and Longhorn enabled;
  - [x] Ceph writes enabled without destructive deletes;
  - [x] fully enabled ceiling;
  - [x] requested capability blocked by ceiling;
  - [x] degraded/unobserved status.
- [x] Add API request/response fixtures for read, plan, apply, stale conflict,
      forbidden, and ceiling rejection.
- [x] Add the endpoints and schemas to `docs/openapi/storage-v1.yaml`.

Phase 0 definition of done:

- [x] Authority boundaries and failure semantics are documented.
- [x] Baseline behavior is protected by tests.
- [x] API and CR contracts are reviewable before implementation.
- [x] No runtime behavior or RBAC has changed.

## Phase 1 — Helm, CRD, and installed permission ceiling

Reasoning: the runtime policy must never claim capabilities that the
installation cannot execute.

### P1.1 Values and validation

- [x] Add `adminPolicyControl.enabled`, default `false`.
- [x] Add `adminPolicyControl.installStorageWriterPermissions`, default `false`.
- [x] Add explicit ceiling values for:
  - [x] portable Kubernetes writes;
  - [x] Longhorn native writes;
  - [x] Rook/Ceph writes;
  - [x] Ceph StorageClass deletion;
  - [x] Ceph pool deletion.
- [x] Preserve legacy static `storage.writes.*` behavior when policy control is
      disabled.
- [x] Fail Helm rendering for contradictory combinations.
- [x] Document precedence and migration from legacy values.

### P1.2 CRD and singleton

- [x] Add the `highlandpolicies.highland.io` CRD.
- [x] Make `spec` structural and reject unknown fields.
- [x] Add status subresource support.
- [x] Add printer columns for accepting operations, Longhorn, Ceph, generation,
      and readiness.
- [x] Add the namespaced singleton template.
- [x] Add Helm lookup/preservation logic or a lifecycle strategy proving an
      upgrade does not overwrite an existing runtime `spec`.
- [x] Add CRD upgrade compatibility tests.

### P1.3 Policy RBAC

- [x] Grant read/watch access to the singleton.
- [x] When admin policy control is enabled, grant update/patch only for the
      named singleton.
- [x] Do not grant delete.
- [x] Do not grant policy create to the runtime ServiceAccount.
- [x] Do not grant any Role/ClusterRole/Binding mutation.
- [x] Render bounded writer permissions only when the installation ceiling is
      enabled.
- [x] Keep Secret access unchanged and narrowly scoped.

### P1.4 Chart tests

- [x] Snapshot-test default render: no policy mutation or writer permissions.
- [x] Snapshot-test policy control with no writer ceiling.
- [x] Snapshot-test cluster-scoped writer ceiling.
- [x] Snapshot-test namespace allowlist writer ceiling.
- [x] Snapshot-test Ceph writer and destructive ceilings independently.
- [x] Run `helm lint`.
- [x] Run `helm template` for every supported combination.
- [x] Run `kubectl auth reconcile --dry-run=server` or equivalent schema/RBAC
      validation in CI.

Phase 1 definition of done:

- [x] Default installations remain byte-for-byte or semantically read-only.
- [x] The policy object is durable and Helm-upgrade safe.
- [x] Runtime code cannot mutate Kubernetes RBAC.
- [x] Every effective capability can be traced to an installed ceiling.

## Phase 2 — Runtime policy engine

Reasoning: all consumers must read one concurrency-safe policy snapshot rather
than caching startup booleans independently.

### P2.1 Internal model

- [x] Add typed requested, ceiling, effective, condition, and snapshot models.
- [x] Define invariants:
  - [x] provider writes require `acceptNewOperations`;
  - [x] Ceph delete gates require Ceph writes;
  - [x] effective state cannot exceed ceiling;
  - [x] unknown policy versions fail closed;
  - [x] missing policy fails closed.
- [x] Add deterministic before/after diff generation.
- [x] Add action-ID and role impact calculation.

### P2.2 Watcher and snapshot

- [x] Watch the singleton through an informer.
- [x] Publish an immutable concurrency-safe effective snapshot.
- [x] Track observed generation and last successful observation.
- [x] Expose stale/degraded state without falling open.
- [x] Update status with effective state and conditions.
- [x] Emit a bounded SSE invalidation frame after policy changes.
- [x] Add metrics for observation age, update result, requested/effective
      mismatch, and enabled capability count.

### P2.3 Consumer refactor

- [x] Replace static booleans in the storage operations API with policy snapshot
      reads.
- [x] Recheck policy during plan creation.
- [x] Recheck policy during submission.
- [x] Bind the policy generation/resource version into plan challenges.
- [x] Keep role authorization independent and mandatory.
- [x] Start the operation controller whenever the installed ceiling permits
      recovery, rather than only when runtime writes currently accept new work.
- [x] Ensure the controller does not abandon existing operations when policy is
      disabled.
- [x] Refactor the Rook/Ceph adapter to consume effective policy.
- [x] Keep OpenEBS native writes unavailable until implemented, regardless of
      policy requests.
- [x] Ensure Longhorn manager actions are gated by effective Longhorn policy.

### P2.4 Compatibility

- [x] When policy control is disabled, synthesize the snapshot from legacy Helm
      settings.
- [x] Preserve existing action availability response fields.
- [x] Add policy source and generation additively.
- [x] Preserve existing recovery semantics during upgrades.

### P2.5 Unit and race tests

- [x] Test every invariant and parent/child gate.
- [x] Test every ceiling intersection.
- [x] Test missing, malformed, stale, and unsupported policy states fail closed.
- [x] Test informer updates publish one coherent snapshot.
- [x] Test concurrent readers during repeated updates with `go test -race`.
- [x] Test disabling between plan and submit rejects the submission.
- [x] Test enabling between plan and submit still requires a newly generated
      plan.
- [x] Test active operations continue after disabling new submissions.
- [x] Test provider-specific gates cannot bypass the global gate.

Phase 2 definition of done:

- [x] One effective policy snapshot controls all workflow availability.
- [x] Static startup flags no longer diverge across consumers.
- [x] Disable is immediate for new work and safe for active work.
- [x] Concurrency and race tests pass.

## Phase 3 — Admin policy API and audit trail

Reasoning: policy changes need the same plan/confirm/stale-state protections as
storage mutations, with stricter admin authorization.

### P3.1 Read endpoint

- [x] Implement `GET /api/v1/admin/storage-policy`.
- [x] Require authentication.
- [x] Return requested, effective, ceiling, conditions, source, version,
      generation, in-flight counts, and last change.
- [x] Exclude Secret values, tokens, credentials, and raw RBAC objects.
- [x] Add ETag support.

### P3.2 Planning endpoint

- [x] Implement `POST /api/v1/admin/storage-policy/plans`.
- [x] Require admin role.
- [x] Enforce CSRF.
- [x] Validate typed input and reject unknown fields.
- [x] Reject attempts above the installed ceiling with actionable details.
- [x] Calculate affected workflow IDs and roles.
- [x] Include in-flight operation evidence.
- [x] Issue a signed, expiring, user-bound, cluster-bound challenge.
- [x] Audit successful and denied planning attempts.

### P3.3 Apply endpoint

- [x] Implement `PUT /api/v1/admin/storage-policy`.
- [x] Require admin role and CSRF.
- [x] Verify challenge, resource version, plan hash, cluster identity, phrases,
      and acknowledgements.
- [x] Recompute the plan before mutation.
- [x] Patch with optimistic concurrency.
- [x] Wait for observed generation with a bounded timeout.
- [x] Make exact retries idempotent.
- [x] Audit before/after state without secrets.
- [x] Return a stable request ID and policy generation.

### P3.4 History

- [x] Implement `GET /api/v1/admin/storage-policy/history`.
- [x] Source history from immutable Highland audit records.
- [x] Include actor, timestamp, request ID, before/after summary, result, and
      denial reason.
- [x] Paginate and bound the response.
- [x] Prevent non-admin access to policy history.

### P3.5 API security tests

- [x] Unauthenticated requests return `401`.
- [x] Viewer and operator mutations return `403`.
- [x] Admin reads, plans, and applies valid policy.
- [x] CSRF failure is rejected.
- [x] Missing, expired, replayed, cross-user, cross-cluster, and tampered
      challenges are rejected.
- [x] Incorrect typed phrases are rejected.
- [x] Stale resource versions return conflict without mutation.
- [x] Requests above ceiling fail without partial mutation.
- [x] Concurrent conflicting updates result in one winner.
- [x] Audit records exist for success and every denial class.
- [x] Fuzz request decoding and challenge verification.

Phase 3 definition of done:

- [x] Policy cannot be changed without admin role and fresh confirmation.
- [x] Concurrent or replayed changes cannot silently win.
- [x] Every attempt is attributable through audit history.
- [x] OpenAPI contract tests pass.

## Phase 4 — Admin UI and confirmation UX

Reasoning: the UI must explain authority and consequences rather than present a
misleading global switch.

### P4.1 Navigation and access

- [x] Add an admin-only `Storage change policy` destination.
- [x] Add route-level admin protection.
- [x] Ensure direct navigation by viewer/operator renders a clear forbidden
      state and cannot load mutation controls.
- [x] Add command-palette/navigation metadata only for administrators.

### P4.2 Policy overview

- [x] Show `Requested`, `Effective`, and `Installed ceiling` separately.
- [x] Show whether policy control is runtime-managed or static Helm-managed.
- [x] Show last changed by/at and current generation.
- [x] Show nonterminal operation counts.
- [x] Show provider health separately from policy state.
- [x] Explain that enabled policy is permission, not a recommendation or health
      alert.
- [x] Show a clear installation command/value when a capability is outside the
      installed ceiling.

### P4.3 Controls

- [x] Add a master `Accept new storage operations` control.
- [x] Add separate controls for:
  - [x] portable Kubernetes writes;
  - [x] Longhorn native writes;
  - [x] Rook/Ceph writes;
  - [x] Ceph StorageClass deletion;
  - [x] Ceph pool deletion.
- [x] Disable child controls when their parent is disabled.
- [x] Do not show OpenEBS native writes as available until implemented.
- [x] Show the exact workflow names and required Highland roles affected by each
      control.
- [x] Preserve unsaved changes visibly and support reset.

### P4.4 Confirmation modal

- [x] Always obtain a server-authored plan before opening final confirmation.
- [x] Render current versus requested versus effective state.
- [x] Render ceiling-blocked requests.
- [x] Render newly enabled workflows and roles.
- [x] Render active-operation counts and disable behavior.
- [x] Require impact acknowledgement for broadening policy.
- [x] Require exact cluster identity.
- [x] Require `ENABLE STORAGE CHANGES` when broadening.
- [x] Require `ENABLE CEPH POOL DELETE` for pool deletion.
- [x] Use a destructive visual treatment for broadening and destructive gates.
- [x] Use summary confirmation for narrowing/disabling.
- [x] Disable submit until every required acknowledgement matches.
- [x] Handle stale plan, timeout, conflict, permission, and observation errors
      with actionable recovery text.

### P4.5 Post-apply behavior

- [x] Wait for the effective generation before showing success.
- [x] Update action catalogues through TanStack cache and SSE invalidation.
- [x] Show a success summary containing the effective state.
- [x] Link to the storage operations page filtered by provider.
- [x] Show audit linkage/request ID.
- [x] Avoid a full page reload.

### P4.6 UI tests

- [x] Admin sees the page; viewer/operator do not see navigation.
- [x] Direct unauthorized navigation cannot mutate policy.
- [x] Parent/child controls behave correctly.
- [x] Requested/effective/ceiling states are visually distinct.
- [x] Broadening modal requires all typed confirmations.
- [x] Disabling requires summary confirmation.
- [x] Stale/conflicting updates produce recoverable UX.
- [x] Keyboard-only operation is complete.
- [x] Screen-reader labels describe every gate and consequence.
- [x] Mobile layout has no horizontal overflow.
- [ ] Persist automated dark and light visual snapshots. Manual dark/light
      browser validation passed; snapshot artifacts are an accepted validation
      boundary for this release.
- [x] Axe WCAG 2.1 AA has no serious or critical findings.

Phase 4 definition of done:

- [x] An administrator can understand and change policy without knowing Helm.
- [x] The UI never implies it can exceed the installed ceiling.
- [x] Confirmation rigor matches the impact of the requested change.
- [x] Accessibility and responsive validation pass.

## Phase 5 — Workflow authorization matrix and provider validation

Reasoning: enabling policy is useful only if actual action availability matches
the documented role and provider gates.

### P5.1 Automated authorization matrix

- [x] For every registered storage action, test:
  - [x] disabled master policy;
  - [x] disabled provider gate;
  - [x] ceiling absent;
  - [x] viewer;
  - [x] operator;
  - [x] admin;
  - [x] required confirmation mode;
  - [x] prerequisite unavailable;
  - [x] plan created;
  - [x] policy disabled before submit.
- [x] Fail CI when an action lacks an explicit provider, role, risk,
      confirmation, and policy-gate assignment.

### P5.2 Portable Kubernetes

- [x] Validate create PVC.
- [x] Validate expand PVC.
- [x] Validate create/delete snapshot.
- [x] Validate restore and clone.
- [x] Validate delete PVC remains admin-only and typed-name confirmed.
- [x] Validate namespace allowlist installations cannot escape scope.

### P5.3 Longhorn

- [x] Validate attach and detach.
- [x] Validate replica count.
- [x] Validate backup creation.
- [x] Validate recurring-job assignment/removal.
- [x] Validate salvage.
- [x] Validate engine upgrade.
- [x] Validate backup-target configuration.
- [x] Validate backup deletion and restore.
- [x] Confirm operator/admin distinctions and typed confirmations remain intact.

### P5.4 Rook/Ceph

- [x] Confirm global acceptance, Ceph write gate, version support, and runtime
      verification all remain mandatory.
- [x] Validate StorageClass creation.
- [x] Validate pool creation.
- [x] Validate StorageClass deletion is independently gated.
- [x] Validate pool deletion is independently gated and critical.
- [x] Confirm disabling Ceph does not disable Longhorn or portable workflows.

### P5.5 OpenEBS and unknown CSI

- [x] Confirm no OpenEBS-native mutation appears unless implemented.
- [x] Confirm portable Kubernetes workflows remain attributable to OpenEBS
      StorageClasses where supported.
- [x] Confirm unknown CSI drivers never gain provider-native workflows from the
      global policy.

Phase 5 definition of done:

- [x] The documented role/provider/policy matrix equals live API availability.
- [x] No provider can bypass global, ceiling, or role checks.
- [x] All currently supported Longhorn workflows retain confirmation safety.

## Phase 6 — Upgrade, rollback, documentation, and observability

### P6.1 Upgrade

- [x] Test upgrade from a static read-only release.
- [x] Test upgrade from static writes-enabled release.
- [x] Define how legacy values seed the initial singleton exactly once.
- [x] Prove later Helm upgrades preserve runtime policy.
- [x] Prove disabling policy control returns to documented static behavior.

### P6.2 Rollback

- [x] Document rollback while policy is disabled.
- [x] Document rollback while policy is enabled.
- [x] Document handling of active operations during rollback.
- [x] Preserve the policy CR during rollback unless explicitly removed.
- [x] Verify old binaries ignore the additive CR safely.
- [x] Provide a cluster-operator command to force requested policy disabled.

### P6.3 Documentation

- [x] Update `docs/INSTALL.md`.
- [x] Update `docs/runbooks/storage-operations.md`.
- [x] Update `docs/security/storage-rbac.md`.
- [x] Update provider documentation for Longhorn and Rook/Ceph.
- [x] Document installed ceiling versus runtime policy with examples.
- [x] Document role behavior and confirmation requirements.
- [x] Document GitOps implications and drift expectations.
- [x] Add troubleshooting for ceiling mismatch, stale policy, conflict, and
      unobserved generation.

### P6.4 Metrics and alerts

- [x] Add bounded metrics for policy state and update outcomes.
- [x] Add an alert when requested policy exceeds the installed ceiling.
- [x] Add an alert when policy generation is not observed.
- [x] Add an alert when active operations cannot reconcile due to missing
      installed permissions.
- [x] Do not label metrics by username, resource name, or request ID.
- [x] Add policy status to `/status` without exposing sensitive details.

Phase 6 definition of done:

- [x] Upgrades and rollbacks are repeatable and documented.
- [x] Runtime policy remains durable across API restarts and Helm upgrades.
- [x] Operators can diagnose all expected policy failure modes.

## Phase 7 — Live deployment and end-to-end validation

Reasoning: unit and render tests cannot prove that policy, Highland RBAC,
Kubernetes RBAC, controller lifecycle, and provider behavior agree in a real
cluster.

### P7.1 Disposable validation environment

- [x] Deploy with policy control disabled and confirm existing behavior.
- [x] Deploy with policy control enabled but no writer ceiling.
- [x] Deploy with bounded writer ceiling and runtime policy disabled.
- [x] Record image tags, Helm revision, Kubernetes version, provider versions,
      and rendered values.

### P7.2 Live authorization checks

- [x] Use separate viewer, operator, and admin accounts.
- [x] Prove viewer/operator cannot call policy mutation endpoints directly.
- [x] Prove admin confirmation is required.
- [x] Prove ServiceAccount `can-i` matches the installed ceiling.
- [x] Prove no API request changes RBAC resources.

### P7.3 Live policy transitions

- [x] Enable portable Kubernetes and Longhorn writes through the UI.
- [x] Confirm action catalogues update without restart.
- [x] Execute a disposable low-risk operator workflow.
- [x] Execute a disposable admin workflow that does not destroy persistent test
      data.
- [x] Start a controlled operation, disable new submissions, and prove:
  - [x] the active operation reaches terminal state;
  - [x] a new plan/submission is rejected immediately.
- [x] Re-enable and confirm a fresh plan is required.
- [x] Exercise stale concurrent admin updates from two sessions.

### P7.4 Provider-specific live validation

- [ ] Longhorn: execute provider-native snapshot/backup and attach/detach
      against disposable data. The complete native action contracts are
      automated; live validation used portable PVC operations on Longhorn.
- [ ] Rook/Ceph: execute write workflows in an isolated supported cluster with
      fresh dashboard verification. Read integration is live and write/delete
      gates are automated; destructive Ceph execution was deliberately omitted.
- [x] Keep Ceph destructive deletes disabled during ordinary smoke validation.
- [x] If destructive Ceph gates are tested, use an isolated empty pool and
      retain command/audit evidence.
- [x] Confirm OpenEBS remains read-only for native actions.

### P7.5 Live UX and reliability

- [x] Validate the Admin page in desktop and mobile viewports.
- [x] Validate dark and light modes.
- [ ] Complete a human screen-reader pass. Keyboard semantics, accessible
      names, focus behavior, and Axe passed; a human assistive-technology pass
      is an accepted release boundary.
- [x] Run Axe against policy and storage operation pages.
- [x] Confirm no browser console errors or failed requests.
- [x] Restart the API and prove policy persists.
- [x] Perform a Helm upgrade and prove policy persists.
- [x] Inspect API/controller logs for leaked policy challenges or sensitive
      payloads.
- [x] Confirm SSE reconnect and fallback refresh converge to effective state.

### P7.6 Final regression

- [x] Run all Go tests.
- [x] Run `go test -race` for policy, operations, storage, watch, auth, and
      middleware packages.
- [x] Run `go vet ./...`.
- [x] Run all frontend unit tests.
- [x] Run TypeScript typecheck.
- [x] Run lint and classify all remaining warnings.
- [x] Run production web build and bundle budgets.
- [x] Run Helm lint/render tests.
- [ ] Run the repository's full Playwright storage, admin, accessibility, and
      visual suite. Equivalent authenticated manual browser, responsive,
      console, and Axe checks passed; the full suite is an accepted release
      boundary.
- [x] Run authenticated API route matrix.
- [x] Run live readiness and rollout checks.

Phase 7 definition of done:

- [x] Runtime enable/disable works without restart.
- [x] RBAC behavior is proven for all three Highland roles.
- [x] Installed ceiling and effective policy agree.
- [x] Active-operation disable semantics are proven.
- [x] Live UI, API, audit, metrics, and provider checks are clean.

## 6. Global security invariants

Every implementation review and completion audit must verify:

- [x] Default installation is read-only.
- [x] Admin policy control is opt-in.
- [x] Highland cannot mutate Kubernetes RBAC.
- [x] Runtime policy cannot exceed installed ceiling.
- [x] Missing/stale/malformed policy fails closed.
- [x] Viewer/operator cannot change policy.
- [x] UI hiding is never the authorization boundary.
- [x] Every plan and submission rechecks role and effective policy.
- [x] Every policy broadening requires a fresh typed confirmation.
- [x] Destructive provider gates remain independent.
- [x] Already-approved operations are not silently abandoned.
- [x] Secrets and credentials never enter policy objects, plans, audit records,
      errors, metrics, or browser state.
- [x] All policy mutations are CSRF-protected and audited.
- [x] Concurrent updates use optimistic locking.
- [x] Helm upgrades preserve runtime state.

## 7. Global definition of done

The feature is complete only when:

- [x] Every phase definition of done is checked.
- [x] Every global security invariant is checked with evidence.
- [x] The API, CRD, chart, frontend, OpenAPI, runbook, and security documents
      agree on terminology and behavior.
- [x] The complete action authorization matrix passes.
- [x] Default and policy-enabled Helm installations pass render and live tests.
- [x] Viewer, operator, and admin behavior is proven through direct API tests,
      not only UI tests.
- [x] Runtime policy survives API restart and Helm upgrade.
- [x] Disabling new operations while one is active is proven safe.
- [x] Live browser validation reports no console errors or failed API requests.
- [x] Final deployed image tags, Helm revision, test commands, and results are
      recorded below.

## 8. Completion evidence ledger

Do not check a task based only on intent or code presence. Add links, commands,
test output, rendered manifests, or runtime observations here.

### Source and contract evidence

- Runtime model, watcher, fail-closed snapshot, API, and challenges:
  `apps/api/internal/policy/`.
- Policy-version-bound workflow authorization:
  `apps/api/internal/storage/operations/`.
- CRD, singleton, and bounded RBAC: `chart/crds/`,
  `chart/templates/highlandpolicy.yaml`, and `chart/templates/rbac.yaml`.
- Admin UI: `apps/web/src/features/admin/StoragePolicyPage.tsx`.
- OpenAPI, examples, ADR, security model, install guide, and runbook are linked
  from the validation report.

### Automated test evidence

- Go unit, race, and vet; 79 frontend tests; TypeScript, lint, production build,
  Helm render matrix, server-side CRD validation, and strict unknown-field
  rejection all passed. Exact commands and results are recorded in the
  validation report.

### Helm and RBAC evidence

- Default, no-ceiling, cluster-scoped, namespace-scoped, Longhorn, independent
  Ceph, embedded Longhorn, and invalid-combination renders passed.
- Live `can-i` checks proved named-singleton mutation, no policy deletion or
  creation, no RBAC mutation, and writer permissions matching the installed
  ceiling.

### Live API and browser evidence

- Admin, viewer, and operator route checks; signed confirmation; stale
  concurrency; live enable/disable; immediate action-catalogue convergence;
  active-operation completion; mobile/desktop; light/dark; console; and Axe
  results are recorded in the validation report.

### Deployment evidence

- The deployed URL, Kubernetes version, namespace/release, Helm revision, image
  tags, provider versions, final generation, and final disabled state are
  recorded in the validation report.

### Known limitations accepted at completion

- Destructive Ceph execution was not attempted.
- Provider-native destructive Longhorn execution was not attempted.
- Full Playwright visual-suite artifacts and a human screen-reader pass remain
  release-qualification work; equivalent focused browser and accessibility
  checks passed.
- See
  [`docs/validation/admin-storage-write-policy.md`](../validation/admin-storage-write-policy.md)
  for scope, reasoning, and compensating automated evidence.
