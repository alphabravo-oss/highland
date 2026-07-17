#!/usr/bin/env bash
# Render and validate both Highland deployment modes. The default-render digest
# is the pre-embedded-chart output with the expected chart label normalized, so
# dependency and helper changes cannot silently alter the bolt-on manifests.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHART="$ROOT/chart"

if [[ "${SKIP_DEPENDENCY_BUILD:-0}" != "1" ]]; then
  helm dependency build "$CHART"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

common_values=(
  --set auth.local.createSecret=true
  --set auth.local.password=ci-test
  --set config.sessionSecret=ci-session-secret
  --set longhorn.enabled=true
)

helm lint "$CHART" "${common_values[@]}"
helm show crds "$CHART" >"$tmp/crds.yaml"
helm template highland "$CHART" \
  --namespace highland-system \
  "${common_values[@]}" >"$tmp/default.yaml"
helm template highland "$CHART" \
  --namespace longhorn-system \
  "${common_values[@]}" \
  --set embeddedLonghorn.enabled=true \
  --set longhorn.namespace=should-not-be-used >"$tmp/embedded.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set storage.scope.mode=namespaces \
  --set 'storage.scope.namespaces={tenant-a,tenant-b}' >"$tmp/namespaces.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set providers.longhorn.enabled=false >"$tmp/no-longhorn.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set providers.rookCeph.enabled=true \
  --set providers.rookCeph.dashboard.url=https://rook-ceph-mgr-dashboard.rook-ceph.svc:8443 \
  --set providers.rookCeph.dashboard.existingSecret=ceph-dashboard-user \
  --set providers.rookCeph.dashboard.credentialReveal.enabled=true >"$tmp/ceph-read.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set providers.rookCeph.enabled=true \
  --set providers.rookCeph.dashboard.url=https://rook-ceph-mgr-dashboard.rook-ceph.svc:8443 \
  --set providers.rookCeph.dashboard.existingSecret=ceph-dashboard-user \
  --set providers.rookCeph.prometheus.url=http://prometheus-operated.monitoring.svc:9090 >"$tmp/ceph-observability.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set storage.writes.recoveryEnabled=true >"$tmp/recovery.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set storage.writes.enabled=true \
  --set providers.rookCeph.enabled=true \
  --set providers.rookCeph.dashboard.url=https://rook-ceph-mgr-dashboard.rook-ceph.svc:8443 \
  --set providers.rookCeph.dashboard.existingSecret=ceph-dashboard-user \
  --set providers.rookCeph.writes.enabled=true >"$tmp/ceph-create-only.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set storage.writes.enabled=true \
  --set providers.rookCeph.enabled=true \
  --set providers.rookCeph.dashboard.url=https://rook-ceph-mgr-dashboard.rook-ceph.svc:8443 \
  --set providers.rookCeph.dashboard.existingSecret=ceph-dashboard-user \
  --set providers.rookCeph.writes.enabled=true \
  --set providers.rookCeph.writes.allowStorageClassDelete=true \
  --set providers.rookCeph.writes.allowPoolDelete=true \
  --set metrics.prometheusRule.enabled=true >"$tmp/writes.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set metrics.prometheusRule.enabled=true >"$tmp/alerts.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set metrics.grafanaDashboard.enabled=true >"$tmp/dashboard.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set benchmark.kubernetesJobEnabled=true >"$tmp/benchmark.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set adminPolicyControl.enabled=true >"$tmp/policy-no-ceiling.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --include-crds --set adminPolicyControl.enabled=true >"$tmp/policy-fresh-install.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set storage.writes.enabled=true \
  --set adminPolicyControl.enabled=true \
  --set adminPolicyControl.installStorageWriterPermissions=true \
  --set adminPolicyControl.ceiling.portableKubernetesWrites=true >"$tmp/policy-legacy-portable.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set adminPolicyControl.enabled=true \
  --set adminPolicyControl.installStorageWriterPermissions=true \
  --set adminPolicyControl.ceiling.portableKubernetesWrites=true \
  --set adminPolicyControl.ceiling.longhornWrites=true \
  --set metrics.prometheusRule.enabled=true >"$tmp/policy-portable-longhorn.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set storage.scope.mode=namespaces \
  --set 'storage.scope.namespaces={tenant-a,tenant-b}' \
  --set adminPolicyControl.enabled=true \
  --set adminPolicyControl.installStorageWriterPermissions=true \
  --set adminPolicyControl.ceiling.portableKubernetesWrites=true >"$tmp/policy-namespaces.yaml"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set adminPolicyControl.enabled=true \
  --set adminPolicyControl.installStorageWriterPermissions=true \
  --set adminPolicyControl.ceiling.rookCephWrites=true \
  --set adminPolicyControl.ceiling.allowCephStorageClassDelete=true \
  --set adminPolicyControl.ceiling.allowCephPoolDelete=true \
  --set providers.rookCeph.enabled=true \
  --set providers.rookCeph.dashboard.url=https://rook-ceph-mgr-dashboard.rook-ceph.svc:8443 \
  --set providers.rookCeph.dashboard.existingSecret=ceph-dashboard-user >"$tmp/policy-ceph.yaml"

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

