#!/usr/bin/env bash
# Install a pinned Rook/Ceph matrix profile on a disposable three-node cluster.
# This intentionally refuses any cluster that is not wholly labeled as a lab.
set -euo pipefail

ROOK_VERSION="${ROOK_VERSION:-v1.20.2}"
CEPH_IMAGE="${CEPH_IMAGE:-quay.io/ceph/ceph:v20.2.1}"
LAB_ACK="${HIGHLAND_STORAGE_LAB_ACK:-}"
NAMESPACE="${ROOK_NAMESPACE:-rook-ceph}"

if [[ "$LAB_ACK" != "destroy-scratch-disks" ]]; then
  echo "Refusing: set HIGHLAND_STORAGE_LAB_ACK=destroy-scratch-disks on disposable VM nodes." >&2
  exit 2
fi
for command in kubectl curl; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 2; }
done

context="$(kubectl config current-context)"
nodes="$(kubectl get nodes --no-headers | wc -l | tr -d ' ')"
lab_nodes="$(kubectl get nodes -l highland.io/storage-lab=true --no-headers | wc -l | tr -d ' ')"
if [[ "$nodes" != "3" || "$lab_nodes" != "3" ]]; then
  echo "Refusing context $context: exactly three nodes must exist and all must have highland.io/storage-lab=true." >&2
  exit 2
fi
if kubectl get cephclusters.ceph.rook.io -A --no-headers 2>/dev/null | grep -q .; then
  echo "Refusing: a CephCluster already exists in context $context." >&2
  exit 2
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
base="https://raw.githubusercontent.com/rook/rook/${ROOK_VERSION}/deploy/examples"
for manifest in crds.yaml common.yaml operator.yaml; do
  curl --fail --silent --show-error --location "$base/$manifest" -o "$tmp/$manifest"
done
kubectl apply -f "$tmp/crds.yaml" -f "$tmp/common.yaml"
# Rook 1.20 requires the Ceph CSI operator. Prior profiles may contain the
# manifest as well; apply it whenever the pinned release publishes it.
if curl --fail --silent --show-error --location "$base/csi-operator.yaml" -o "$tmp/csi-operator.yaml"; then
  kubectl apply -f "$tmp/csi-operator.yaml"
fi
kubectl apply -f "$tmp/operator.yaml"
kubectl -n "$NAMESPACE" rollout status deployment/rook-ceph-operator --timeout=10m

cat >"$tmp/cluster.yaml" <<EOF
apiVersion: ceph.rook.io/v1
kind: CephCluster
metadata:
  name: rook-ceph
  namespace: ${NAMESPACE}
  labels:
    highland.io/storage-lab: "true"
spec:
  dataDirHostPath: /var/lib/rook-highland-lab
  cephVersion:
    image: ${CEPH_IMAGE}
    allowUnsupported: false
  skipUpgradeChecks: false
  continueUpgradeAfterChecksEvenIfNotHealthy: false
  mon:
    count: 3
    allowMultiplePerNode: false
  mgr:
    count: 2
    allowMultiplePerNode: false
  dashboard:
    enabled: true
    ssl: true
  crashCollector:
    disable: true
  cleanupPolicy:
    confirmation: ""
    sanitizeDisks:
      method: quick
      dataSource: zero
      iteration: 1
    allowUninstallWithVolumes: false
  storage:
    useAllNodes: true
    useAllDevices: true
    config:
      osdsPerDevice: "1"
---
apiVersion: ceph.rook.io/v1
kind: CephBlockPool
metadata:
  name: highland-lab-rbd
  namespace: ${NAMESPACE}
  labels:
    highland.io/storage-lab: "true"
spec:
  failureDomain: host
  replicated:
    size: 3
    requireSafeReplicaSize: true
---
apiVersion: ceph.rook.io/v1
kind: CephFilesystem
metadata:
  name: highland-lab-fs
  namespace: ${NAMESPACE}
  labels:
    highland.io/storage-lab: "true"
spec:
  metadataPool:
    failureDomain: host
    replicated:
      size: 3
  dataPools:
    - name: replicated
      failureDomain: host
      replicated:
        size: 3
  metadataServer:
    activeCount: 1
    activeStandby: true
EOF

kubectl apply -f "$tmp/cluster.yaml"
echo "Waiting up to 45 minutes for Ceph HEALTH_OK..."
deadline=$((SECONDS + 2700))
until [[ $SECONDS -ge $deadline ]]; do
  state="$(kubectl -n "$NAMESPACE" get cephcluster rook-ceph -o jsonpath='{.status.state}' 2>/dev/null || true)"
  health="$(kubectl -n "$NAMESPACE" get cephcluster rook-ceph -o jsonpath='{.status.ceph.health}' 2>/dev/null || true)"
  osds="$(kubectl -n "$NAMESPACE" get pods -l app=rook-ceph-osd --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "$state" =~ ^(Created|Ready)$ && "$health" == "HEALTH_OK" && "$osds" -ge 3 ]]; then
    echo "Rook $ROOK_VERSION / $CEPH_IMAGE ready with $osds OSDs in context $context."
    exit 0
  fi
  sleep 15
done
kubectl -n "$NAMESPACE" get cephcluster,pods -o wide
echo "Timed out waiting for the disposable Ceph lab." >&2
exit 1
