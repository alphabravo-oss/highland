# Storage context and native handoff threat model

This document covers Highland's relationship, drift, impact, timeline, capacity, comparison,
remediation, and native Ceph Dashboard handoff surfaces.

## Trust boundaries

- Kubernetes and Rook informers provide desired state and workload identity.
- Provider readers provide bounded runtime observations using separately configured machine
  credentials.
- Highland's browser session authorizes Highland APIs only.
- A Ceph Dashboard browser link crosses into a separate application, origin, identity, session, and
  authorization boundary. Highland never brokers that session.
- `dashboard.publicUrl` is browser navigation data only. The API never fetches it.

## Threats and controls

| Threat | Control |
|---|---|
| Public-link SSRF | `publicUrl` is never passed to an HTTP client. Private reader and public browser URLs are separate settings. |
| Scheme/userinfo/query injection | HTTPS is required by default; userinfo, unsafe schemes, fragments, queries, malformed hosts, protocol-relative URLs, and control characters are rejected. HTTP requires an explicit disposable-lab flag and displays a warning. |
| Deep-link data leakage | The versioned destination registry contains identifier-free reviewed routes. Unknown versions, kinds, or destinations fall back to the root URL. Resource names, namespaces, IDs, credentials, and tokens are not appended. |
| Reverse-tabnabbing or embedding | External links use a new tab with `noopener noreferrer`. Ceph Dashboard is not embedded and Highland's CSP is not relaxed for it. |
| Identity confusion | UI copy states that native administration uses a separate login. Highland's machine reader, Highland users, and human Ceph users remain separate. |
| Compromised native dashboard | Opening the link grants no Highland cookie, provider-reader credential, or command channel. Operators must secure the native endpoint with valid TLS and its own access policy. |
| Stale or ambiguous correlation | Every graph edge carries source, observation time, freshness, and confidence. Missing exact identifiers remain unknown; display-name matching is not promoted to authoritative. |
| False workload impact | Confirmed, potential, and unknown impact are separate. OSD-to-workload relationships remain potential without authoritative placement evidence. Destructive plans fail closed on incomplete impact. |
| Query amplification | Provider, kind, namespace, depth, page size, timeline observations, capacity groups, forecast samples/windows, remediation results, and provider candidates have server-side bounds. Provider data is fetched once per bounded observation, never once per table row. |
| Tenant information leakage | Namespace scope is applied by the inventory before graph, timeline, impact, and capacity construction. The browser is not trusted to filter hidden objects. Cluster-scoped objects remain unavailable in namespace mode. |
| Secret leakage | Graph attributes, provider facts, events, remediation evidence, and links use allowlisted fields. Secret values and credential-shaped fields are not copied into context models. |
| Unsafe remediation | Guidance is read-only, evidence-backed, compatibility-aware, and labels the responsible surface. Raw command execution, force deletion, finalizer removal, purge guidance, and arbitrary native URLs are rejected. |

## Independent source failures

- Kubernetes informer failure: common inventory and newly built context return unavailable or partial
  conditions; destructive impact checks fail closed.
- Snapshot API absence: snapshot relationships are explicitly partial; other inventory remains
  available.
- Rook operator or CRD absence: desired-state comparison is partial; Ceph runtime data never
  overwrites or fabricates Rook desired state.
- Private Ceph Dashboard reader outage: cached runtime evidence can be marked stale within its
  bounded policy; common Kubernetes/Rook inventory and Longhorn remain independent.
- Public Ceph Dashboard outage: Highland does not probe it, so common inventory and the private
  reader remain unaffected. Browser navigation fails only at the external boundary.
- Prometheus absence: metric-derived capacity and forecasts are unavailable; Highland does not
  fabricate a trend from a point-in-time Dashboard observation.
- StorageOperation or audit source outage: graph/timeline responses become partial where those
  durable sources are required; operation recovery remains controlled by Kubernetes CRs.

## Operational signals

Highland exports bounded metrics for graph build duration, unresolved relationships, active drift,
impact failures, forecast data sufficiency, provider freshness/errors, and operation postflight
mismatch. The chart includes alerts for sustained drift, unresolved correlation, repeated impact
failure, stale inventory, provider errors, and write-controller failures.
