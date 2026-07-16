#!/usr/bin/env bash
# Create RBD, CephFS, workload, checksum, and snapshot fixtures outside
# Highland, then validate provider-neutral and Ceph read APIs.
set -euo pipefail

NAMESPACE="${ROOK_NAMESPACE:-rook-ceph}"
RUN_ID="${HIGHLAND_RUN_ID:-$(date -u +%Y%m%d%H%M%S)}"
RUN_SLUG="$(printf '%s' "$RUN_ID" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9-' '-' | sed -e 's/^-//' -e 's/-$//' | cut -c1-30)"
if [[ -z "$RUN_SLUG" ]]; then
  echo "HIGHLAND_RUN_ID must contain at least one letter or number." >&2
  exit 2
fi
TEST_NAMESPACE="highland-storage-${RUN_SLUG}"
RBD_CLASS="highland-rbd-${RUN_SLUG}"
CEPHFS_CLASS="highland-cephfs-${RUN_SLUG}"
SNAPSHOT_CLASS="highland-rbd-snap-${RUN_SLUG}"

for command in kubectl jq curl; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 2; }
done
if [[ "$(kubectl get nodes -l highland.io/storage-lab=true --no-headers | wc -l | tr -d ' ')" -lt 3 ]]; then
  echo "Refusing: this suite runs only on a labeled disposable three-node lab." >&2
  exit 2
fi
health="$(kubectl -n "$NAMESPACE" get cephcluster rook-ceph -o jsonpath='{.status.ceph.health}')"
if [[ "$health" != "HEALTH_OK" ]]; then
  echo "Refusing lifecycle validation while Ceph health is $health." >&2
  exit 1
fi

