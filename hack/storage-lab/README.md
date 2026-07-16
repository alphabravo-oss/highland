# Disposable storage labs

These scripts are release-test tooling, not production installers. They refuse unlabeled clusters,
require exactly three nodes, and require the literal destructive acknowledgement before Rook may
consume raw devices. Use three disposable VMs with one blank data disk each and label every node:

```sh
kubectl label nodes --all highland.io/storage-lab=true
export HIGHLAND_STORAGE_LAB_ACK=destroy-scratch-disks
ROOK_VERSION=v1.20.2 CEPH_IMAGE=quay.io/ceph/ceph:v20.2.1 \
  ./hack/storage-lab/install-rook-ceph.sh
./hack/storage-lab/validate-rook-ceph.sh
```

Run the same sequence for the `current` and `previous` profiles in
`docs/compatibility.yaml`, on freshly rebuilt VMs. The validation suite creates uniquely labeled
RBD/CephFS Classes, claims, a checksum workload, and a snapshot when the API exists. It can also
authenticate to a deployed Highland and verify bounded common/provider responses by setting
`HIGHLAND_BASE_URL`, `HIGHLAND_USERNAME`, and `HIGHLAND_PASSWORD`.

Fixtures are removed on exit unless `HIGHLAND_KEEP_FIXTURES=1`. The release operator must verify no
test namespace, Class, PV, snapshot content, RBD image, or pool remains. Cluster destruction requires
an additional `HIGHLAND_DESTROY_CEPH_CLUSTER=1`; rebuild the scratch VMs after cleanup rather than
running a generic disk-zap command against a reusable host.