# Default mode remains bolt-on Longhorn while the common storage core is
# read-only. StorageOperation is installed so operation history remains
# readable across later feature-gate changes, but no controller/writer role is
# rendered.
not_contains "$tmp/default.yaml" "# Source: highland/charts/embeddedLonghorn/"
contains "$tmp/default.yaml" '"managerUrl": "http://longhorn-backend.longhorn-system.svc.cluster.local:9500"'
contains "$tmp/default.yaml" 'kubernetes.io/metadata.name: longhorn-system'
contains "$tmp/default.yaml" "name: storageoperations.highland.io"
contains "$tmp/default.yaml" "name: highland-storage-read"
contains "$tmp/default.yaml" "name: highland-storage-operations-read"
contains "$tmp/default.yaml" 'cidr: "10.0.0.0/8"'
not_contains "$tmp/default.yaml" "port: 8443"
not_contains "$tmp/default.yaml" "port: 9090"
not_contains "$tmp/default.yaml" "name: highland-storage-operation-controller"
not_contains "$tmp/default.yaml" "name: highland-namespaced-storage-writer"
not_contains "$tmp/default.yaml" "name: highland-ceph-pool-writer"
not_contains "$tmp/default.yaml" "name: highland-benchmark"
not_contains "$tmp/default.yaml" "kind: PrometheusRule"
not_contains "$tmp/default.yaml" "apiVersion: highland.io/v1alpha1"
not_contains "$tmp/default.yaml" "name: highland-policy"
contains "$tmp/crds.yaml" "name: highlandpolicies.highland.io"
contains "$tmp/crds.yaml" "subresources:"
contains "$tmp/crds.yaml" "x-kubernetes-validations:"
contains "$tmp/crds.yaml" "portableKubernetesProviderIds:"
contains "$tmp/crds.yaml" "self.portableKubernetesProviderIds.size() > 0"
contains "$tmp/policy-fresh-install.yaml" "helm.sh/resource-policy: keep"
contains "$tmp/policy-fresh-install.yaml" "x-kubernetes-list-type: set"
contains "$tmp/policy-fresh-install.yaml" "self.portableKubernetesProviderIds.size() > 0"

# Runtime policy control installs only the singleton access role until a
# reviewed writer ceiling is explicitly installed.
contains "$tmp/policy-no-ceiling.yaml" "apiVersion: highland.io/v1alpha1"
contains "$tmp/policy-no-ceiling.yaml" "name: highland-policy"
contains "$tmp/policy-no-ceiling.yaml" 'resourceNames: ["highland"]'
not_contains "$tmp/policy-no-ceiling.yaml" "name: highland-namespaced-storage-writer"
not_contains "$tmp/policy-no-ceiling.yaml" "name: highland-ceph-pool-writer"
contains "$tmp/policy-no-ceiling.yaml" "portableKubernetesProviderIds: []"
contains "$tmp/policy-legacy-portable.yaml" 'portableKubernetesProviderIds: ["*"]'
crd_line="$(grep -n -m1 'name: highlandpolicies.highland.io' "$tmp/policy-fresh-install.yaml" | cut -d: -f1)"
policy_line="$(grep -n -m1 'apiVersion: highland.io/v1alpha1' "$tmp/policy-fresh-install.yaml" | cut -d: -f1)"
if [[ -z "$crd_line" || -z "$policy_line" || "$crd_line" -ge "$policy_line" ]]; then
  fail "fresh install did not render the HighlandPolicy CRD before its singleton"
