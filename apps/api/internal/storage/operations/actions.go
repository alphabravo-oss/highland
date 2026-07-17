package operations

import (
	"fmt"
	"sort"

	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/policy"
)

var actionRegistry = map[string]Action{
	"create-pvc":                       {ID: "create-pvc", Capability: "volume.create", MinimumRole: "operator", Risk: RiskLow, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"namespace-scope", "storage-class", "quantity", "access-mode", "quota-dry-run"}, AuditAction: "storage_pvc_create"},
	"expand-pvc":                       {ID: "expand-pvc", Capability: "volume.expand", MinimumRole: "operator", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"namespace-scope", "expansion-supported", "size-increase", "resource-version", "quota-dry-run"}, AuditAction: "storage_pvc_expand"},
	"create-snapshot":                  {ID: "create-snapshot", Capability: "snapshot.create", MinimumRole: "operator", Risk: RiskLow, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"snapshot-api", "snapshot-class", "source-claim", "driver-match"}, AuditAction: "storage_snapshot_create"},
	"restore-snapshot":                 {ID: "restore-snapshot", Capability: "snapshot.restore", MinimumRole: "operator", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"snapshot-ready", "storage-class", "driver-match", "quota-dry-run"}, AuditAction: "storage_snapshot_restore"},
	"clone-pvc":                        {ID: "clone-pvc", Capability: "volume.clone", MinimumRole: "operator", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"source-claim", "storage-class", "driver-match", "quota-dry-run"}, AuditAction: "storage_pvc_clone"},
	"delete-snapshot":                  {ID: "delete-snapshot", Capability: "snapshot.delete", MinimumRole: "operator", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"snapshot-api", "resource-version", "deletion-policy"}, AuditAction: "storage_snapshot_delete"},
	"delete-pvc":                       {ID: "delete-pvc", Capability: "volume.delete", MinimumRole: "admin", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"namespace-scope", "live-workloads", "attachments", "snapshots", "reclaim-policy", "finalizers", "resource-version"}, AuditAction: "storage_pvc_delete"},
	"create-ceph-rbd-storageclass":     {ID: "create-ceph-rbd-storageclass", Capability: "ceph.storageclass.create", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "providers.rookCeph.writes.enabled", PreflightChecks: []string{"provider-health", "pool-ready", "class-name", "default-conflict", "server-dry-run"}, AuditAction: "ceph_rbd_storageclass_create"},
	"create-cephfs-storageclass":       {ID: "create-cephfs-storageclass", Capability: "ceph.storageclass.create", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "providers.rookCeph.writes.enabled", PreflightChecks: []string{"provider-health", "filesystem-ready", "class-name", "default-conflict", "server-dry-run"}, AuditAction: "cephfs_storageclass_create"},
	"delete-ceph-storageclass":         {ID: "delete-ceph-storageclass", Capability: "ceph.storageclass.delete", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "providers.rookCeph.writes.allowStorageClassDelete", PreflightChecks: []string{"class-dependencies", "retain-risk", "resource-version"}, AuditAction: "ceph_storageclass_delete"},
	"create-ceph-blockpool":            {ID: "create-ceph-blockpool", Capability: "ceph.pool.create", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskHigh, Confirmation: ConfirmSummary, FeatureFlag: "providers.rookCeph.writes.enabled", PreflightChecks: []string{"provider-health", "cluster-ready", "health-policy", "failure-domains", "replica-safety", "server-dry-run"}, AuditAction: "ceph_blockpool_create"},
	"delete-ceph-blockpool":            {ID: "delete-ceph-blockpool", Capability: "ceph.pool.delete", MinimumRole: "admin", ProviderKind: "rook-ceph", Risk: RiskCritical, Confirmation: ConfirmTypedName, FeatureFlag: "providers.rookCeph.writes.allowPoolDelete", PreflightChecks: []string{"fresh-provider-health", "storageclasses", "claims-volumes", "snapshots", "images", "mirroring", "rook-dependencies", "resource-version"}, AuditAction: "ceph_blockpool_delete"},
	"longhorn-volume-attach":           {ID: "longhorn-volume-attach", Capability: "longhorn.volume.attach", MinimumRole: "operator", ProviderKind: "longhorn", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "volume-state", "target-node", "action-available"}, AuditAction: "longhorn_volume_attach"},
	"longhorn-volume-detach":           {ID: "longhorn-volume-detach", Capability: "longhorn.volume.detach", MinimumRole: "operator", ProviderKind: "longhorn", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "volume-state", "workload-impact", "action-available"}, AuditAction: "longhorn_volume_detach"},
	"longhorn-volume-replica-count":    {ID: "longhorn-volume-replica-count", Capability: "longhorn.volume.replica-count", MinimumRole: "operator", ProviderKind: "longhorn", Risk: RiskMedium, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "volume-state", "replica-safety", "action-available"}, AuditAction: "longhorn_volume_replica_count"},
	"longhorn-volume-backup":           {ID: "longhorn-volume-backup", Capability: "longhorn.backup.create", MinimumRole: "operator", ProviderKind: "longhorn", Risk: RiskLow, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "snapshot-exists", "backup-target", "action-available"}, AuditAction: "longhorn_backup_create"},
	"longhorn-recurring-job-add":       {ID: "longhorn-recurring-job-add", Capability: "longhorn.recurring-job.assign", MinimumRole: "operator", ProviderKind: "longhorn", Risk: RiskLow, Confirmation: ConfirmSummary, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "recurring-job-exists", "action-available"}, AuditAction: "longhorn_recurring_job_add"},
	"longhorn-recurring-job-remove":    {ID: "longhorn-recurring-job-remove", Capability: "longhorn.recurring-job.remove", MinimumRole: "operator", ProviderKind: "longhorn", Risk: RiskMedium, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "recurring-job-assigned", "action-available"}, AuditAction: "longhorn_recurring_job_remove"},
	"longhorn-volume-salvage":          {ID: "longhorn-volume-salvage", Capability: "longhorn.volume.salvage", MinimumRole: "admin", ProviderKind: "longhorn", Risk: RiskCritical, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "volume-faulted", "replica-selection", "action-available"}, AuditAction: "longhorn_volume_salvage"},
	"longhorn-engine-upgrade":          {ID: "longhorn-engine-upgrade", Capability: "longhorn.volume.engine-upgrade", MinimumRole: "admin", ProviderKind: "longhorn", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "engine-image-ready", "volume-state", "action-available"}, AuditAction: "longhorn_engine_upgrade"},
	"longhorn-backup-target-configure": {ID: "longhorn-backup-target-configure", Capability: "longhorn.backup-target.configure", MinimumRole: "admin", ProviderKind: "longhorn", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "backup-url", "credential-reference"}, AuditAction: "longhorn_backup_target_configure"},
	"longhorn-backup-delete":           {ID: "longhorn-backup-delete", Capability: "longhorn.backup.delete", MinimumRole: "admin", ProviderKind: "longhorn", Risk: RiskCritical, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "backup-exists", "action-available"}, AuditAction: "longhorn_backup_delete"},
	"longhorn-backup-restore":          {ID: "longhorn-backup-restore", Capability: "longhorn.backup.restore", MinimumRole: "admin", ProviderKind: "longhorn", Risk: RiskHigh, Confirmation: ConfirmTypedName, FeatureFlag: "storage.writes.enabled", PreflightChecks: []string{"provider-health", "backup-exists", "target-name", "capacity"}, AuditAction: "longhorn_backup_restore"},
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
	case "longhorn-backup-target-configure":
		return "LonghornBackupTarget"
	case "longhorn-backup-delete":
		return "LonghornBackup"
	case "longhorn-backup-restore":
		return "LonghornVolume"
	case "longhorn-volume-attach", "longhorn-volume-detach", "longhorn-volume-replica-count",
		"longhorn-volume-backup", "longhorn-recurring-job-add", "longhorn-recurring-job-remove",
		"longhorn-volume-salvage", "longhorn-engine-upgrade":
		return "LonghornVolume"
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

