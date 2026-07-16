# Storage control-plane threat model

## Assets and trust boundaries

Highland holds a Kubernetes service-account token, local/OIDC sessions, an operation-signing key,
and optional Longhorn/Ceph credentials. The browser, Kubernetes API, Longhorn manager, Ceph
Dashboard, Prometheus, and Rook operator are separate trust boundaries.

Provider URLs come only from trusted configuration. The browser never receives upstream URLs,
authorization headers, JWTs, CA material, passwords, or Secret values. The Ceph adapter exposes no
generic proxy and performs only allowlisted internal GETs.

## Controls

| Threat | Control |
|---|---|
| Confused-deputy/cross-namespace write | Session authentication, action-level role policy, namespace allowlist, fixed provider ID |
| CSRF | Existing signed-session double-submit CSRF middleware on every mutation |
| Replayed/stale approval | Five-minute HMAC challenge bound to requester, action, provider, UID, resourceVersion, and plan hash; fresh reconciler preflight |
| Concurrent/double execution | Durable immutable operation, equivalent-target detection, leader election, idempotent reconciliation |
| Name reuse after delete | UID and resourceVersion preconditions |
| Admission/quota bypass | Kubernetes server-side dry-run and ordinary API admission; no CSI socket calls |
| SSRF | Parsed configured URL only; request payloads cannot set provider endpoints; no redirects supplied by users |
| Credential/token leakage | Secret keyRefs, in-memory Ceph JWT, sanitized bounded errors/audit, no raw response logging |
| Malicious/large provider response | TLS verification, media-type check, timeout, 16 MiB response cap, bounded decoded objects and arrays |
| Provider exhaustion | Connection limits, bounded retries/backoff, circuit breaker, stale last-known cache |
| Incorrect backend correlation | Exact driver and `volumeHandle`/documented ID equality only; no PVC-name guessing |
| Unsafe Ceph deletion | Separate disabled-by-default gate, admin + typed name, fresh health, dual Rook/runtime dependency checks, fail closed |
| Excessive RBAC | Read/write role split, per-namespace Roles, no wildcard resources/verbs, no Secret list |

## Ceph credential requirements

Use a dedicated Dashboard account with read-only permissions for health, pools, OSDs, images, and
quorum. Do not use Rook's generated administrator password. TLS verification is mandatory outside a
disposable lab; `insecureSkipVerify` produces a visible warning. The configured Kubernetes Secret
must contain only `username` and `password`; Highland requires no permission to list it because the
Deployment mounts the named keys as environment variables.

## Review checklist

- Render every Helm mode and verify no wildcard RBAC and no writer role in read-only/recovery mode.
- Confirm every new mutation is in the action registry and returns a durable operation ID.
- Exercise viewer/operator/admin, namespace, CSRF, forged provider, stale plan, and changed-UID cases.
- Search logs, audit exports, operation status, API responses, and browser storage for fixture secrets.
- Verify Ceph Dashboard requests are GET except `/api/auth`, and no request body can alter the host.
- Block Dashboard/Prometheus independently and verify common Kubernetes and Longhorn inventory stays available.
- Treat an inability to prove pool emptiness or runtime creation as terminally unsafe.
