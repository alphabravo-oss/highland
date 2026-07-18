# Artifact verification (production)

Highland production installs should pin images and charts by **digest**, not
mutable tags. See [ADR-0006](../adr/0006-software-supply-chain-policy.md).

## Verify a release image (when signatures published)

```bash
# Replace REGISTRY, REPO, DIGEST with release metadata values.
cosign verify \
  --certificate-identity-regexp 'https://github.com/alphabravo-oss/highland/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  REGISTRY/REPO@DIGEST
```

## Install by digest

```yaml
image:
  api:
    repository: ghcr.io/alphabravo-oss/highland-api
    digest: sha256:...   # preferred
  web:
    repository: ghcr.io/alphabravo-oss/highland-web
    digest: sha256:...
benchmark:
  fioImage:
    repository: ghcr.io/aksakalli/fio
    tag: "3.39"
    digest: sha256:...   # required for production benchmark Jobs
```

```bash
helm upgrade --install highland ./chart -n highland-system --create-namespace \
  -f chart/examples/values-production-ha.yaml \
  --set auth.local.password='...'
```

## SBOM and provenance

Release workflows generate SPDX SBOMs (best-effort) and scaffold Cosign keyless
signing. Until keyless publish is enabled in the environment, treat digests and
CI scan results as the primary integrity evidence.

## Rollback

Always roll back to a **known prior digest**, never to a floating tag.
