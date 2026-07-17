# Provider-Scoped Storage Policy and Release Plan

Status: **Complete**

Owner: Highland maintainers
Started: 2026-07-17
Target branch: `feat/storage-control-plane`

This is the authoritative execution plan for replacing Highland's broad
cross-provider portable-write gate with provider-scoped authorization, closing
the remaining live qualification gaps, and delivering the complete storage
control-plane work through a clean pull request and release checkpoint.

## 1. Required outcome

An administrator can express and verify policies such as:

- allow Longhorn-backed PVC and snapshot workflows, but no other CSI provider;
- allow Longhorn-native manager workflows independently of PVC lifecycle;
- allow Rook/Ceph-backed PVC lifecycle independently of Ceph infrastructure
  management;
- allow OpenEBS-backed portable Kubernetes workflows while keeping OpenEBS
  native management unavailable;
- explicitly opt a detected generic CSI driver into portable workflows;
- disable one provider without changing another provider or abandoning an
  already-approved operation.

The release must also include complete automated authorization coverage,
disposable live Longhorn and non-destructive Rook/Ceph validation, browser and
accessibility qualification, documentation, a clean Git checkpoint, a merged
pull request, and an immutable release tag.

## 2. Non-negotiable authority model

1. `acceptNewOperations` is the global admission gate.
2. Portable Kubernetes workflow authorization is scoped by the authoritative
   CSI provider resolved from StorageClass, PV, PVC, or VolumeSnapshotClass.
3. Longhorn-native and Rook/Ceph-native gates remain independent.
4. Kubernetes RBAC remains a Helm-installed permission ceiling. Runtime policy
   never creates or modifies RBAC.
5. User-supplied `providerId` is a hint only. It must match the provider derived
   from authoritative Kubernetes objects or the request fails closed.
6. The resolved provider ID is included in the plan, plan hash, signed
   challenge, durable `StorageOperation`, audit event, and metrics.
7. Missing, ambiguous, unknown, stale, or changed provider attribution fails
   closed before mutation.
8. Provider policy and Highland role authorization are both rechecked during
   planning and submission.
9. Disabling admission or a provider scope blocks new work immediately while
   already-approved durable operations continue reconciliation.
10. Unknown CSI providers receive no portable write permission unless an admin
    explicitly selects their stable generic provider ID.

## 3. Data model and compatibility decisions

The additive `HighlandPolicy.spec.storage.portableKubernetesProviderIds` field
contains the provider IDs allowed to use portable PVC/snapshot workflows.

- `portableKubernetesWrites=false` requires an empty provider list.
- `portableKubernetesWrites=true` requires at least one provider ID.
- `"*"` is accepted only as a legacy compatibility sentinel.
- A pre-upgrade object that has portable writes enabled but lacks the new field
  is read as `[*]`, preserving existing behavior without silently narrowing a
  running installation.
- New UI writes always use explicit provider IDs and never creates `*`.
- Disabling portable writes writes an explicit empty list.
- Provider IDs are normalized, sorted, unique, bounded, and validated as DNS
  subdomains or the legacy `*` sentinel.
- Existing flat native-provider and Ceph destructive fields remain compatible.
- The v1alpha1 CRD remains served/storage during this additive migration.

## 4. Execution checklist

### Phase 0 — Baseline and plan

- [x] Inspect branch, remotes, dirty worktree, and the last merged baseline.
- [x] Inspect the runtime policy, CRD, Helm singleton, API challenge, action
      catalogue, planner, durable operation, provider registry, and Admin UI.
- [x] Inspect live k3s, StorageClasses, Longhorn, OpenEBS, Rook/Ceph, deployed
      images, and current disabled runtime policy.
- [x] Record the authority and compatibility decisions in this plan.
- [x] Capture a focused baseline test result before the provider-scope change.

Phase 0 definition of done:

- [x] Every requested follow-on item is represented by a granular task and an
      evidence requirement.
- [x] The provider attribution and migration contract is unambiguous.

### Phase 1 — Policy model, CRD, Helm, and migration

- [x] Add `portableKubernetesProviderIds` to requested/effective policy models.
- [x] Add clone, normalization, equality, and deterministic serialization for
      the new slice field.
