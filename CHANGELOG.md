# Changelog

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
