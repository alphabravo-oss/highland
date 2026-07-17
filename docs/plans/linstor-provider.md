# Piraeus / LINSTOR Managed Provider Implementation Plan

- Status: **Implemented — production qualification gates complete**
- Owner: **Highland storage control plane**
- Last updated: **2026-07-17**
- Target: **Highland 0.3.0**

## 1. Product boundary

Highland will manage and explain an independently deployed Piraeus/LINSTOR data plane. Highland
will not install, upgrade, roll back, reconcile, or uninstall Piraeus Operator, LINSTOR, DRBD, or
LINSTOR CSI. Removing or stopping Highland must have no effect on provisioning, attachment,
replication, snapshots, backups, or existing workloads.

The managed provider has stable ID and kind `linstor`; it claims the documented CSI driver
`linstor.csi.linbit.com`. Piraeus Kubernetes resources remain authoritative for desired state and
operator status. The LINSTOR Controller REST API supplies bounded runtime observations. Kubernetes
remains authoritative for PVC/PV, workload, attachment, snapshot, and StorageClass lifecycle.

### Explicit non-goals

- No Helm, Operator, CRD, CSI, DRBD, or kernel-module lifecycle controls.
- No raw CSI socket calls, `kubectl exec`, LINSTOR CLI execution, arbitrary GVR selection, or
  generic LINSTOR REST proxy.
- No raw-device preparation, storage-pool creation/deletion, volume shrinking, quorum mutation,
  replica deletion, node evacuation, backup deletion, or native writes in the initial release.
- No automatic adoption or ownership transfer from Helm, Flux, Argo CD, Rancher, or Piraeus.
- No provider health dependency in Highland `/readyz` unless the operator explicitly adds
  `linstor` to `storage.requiredProviders`.

## 2. User outcomes

An operator must be able to answer:

1. Is the Piraeus Operator, LINSTOR Controller, CSI controller, and expected satellite set healthy?
2. Which LINSTOR nodes are online, offline, evacuating, or missing from Kubernetes?
3. Which storage pools provide capacity, what backs them, and which are near exhaustion?
4. Which LINSTOR resources and replicas back each Kubernetes PV/PVC and workload?
5. Are DRBD replicas connected, current, sufficiently placed, and able to promote?
6. Which resource groups and StorageClasses define placement, replication, layers, encryption,
   filesystem, and topology behavior?
7. Which snapshots, remotes, and backup schedules protect each workload?
8. Is information unavailable because of missing CRDs, denied RBAC, API authentication, TLS,
   unsupported versions, partial data, or an unhealthy component?
9. What is the workload impact and the safest next diagnostic for every degraded condition?

## 3. Authoritative sources and normalized resources

### Kubernetes and Piraeus sources

- `piraeus.io/v1`: `LinstorCluster`, `LinstorSatellite`,
  `LinstorSatelliteConfiguration`, and `LinstorNodeConnection`.
- Fixed namespace workloads: Operator, Controller, CSI controller/node, HA controller, and
  satellite Deployments/DaemonSets/Pods.
- Standard storage resources already collected by Highland.

### LINSTOR REST sources

- Controller version/health and API reachability.
- Nodes, network interfaces, and connection state.
- Storage pools and space reports.
- Resource definitions, resources/replicas, volume definitions, and volumes.
- Resource groups and placement policy.
- Snapshots and snapshot definitions.
- S3/LINSTOR remotes and schedules.
- Error reports; payloads must be summarized and secret-like fields removed.

### Provider resource kinds

- `components`
- `clusters`
- `satellites`
- `satellite-configurations`
- `node-connections`
- `nodes`
- `storage-pools`
- `resource-groups`
- `resource-definitions`
- `resources`
- `snapshots`
- `remotes`
- `schedules`
- `error-reports`

Every normalized record must include `id`, `name`, `providerId`, `providerKind`, `source`, and
`observedAt`, plus promoted type-specific fields. Lists are capped at 500 records, strings and
nested structures are bounded, and secret-like keys are removed recursively.

## 4. Delivery checklist

### Phase A — contract, compatibility, and fixtures

- [x] Record the product boundary, sources, resource kinds, health rules, correlation rules,
  security model, UI information architecture, tests, and definitions of done in this plan.
- [x] Add provider documentation explaining independent deployment, prerequisites, configuration,
  API authentication, TLS, RBAC, failure behavior, and troubleshooting.
- [x] Add exact tested Piraeus Operator, LINSTOR Controller, LINSTOR CSI, and Kubernetes versions to
  the compatibility matrix.
- [x] Add sanitized fixtures for Piraeus CRDs and LINSTOR REST responses, including healthy,
  degraded, partial, empty, unauthenticated, and unavailable cases.
