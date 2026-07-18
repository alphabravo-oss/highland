# Changelog

## 0.4.0 — Production-readiness foundations and engineering scale (2026-07-18)

Highland 0.4.0 establishes the architecture, contracts, and automated validation needed to move
the multi-provider control plane toward production HA. It adds durable-audit and shared-limiter
foundations, safer multi-replica chart behavior, canonical API contracts, qualification evidence,
and release-integrity scaffolding while retaining the existing alpha support boundary.

### High availability and durable audit

- Add a context-aware audit-sink contract with memory, JSONL, PostgreSQL, redaction, health, typed
  errors, and a resumable JSONL import path.
- Add required pre-mutation audit admission for storage operations, identity administration, and
  storage-policy changes so configured durable-audit outages fail closed before privileged work is
  created.
- Add a shared Redis login limiter with atomic IP and user/IP accounting, IPv4 and IPv6
  normalization, fail-closed production behavior, and an explicit fail-open break-glass option.
- Extract API construction into a testable application package and add bootstrap-matrix coverage
  for development, durable-audit-required, Redis, disabled-provider, and failure configurations.
- Add production-HA example values and runbooks for durable audit, limiter outages, and replica
  availability.

### Helm and multi-replica operation

- Add API and web PodDisruptionBudgets, rolling-update controls, startup probes, pod anti-affinity,
  and topology-spread support.
- Add digest-aware image references for API, web, and benchmark helper images, with chart-render
  coverage for default, embedded, production-HA, RBAC, and digest configurations.
- Keep provider writes and Kubernetes benchmark Jobs opt-in, preserving the least-privilege default
  installation.

### API contracts and engineering structure

- Add a canonical modular OpenAPI contract, Redocly linting, route-inventory enforcement, bundled
  specifications, generated Go server types, generated TypeScript client types, and migration
  guidance.
- Add a typed storage-operation registry and exhaustiveness tests so registered actions and planner
  behavior cannot silently diverge.
- Reduce the command entry point to composition and lifecycle management through the new
  `internal/app` package.
- Add inventory characterization and a bounded 10,000-claim browser test to protect large-cluster
  behavior.

### Qualification, CI, and release integrity

- Add ten machine-readable qualification profiles and an evidence aggregator that rejects skipped,
  not-run, malformed, redaction-unsafe, or incomplete production evidence.
- Link qualification profiles to the compatibility matrix and extend nightly race, contract,
  accessibility, and scale validation.
- Add `govulncheck`, deterministic OpenAPI generation checks, a pattern-based secret scan, coverage
  tier reporting, SBOM artifact generation, digest-promotion documentation, and artifact
  verification guidance.
- Expand unit, race, Playwright, Helm, fail-closed audit, shared-limiter, application-bootstrap, and
  provider-disabled validation.

### Known limitations

- Highland remains alpha software and is not yet recommended for production use.
- Live multi-node drain/failover and the complete provider qualification matrix require dedicated
  storage labs and are not release gates for 0.4.0.
- PostgreSQL audit pagination/migration hardening and end-to-end audit/limiter health reporting
  remain follow-up work.
- Container signing, provenance, mandatory final-image vulnerability scanning, and reviewed base
  image digests are not yet enforced; SBOM generation is currently best-effort.
- Generated OpenAPI wire types are present but adoption by all runtime handlers and web call sites
  remains incremental.

## 0.3.0 — Enterprise identity and LINSTOR management (2026-07-18)

Highland 0.3.0 extends the multi-provider storage control plane with a managed Piraeus/LINSTOR
workspace, enterprise local-identity controls, consistent provider dashboards, and a complete
provider-neutral system status experience. Storage data planes remain independently managed and
continue operating if Highland is unavailable or removed.

### Piraeus / LINSTOR provider

- Add bounded discovery of Piraeus cluster and satellite convergence, component rollout state,
  LINSTOR controller connectivity, nodes, storage pools, resource groups, resources and replicas,
  snapshots, remotes, schedules, and error reports.
- Correlate Kubernetes PVs and PVCs to LINSTOR resources using validated CSI volume handles, while
  rejecting ambiguous handles and redacting provider credentials and sensitive fields.
