#!/usr/bin/env bash
# Create a kind cluster and install Longhorn + Highland (best-effort local dev).
# Requires: kind, kubectl, helm, docker
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CLUSTER_NAME="${CLUSTER_NAME:-highland}"
LONGHORN_NAMESPACE="${LONGHORN_NAMESPACE:-longhorn-system}"
HIGHLAND_NAMESPACE="${HIGHLAND_NAMESPACE:-highland-system}"
LONGHORN_CHART_VERSION="${LONGHORN_CHART_VERSION:-}" # empty = latest

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 1
  }
}

need kind
need kubectl
need helm
need docker

if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  echo "[kind-up] creating cluster $CLUSTER_NAME..."
  kind create cluster --name "$CLUSTER_NAME"
else
  echo "[kind-up] cluster $CLUSTER_NAME already exists"
fi

kubectl cluster-info

echo "[kind-up] adding Longhorn helm repo..."
helm repo add longhorn https://charts.longhorn.io 2>/dev/null || true
helm repo update longhorn

echo "[kind-up] installing Longhorn into $LONGHORN_NAMESPACE (this can take several minutes)..."
HELM_ARGS=(upgrade --install longhorn longhorn/longhorn
  --namespace "$LONGHORN_NAMESPACE"
  --create-namespace
  --set defaultSettings.createDefaultDiskLabeledNodes=true
  --wait
  --timeout 15m
)
if [[ -n "$LONGHORN_CHART_VERSION" ]]; then
  HELM_ARGS+=(--version "$LONGHORN_CHART_VERSION")
fi
helm "${HELM_ARGS[@]}"

echo "[kind-up] waiting for longhorn-backend..."
kubectl -n "$LONGHORN_NAMESPACE" rollout status deploy/longhorn-driver-deployer --timeout=10m || true
kubectl -n "$LONGHORN_NAMESPACE" wait --for=condition=available deploy -l app=longhorn-manager --timeout=10m || true

echo "[kind-up] creating Highland admin secret + install chart..."
kubectl create namespace "$HIGHLAND_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$HIGHLAND_NAMESPACE" create secret generic highland-admin \
  --from-literal=username=admin \
  --from-literal=password="${HIGHLAND_ADMIN_PASSWORD:-highland}" \
  --dry-run=client -o yaml | kubectl apply -f -

# Chart currently expects prebuilt images; for local, document port-forward of API/web from host.
# Install chart resources for NetworkPolicy/deploy skeleton.
helm upgrade --install highland "$ROOT/chart" \
  --namespace "$HIGHLAND_NAMESPACE" \
  --set longhorn.enabled=false \
  --set longhorn.namespace="$LONGHORN_NAMESPACE" \
  --set longhorn.managerService=longhorn-backend \
  --set longhorn.managerPort=9500 \
  --set auth.local.existingSecret=highland-admin \
  --set image.api.repository=ghcr.io/example/highland-api \
  --set image.web.repository=ghcr.io/example/highland-web \
  --set replicaCount.api=0 \
  --set replicaCount.web=0

echo ""
echo "[kind-up] Longhorn installed. For local Highland API against in-cluster manager:"
echo "  kubectl -n $LONGHORN_NAMESPACE port-forward svc/longhorn-backend 9500:9500"
echo "  cd $ROOT/apps/api && HIGHLAND_MANAGER_URL=http://127.0.0.1:9500 go run ./cmd/highland-api"
echo "  cd $ROOT/apps/web && npm run dev"
echo ""
echo "For fixture e2e without kind: cd apps/web && npm run test:e2e"