tmp="$(mktemp -d)"
cleanup() {
  if [[ "${HIGHLAND_KEEP_FIXTURES:-0}" == "1" ]]; then
    echo "Keeping fixtures for run $RUN_ID by request."
    rm -rf "$tmp"
    return
  fi
  kubectl delete namespace "$TEST_NAMESPACE" --wait=true --timeout=10m --ignore-not-found
  kubectl delete volumesnapshotclass "$SNAPSHOT_CLASS" --ignore-not-found
  kubectl delete storageclass "$RBD_CLASS" "$CEPHFS_CLASS" --ignore-not-found
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

cat >"$tmp/fixtures.yaml" <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${TEST_NAMESPACE}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${RBD_CLASS}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
provisioner: ${NAMESPACE}.rbd.csi.ceph.com
allowVolumeExpansion: true
reclaimPolicy: Delete
volumeBindingMode: Immediate
parameters:
  clusterID: ${NAMESPACE}
  pool: highland-lab-rbd
  imageFeatures: layering
  csi.storage.k8s.io/provisioner-secret-name: rook-csi-rbd-provisioner
  csi.storage.k8s.io/provisioner-secret-namespace: ${NAMESPACE}
  csi.storage.k8s.io/controller-expand-secret-name: rook-csi-rbd-provisioner
  csi.storage.k8s.io/controller-expand-secret-namespace: ${NAMESPACE}
  csi.storage.k8s.io/node-stage-secret-name: rook-csi-rbd-node
  csi.storage.k8s.io/node-stage-secret-namespace: ${NAMESPACE}
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ${CEPHFS_CLASS}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
provisioner: ${NAMESPACE}.cephfs.csi.ceph.com
allowVolumeExpansion: true
reclaimPolicy: Delete
volumeBindingMode: Immediate
parameters:
  clusterID: ${NAMESPACE}
  fsName: highland-lab-fs
  pool: highland-lab-fs-replicated
  csi.storage.k8s.io/provisioner-secret-name: rook-csi-cephfs-provisioner
  csi.storage.k8s.io/provisioner-secret-namespace: ${NAMESPACE}
  csi.storage.k8s.io/controller-expand-secret-name: rook-csi-cephfs-provisioner
  csi.storage.k8s.io/controller-expand-secret-namespace: ${NAMESPACE}
  csi.storage.k8s.io/node-stage-secret-name: rook-csi-cephfs-node
  csi.storage.k8s.io/node-stage-secret-namespace: ${NAMESPACE}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: rbd-data
  namespace: ${TEST_NAMESPACE}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: ${RBD_CLASS}
  resources: {requests: {storage: 1Gi}}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: cephfs-data
  namespace: ${TEST_NAMESPACE}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
spec:
  accessModes: [ReadWriteMany]
  storageClassName: ${CEPHFS_CLASS}
  resources: {requests: {storage: 1Gi}}
---
apiVersion: v1
kind: Pod
metadata:
  name: checksum-writer
  namespace: ${TEST_NAMESPACE}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
spec:
  restartPolicy: Never
  containers:
    - name: writer
      image: busybox:1.37.0
      command: [sh, -ceu, "printf highland-${RUN_SLUG} >/rbd/payload; sha256sum /rbd/payload >/rbd/checksum; cp /rbd/checksum /cephfs/checksum; cat /rbd/checksum"]
      volumeMounts:
        - {name: rbd, mountPath: /rbd}
        - {name: cephfs, mountPath: /cephfs}
  volumes:
    - name: rbd
      persistentVolumeClaim: {claimName: rbd-data}
    - name: cephfs
      persistentVolumeClaim: {claimName: cephfs-data}
EOF
kubectl apply -f "$tmp/fixtures.yaml"
kubectl -n "$TEST_NAMESPACE" wait pvc/rbd-data pvc/cephfs-data --for=jsonpath='{.status.phase}'=Bound --timeout=10m
kubectl -n "$TEST_NAMESPACE" wait pod/checksum-writer --for=jsonpath='{.status.phase}'=Succeeded --timeout=10m
kubectl -n "$TEST_NAMESPACE" logs checksum-writer | tee "$tmp/checksum.txt"
grep -Eq '^[0-9a-f]{64}' "$tmp/checksum.txt"

if kubectl api-resources --api-group=snapshot.storage.k8s.io -o name | grep -qx volumesnapshots; then
  cat >"$tmp/snapshot.yaml" <<EOF
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshotClass
metadata:
  name: ${SNAPSHOT_CLASS}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
driver: ${NAMESPACE}.rbd.csi.ceph.com
deletionPolicy: Delete
parameters:
  clusterID: ${NAMESPACE}
  csi.storage.k8s.io/snapshotter-secret-name: rook-csi-rbd-provisioner
  csi.storage.k8s.io/snapshotter-secret-namespace: ${NAMESPACE}
---
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: rbd-snapshot
  namespace: ${TEST_NAMESPACE}
  labels: {highland.io/test-run: "${RUN_SLUG}"}
spec:
  volumeSnapshotClassName: ${SNAPSHOT_CLASS}
  source:
    persistentVolumeClaimName: rbd-data
EOF
  kubectl apply -f "$tmp/snapshot.yaml"
  kubectl -n "$TEST_NAMESPACE" wait volumesnapshot/rbd-snapshot --for=jsonpath='{.status.readyToUse}'=true --timeout=10m
fi

if [[ -n "${HIGHLAND_BASE_URL:-}" ]]; then
  : "${HIGHLAND_USERNAME:?HIGHLAND_USERNAME is required when HIGHLAND_BASE_URL is set}"
  : "${HIGHLAND_PASSWORD:?HIGHLAND_PASSWORD is required when HIGHLAND_BASE_URL is set}"
  cookie="$tmp/cookies"
  jq -n '{username:env.HIGHLAND_USERNAME,password:env.HIGHLAND_PASSWORD}' |
    curl --fail --silent --show-error --cookie-jar "$cookie" -H 'Content-Type: application/json' --data-binary @- "$HIGHLAND_BASE_URL/auth/login" >/dev/null
  curl --fail --silent --show-error --cookie "$cookie" "$HIGHLAND_BASE_URL/api/v1/storage/providers" >"$tmp/providers.json"
  jq -e '.data[] | select(.id == "rook-ceph" and .supportLevel == "managed") | (.drivers | length >= 2)' "$tmp/providers.json" >/dev/null
  for endpoint in classes claims volumes snapshots attachments capacity events; do
    curl --fail --silent --show-error --cookie "$cookie" "$HIGHLAND_BASE_URL/api/v1/storage/$endpoint?search=$RUN_SLUG&limit=100" >"$tmp/$endpoint.json"
    jq -e '(.page.limit <= 100) and (.data | type == "array")' "$tmp/$endpoint.json" >/dev/null
  done
  curl --fail --silent --show-error --cookie "$cookie" "$HIGHLAND_BASE_URL/api/v1/providers/rook-ceph/summary" | jq -e '.health and .pools and .filesystems' >/dev/null
fi

echo "Rook/Ceph lifecycle and Highland read validation passed for run $RUN_ID."