fi
contains "$tmp/policy-portable-longhorn.yaml" "name: highland-namespaced-storage-writer"
contains "$tmp/policy-portable-longhorn.yaml" "name: highland-storage-operation-controller"
contains "$tmp/policy-portable-longhorn.yaml" "alert: HighlandStoragePolicyNotObserved"
not_contains "$tmp/policy-portable-longhorn.yaml" "name: highland-ceph-pool-writer"
contains "$tmp/policy-namespaces.yaml" 'namespace: "tenant-a"'
contains "$tmp/policy-namespaces.yaml" 'namespace: "tenant-b"'
contains "$tmp/policy-namespaces.yaml" "name: highland-storage-writer"
contains "$tmp/policy-ceph.yaml" "name: highland-ceph-storageclass-writer"
contains "$tmp/policy-ceph.yaml" "name: highland-ceph-pool-writer"

# Namespace mode grants only namespaced readers in the explicit allowlist and
# deliberately omits PV/driver/attachment cluster metadata.
contains "$tmp/namespaces.yaml" 'namespace: "tenant-a"'
contains "$tmp/namespaces.yaml" 'namespace: "tenant-b"'
contains "$tmp/namespaces.yaml" 'resources: ["persistentvolumeclaims", "pods", "events"]'
not_contains "$tmp/namespaces.yaml" 'resources: ["persistentvolumes", "persistentvolumeclaims", "pods", "events"]'

# Optional providers are independent. Ceph read mode gets only the configured
# CRD reads and named credentials. Recovery rejects new submissions but keeps
# the writer permissions needed by already-approved durable operations.
contains "$tmp/no-longhorn.yaml" '"enabled": false'
not_contains "$tmp/no-longhorn.yaml" "name: highland-longhorn-read"
contains "$tmp/ceph-read.yaml" "name: highland-rook-ceph-read"
contains "$tmp/ceph-read.yaml" 'resourceNames: ["rook-ceph-operator"]'
contains "$tmp/ceph-read.yaml" 'name: "ceph-dashboard-user"'
contains "$tmp/ceph-read.yaml" "port: 8443"
contains "$tmp/ceph-read.yaml" 'name: HIGHLAND_CEPH_DASHBOARD_UPSTREAM'
contains "$tmp/ceph-read.yaml" 'value: "rook-ceph-mgr-dashboard.rook-ceph.svc:8443"'
contains "$tmp/ceph-read.yaml" "name: highland-web-egress"
contains "$tmp/ceph-read.yaml" 'resourceNames: ["rook-ceph-dashboard-password"]'
contains "$tmp/ceph-read.yaml" 'name: HIGHLAND_ROOK_CEPH_CREDENTIAL_REVEAL_ENABLED'
not_contains "$tmp/ceph-read.yaml" "port: 9090"
not_contains "$tmp/ceph-read.yaml" "name: highland-ceph-pool-writer"
contains "$tmp/ceph-observability.yaml" "port: 8443"
contains "$tmp/ceph-observability.yaml" "port: 9090"
contains "$tmp/recovery.yaml" "name: highland-storage-operation-controller"
contains "$tmp/recovery.yaml" '"get", "list", "watch", "delete"'
contains "$tmp/recovery.yaml" "name: highland-namespaced-storage-writer"
not_contains "$tmp/recovery.yaml" "name: highland-ceph-storageclass-writer"
contains "$tmp/ceph-create-only.yaml" "name: highland-ceph-storageclass-writer"
contains "$tmp/ceph-create-only.yaml" "name: highland-ceph-pool-writer"
contains "$tmp/ceph-create-only.yaml" '"allowStorageClassDelete": false'
contains "$tmp/ceph-create-only.yaml" '"allowPoolDelete": false'
create_only_storageclass_role="$(awk '
  $0 == "kind: ClusterRole" { block = $0 ORS; capture = 1; next }
  capture { block = block $0 ORS }
  capture && $0 == "  name: highland-ceph-storageclass-writer" { wanted = 1 }
  capture && $0 == "---" {
    if (wanted) { printf "%s", block; exit }
    capture = 0; wanted = 0; block = ""
  }
' "$tmp/ceph-create-only.yaml")"
printf '%s' "$create_only_storageclass_role" | grep -Fq '"create", "update", "patch"' || fail "create-only StorageClass role omitted create permissions"
if printf '%s' "$create_only_storageclass_role" | grep -Fq '"delete"'; then
  fail "create-only StorageClass role unexpectedly granted delete"