func PolicyImpact(before, after policy.StoragePolicy) policy.Impact {
	before = policy.Normalize(before)
	after = policy.Normalize(after)
	impact := policy.Impact{
		ActionIDs: []string{}, Roles: []string{},
		AddedPortableProviderIDs: []string{}, RemovedPortableProviderIDs: []string{},
	}
	roles := map[string]bool{}
	for _, action := range Actions() {
		if actionEnabledByPolicy(action, before) || !actionEnabledByPolicy(action, after) {
			continue
		}
		impact.ActionIDs = append(impact.ActionIDs, action.ID)
		roles[action.MinimumRole] = true
	}
	for role := range roles {
		impact.Roles = append(impact.Roles, role)
	}
	beforeProviders := stringSet(before.PortableKubernetesProviderIDs)
	afterProviders := stringSet(after.PortableKubernetesProviderIDs)
	for providerID := range afterProviders {
		if _, existed := beforeProviders[providerID]; !existed {
			impact.AddedPortableProviderIDs = append(impact.AddedPortableProviderIDs, providerID)
		}
	}
	for providerID := range beforeProviders {
		if _, remains := afterProviders[providerID]; !remains {
			impact.RemovedPortableProviderIDs = append(impact.RemovedPortableProviderIDs, providerID)
		}
	}
	sort.Strings(impact.Roles)
	sort.Strings(impact.AddedPortableProviderIDs)
	sort.Strings(impact.RemovedPortableProviderIDs)
	return impact
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func actionEnabledByPolicy(action Action, value policy.StoragePolicy) bool {
	if !value.AcceptNewOperations {
		return false
	}
	switch action.FeatureFlag {
	case "storage.writes.enabled":
		if action.ProviderKind == "longhorn" {
			return value.LonghornWrites
		}
		return value.PortableKubernetesWrites
	case "providers.rookCeph.writes.enabled":
		return value.RookCephWrites
	case "providers.rookCeph.writes.allowStorageClassDelete":
		return value.RookCephWrites && value.AllowCephStorageClassDelete
	case "providers.rookCeph.writes.allowPoolDelete":
		return value.RookCephWrites && value.AllowCephPoolDelete
	default:
		return false
	}
}
