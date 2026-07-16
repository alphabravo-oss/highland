# Storage troubleshooting decision tree

Start with `/healthz`, `/readyz`, `/api/v1/status`, and the provider condition shown in the UI. Do
not infer “no objects” from an error condition.

1. **Is `/healthz` failing?** Inspect the API process/pod and configuration. Provider health does not
   control liveness.
2. **Is `/readyz` failing with storage core enabled?** Check Kubernetes API connectivity, service
   account RBAC, informer sync timestamps, and `highland_storage_watch_errors_total`. In namespace
   mode, confirm each configured namespace exists and its RoleBinding is rendered.
3. **Is an unknown CSI driver absent?** Check `CSIDriver`, StorageClass `provisioner`, and CSI-backed
   PV `spec.csi.driver`. Highland creates one collision-resistant detected provider per driver.
4. **Are snapshots unavailable?** Check discovery of `snapshot.storage.k8s.io/v1`, the three snapshot
   CRDs, Highland read RBAC, and a matching `VolumeSnapshotClass`. Absence is a supported partial
   state.
5. **Is Longhorn degraded?** Check the configured manager Service/port, namespace, supported-version
   warning, and `/api/v1/lh`. Optional Longhorn must not make common/Ceph inventory unavailable.
6. **Is Rook/Ceph degraded?** Check the configured CephCluster name (ambiguity is rejected), Rook
   operator conditions, then Dashboard and Prometheus independently. A Dashboard failure may return
   bounded stale runtime data while CRD inventory remains current.
7. **Is Dashboard authentication failing?** Verify the named read-only Secret keys, CA chain, URL,
   media type, and account permissions. Rotate the Secret and restart the API to clear memory-only
   JWT state. Never switch to the generated admin account as a diagnostic shortcut.
8. **Is Prometheus partial?** Test the configured query endpoint from the API network path and review
   the `unavailable` metric names. Highland executes only its fixed query allowlist.
9. **Is backend correlation unavailable?** Compare the PV's exact CSI driver/volume handle with the
   provider's documented identifier. Ambiguous and guessed PVC-name mappings are intentionally
   rejected.
10. **Is an operation blocked?** Use its machine-readable error code and
    [`runbooks/storage-operations.md`](runbooks/storage-operations.md). Obtain a new plan after stale
    UID/resourceVersion/dependencies; never edit the operation spec or force-remove storage
    finalizers.

For alerts, inspect the optional PrometheusRule and Grafana dashboard rendered by the chart. For a
permission-level inventory, see [`security/storage-rbac.md`](security/storage-rbac.md).
