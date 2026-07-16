package operations

import (
	"fmt"
	"sort"

	"github.com/highland-io/highland/apps/api/internal/auth"
)

var actionRegistry = map[string]Action{
	"create-pvc":                   {ID: "create-pvc", Capability: "volume.create", MinimumRole: "operator", Risk: RiskLow, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"namespace-scope", "storage-class", "quantity", "access-mode", "quota-dry-run"}, AuditAction: "storage_pvc_create"},
	"expand-pvc":                   {ID: "expand-pvc", Capability: "volume.expand", MinimumRole: "operator", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"namespace-scope", "expansion-supported", "size-increase", "resource-version", "quota-dry-run"}, AuditAction: "storage_pvc_expand"},
	"create-snapshot":              {ID: "create-snapshot", Capability: "snapshot.create", MinimumRole: "operator", Risk: RiskLow, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"snapshot-api", "snapshot-class", "source-claim", "driver-match"}, AuditAction: "storage_snapshot_create"},
	"restore-snapshot":             {ID: "restore-snapshot", Capability: "snapshot.restore", MinimumRole: "operator", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"snapshot-ready", "storage-class", "driver-match", "quota-dry-run"}, AuditAction: "storage_snapshot_restore"},
	"clone-pvc":                    {ID: "clone-pvc", Capability: "volume.clone", MinimumRole: "operator", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"source-claim", "storage-class", "driver-match", "quota-dry-run"}, AuditAction: "storage_pvc_clone"},
	"delete-snapshot":              {ID: "delete-snapshot", Capability: "snapshot.delete", MinimumRole: "operator", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"snapshot-api", "resource-version", "deletion-policy"}, AuditAction: "storage_snapshot_delete"},
	"delete-pvc":                   {ID: "delete-pvc", Capability: "volume.delete", MinimumRole: "admin", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"namespace-scope", "live-workloads", "attachments", "snapshots", "reclaim-policy", "finalizers", "resource-version"}, AuditAction: "storage_pvc_delete"},
	"create-ceph-rbd-storageclass": {ID: "create-ceph-rbd-storageclass", Capability: "ceph.storageclass.create", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "providers.rookCeph.writes.enabled", PreflightChecks: []string{"provider-health", "pool-ready", "class-name", "default-conflict", "server-dry-run"}, AuditAction: "ceph_rbd_storageclass_create"},
	"create-cephfs-storageclass":   {ID: "create-cephfs-storageclass", Capability: "ceph.storageclass.create", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "providers.rookCeph.writes.enabled", PreflightChecks: []string{"provider-health", "filesystem-ready", "class-name", "default-conflict", "server-dry-run"}, AuditAction: "cephfs_storageclass_create"},
	"delete-ceph-storageclass":     {ID: "delete-ceph-storageclass", Capability: "ceph.storageclass.delete", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "providers.rookCeph.writes.allowStorageClassDelete", PreflightChecks: []string{"class-dependencies", "retain-risk", "resource-version"}, AuditAction: "ceph_storageclass_delete"},
	"create-ceph-blockpool":        {ID: "create-ceph-blockpool", Capability: "ceph.pool.create", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskHigh, Confirmation: ConfirmSummary, FeatureFlag: "providers.rookCeph.writes.enabled", PreflightChecks: []string{"provider-health", "cluster-ready", "health-policy", "failure-domains", "replica-safety", "server-dry-run"}, AuditAction: "ceph_blockpool_create"},
	"delete-ceph-blockpool":        {ID: "delete-ceph-blockpool", Capability: "ceph.pool.delete", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskCritical, Confirmation: ConfirmTypedName, FeatureFlag: "providers.rookCeph.writes.allowPoolDelete", PreflightChecks: []string{"fresh-provider-health", "storageclasses", "claims-volumes", "snapshots", "images", "mirroring", "rook-dependencies", "resource-version"}, AuditAction: "ceph_blockpool_delete"},
}

func Actions() []Action {
	result := make([]Action, 0, len(actionRegistry))
	for _, action := range actionRegistry {
		result = append(result, action)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func ActionByID(id string) (Action, bool) { action, ok := actionRegistry[id]; return action, ok }

func targetKindForAction(actionID string) string {
	switch actionID {
	case "create-snapshot", "delete-snapshot":
		return "VolumeSnapshot"
	case "create-ceph-blockpool", "delete-ceph-blockpool":
		return "CephBlockPool"
	case "create-ceph-rbd-storageclass", "create-cephfs-storageclass", "delete-ceph-storageclass":
		return "StorageClass"
	default:
		return "PersistentVolumeClaim"
	}
}

func Authorize(action Action, role auth.Role) error {
	if action.MinimumRole == "admin" && role != auth.RoleAdmin {
		return fmt.Errorf("admin role is required for %s", action.ID)
	}
	if action.MinimumRole == "operator" && role != auth.RoleOperator && role != auth.RoleAdmin {
		return fmt.Errorf("operator role is required for %s", action.ID)
	}
	return nil
}
