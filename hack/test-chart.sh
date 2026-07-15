#!/usr/bin/env bash
# Render and validate both Highland deployment modes. The default-render digest
# is the pre-embedded-chart output with the expected chart label normalized, so
# dependency and helper changes cannot silently alter the bolt-on manifests.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHART="$ROOT/chart"

# Helm 3 and 4 emit equivalent resources with different document ordering.
# Both digests come from the unchanged branch-point chart after normalization.
case "$(helm version --short)" in
  v3.*) EXPECTED_DEFAULT_SHA256="b79986ddb6a0f2785e2463f1832347122f5fd80313db5f9deb77b87bab174029" ;;
  v4.*) EXPECTED_DEFAULT_SHA256="1cc6522918655448afe72ee7ec42f46fdd24fd2aec392deee0d9dec2bfb8ceaa" ;;
  *)
    echo "ERROR: unsupported Helm major version" >&2
    exit 1
    ;;
esac

if [[ "${SKIP_DEPENDENCY_BUILD:-0}" != "1" ]]; then
  helm dependency build "$CHART"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

common_values=(
  --set auth.local.createSecret=true
  --set auth.local.password=ci-test
  --set config.sessionSecret=ci-session-secret
)

helm lint "$CHART" "${common_values[@]}"
helm template highland "$CHART" \
  --namespace highland-system \
  "${common_values[@]}" >"$tmp/default.yaml"
helm template highland "$CHART" \
  --namespace longhorn-system \
  "${common_values[@]}" \
  --set embeddedLonghorn.enabled=true \
  --set longhorn.namespace=should-not-be-used >"$tmp/embedded.yaml"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

contains() {
  local file="$1"
  local value="$2"
  grep -Fq -- "$value" "$file" || fail "expected $file to contain: $value"
}

not_contains() {
  local file="$1"
  local value="$2"
  if grep -Fq -- "$value" "$file"; then
    fail "expected $file not to contain: $value"
  fi
}

# Default mode remains a pure bolt-on render. Normalize only the deliberate
# chart version bump and the ConfigMap checksum derived from that label before
# comparing the whole rendered output to main's digest.
actual_default_sha256="$({
  sed -E \
    -e 's#(helm\.sh/chart: highland-)[^[:space:]]+#\1VERSION#g' \
    -e 's#(checksum/config: )[[:xdigit:]]+#\1CONFIG_CHECKSUM#g' \
    "$tmp/default.yaml"
} | sha256sum | awk '{print $1}')"
[[ "$actual_default_sha256" == "$EXPECTED_DEFAULT_SHA256" ]] ||
  fail "default render changed (expected $EXPECTED_DEFAULT_SHA256, got $actual_default_sha256)"
not_contains "$tmp/default.yaml" "# Source: highland/charts/embeddedLonghorn/"
contains "$tmp/default.yaml" '"managerUrl": "http://longhorn-backend.longhorn-system.svc.cluster.local:9500"'
contains "$tmp/default.yaml" 'kubernetes.io/metadata.name: longhorn-system'

# Embedded mode must render the backend, suppress its ingress/UI replicas, and
# direct every Highland integration point to the release namespace.
contains "$tmp/embedded.yaml" "# Source: highland/charts/embeddedLonghorn/templates/daemonset-sa.yaml"
contains "$tmp/embedded.yaml" "name: longhorn-backend"
contains "$tmp/embedded.yaml" "name: longhorn-manager"
contains "$tmp/embedded.yaml" "name: longhorn-ui"
contains "$tmp/embedded.yaml" "replicas: 0"
not_contains "$tmp/embedded.yaml" "name: longhorn-ingress"
contains "$tmp/embedded.yaml" '"managerUrl": "http://longhorn-backend.longhorn-system.svc.cluster.local:9500"'
contains "$tmp/embedded.yaml" 'kubernetes.io/metadata.name: longhorn-system'
contains "$tmp/embedded.yaml" 'value: "longhorn-system"'
not_contains "$tmp/embedded.yaml" "should-not-be-used"

echo "OK: default and embedded chart renders passed"
