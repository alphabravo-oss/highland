# Changelog

## 0.2.0

- Add an opt-in, pinned Longhorn 1.12.0 subchart under `embeddedLonghorn`.
- Keep the existing external Longhorn deployment mode as the default.
- Disable the stock Longhorn UI and ingress in embedded mode so Highland is the
  only console.
- Route Highland manager access, Kubernetes watches, RBAC, and NetworkPolicy to
  the Helm release namespace when embedded mode is enabled.
- Build and validate chart dependencies in CI and release packaging.