- [x] Document the CSI volume-handle contract as `<resource-name>[/<volume-number>]` and verify it
  against the current upstream driver source and fixtures.

Definition of done:

- [x] No contract depends on arbitrary raw CRD or REST passthrough.
- [x] No plan item gives Highland deployment ownership over LINSTOR.
- [x] Compatibility and fixture provenance are explicit and contain no credentials/customer data.

### Phase B — configuration, security, and registration

- [x] Add `providers.linstor.enabled`, namespace, fixed controller URL, token Secret, CA Secret,
  TLS verification, request timeout, and response-size limit values.
- [x] Validate controller URLs: fixed configuration only, HTTP allowed only through an explicit lab
  flag, no userinfo/query/fragment, and no request-controlled upstream.
- [x] Load the bearer token from an explicitly named Secret key without exposing it in config,
  descriptors, logs, API responses, or rendered environment diagnostics.
- [x] Mount an explicitly named CA Secret and default to verified TLS.
- [x] Add cluster-scoped read RBAC for allowlisted Piraeus CRDs and namespace-scoped read RBAC for
  component workloads.
- [x] Add NetworkPolicy egress only to the configured namespace/ports plus existing Kubernetes/DNS
  access; do not add internet egress.
- [x] Register `linstor` only when enabled and allow it to claim `linstor.csi.linbit.com`.
- [x] Ensure missing optional CRDs/API access degrades only LINSTOR and does not stop Highland.
- [x] Add config, chart render, RBAC, Secret, URL-validation, and registration tests.

Definition of done:

- [x] Default chart render is unchanged except additive disabled values.
- [x] Enabling LINSTOR grants no mutation verbs.
- [x] Highland starts and serves unrelated providers when LINSTOR is absent or unavailable.

### Phase C — secure REST client and adapter

- [x] Implement a fixed-endpoint REST client with TLS, optional bearer token, bounded timeouts,
  bounded response bodies, content-type validation, context cancellation, and redacted errors.
- [x] Implement bounded caching so repeated descriptor/summary calls do not fan out to LINSTOR.
- [x] Implement descriptor metadata for namespace, LINSTOR version, controller API state, and
  read-only state; record the full tested stack in compatibility documentation.
- [x] Implement provider health from Piraeus conditions, component readiness, controller API,
  satellite/node state, storage-pool state, resource/DRBD replica state, and observation freshness.
- [x] Implement provider summary with actionable counts, capacity, protection, partial-data
  conditions, and timestamps.
- [x] Implement all allowlisted resource kinds with normalized bounded records, pagination, search,
  typed not-found behavior, and stable identifiers.
- [x] Implement exact PV/PVC enrichment by parsing the documented CSI volume handle and verifying a
  matching LINSTOR resource/volume; never infer by display-name heuristics.
- [x] Preserve Kubernetes ownership as authoritative and expose ambiguous/missing correlation as a
  condition rather than a guessed provider reference.
- [x] Add observer metrics for bounded provider/resource labels and request latency/failures.

Definition of done:

- [x] Provider APIs remain bounded, paginated, cacheable, and fail independently.
- [x] Credentials, DRBD secrets, arbitrary properties, and oversized error bodies never escape.
- [x] Healthy, degraded, unavailable, unauthenticated, partial, and unsupported-version fixtures are
  covered by tests.

### Phase D — UI and operator experience

- [x] Add a distinct LINSTOR workspace and provider selector entry.
- [x] Add navigation groups for overview, cluster, resources, protection, Kubernetes inventory,
  context/insights, operations, and provider details.
- [x] Show only routes backed by supported resource kinds; retain useful empty states when an
  optional feature has no records.
- [x] Build a dashboard answering availability, replication safety, satellite health, capacity,
  protection, and affected-workload questions before showing implementation detail.
- [x] Add accessible paginated/searchable/exportable tables for every resource kind.
- [x] Add focused detail views with meaning, health interpretation, Kubernetes relationships,
  observation source/freshness, and safe next diagnostics.
- [x] Add exact resource links from common PV/PVC context to LINSTOR resource details.
- [x] Integrate LINSTOR into provider-scoped inventory, relationships, timeline, impact, capacity
  forecast, risk findings, remediation guidance, operations explanation, and benchmarks.
- [x] Make read-only wording explicit: Highland observes and explains LINSTOR; common Kubernetes
  workflows remain independently gated; no LINSTOR-native mutation is advertised.
- [x] Add loading, empty, partial, stale, permission-denied, unsupported-version, and unavailable
  states without horizontal overflow or raw API errors.
- [x] Add English and Spanish navigation/page terminology.
- [x] Add component, navigation, routing, rendering, accessibility, and responsive tests.

