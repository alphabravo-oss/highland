# Software supply-chain policy

This document operationalizes [ADR-0006: Software supply-chain policy](../adr/0006-software-supply-chain-policy.md)
(DEC-7 SBOM/scanners, DEC-8 signing/provenance, DEC-9 vulnerability thresholds).

## Trusted inputs

| Input class | Trust rule |
|---|---|
| GitHub Actions | Full commit SHA pin + version comment (no floating major tags) |
| Container bases | Tag + digest preferred; human-readable version comment required |
| Go modules | `go.sum` verified; `govulncheck ./...` on PRs/main |
| npm packages | lockfile required; production audit under severity gates below |
| Helm deps | `Chart.lock` + reviewed Longhorn chart pin |
| Runtime helpers | repository / tag / digest triple; no bare `latest` in production |

### High-impact runtime inputs

Treat these as **high impact** (customer-cluster execution privilege or install path):

1. **`benchmark.fioImage`** — Highland may schedule fio Jobs in the customer cluster when benchmarks are enabled. A floating tag (e.g. `xridge/fio:latest` / `ghcr.io/aksakalli/fio:latest`) is a supply-chain and integrity risk: image contents can change without a Highland release. Production profiles should pin a reviewed repository, tag, **and** digest; treat unpinned fio images as a release blocker for benchmark-enabled charts.
2. **Embedded Longhorn chart** — ships inside the Highland chart dependency tree; upgrades must be intentional and scanned.
3. **API/web final images and OCI chart** — signed and digest-promoted per DEC-8.

## Scanners and CI gates

| Gate | Tool / job | When |
|---|---|---|
| Go vulns | `govulncheck ./...` in CI (`apps/api`) | PR + main |
| Secrets | `./hack/scan-secrets.sh` (local patterns); optional gitleaks | PR + main |
| npm (prod) | `npm audit --omit=dev` (when enabled) | PR + main / release |
| Images / charts | Trivy (or equivalent) on final digests | release / scheduled |
| SBOM | SPDX JSON 2.3 (syft / docker sbom) | release (best-effort until required) |
| Sign / attest | Cosign keyless (GitHub OIDC) + provenance | release when OIDC enabled |

Secret scan focuses on high-confidence patterns (private key blocks, AWS keys, common PATs) in **tracked source**, excluding `node_modules`, build output, and chart packages. Run locally:

```bash
./hack/scan-secrets.sh
```

## Severity gates (DEC-9)

| Severity | Gate |
|---|---|
| **Critical, fix available** | **Fail** CI / block release unless a non-expired exception exists |
| **Critical, no fix** | Fail release promotion unless exception documents a compensating control |
| **High** | Fail when reachable/exploitable on the Highland runtime path; otherwise **warn** with explicit review |
| **Medium / Low** | Report only; do not block by default |

“Reachable/exploitable” for High means the vulnerable code path is exercised by Highland API/web runtime, a Job helper (including fio), or install-time chart content—not merely present in a transitive build-only dependency.

## Exception process

Exceptions **must** include all of the following fields. Unbounded allowlists are prohibited.

| Field | Description |
|---|---|
| `identifier` | Unique exception ID (e.g. `SCX-2026-004`) |
| `artifact` | Image digest, module path, npm package, chart, or Action pin |
| `cve` | CVE / GHSA / advisory ID (or `N/A` with reason) |
| `justification` | Why the risk is accepted for this period |
| `owner` | Responsible engineer or team |
| `compensating_control` | Mitigation (network policy, feature disabled, WAF, etc.) |
| `expiry` | ISO date, **≤ 90 days** from grant |
| `review_link` | PR, ticket, or security review URL |

Expired exceptions are treated as missing: the original severity gate applies.

## Signing and promote-by-digest (DEC-8)

1. **Build once** — a release candidate image is built a single time; later stages (chart publish, environment promotion) reference the **digest**, not a rebuilt tag.
2. **Cosign keyless** — `cosign sign` with GitHub Actions OIDC (`id-token: write`). Requires the repository to allow the workflow identity with the registry; no long-lived cosign keys in secrets for the default path.
3. **SBOM** — SPDX JSON attached or uploaded alongside the image digest; must match the pushed digest when enforcement is enabled.
4. **Verification** (installers / operators):

```bash
# Example once signing is enabled for a release digest:
# cosign verify ghcr.io/<org>/highland-api@sha256:<digest> \
#   --certificate-identity-regexp='https://github.com/<org>/highland/.*' \
#   --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

Until OIDC signing is turned on in `.github/workflows/release.yaml`, signing steps remain conditional so the existing GHCR publish path is not broken.

## Action and base-image pins

- Workflows under `.github/workflows/` pin third-party actions to full 40-character SHAs with `# vX.Y.Z` comments.
- Dockerfiles keep a version tag comment and prefer `image:tag@sha256:…` when digests are reviewed; `TODO` markers indicate pending digest capture without blocking builds.

## Related documents

- [ADR-0006](../adr/0006-software-supply-chain-policy.md)
- [Production readiness plan — Workstream B](../plans/production-readiness-and-engineering-scale.md)
- [HA multi-replica threat model](./ha-multi-replica-threat-model.md)