fi

# Explicit writes render only the typed workflow roles. Pool deletion appears
# solely behind its second feature gate.
contains "$tmp/writes.yaml" "name: highland-namespaced-storage-writer"
contains "$tmp/writes.yaml" "name: highland-ceph-storageclass-writer"
contains "$tmp/writes.yaml" "name: highland-ceph-pool-writer"
contains "$tmp/writes.yaml" '"create", "update", "patch", "delete"'
contains "$tmp/writes.yaml" "alert: HighlandStorageOperationLeaderAbsent"
contains "$tmp/writes.yaml" "alert: HighlandStorageCephPostflightMismatch"
contains "$tmp/alerts.yaml" "kind: PrometheusRule"
contains "$tmp/alerts.yaml" "alert: HighlandStorageCacheNotSynced"
not_contains "$tmp/alerts.yaml" "alert: HighlandStorageOperationLeaderAbsent"
contains "$tmp/dashboard.yaml" "highland-storage.json"
contains "$tmp/dashboard.yaml" "highland_storage_provider_up"
contains "$tmp/benchmark.yaml" "name: highland-benchmark"
contains "$tmp/benchmark.yaml" 'resources: ["persistentvolumeclaims"]'

for rendered in "$tmp/default.yaml" "$tmp/namespaces.yaml" "$tmp/no-longhorn.yaml" "$tmp/ceph-read.yaml" "$tmp/recovery.yaml" "$tmp/ceph-create-only.yaml" "$tmp/writes.yaml" "$tmp/benchmark.yaml" "$tmp/policy-no-ceiling.yaml" "$tmp/policy-portable-longhorn.yaml" "$tmp/policy-namespaces.yaml" "$tmp/policy-ceph.yaml"; do
  not_contains "$rendered" 'resources: ["*"]'
  not_contains "$rendered" 'verbs: ["*"]'
  not_contains "$rendered" 'resources: ["roles"'
  not_contains "$rendered" 'resources: ["clusterroles"'
  not_contains "$rendered" 'resources: ["rolebindings"'
  not_contains "$rendered" 'resources: ["clusterrolebindings"'
done

if helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set adminPolicyControl.installStorageWriterPermissions=true >"$tmp/invalid-policy-install.yaml" 2>&1; then
  fail "writer ceiling unexpectedly rendered without policy control"
fi
contains "$tmp/invalid-policy-install.yaml" "requires adminPolicyControl.enabled"

if helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set adminPolicyControl.enabled=true \
  --set adminPolicyControl.ceiling.longhornWrites=true >"$tmp/invalid-policy-ceiling.yaml" 2>&1; then
  fail "policy ceiling unexpectedly rendered without writer permission installation"
fi
contains "$tmp/invalid-policy-ceiling.yaml" "require installStorageWriterPermissions"

if helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set adminPolicyControl.enabled=true \
  --set adminPolicyControl.installStorageWriterPermissions=true \
  --set adminPolicyControl.ceiling.allowCephPoolDelete=true >"$tmp/invalid-policy-parent.yaml" 2>&1; then
  fail "Ceph pool delete ceiling unexpectedly rendered without Ceph writes"
fi
contains "$tmp/invalid-policy-parent.yaml" "requires the Rook/Ceph write ceiling"

if helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set-json 'networkPolicy.kubernetesApiCIDRs=[]' >"$tmp/invalid-network-policy.yaml" 2>&1; then
  fail "empty Kubernetes API CIDR allowlist unexpectedly rendered"
fi
contains "$tmp/invalid-network-policy.yaml" "networkPolicy.kubernetesApiCIDRs must contain at least one"

if helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set persistence.audit.enabled=true >"$tmp/invalid-audit-volume.yaml" 2>&1; then
  fail "multi-replica API unexpectedly accepted a ReadWriteOnce audit volume"
fi
contains "$tmp/invalid-audit-volume.yaml" "persistence.audit.accessMode must be ReadWriteMany"
helm template highland "$CHART" --namespace highland-system "${common_values[@]}" \
  --set persistence.audit.enabled=true \
  --set persistence.audit.accessMode=ReadWriteMany >"$tmp/audit-rwx.yaml"
contains "$tmp/audit-rwx.yaml" "ReadWriteMany"

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

echo "OK: storage RBAC, provider, default, and embedded chart renders passed"
