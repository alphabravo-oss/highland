#!/usr/bin/env bash
# Tear down the CephCluster only on a disposable labeled lab. Rebuild the
# scratch VMs afterward; this script does not run generic host disk wipe tools.
set -euo pipefail

NAMESPACE="${ROOK_NAMESPACE:-rook-ceph}"
if [[ "${HIGHLAND_STORAGE_LAB_ACK:-}" != "destroy-scratch-disks" || "${HIGHLAND_DESTROY_CEPH_CLUSTER:-0}" != "1" ]]; then
  echo "Refusing: set the lab acknowledgement and HIGHLAND_DESTROY_CEPH_CLUSTER=1." >&2
  exit 2
fi
nodes="$(kubectl get nodes --no-headers | wc -l | tr -d ' ')"
lab_nodes="$(kubectl get nodes -l highland.io/storage-lab=true --no-headers | wc -l | tr -d ' ')"
if [[ "$nodes" != "3" || "$lab_nodes" != "3" ]]; then
  echo "Refusing: exactly three nodes must exist and all must be labeled storage-lab." >&2
  exit 2
fi
if kubectl get pv -o json | jq -e '.items[] | select((.spec.csi.driver? // "") | test("(rbd|cephfs).csi.ceph.com$")) | select(.metadata.labels["highland.io/test-run"] == null)' >/dev/null; then
  echo "Refusing: Ceph PVs without a Highland test-run label still exist." >&2
  exit 1
fi
kubectl -n "$NAMESPACE" patch cephcluster rook-ceph --type merge -p '{"spec":{"cleanupPolicy":{"confirmation":"yes-really-destroy-data"}}}'
kubectl -n "$NAMESPACE" delete cephcluster rook-ceph --wait=true --timeout=30m
kubectl delete namespace "$NAMESPACE" --wait=true --timeout=10m
echo "Kubernetes Ceph resources removed. Destroy/rebuild the three scratch VMs before reusing their disks."