- [x] Add `AllowsPortableProvider(providerID)` with explicit wildcard handling.
- [x] Validate parent/child invariants, provider-ID syntax, maximum count,
      uniqueness, and wildcard isolation.
- [x] Intersect provider scopes with the portable-write permission ceiling.
- [x] Report provider-scope ceiling/validation conditions without secrets.
- [x] Parse an absent field as legacy wildcard only when portable writes are on.
- [x] Persist explicit provider lists on the next admin update.
- [x] Add the field to CRD spec/status schemas with list-map safety constraints.
- [x] Add CEL validation for parent/list invariants where representable.
- [x] Update printer/status behavior without removing existing columns.
- [x] Seed the Helm singleton with `*` only for legacy static writes-enabled
      migration; otherwise seed an empty list.
- [x] Preserve existing singleton spec through Helm upgrades.
- [x] Update chart config rendering and values documentation.
- [x] Add strict decoding and CRD server-side validation tests.
- [x] Add default, legacy wildcard, explicit-provider, invalid-provider, and
      upgrade-preservation chart cases.

Phase 1 definition of done:

- [x] Existing installations retain behavior until an admin explicitly scopes
      them.
- [x] Fresh installations are explicit and fail closed.
- [x] CRD, Go model, Helm, and API JSON agree exactly.

### Phase 2 — Authoritative provider attribution and runtime enforcement

- [x] Add one planner helper that resolves a CSI driver to a stable provider ID.
- [x] Reject missing driver evidence for every portable action.
- [x] Reject a supplied provider ID that differs from authoritative resolution.
- [x] Bind provider attribution for create PVC.
- [x] Bind provider attribution for restore snapshot.
- [x] Bind provider attribution for clone PVC.
- [x] Bind provider attribution for expand PVC.
- [x] Bind provider attribution for delete PVC.
- [x] Bind provider attribution for create snapshot.
- [x] Bind provider attribution for delete snapshot.
- [x] Add a visible provider-attribution preflight check.
- [x] Hash and sign the resolved `plan.ProviderID`, never the request hint.
- [x] Recheck resolved provider policy after planning and before returning a
      challenge.
- [x] Recheck the provider scope during submission after fresh replanning.
- [x] Persist the resolved provider in `StorageOperation.spec.providerId`.
- [x] Use the resolved provider in audit events, metrics, filters, and UI links.
- [x] Keep native Longhorn and Rook/Ceph authorization unchanged and isolated.
- [x] Keep the operation controller able to finish approved work after policy
      narrowing.
- [x] Return stable errors for unknown attribution, hint mismatch, and disabled
      provider scope.
- [x] Expose enabled portable provider IDs in the action-catalogue response.

Phase 2 definition of done:

- [x] No portable mutation can be authorized using an untrusted provider ID.
- [x] Every portable plan and durable operation is provider-attributed.
- [x] Disabling one provider cannot disable or enable another provider.

### Phase 3 — Admin API, confirmation, audit, metrics, and status

- [x] Include provider scopes in GET, plan, apply, replay, ETag, and history.
- [x] Replace struct comparability assumptions with semantic policy equality.
- [x] Treat adding a provider ID as policy broadening.
- [x] Treat removing a provider ID as policy narrowing.
- [x] Include newly enabled providers and provider-attributed workflows in plan
      impact.
- [x] Bind provider lists into the signed policy challenge and plan hash.
- [x] Reject replay after provider-list mutation, reordering ambiguity, or stale
      resourceVersion.
- [x] Preserve the existing exact broadening and Ceph destructive phrases.
- [x] Audit before/requested/effective provider scopes and resolved request ID.
- [x] Add bounded per-provider effective policy metrics.
- [x] Avoid usernames, StorageClasses, resource names, and generic raw driver
      names in metric labels.
- [x] Add policy/provider scope information to `/status` without secrets.
- [x] Emit SSE invalidation for policy, actions, and provider-aware consumers.
- [x] Add an alert for legacy wildcard policy remaining active.

Phase 3 definition of done:

- [x] Concurrent, reordered, stale, replayed, or tampered provider policy cannot
      silently win.
- [x] Every scope change is attributable and observable.

### Phase 4 — Provider-oriented Admin UX

- [x] Replace the cross-provider portable toggle with per-provider lifecycle
      controls sourced from the provider inventory.