- Add a dedicated LINSTOR dashboard and provider navigation with capacity, placement, protection,
  lifecycle, and diagnostic signals aligned with the Longhorn, Rook/Ceph, and OpenEBS workspaces.
- Keep Piraeus/LINSTOR lifecycle ownership outside Highland. The integration is read-only and uses
  allowlisted REST reads; portable Kubernetes storage workflows remain separately policy-gated.
- Document the exact tested Piraeus 2.10.8, LINSTOR 1.33.3, and CSI 1.11.3 profile, including the
  completed single-node k3s qualification and the remaining three-node DRBD production gate.

### Enterprise identity and access

- Add durable Kubernetes Secret-backed local identities with administrator-managed user creation,
  role changes, disable/enable, password reset, and session revocation.
- Add self-service email and password changes with Argon2id password hashing, configurable length
  and complexity requirements, password history, and a common-password blocklist.
- Add optional or administrator-enforced TOTP two-factor authentication, recovery codes, MFA
  challenge handling, and audited security-policy changes.
- Expand roles to admin, operator, and viewer while preserving namespace policy and server-side
  authorization for every privileged API and UI route.
- Refine Administration and Account pages so platform facts, identity operations, storage policy,
  and authentication security are clearly separated.

### Provider experience and operational clarity

- Align all provider dashboards around the same operational hierarchy: health and readiness,
  capacity, workload footprint, protection, performance, and conditions, followed by
  provider-specific resources and workflows.
- Replace the Longhorn-era system status view with a provider-neutral readiness page covering all
  detected providers, component health, runtime policy, compatibility, versions, and actionable
  conditions. Unknown generic-CSI health is explicitly distinguished from a failure.
- Add a stable runtime compatibility contract for Kubernetes 1.34–1.36, Longhorn, Rook/Ceph,
  OpenEBS, Piraeus/LINSTOR, and generic CSI discovery.
- Improve provider-scoped context and operations pages so cross-provider facts, native workflows,
  risk, write gates, and read-only boundaries are explained in user terms.
- Add provider workload-footprint summaries and normalize composite provider resource identifiers
  across links and API round trips.

### UX, benchmarks, and quality

- Rework benchmark history into a compact table with expandable, contained run details and clearer
  provider and StorageClass attribution.
- Left-align the sidebar brand, move collapse control beside the workspace selector, and replace
  legacy Longhorn-only shell copy with provider-neutral language.
- Add release/version visibility in the sidebar and make stale status snapshots refresh promptly
  without losing bounded polling behavior.
- Complete an enterprise quality audit across provider contracts, RBAC, API routing, localization,
  accessibility, responsive layouts, console/network errors, and live provider pages.
- Add live site, provider-contract, and LINSTOR qualification suites alongside expanded unit,
  integration, Playwright, Helm, OpenAPI, and parity gates.

### Compatibility and boundaries

- Validated with Kubernetes 1.36.2+k3s1 in the integrated lab; the declared client/API contract is
  Kubernetes 1.34–1.36. Provider versions and release gates are recorded in
  [`docs/compatibility.yaml`](docs/compatibility.yaml).
- Managed native changes remain available for Longhorn and the explicitly supported Rook/Ceph
  workflows. OpenEBS and LINSTOR native resources remain read-only in this release; portable
  Kubernetes workflows require capability discovery, RBAC, runtime policy, and confirmation.
- Highland remains alpha software. Review the compatibility matrix, permission ceilings, provider
  guides, and data-plane lifecycle boundaries before enabling write workflows.

## 0.2.0 — Multi-provider storage control plane (2026-07-17)

Highland 0.2.0 expands the original Longhorn console into a provider-aware Kubernetes storage
control plane. It adds universal CSI inventory, dedicated Rook/Ceph and OpenEBS workspaces,
provider-scoped policy, durable guarded workflows, storage context and insights, and an opt-in
embedded Longhorn deployment.

### Providers and inventory

- Discover arbitrary CSI drivers, StorageClasses, claims, volumes, workloads, snapshots,
  attachments, capacity, topology, and events directly from Kubernetes.
- Add distinct, capability-driven navigation and dashboards for Longhorn, Rook/Ceph, OpenEBS, and
  detected generic CSI providers.