Definition of done:

- [x] A user can move from an affected workload to its PV, LINSTOR resource, replicas, nodes, and
  storage pools without leaving Highland.
- [x] Dashboard language explains what happened, why it matters, and the next safe action.
- [x] Switching among Longhorn, Rook/Ceph, OpenEBS, LINSTOR, and generic CSI workspaces preserves
  provider-specific navigation without leaking unrelated resources.

### Phase E — documentation and contracts

- [x] Update README feature/provider tables and Quick Start managed-provider configuration.
- [x] Update installation, storage control-plane, capability, RBAC, threat-model, troubleshooting,
  OpenAPI, parity, and release-facing documentation.
- [x] Document that the upstream LINSTOR GUI remains the full native backend console and that
  Highland adds Kubernetes correlation, unified access, audit, insight, and bounded workflows.
- [x] Document token rotation, TLS/CA changes, external controller support, namespace changes,
  provider disablement, and Highland uninstall behavior.
- [x] Add validation evidence mapping every checklist item to a test, rendered manifest, API probe,
  browser assertion, or live-cluster observation.

Definition of done:

- [x] An operator can configure or remove Highland’s integration without changing LINSTOR.
- [x] Every advertised capability has an implementation and verification reference.

### Phase F — automated and live validation

- [x] Run Go format/vet/test/build and race-sensitive adapter/client tests where practical.
- [x] Run web lint/typecheck/unit/build/bundle-budget/Storybook tests.
- [x] Run Helm dependency/lint and enabled/disabled render assertions, including RBAC and Secret
  negative checks.
- [x] Run OpenAPI/parity/document consistency gates.
- [x] Install Piraeus/LINSTOR independently in the local k3s cluster with a disposable test pool;
  do not add it as a Highland subchart or Helm-owned dependency.
- [x] Create a LINSTOR StorageClass, PVC, mounted workload, data write/read, snapshot/restore or
  explicitly record why the lab backend cannot support one capability.
- [x] Enable Highland’s adapter, then probe every summary/resource/detail/common-context API.
- [x] Browser-test every LINSTOR route as separate admin and viewer users in light/dark themes at
  desktop and mobile viewports, including routing, accessibility, responsive behavior, API
  failures, console errors, and role-specific navigation.
- [x] Assert no unexpected console errors, failed requests, serious accessibility findings,
  horizontal overflow, cross-provider data leakage, credentials, or unbounded payloads.
- [x] Test controller unavailable and provider-disabled recovery live; cover token rejection,
  missing/denied sources, and degraded satellite/replica states with isolated contract tests so the
  shared storage lab is not disrupted.
- [x] Stop Highland and prove the LINSTOR-backed workload continues reading/writing.
- [x] Re-enable Highland and prove inventory recovers without data-plane intervention.
- [x] Uninstall only Highland in a disposable qualification path or render/prove ownership
  isolation when the shared local lab makes uninstall inappropriate; confirm LINSTOR resources are
  not owned by the Highland Helm release.
- [x] Run the full repository CI-equivalent suite after all targeted validation.

Definition of done:

- [x] All checks pass against exact tested versions and evidence is recorded.
- [x] The live cluster proves Highland is not on the CSI provisioning or I/O path.
- [x] Existing Longhorn, Rook/Ceph, OpenEBS, generic CSI, auth, policy, and benchmark tests remain
  green.

## 5. Acceptance criteria

- [x] Every preview-required Phase A–F task and definition-of-done checkbox is checked with
  authoritative evidence.
- [x] LINSTOR appears as managed read-only only when explicitly enabled.
- [x] All provider APIs and UI pages are useful, provider-scoped, bounded, secure, and tested.
- [x] No installation/lifecycle ownership or provider-native write capability is present.
- [x] Highland removal/failure has no effect on LINSTOR CSI or workload I/O.
- [x] Documentation and compatibility claims exactly match tested behavior.
- [x] Changes are committed and pushed only after the full local CI-equivalent suite is green;
  remote CI is monitored after push.

## 6. Production qualification

The preview implementation was promoted through the following live qualification gates on
2026-07-17:

- [x] Repeat provisioning, failover, replica-state, quorum, and satellite-loss validation on a
  disposable three-node DRBD cluster.
- [x] Exercise Kubernetes VolumeSnapshot creation and restore in a cluster with snapshot CRDs and a
  compatible snapshot controller installed.
- [x] Exercise a real verified-HTTPS LINSTOR controller with bearer-token rotation and rejection.
- [x] Repeat the complete live browser matrix with separate viewer/admin accounts and light/dark
  desktop/mobile themes.

See [the validation record](../validation/linstor-provider.md) for commands, observations, and the
captured evidence for every completed production gate.