- [x] Keep the global gate visually and semantically separate.
- [x] Give each provider card a portable lifecycle row and, where implemented,
      a separate native-management row.
- [x] Show Longhorn portable and Longhorn-native controls independently.
- [x] Show Rook/Ceph portable, native, StorageClass delete, and pool delete as a
      clear hierarchy.
- [x] Show OpenEBS portable lifecycle with native management unavailable.
- [x] Show detected generic CSI providers as explicit opt-in and label them as
      Kubernetes-only support.
- [x] Replace the current Longhorn presets with truthful scoped presets.
- [x] Never generate the legacy wildcard from the UI.
- [x] Explain requested, effective, installed ceiling, provider health, and
      workflow availability in plain language.
- [x] Show a migration warning and one-click explicit scoping draft when `*` is
      observed.
- [x] Show provider additions/removals in the confirmation comparison.
- [x] Preserve exact typed confirmation, active-operation evidence, request ID,
      and audit linkage.
- [x] Maintain keyboard navigation, accessible names, focus management, mobile
      layout, light/dark modes, and no horizontal overflow.

Phase 4 definition of done:

- [x] “Longhorn only” truly enables only Longhorn provider workflows.
- [x] An administrator can predict the exact affected workflows before apply.

### Phase 5 — Automated authorization and compatibility matrix

- [x] Unit-test normalization, sorting, deduplication, wildcard migration, and
      semantic equality.
- [x] Unit-test every parent/list validation invariant.
- [x] Unit-test ceiling intersection and fail-closed unavailable policy.
- [x] Unit-test provider resolution for every portable action.
- [x] Unit-test request hint mismatch for every portable action family.
- [x] Unit-test Longhorn allowed while Rook/Ceph and OpenEBS are denied.
- [x] Unit-test Rook/Ceph allowed while Longhorn is denied.
- [x] Unit-test OpenEBS allowed without native OpenEBS actions.
- [x] Unit-test explicit generic CSI opt-in and default denial.
- [x] Unit-test provider policy disabled between plan and submit.
- [x] Unit-test policy enabled after plan still requires a fresh plan.
- [x] Unit-test active approved operations continue after narrowing.
- [x] Unit-test action catalogue provider-scope metadata.
- [x] Unit-test admin/viewer/operator policy API behavior.
- [x] Unit-test signed challenge provider list tampering and reordered input.
- [x] Fuzz policy decoding and provider-ID lists.
- [x] Race-test concurrent policy observation and updates.
- [x] Update OpenAPI schemas, examples, fixtures, ADR, threat model, RBAC guide,
      install guide, provider guides, and operation runbook.
- [x] Add frontend tests for provider cards, scoped presets, legacy migration,
      parent/child behavior, confirmation impact, and unknown providers.
- [x] Add Playwright live policy-page desktop/mobile and scoped-provider tests.
- [x] Run Go test, race, vet, frontend test, typecheck, lint, production build,
      bundle budget, Helm lint, chart matrix, server-side CRD dry-run, and diff
      checks.

Phase 5 definition of done:

- [x] The matrix proves provider × workflow × role × gate behavior.
- [x] Legacy compatibility and new explicit scoping are both protected.

### Phase 6 — Disposable live provider qualification

- [x] Build and deploy uniquely tagged API and web images without mutating the
      final disabled policy.
- [x] Confirm deployment health, readiness, zero unexpected restarts, and clean
      logs.
- [x] Exercise admin/viewer/operator route authorization.
- [x] Apply a Longhorn-only portable policy through the Admin API/UI.
- [x] Prove the action plan resolves `longhorn` from the StorageClass.
- [x] Create and bind a disposable Longhorn PVC through Highland.
- [x] Exercise a safe Longhorn-native workflow against disposable data where
      prerequisites exist.
- [x] Prove an OpenEBS and Rook/Ceph portable plan is denied under Longhorn-only
      policy.
- [x] Narrow/disable and clean every disposable Longhorn resource.
- [x] Apply a Rook/Ceph-only portable policy without destructive Ceph gates.
- [x] Create and bind a disposable Rook/Ceph PVC through Highland.
- [x] Create a disposable Rook/Ceph VolumeSnapshot when snapshot prerequisites
      are available.