- Add a managed Rook/Ceph adapter for cluster health, quorum, OSDs, pools, RBD images, CephFS,
  mirroring, Ceph Dashboard facts, and allowlisted Prometheus observations.
- Add an authenticated same-origin gateway to the native Ceph Dashboard, with TLS validation,
  path-scoped Ceph cookies, separate Ceph authentication, and optional audited admin credential
  reveal for disposable or controlled environments.
- Add a read-only OpenEBS adapter covering Dynamic LocalPV HostPath, LocalPV LVM, LocalPV ZFS,
  Replicated PV Mayastor, RawFile detection, engine components, placement, and capacity facts.
- Preserve the full Longhorn workspace for volumes, nodes/disks, snapshots, backups, recurring
  jobs, backup targets, images, runtime components, settings, support, and maintenance workflows.

### Safe storage operations

- Add durable Kubernetes `StorageOperation` records with server-generated plans, signed expiring
  confirmation challenges, fresh dependency checks, optimistic concurrency, recovery, and audit.
- Add portable PVC create/expand/delete and snapshot/restore/clone workflows where installed APIs,
  driver capabilities, StorageClass policy, RBAC, and runtime policy allow them.
- Add guarded Rook/Ceph replicated-pool and RBD/CephFS StorageClass creation. StorageClass and pool
  deletion use independent destructive gates and fail closed unless Highland proves dependencies
  are absent from fresh Kubernetes and Ceph state.
- Add an admin-managed runtime policy constrained by Helm-installed permission ceilings. Portable,
  Longhorn-native, Rook/Ceph-native, Ceph StorageClass deletion, and Ceph pool deletion gates are
  independently visible and default to disabled.
- Keep OpenEBS-native mutation read-only in this release; supported portable Kubernetes workflows
  may still target an explicitly selected OpenEBS provider.

### Operator experience and insight

- Add provider-specific dashboards, health explanations, context graphs, relationship timelines,
  capacity forecasts, risk findings, and actionable remediation guidance.
- Rebuild benchmarks around controlled Kubernetes `fio` Jobs with explicit StorageClass/provider
  attribution, retained results, validation state, and clearer run identity.
- Add a dedicated admin area for storage policy and configuration controls, hidden from users who
  do not hold the server-side admin role.
- Align generic workspace naming while retaining backend-specific resource names and workflows.
- Add responsive dark/light/system themes, mobile navigation, accessibility coverage, route
  prefetching, lazy-loaded provider workspaces, query caching, conditional requests, and SSE-driven
  invalidation with polling fallback.

### Installation, security, and observability

- Add an opt-in pinned Longhorn 1.12.0 subchart under `embeddedLonghorn`; external Longhorn remains
  the default. Embedded mode disables the stock Longhorn UI and ingress and routes Highland's
  watches, manager access, RBAC, and NetworkPolicy to the release namespace.
- Add narrowly split provider read/write RBAC, NetworkPolicy rules, namespace scoping, credential
  redaction, rate limiting, fail-closed version checks, and explicit provider health degradation.
- Add Prometheus metrics, optional ServiceMonitor/PrometheusRule resources, a Grafana dashboard,
  API compatibility reporting, OpenAPI contracts, provider compatibility fixtures, and storage
  validation/runbook documentation.
- Publish multi-architecture API and web images plus the dependency-bundled Helm chart from the
  `v0.2.0` release tag.

### Compatibility and boundaries

- Validated with Kubernetes 1.34/1.35, Longhorn 1.11/1.12, Rook 1.19/1.20, Ceph 19.2 and supported
  20.2.1+ builds, Ceph CSI 3.16, and OpenEBS 4.5 profiles documented in
  [`docs/compatibility.yaml`](docs/compatibility.yaml).
- Highland does not replace storage data planes, call raw CSI sockets, expose provider credentials
  to the browser, relay arbitrary Ceph commands, perform Ceph OSD/MON/MGR repair, manage backend
  upgrades, mutate OpenEBS-native resources, or migrate data across providers.
- This remains an alpha release. Review the permission ceilings, provider guides, compatibility
  matrix, and uninstall runbooks before enabling write workflows or embedded Longhorn.
