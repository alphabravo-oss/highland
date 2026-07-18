# ADR-0006: Software supply-chain policy

Status: **Accepted**
Date: 2026-07-18
Decisions: DEC-7, DEC-8, DEC-9

## Context

Release workflows build images and an OCI chart but do not publish SBOM,
provenance, signatures, or enforce vulnerability/secret gates. The fio helper
defaults to a floating `latest` tag.

## Decision

### DEC-7 — SBOM format and scanners

- **SBOM**: SPDX JSON 2.3 (primary) for API and web final images; chart inventory
  includes the embedded Longhorn dependency list.
- **Scanners**:
  - Go: `govulncheck ./...`
  - Containers/charts: Trivy (or equivalent) on rendered manifests and final
    images, including the reviewed fio helper digest
  - Secrets: gitleaks (or GitHub secret scanning) on source and generated
    artifacts
  - npm: `npm audit --omit=dev` with documented severity policy

### DEC-8 — Signing and provenance

- **Signatures**: Cosign keyless signing via GitHub OIDC for API/web digests and
  the OCI chart.
- **Provenance**: SLSA-compatible build provenance via `actions/attest-build-provenance`
  (or Cosign attest) including repository, workflow, commit, builder identity,
  platforms, and output digests.
- **Build-once**: release candidates are built once and promoted by digest; no
  independent rebuild of the same version in a later job.

### DEC-9 — Vulnerability thresholds and exceptions

| Severity | Gate |
|---|---|
| Critical, fix available | **fail** unless non-expired exception |
| Critical, no fix | fail for release promotion unless exception with compensating control |
| High | fail when reachable/exploitable in Highland runtime path; otherwise warn with review |
| Medium/Low | report; do not block by default |

Exceptions require: identifier, artifact/CVE, justification, owner, compensating
control, **expiry ≤ 90 days**, and review link. Unbounded allowlists are
prohibited.

### Immutable images

- Base images pinned by digest with human-readable version comments.
- `benchmark.fioImage` defaults to a reviewed repository/tag/digest triple.
- Chart supports `image.*.digest` and helper digests; digest mode is the
  production recommendation.

## Consequences

- CI gains security jobs; release gains SBOM/sign/attest steps.
- Production installs document verification commands for digests and signatures.
- Operational severity gates, exception fields, and fio high-impact notes live in
  [docs/security/supply-chain-policy.md](../security/supply-chain-policy.md).