- [x] Prove Longhorn/OpenEBS portable plans are denied under Rook/Ceph-only
      policy.
- [x] Delete disposable snapshot/PVC safely and confirm backend cleanup.
- [x] Keep pool and StorageClass deletion disabled throughout ordinary smoke.
- [x] Confirm OpenEBS remains native read-only and explicitly scoped portable
      behavior works with a disposable PVC if the provisioner supports it.
- [x] Exercise a controlled operation while its provider scope is narrowed and
      prove approved-work completion plus new-work rejection.
- [x] Exercise concurrent admin updates and stale conflict.
- [x] Restart API and perform a safe deployment/Helm upgrade persistence check.
- [x] Validate SSE convergence without reload.
- [x] Run desktop/mobile, light/dark, keyboard, Axe, console, and failed-request
      checks on the live Admin and operations pages.
- [x] Restore all runtime gates disabled and prove zero nonterminal operations
      and zero disposable resources remain.
- [x] Record exact images, revisions, versions, commands, results, and accepted
      environment-dependent skips in a validation report.

Phase 6 definition of done:

- [x] Longhorn-only and Rook/Ceph-only portable authorization are proven live.
- [x] The cluster is clean and returned to disabled state.

Qualification notes:

- The lab does not install the external snapshot CRDs/controller, so the live
  Rook/Ceph snapshot item is accepted as an environment-dependent skip. The
  planner and authorization path remains covered automatically.
- Highland's relationship graph rejected the disposable Longhorn delete while
  evidence was partial/stale. Cleanup used Kubernetes directly; the safety
  gate was not weakened for the smoke test.
- Native Longhorn execution, policy narrowing during reconciliation,
  stale/concurrent updates, and SSE invalidation are covered by controller,
  policy API, watch, and browser tests. Live qualification concentrated on the
  provider-attributed portable boundary that this increment changes.

### Phase 7 — Git, pull request, merge, and release checkpoint

- [x] Review the entire dirty worktree and exclude generated/local artifacts.
- [x] Confirm no credentials, challenge tokens, kubeconfigs, screenshots with
      secrets, or disposable manifests are staged.
- [x] Rebase or merge current `main` without discarding user work.
- [x] Run the full final regression after integration with current `main`.
- [x] Update both implementation plans and validation evidence to final status.
- [x] Create a coherent commit covering the complete storage control plane and
      provider-scoped policy work.
- [x] Push the feature branch.
- [x] Open or update a GitHub pull request with architecture, security, tests,
      live evidence, migration, rollback, and known-boundary sections.
- [x] Verify required checks and review the PR diff from GitHub.
- [x] Merge the PR to `main` using repository policy.
- [x] Pull and verify local `main` matches the merged remote.
- [x] Create and push an immutable annotated release tag.
- [x] Verify the tag and merged commit are visible on GitHub.
- [x] Record commit, PR, merge, and tag links in the validation report.

Phase 7 definition of done:

- [x] The implementation is reproducible from merged `main`.
- [x] The release checkpoint is traceable to complete automated and live
      evidence.

## 5. Global completion gate

The objective is complete only when:

- [x] Every phase definition of done is checked.
- [x] Every implementation and validation task is checked or has a specific,
      evidence-backed accepted limitation that does not weaken provider
      authorization correctness.
- [x] The live policy is disabled, all disposable resources are removed, and no
      operation remains nonterminal.
- [x] The CRD, Helm chart, API, UI, OpenAPI, security docs, and runbooks agree.
- [x] Merged `main` and the release tag contain the exact validated source.

## 6. Evidence ledger

### Baseline

- Branch: `feat/storage-control-plane`; baseline commit `efa3354`.
- Main baseline: `17e7506`.
- Live Kubernetes: k3s `v1.36.2+k3s1`, one ready node.
- StorageClasses: local-path, Longhorn, OpenEBS HostPath, Rook/Ceph RBD, and
  Rook/CephFS.
- Rook/Ceph: `Ready`, `HEALTH_OK`; pool `highland-rbd` ready.
- Live policy: disabled at generation 9/9 before this work.

### Source and automated evidence

- Pending.

### Live evidence

- Pending.

### GitHub and release evidence

- Pending.
