package operations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/highland-io/highland/apps/api/internal/storage"
)

var snapshotGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshots"}
var snapshotClassGVR = schema.GroupVersionResource{Group: "snapshot.storage.k8s.io", Version: "v1", Resource: "volumesnapshotclasses"}
var poolGVR = schema.GroupVersionResource{Group: "ceph.rook.io", Version: "v1", Resource: "cephblockpools"}
var filesystemGVR = schema.GroupVersionResource{Group: "ceph.rook.io", Version: "v1", Resource: "cephfilesystems"}
var clusterGVR = schema.GroupVersionResource{Group: "ceph.rook.io", Version: "v1", Resource: "cephclusters"}

type SafetyVerifier interface {
	VerifyPoolEmpty(context.Context, string, string) (bool, string, error)
}

type PoolPostflightVerifier interface {
	VerifyPoolPresent(context.Context, string, string) (bool, string, error)
}

type PlannerConfig struct {
	Core                  kubernetes.Interface
	Dynamic               dynamic.Interface
	Scope                 storage.Scope
	Secret                []byte
	RookNamespace         string
	RookClusterName       string
	Safety                SafetyVerifier
	PlanDryRun            bool
	ImpactAnalyzer        storage.ImpactAnalyzer
	ProviderForDriver     func(string) string
	RequireImpactAnalysis bool
	Longhorn              LonghornClient
	PolicyVersion         func() string
}

type Planner struct {
	core                  kubernetes.Interface
	dynamic               dynamic.Interface
	scope                 storage.Scope
	secret                []byte
	rookNamespace         string
	rookClusterName       string
	safety                SafetyVerifier
	planDryRun            bool
	impactAnalyzer        storage.ImpactAnalyzer
	providerForDriver     func(string) string
	requireImpactAnalysis bool
	longhorn              LonghornClient
	policyVersion         func() string
}

type PlanError struct {
	Code      string
	Message   string
	Details   map[string]any
	Retryable bool
}

func (e *PlanError) Error() string { return e.Message }

func NewPlanner(cfg PlannerConfig) (*Planner, error) {
	if cfg.Core == nil || cfg.Dynamic == nil {
		return nil, fmt.Errorf("operation planner requires Kubernetes clients")
	}
	if len(cfg.Secret) < 32 {
		return nil, fmt.Errorf("operation planner requires a 256-bit signing secret")
	}
	if cfg.RookNamespace == "" {
		cfg.RookNamespace = "rook-ceph"
	}
	if cfg.RookClusterName == "" {
		cfg.RookClusterName = "rook-ceph"
	}
	return &Planner{
		core: cfg.Core, dynamic: cfg.Dynamic, scope: cfg.Scope, secret: append([]byte(nil), cfg.Secret...),
		rookNamespace: cfg.RookNamespace, rookClusterName: cfg.RookClusterName, safety: cfg.Safety,
		planDryRun: cfg.PlanDryRun, impactAnalyzer: cfg.ImpactAnalyzer,
		providerForDriver: cfg.ProviderForDriver, requireImpactAnalysis: cfg.RequireImpactAnalysis,
		longhorn: cfg.Longhorn, policyVersion: cfg.PolicyVersion,
	}, nil
}

func (p *Planner) Plan(ctx context.Context, requester string, request Request) (Plan, error) {
	action, ok := ActionByID(request.ActionID)
	if !ok {
		return Plan{}, &PlanError{Code: "ACTION_NOT_SUPPORTED", Message: "storage action is not supported"}
	}
	if request.Parameters == nil {
		request.Parameters = map[string]any{}
	}
	expectedKind := targetKindForAction(request.ActionID)
	if request.Target.Kind == "" {
		request.Target.Kind = expectedKind
	} else if request.Target.Kind != expectedKind {
		return Plan{}, &PlanError{Code: "TARGET_KIND_MISMATCH", Message: "target kind does not match the requested storage action", Details: map[string]any{"expectedKind": expectedKind}}
	}
	if err := validateActionParameters(request.ActionID, request.Parameters); err != nil {
		return Plan{}, err
	}
	request.Target.Name = strings.TrimSpace(request.Target.Name)
	request.Target.Namespace = strings.TrimSpace(request.Target.Namespace)
	if request.Target.Name == "" {
		return Plan{}, invalid("target.name", "target name is required")
	}
	if messages := validation.IsDNS1123Subdomain(request.Target.Name); len(messages) > 0 {
		return Plan{}, invalid("target.name", "target name must be a valid Kubernetes DNS subdomain")
	}
	if request.Target.Namespace != "" {
		if messages := validation.IsDNS1123Label(request.Target.Namespace); len(messages) > 0 {
			return Plan{}, invalid("target.namespace", "target namespace must be a valid Kubernetes DNS label")
		}
	}
	if action.ProviderKind != "" {
		if request.ProviderID == "" {
			request.ProviderID = action.ProviderKind
		} else if request.ProviderID != action.ProviderKind {
			return Plan{}, &PlanError{Code: "PROVIDER_MISMATCH", Message: "action is bound to the configured " + action.ProviderKind + " provider"}
		}
	}
	if request.Target.Namespace != "" && !p.scope.Allows(request.Target.Namespace) {
		return Plan{}, &PlanError{Code: "NAMESPACE_NOT_ALLOWED", Message: "target namespace is outside Highland's configured scope", Details: map[string]any{"namespace": request.Target.Namespace}}
	}

	plan := Plan{Action: action, ProviderID: request.ProviderID, Target: request.Target, BlastRadius: "one Kubernetes resource", ObservedAt: time.Now().UTC()}
	plan.Checks = append(plan.Checks, Check{ID: "namespace-scope", Status: "pass", Message: "target is within the configured namespace scope"})
	var err error
	switch action.ID {
	case "create-pvc":
		err = p.planPVC(ctx, &plan, request, "create")
	case "restore-snapshot":
		err = p.planPVC(ctx, &plan, request, "restore")
	case "clone-pvc":
		err = p.planPVC(ctx, &plan, request, "clone")
	case "expand-pvc":
		err = p.planExpand(ctx, &plan, request)
	case "delete-pvc":
		err = p.planDeletePVC(ctx, &plan, request)
	case "create-snapshot":
		err = p.planSnapshot(ctx, &plan, request, false)
	case "delete-snapshot":
		err = p.planSnapshot(ctx, &plan, request, true)
	case "create-ceph-rbd-storageclass", "create-cephfs-storageclass":
		err = p.planCephClass(ctx, &plan, request)
	case "delete-ceph-storageclass":
		err = p.planDeleteClass(ctx, &plan, request)
	case "create-ceph-blockpool":
		err = p.planPool(ctx, &plan, request, false)
	case "delete-ceph-blockpool":
		err = p.planPool(ctx, &plan, request, true)
	case "longhorn-volume-attach", "longhorn-volume-detach", "longhorn-volume-replica-count",
		"longhorn-volume-backup", "longhorn-recurring-job-add", "longhorn-recurring-job-remove",
		"longhorn-volume-salvage", "longhorn-engine-upgrade", "longhorn-backup-target-configure",
		"longhorn-backup-delete", "longhorn-backup-restore":
		err = p.planLonghorn(ctx, &plan, request)
	}
	if err != nil {
		return Plan{}, err
	}
	if destructiveAction(action.ID) {
		if err := p.sharedImpactPreflight(ctx, &plan, request); err != nil {
			return Plan{}, err
		}
	}
	if p.planDryRun {
		if err := p.serverDryRun(ctx, request, plan); err != nil {
			return Plan{}, &PlanError{Code: "SERVER_DRY_RUN_FAILED", Message: "Kubernetes server-side dry-run rejected the planned operation", Details: map[string]any{"reason": sanitize(err.Error())}}
		}
		plan.Checks = append(plan.Checks, Check{ID: "server-dry-run", Status: "pass", Message: "Kubernetes admission accepted a server-side dry-run"})
	}
	plan.Hash = hashValue(map[string]any{"action": request.ActionID, "provider": plan.ProviderID, "target": plan.Target, "parameters": request.Parameters, "resources": plan.Resources, "dependencies": plan.Dependencies, "warnings": plan.Warnings})
	if p.policyVersion != nil {
		plan.PolicyVersion = p.policyVersion()
	}
	expires := time.Now().UTC().Add(5 * time.Minute)
	plan.ChallengeExpiresAt = expires
	plan.Challenge = p.sign(challengePayload{Requester: requester, ActionID: request.ActionID, ProviderID: plan.ProviderID, Target: plan.Target, PlanHash: plan.Hash, PolicyVersion: plan.PolicyVersion, Expires: expires.Unix()})
	return plan, nil
}

func destructiveAction(actionID string) bool {
	switch actionID {
	case "delete-pvc", "delete-snapshot", "delete-ceph-storageclass", "delete-ceph-blockpool":
		return true
	default:
		return false
	}
}

func (p *Planner) sharedImpactPreflight(ctx context.Context, plan *Plan, request Request) error {
	if p.impactAnalyzer == nil {
		if p.requireImpactAnalysis {
			return &PlanError{Code: "IMPACT_ANALYSIS_UNAVAILABLE", Message: "the shared storage dependency engine is unavailable; destructive planning is blocked", Retryable: true}
		}
		return nil
	}
	provider, kind, graphName, err := p.impactTarget(ctx, request, plan.Target)
	if err != nil {
		return err
	}
	if provider == "" {
		if p.requireImpactAnalysis {
			return &PlanError{Code: "IMPACT_PROVIDER_UNKNOWN", Message: "the target storage provider could not be established authoritatively; destructive planning is blocked"}
		}
		return nil
	}
	targetID := storage.CanonicalGraphID(kind, provider, plan.Target.Namespace, graphName)
	result, err := p.impactAnalyzer.AnalyzeImpact(ctx, provider, kind, targetID, 5)
	if err != nil {
		return &PlanError{Code: "IMPACT_ANALYSIS_UNAVAILABLE", Message: "the shared storage dependency result could not be built; destructive planning is blocked", Details: map[string]any{"reason": sanitize(err.Error())}, Retryable: true}
	}
	if result.Incomplete {
		return &PlanError{Code: "IMPACT_ANALYSIS_INCOMPLETE", Message: "required dependency evidence is partial or stale; destructive planning is blocked", Details: map[string]any{"conditions": result.Conditions}, Retryable: true}
	}
	seen := map[string]struct{}{}
	for _, dependency := range plan.Dependencies {
		seen[dependency.Kind+"\x00"+dependency.Namespace+"\x00"+dependency.Name] = struct{}{}
	}
	for _, impacted := range result.Confirmed {
		node := impacted.Node
		if node.ID == result.Target.ID || node.Kind == "provider" || node.Kind == "csidriver" {
			continue
		}
		dependency := ResourceTarget{
			Kind: graphKindToKubernetesKind(node.Kind), Namespace: node.Namespace,
			Name: node.Name, UID: node.UID,
		}
		key := dependency.Kind + "\x00" + dependency.Namespace + "\x00" + dependency.Name
		if dependency.Name == "" {
			continue
		}
		if _, exists := seen[key]; !exists {
			plan.Dependencies = append(plan.Dependencies, dependency)
			seen[key] = struct{}{}
		}
	}
	if len(result.Potential) > 0 || len(result.Unknown) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"Shared impact analysis found %d potential and %d unknown relationships; they are not presented as confirmed.",
			len(result.Potential), len(result.Unknown),
		))
	}
	plan.BlastRadius = fmt.Sprintf(
		"%d workloads, %d pods, %d namespaces, %d snapshots; %d confirmed, %d potential, %d unknown related resources",
		result.Summary.WorkloadCount, result.Summary.PodCount, result.Summary.NamespaceCount,
		result.Summary.SnapshotCount, len(result.Confirmed), len(result.Potential), len(result.Unknown),
	)
	plan.Checks = append(plan.Checks, Check{
		ID: "shared-impact-analysis", Status: "pass",
		Message: "the plan uses the same complete dependency result as the read-only impact API",
	})
	return nil
}

func (p *Planner) impactTarget(ctx context.Context, request Request, target ResourceTarget) (provider, kind, graphName string, err error) {
	switch request.ActionID {
	case "delete-ceph-blockpool":
		return "rook-ceph", "ceph-block-pool", target.Namespace + "/" + target.Name, nil
	case "delete-ceph-storageclass":
		return "rook-ceph", "storageclass", target.Name, nil
	case "delete-pvc":
		claim, getErr := p.core.CoreV1().PersistentVolumeClaims(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if getErr != nil {
			return "", "", "", classifyReadError(getErr, "CLAIM_NOT_FOUND", "PVC was not found", "PVC provider could not be resolved")
		}
		if claim.Spec.VolumeName == "" {
			return "", "", "", &PlanError{Code: "IMPACT_PROVIDER_UNKNOWN", Message: "an unbound PVC has no authoritative CSI provider identity"}
		}
		volume, getErr := p.core.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
		if getErr != nil {
			return "", "", "", classifyDependencyError(getErr, "the bound PV could not be inspected for provider attribution")
		}
		driver := ""
		if volume.Spec.CSI != nil {
			driver = volume.Spec.CSI.Driver
		}
		return p.resolveProvider(driver, request.ProviderID), "pvc", target.Name, nil
	case "delete-snapshot":
		snapshot, getErr := p.dynamic.Resource(snapshotGVR).Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if getErr != nil {
			return "", "", "", classifyReadError(getErr, "SNAPSHOT_NOT_FOUND", "VolumeSnapshot was not found", "snapshot provider could not be resolved")
		}
		className, _, _ := unstructured.NestedString(snapshot.Object, "spec", "volumeSnapshotClassName")
		if className == "" {
			return "", "", "", &PlanError{Code: "IMPACT_PROVIDER_UNKNOWN", Message: "VolumeSnapshotClass is unavailable, so the CSI provider cannot be established"}
		}
		class, getErr := p.dynamic.Resource(snapshotClassGVR).Get(ctx, className, metav1.GetOptions{})
		if getErr != nil {
			return "", "", "", classifyDependencyError(getErr, "VolumeSnapshotClass could not be inspected for provider attribution")
		}
		driver, _, _ := unstructured.NestedString(class.Object, "driver")
		return p.resolveProvider(driver, request.ProviderID), "volumesnapshot", target.Name, nil
	default:
		return "", "", "", &PlanError{Code: "IMPACT_TARGET_UNSUPPORTED", Message: "the destructive target is not supported by shared impact analysis"}
	}
}

func (p *Planner) resolveProvider(driver, requested string) string {
	if p.providerForDriver != nil && driver != "" {
		return p.providerForDriver(driver)
	}
	if driver != "" {
		return storage.GenericProviderID(driver)
	}
	return requested
}

// bindPortableProvider establishes the provider from an authoritative CSI
// driver discovered on the selected Kubernetes object. A client-supplied
// provider is only a consistency hint and can never override that attribution.
func (p *Planner) bindPortableProvider(plan *Plan, requested, driver string) error {
	driver = strings.TrimSpace(driver)
	if driver == "" {
		return &PlanError{Code: "PROVIDER_ATTRIBUTION_UNKNOWN", Message: "the selected resource has no authoritative CSI driver identity"}
	}
	providerID := strings.TrimSpace(p.resolveProvider(driver, ""))
	if providerID == "" {
		return &PlanError{Code: "PROVIDER_ATTRIBUTION_UNKNOWN", Message: "the selected CSI driver could not be mapped to a storage provider", Details: map[string]any{"driver": driver}}
	}
	if requested = strings.TrimSpace(requested); requested != "" && requested != providerID {
		return &PlanError{Code: "PROVIDER_MISMATCH", Message: "the requested provider does not own the selected storage resource", Details: map[string]any{"requestedProviderId": requested, "resolvedProviderId": providerID, "driver": driver}}
	}
	plan.ProviderID = providerID
	plan.Checks = append(plan.Checks, Check{ID: "provider-attribution", Status: "pass", Message: "CSI driver " + driver + " resolves to provider " + providerID})
	return nil
}

func graphKindToKubernetesKind(kind string) string {
	switch kind {
	case "pvc":
		return "PersistentVolumeClaim"
	case "pv":
		return "PersistentVolume"
	case "volumesnapshot":
		return "VolumeSnapshot"
	case "volumeattachment":
		return "VolumeAttachment"
	case "storageclass":
		return "StorageClass"
	case "pod":
		return "Pod"
	case "workload":
		return "Workload"
	case "ceph-block-pool":
		return "CephBlockPool"
	case "ceph-filesystem":
		return "CephFilesystem"
	case "rbd-image":
		return "CephRBDImage"
	default:
		return kind
	}
}

func (p *Planner) serverDryRun(ctx context.Context, request Request, plan Plan) error {
	if len(plan.Resources) != 1 {
		return fmt.Errorf("plan must contain exactly one resource")
	}
	resourcePlan := plan.Resources[0]
	switch resourcePlan.Operation {
	case "server-side-apply":
		gvr, namespaced, err := gvrFor(resourcePlan.APIVersion, resourcePlan.Kind)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(resourcePlan.Manifest)
		if err != nil {
			return err
		}
		resourceClient := p.dynamic.Resource(gvr)
		var client dynamic.ResourceInterface = resourceClient
		if namespaced {
			client = resourceClient.Namespace(resourcePlan.Namespace)
		}
		force := false
		_, err = client.Patch(ctx, resourcePlan.Name, k8stypes.ApplyPatchType, encoded, metav1.PatchOptions{FieldManager: "highland-storage-plan", Force: &force, DryRun: []string{metav1.DryRunAll}})
		return err
	case "update":
		claim, err := p.core.CoreV1().PersistentVolumeClaims(resourcePlan.Namespace).Get(ctx, resourcePlan.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if string(claim.UID) != plan.Target.UID || claim.ResourceVersion != plan.Target.ResourceVersion {
			return fmt.Errorf("target changed before dry-run")
		}
		quantity, err := resource.ParseQuantity(stringParameter(request.Parameters, "size"))
		if err != nil {
			return err
		}
		claim.Spec.Resources.Requests[corev1.ResourceStorage] = quantity
		_, err = p.core.CoreV1().PersistentVolumeClaims(resourcePlan.Namespace).Update(ctx, claim, metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
		return err
	case "delete":
		gvr, namespaced, err := gvrFor(resourcePlan.APIVersion, resourcePlan.Kind)
		if err != nil {
			return err
		}
		uid := k8stypes.UID(plan.Target.UID)
		resourceVersion := plan.Target.ResourceVersion
		options := metav1.DeleteOptions{DryRun: []string{metav1.DryRunAll}, Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}}
		resourceClient := p.dynamic.Resource(gvr)
		if namespaced {
			return resourceClient.Namespace(resourcePlan.Namespace).Delete(ctx, resourcePlan.Name, options)
		}
		return resourceClient.Delete(ctx, resourcePlan.Name, options)
	case "longhorn-action", "longhorn-create", "longhorn-update":
		return nil
	default:
		return fmt.Errorf("unsupported planned operation %q", resourcePlan.Operation)
	}
}

func (p *Planner) Verify(requester string, request Request, plan Plan) error {
	if request.Confirmation.Challenge == "" {
		return &PlanError{Code: "CONFIRMATION_REQUIRED", Message: "a current server-generated confirmation challenge is required"}
	}
	payload, err := p.verifyToken(request.Confirmation.Challenge)
	if err != nil {
		return err
	}
	currentPolicyVersion := plan.PolicyVersion
	if p.policyVersion != nil {
		currentPolicyVersion = p.policyVersion()
	}
	if payload.Requester != requester || payload.ActionID != request.ActionID || payload.ProviderID != plan.ProviderID || payload.Target.UID != plan.Target.UID || payload.Target.ResourceVersion != plan.Target.ResourceVersion || payload.PlanHash != plan.Hash || payload.PolicyVersion != currentPolicyVersion {
		return &PlanError{Code: "STALE_CONFIRMATION", Message: "confirmation no longer matches the current plan"}
	}
	if time.Now().Unix() > payload.Expires {
		return &PlanError{Code: "CONFIRMATION_EXPIRED", Message: "confirmation challenge has expired"}
	}
	if plan.Action.Confirmation == ConfirmTypedName && request.Confirmation.TypedName != plan.Target.Name && request.Confirmation.TypedName != plan.Target.Namespace+"/"+plan.Target.Name {
		return &PlanError{Code: "TYPED_NAME_MISMATCH", Message: "typed resource name does not match the target"}
	}
	if len(plan.Warnings) > 0 && !request.Confirmation.WarningsAcknowledged {
		return &PlanError{Code: "WARNING_ACKNOWLEDGEMENT_REQUIRED", Message: "the current plan warnings must be explicitly acknowledged"}
	}
	return nil
}

// ActionPrerequisite performs only bounded, read-only installation checks that
// are independent of a specific target. Resource-state and driver capability
// checks still run in Plan once the user selects a class or source object.
func (p *Planner) ActionPrerequisite(ctx context.Context, action Action) (bool, string) {
	if p == nil {
		return false, "operation planner is unavailable"
	}
	if strings.Contains(action.ID, "snapshot") || action.ID == "restore-snapshot" {
		resources, err := p.core.Discovery().ServerResourcesForGroupVersion("snapshot.storage.k8s.io/v1")
		if err != nil {
			return false, "snapshot.storage.k8s.io/v1 is unavailable or not permitted"
		}
		present := map[string]bool{}
		for _, resource := range resources.APIResources {
			present[resource.Name] = true
		}
		if !present["volumesnapshots"] || !present["volumesnapshotclasses"] {
			return false, "VolumeSnapshot and VolumeSnapshotClass APIs are required"
		}
	}
	if action.ProviderKind == "rook-ceph" {
		cluster, err := p.dynamic.Resource(clusterGVR).Namespace(p.rookNamespace).Get(ctx, p.rookClusterName, metav1.GetOptions{})
		if err != nil {
			return false, "the configured CephCluster is unavailable"
		}
		if !rookObjectReady(cluster) {
			return false, "the configured CephCluster is not ready"
		}
	}
	if action.ProviderKind == "longhorn" {
		if p.longhorn == nil {
			return false, "the Longhorn manager integration is unavailable"
		}
		if _, err := p.longhorn.List(ctx, "volumes"); err != nil {
			return false, "the Longhorn manager is unavailable"
		}
	}
	return true, ""
}

func (p *Planner) planPVC(ctx context.Context, plan *Plan, request Request, mode string) error {
	if request.Target.Namespace == "" {
		return invalid("target.namespace", "PVC namespace is required")
	}
	if _, err := p.core.CoreV1().PersistentVolumeClaims(request.Target.Namespace).Get(ctx, request.Target.Name, metav1.GetOptions{}); err == nil {
		return &PlanError{Code: "ALREADY_EXISTS", Message: "a PVC with this name already exists"}
	} else if !apierrors.IsNotFound(err) {
		return &PlanError{Code: "DEPENDENCY_CHECK_FAILED", Message: "could not prove the target PVC name is available", Retryable: true}
	}
	className := stringParameter(request.Parameters, "storageClass")
	if className == "" {
		return invalid("parameters.storageClass", "storageClass is required")
	}
	class, err := p.core.StorageV1().StorageClasses().Get(ctx, className, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return &PlanError{Code: "STORAGE_CLASS_NOT_FOUND", Message: "selected StorageClass was not found"}
	} else if err != nil {
		return &PlanError{Code: "DEPENDENCY_CHECK_FAILED", Message: "selected StorageClass could not be inspected", Retryable: true}
	}
	if err := p.bindPortableProvider(plan, request.ProviderID, class.Provisioner); err != nil {
		return err
	}
	size := stringParameter(request.Parameters, "size")
	if _, err := resource.ParseQuantity(size); err != nil {
		return invalid("parameters.size", "size must be a valid positive Kubernetes quantity")
	}
	quantity, _ := resource.ParseQuantity(size)
	if quantity.Sign() <= 0 {
		return invalid("parameters.size", "size must be positive")
	}
	accessModes, err := accessModes(request.Parameters)
	if err != nil {
		return err
	}
	manifest := map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": request.Target.Name, "namespace": request.Target.Namespace, "labels": map[string]any{"app.kubernetes.io/managed-by": "highland"}}, "spec": map[string]any{"storageClassName": className, "accessModes": accessModes, "resources": map[string]any{"requests": map[string]any{"storage": size}}}}
	if volumeMode := stringParameter(request.Parameters, "volumeMode"); volumeMode != "" {
		if volumeMode != "Filesystem" && volumeMode != "Block" {
			return invalid("parameters.volumeMode", "volumeMode must be Filesystem or Block")
		}
		manifest["spec"].(map[string]any)["volumeMode"] = volumeMode
	}
	if mode == "restore" {
		sourceNamespace, sourceName := request.Target.Namespace, stringParameter(request.Parameters, "sourceSnapshot")
		if sourceName == "" {
			return invalid("parameters.sourceSnapshot", "sourceSnapshot is required")
		}
		source, getErr := p.dynamic.Resource(snapshotGVR).Namespace(sourceNamespace).Get(ctx, sourceName, metav1.GetOptions{})
		if getErr != nil {
			return classifyReadError(getErr, "SNAPSHOT_NOT_FOUND", "source VolumeSnapshot was not found", "source VolumeSnapshot could not be inspected")
		}
		ready, _, _ := unstructured.NestedBool(source.Object, "status", "readyToUse")
		if !ready {
			return &PlanError{Code: "SNAPSHOT_NOT_READY", Message: "source VolumeSnapshot is not ready"}
		}
		if restoreSize, found, _ := unstructured.NestedString(source.Object, "status", "restoreSize"); found && restoreSize != "" {
			minimum, parseErr := resource.ParseQuantity(restoreSize)
			if parseErr == nil && quantity.Cmp(minimum) < 0 {
				return &PlanError{Code: "RESTORE_SIZE_TOO_SMALL", Message: "target PVC size must be at least the snapshot restore size"}
			}
		}
		snapshotClassName, _, _ := unstructured.NestedString(source.Object, "spec", "volumeSnapshotClassName")
		if snapshotClassName != "" {
			snapshotClass, classErr := p.dynamic.Resource(snapshotClassGVR).Get(ctx, snapshotClassName, metav1.GetOptions{})
			if classErr != nil {
				return classifyReadError(classErr, "SNAPSHOT_CLASS_NOT_FOUND", "source snapshot class was not found", "source snapshot class could not be inspected")
			}
			snapshotDriver, _, _ := unstructured.NestedString(snapshotClass.Object, "driver")
			if snapshotDriver != class.Provisioner {
				return &PlanError{Code: "DRIVER_MISMATCH", Message: "source snapshot and target StorageClass use different CSI drivers"}
			}
		}
		manifest["spec"].(map[string]any)["dataSource"] = map[string]any{"apiGroup": "snapshot.storage.k8s.io", "kind": "VolumeSnapshot", "name": sourceName}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "snapshot.storage.k8s.io/v1", Kind: "VolumeSnapshot", Namespace: sourceNamespace, Name: sourceName, UID: string(source.GetUID()), ResourceVersion: source.GetResourceVersion()})
	}
	if mode == "clone" {
		sourceName := stringParameter(request.Parameters, "sourceClaim")
		if sourceName == "" {
			return invalid("parameters.sourceClaim", "sourceClaim is required")
		}
		source, getErr := p.core.CoreV1().PersistentVolumeClaims(request.Target.Namespace).Get(ctx, sourceName, metav1.GetOptions{})
		if getErr != nil {
			return classifyReadError(getErr, "SOURCE_CLAIM_NOT_FOUND", "source PVC was not found", "source PVC could not be inspected")
		}
		if source.Spec.StorageClassName == nil {
			return &PlanError{Code: "SOURCE_DRIVER_UNKNOWN", Message: "source PVC has no StorageClass"}
		}
		sourceClass, sourceClassErr := p.core.StorageV1().StorageClasses().Get(ctx, *source.Spec.StorageClassName, metav1.GetOptions{})
		if sourceClassErr != nil {
			return classifyReadError(sourceClassErr, "SOURCE_STORAGE_CLASS_NOT_FOUND", "source StorageClass was not found", "source StorageClass could not be inspected")
		}
		if sourceClass.Provisioner != class.Provisioner {
			return &PlanError{Code: "DRIVER_MISMATCH", Message: "source and target StorageClasses use different CSI drivers"}
		}
		if source.Spec.VolumeName == "" {
			return &PlanError{Code: "SOURCE_CLAIM_NOT_BOUND", Message: "source PVC must be bound before cloning"}
		}
		sourceSize := source.Spec.Resources.Requests[corev1.ResourceStorage]
		if statusSize, ok := source.Status.Capacity[corev1.ResourceStorage]; ok {
			sourceSize = statusSize
		}
		if quantity.Cmp(sourceSize) < 0 {
			return &PlanError{Code: "CLONE_SIZE_TOO_SMALL", Message: "target PVC size must be at least the source PVC capacity"}
		}
		manifest["spec"].(map[string]any)["dataSource"] = map[string]any{"apiGroup": "", "kind": "PersistentVolumeClaim", "name": sourceName}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: request.Target.Namespace, Name: sourceName, UID: string(source.UID), ResourceVersion: source.ResourceVersion})
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: sourceClass.Name, UID: string(sourceClass.UID), ResourceVersion: sourceClass.ResourceVersion})
	}
	plan.Resources = []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: request.Target.Namespace, Name: request.Target.Name, Operation: "server-side-apply", Manifest: manifest}}
	plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: class.Name, UID: string(class.UID), ResourceVersion: class.ResourceVersion})
	plan.Checks = append(plan.Checks, Check{ID: "storage-class", Status: "pass", Message: "StorageClass exists"}, Check{ID: "quantity", Status: "pass", Message: "requested capacity is valid"}, Check{ID: "access-mode", Status: "pass", Message: "access and volume modes are supported by the operation schema"}, Check{ID: "quota-dry-run", Status: "advisory", Message: "admission, quota, and LimitRange are re-evaluated by Kubernetes during server-side apply"})
	return nil
}

func (p *Planner) planExpand(ctx context.Context, plan *Plan, request Request) error {
	claim, err := p.core.CoreV1().PersistentVolumeClaims(request.Target.Namespace).Get(ctx, request.Target.Name, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "CLAIM_NOT_FOUND", "PVC was not found", "PVC could not be inspected")
	}
	if claim.Spec.StorageClassName == nil {
		return &PlanError{Code: "EXPANSION_UNSUPPORTED", Message: "PVC has no StorageClass"}
	}
	class, err := p.core.StorageV1().StorageClasses().Get(ctx, *claim.Spec.StorageClassName, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "STORAGE_CLASS_NOT_FOUND", "StorageClass was not found", "StorageClass expansion policy could not be inspected")
	}
	if class.AllowVolumeExpansion == nil || !*class.AllowVolumeExpansion {
		return &PlanError{Code: "EXPANSION_UNSUPPORTED", Message: "StorageClass does not allow volume expansion"}
	}
	if err := p.bindPortableProvider(plan, request.ProviderID, class.Provisioner); err != nil {
		return err
	}
	requested, err := resource.ParseQuantity(stringParameter(request.Parameters, "size"))
	if err != nil {
		return invalid("parameters.size", "size must be a valid Kubernetes quantity")
	}
	current := claim.Spec.Resources.Requests[corev1.ResourceStorage]
	if requested.Cmp(current) <= 0 {
		return &PlanError{Code: "SIZE_NOT_INCREASED", Message: "new PVC size must be greater than the current request"}
	}
	plan.Target.UID, plan.Target.ResourceVersion = string(claim.UID), claim.ResourceVersion
	plan.Resources = []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: claim.Namespace, Name: claim.Name, Operation: "update", Manifest: map[string]any{"oldSize": current.String(), "newSize": requested.String()}}}
	plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: class.Name, UID: string(class.UID), ResourceVersion: class.ResourceVersion})
	plan.Checks = append(plan.Checks, Check{ID: "expansion-supported", Status: "pass", Message: "StorageClass permits expansion"}, Check{ID: "size-increase", Status: "pass", Message: "requested size increases the current size"}, Check{ID: "resource-version", Status: "pass", Message: "update is bound to the current resourceVersion"})
	return nil
}

func (p *Planner) planDeletePVC(ctx context.Context, plan *Plan, request Request) error {
	claim, err := p.core.CoreV1().PersistentVolumeClaims(request.Target.Namespace).Get(ctx, request.Target.Name, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "CLAIM_NOT_FOUND", "PVC was not found", "PVC could not be inspected")
	}
	pods, err := p.core.CoreV1().Pods(request.Target.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return classifyDependencyError(err, "could not prove the PVC is unused")
	}
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == claim.Name {
				return &PlanError{Code: "LIVE_WORKLOAD_REFERENCES_CLAIM", Message: "PVC deletion is blocked while a live workload references it", Details: map[string]any{"pod": pod.Name}}
			}
		}
	}
	plan.Target.UID, plan.Target.ResourceVersion = string(claim.UID), claim.ResourceVersion
	plan.Resources = []PlannedResource{{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: claim.Namespace, Name: claim.Name, Operation: "delete"}}
	if claim.Spec.VolumeName != "" {
		if pv, getErr := p.core.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{}); getErr == nil {
			if pv.Spec.CSI == nil {
				return &PlanError{Code: "PROVIDER_ATTRIBUTION_UNKNOWN", Message: "the bound PV has no CSI driver identity"}
			}
			if bindErr := p.bindPortableProvider(plan, request.ProviderID, pv.Spec.CSI.Driver); bindErr != nil {
				return bindErr
			}
			plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "v1", Kind: "PersistentVolume", Name: pv.Name, UID: string(pv.UID), ResourceVersion: pv.ResourceVersion})
			plan.Warnings = append(plan.Warnings, "PV reclaim policy is "+string(pv.Spec.PersistentVolumeReclaimPolicy))
			if pv.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain {
				plan.Warnings = append(plan.Warnings, "Retain leaves backend data and a released PV requiring manual lifecycle handling.")
			}
		} else {
			return classifyDependencyError(getErr, "could not inspect the bound PV and reclaim policy")
		}
		attachments, attachmentErr := p.core.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
		if attachmentErr != nil {
			return classifyDependencyError(attachmentErr, "could not inspect VolumeAttachments for the PVC")
		}
		for index := range attachments.Items {
			attachment := &attachments.Items[index]
			if attachment.Spec.Source.PersistentVolumeName != nil && *attachment.Spec.Source.PersistentVolumeName == claim.Spec.VolumeName {
				plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "storage.k8s.io/v1", Kind: "VolumeAttachment", Name: attachment.Name, UID: string(attachment.UID), ResourceVersion: attachment.ResourceVersion})
				plan.Warnings = append(plan.Warnings, "VolumeAttachment "+attachment.Name+" still targets the claim's PV on node "+attachment.Spec.NodeName+".")
			}
		}
	} else {
		if claim.Spec.StorageClassName == nil || strings.TrimSpace(*claim.Spec.StorageClassName) == "" {
			return &PlanError{Code: "PROVIDER_ATTRIBUTION_UNKNOWN", Message: "the unbound PVC has no StorageClass for provider attribution"}
		}
		class, classErr := p.core.StorageV1().StorageClasses().Get(ctx, *claim.Spec.StorageClassName, metav1.GetOptions{})
		if classErr != nil {
			return classifyReadError(classErr, "STORAGE_CLASS_NOT_FOUND", "PVC StorageClass was not found", "PVC StorageClass could not be inspected for provider attribution")
		}
		if bindErr := p.bindPortableProvider(plan, request.ProviderID, class.Provisioner); bindErr != nil {
			return bindErr
		}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: class.Name, UID: string(class.UID), ResourceVersion: class.ResourceVersion})
	}
	if len(claim.Finalizers) > 0 {
		plan.Warnings = append(plan.Warnings, "PVC has finalizers: "+strings.Join(claim.Finalizers, ", "))
	}
	if snapshots, snapshotErr := p.dynamic.Resource(snapshotGVR).Namespace(request.Target.Namespace).List(ctx, metav1.ListOptions{}); snapshotErr == nil {
		for index := range snapshots.Items {
			sourceName, _, _ := unstructured.NestedString(snapshots.Items[index].Object, "spec", "source", "persistentVolumeClaimName")
			if sourceName == claim.Name {
				plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "snapshot.storage.k8s.io/v1", Kind: "VolumeSnapshot", Namespace: request.Target.Namespace, Name: snapshots.Items[index].GetName(), UID: string(snapshots.Items[index].GetUID()), ResourceVersion: snapshots.Items[index].GetResourceVersion()})
				plan.Warnings = append(plan.Warnings, "VolumeSnapshot "+snapshots.Items[index].GetName()+" references this claim and may retain backend data.")
			}
		}
	} else if !apierrors.IsNotFound(snapshotErr) {
		return classifyDependencyError(snapshotErr, "could not inspect VolumeSnapshot dependencies for the PVC")
	}
	plan.BlastRadius = "one PVC and its reclaim-policy-controlled PV/backend lifecycle"
	plan.Checks = append(plan.Checks, Check{ID: "live-workloads", Status: "pass", Message: "no live Pod references the PVC"}, Check{ID: "resource-version", Status: "pass", Message: "delete is bound to UID and resourceVersion"})
	return nil
}

func (p *Planner) planSnapshot(ctx context.Context, plan *Plan, request Request, deleting bool) error {
	if deleting {
		snapshot, err := p.dynamic.Resource(snapshotGVR).Namespace(request.Target.Namespace).Get(ctx, request.Target.Name, metav1.GetOptions{})
		if err != nil {
			return classifyReadError(err, "SNAPSHOT_NOT_FOUND", "VolumeSnapshot was not found", "VolumeSnapshot could not be inspected")
		}
		plan.Target.UID, plan.Target.ResourceVersion = string(snapshot.GetUID()), snapshot.GetResourceVersion()
		plan.Resources = []PlannedResource{{APIVersion: "snapshot.storage.k8s.io/v1", Kind: "VolumeSnapshot", Namespace: request.Target.Namespace, Name: request.Target.Name, Operation: "delete"}}
		className, _, _ := unstructured.NestedString(snapshot.Object, "spec", "volumeSnapshotClassName")
		if className == "" {
			return &PlanError{Code: "PROVIDER_ATTRIBUTION_UNKNOWN", Message: "VolumeSnapshot has no VolumeSnapshotClass for provider attribution"}
		}
		snapshotClass, classErr := p.dynamic.Resource(snapshotClassGVR).Get(ctx, className, metav1.GetOptions{})
		if classErr != nil {
			return classifyReadError(classErr, "SNAPSHOT_CLASS_NOT_FOUND", "VolumeSnapshotClass was not found", "VolumeSnapshotClass could not be inspected for provider attribution")
		}
		driver, _, _ := unstructured.NestedString(snapshotClass.Object, "driver")
		if bindErr := p.bindPortableProvider(plan, request.ProviderID, driver); bindErr != nil {
			return bindErr
		}
		policy, _, _ := unstructured.NestedString(snapshotClass.Object, "deletionPolicy")
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "snapshot.storage.k8s.io/v1", Kind: "VolumeSnapshotClass", Name: className, UID: string(snapshotClass.GetUID()), ResourceVersion: snapshotClass.GetResourceVersion()})
		if policy == "Retain" {
			plan.Warnings = append(plan.Warnings, "VolumeSnapshotClass deletionPolicy is Retain; backend snapshot content remains after this object is deleted.")
		} else {
			plan.Warnings = append(plan.Warnings, "VolumeSnapshotClass deletionPolicy is "+policy+"; Kubernetes controls backend content deletion.")
		}
		return nil
	}
	if _, err := p.dynamic.Resource(snapshotGVR).Namespace(request.Target.Namespace).Get(ctx, request.Target.Name, metav1.GetOptions{}); err == nil {
		return &PlanError{Code: "ALREADY_EXISTS", Message: "a VolumeSnapshot with this name already exists"}
	} else if !apierrors.IsNotFound(err) {
		return &PlanError{Code: "DEPENDENCY_CHECK_FAILED", Message: "could not prove the target VolumeSnapshot name is available", Retryable: true}
	}
	sourceName := stringParameter(request.Parameters, "sourceClaim")
	className := stringParameter(request.Parameters, "snapshotClass")
	if sourceName == "" || className == "" {
		return invalid("parameters", "sourceClaim and snapshotClass are required")
	}
	claim, err := p.core.CoreV1().PersistentVolumeClaims(request.Target.Namespace).Get(ctx, sourceName, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "SOURCE_CLAIM_NOT_FOUND", "source PVC was not found", "source PVC could not be inspected")
	}
	if claim.Spec.VolumeName == "" {
		return &PlanError{Code: "SOURCE_CLAIM_NOT_BOUND", Message: "source PVC must be bound before snapshot creation"}
	}
	pv, err := p.core.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "SOURCE_VOLUME_NOT_FOUND", "source PV was not found", "source PV could not be inspected")
	}
	if pv.Spec.CSI == nil {
		return &PlanError{Code: "SOURCE_DRIVER_UNKNOWN", Message: "source PVC CSI driver could not be determined"}
	}
	snapshotClass, err := p.dynamic.Resource(snapshotClassGVR).Get(ctx, className, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "SNAPSHOT_CLASS_NOT_FOUND", "VolumeSnapshotClass was not found", "VolumeSnapshotClass could not be inspected")
	}
	snapshotDriver, _, _ := unstructured.NestedString(snapshotClass.Object, "driver")
	if snapshotDriver != pv.Spec.CSI.Driver {
		return &PlanError{Code: "DRIVER_MISMATCH", Message: "VolumeSnapshotClass driver does not match the source PV CSI driver"}
	}
	if err := p.bindPortableProvider(plan, request.ProviderID, pv.Spec.CSI.Driver); err != nil {
		return err
	}
	manifest := map[string]any{"apiVersion": "snapshot.storage.k8s.io/v1", "kind": "VolumeSnapshot", "metadata": map[string]any{"name": request.Target.Name, "namespace": request.Target.Namespace, "labels": map[string]any{"app.kubernetes.io/managed-by": "highland"}}, "spec": map[string]any{"volumeSnapshotClassName": className, "source": map[string]any{"persistentVolumeClaimName": sourceName}}}
	plan.Resources = []PlannedResource{{APIVersion: "snapshot.storage.k8s.io/v1", Kind: "VolumeSnapshot", Namespace: request.Target.Namespace, Name: request.Target.Name, Operation: "server-side-apply", Manifest: manifest}}
	plan.Dependencies = []ResourceTarget{
		{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: claim.Namespace, Name: claim.Name, UID: string(claim.UID), ResourceVersion: claim.ResourceVersion},
		{APIVersion: "v1", Kind: "PersistentVolume", Name: pv.Name, UID: string(pv.UID), ResourceVersion: pv.ResourceVersion},
		{APIVersion: "snapshot.storage.k8s.io/v1", Kind: "VolumeSnapshotClass", Name: className, UID: string(snapshotClass.GetUID()), ResourceVersion: snapshotClass.GetResourceVersion()},
	}
	plan.Checks = append(plan.Checks, Check{ID: "source-claim", Status: "pass", Message: "source PVC is bound"}, Check{ID: "snapshot-class", Status: "pass", Message: "snapshot class CSI driver matches the source PV"})
	return nil
}

func (p *Planner) planCephClass(ctx context.Context, plan *Plan, request Request) error {
	if request.ProviderID == "" {
		request.ProviderID = "rook-ceph"
		plan.ProviderID = request.ProviderID
	}
	if _, err := p.core.StorageV1().StorageClasses().Get(ctx, request.Target.Name, metav1.GetOptions{}); err == nil {
		return &PlanError{Code: "ALREADY_EXISTS", Message: "StorageClass already exists"}
	} else if !apierrors.IsNotFound(err) {
		return &PlanError{Code: "DEPENDENCY_CHECK_FAILED", Message: "could not prove the StorageClass name is available", Retryable: true}
	}
	if err := validateAllowed(request.Parameters, "reclaimPolicy", []string{"Delete", "Retain"}); err != nil {
		return err
	}
	if err := validateAllowed(request.Parameters, "volumeBindingMode", []string{"Immediate", "WaitForFirstConsumer"}); err != nil {
		return err
	}
	for _, key := range []string{"allowVolumeExpansion", "default", "encrypted"} {
		if err := validateBoolParameter(request.Parameters, key); err != nil {
			return err
		}
	}
	if boolParameter(request.Parameters, "default", false) {
		return &PlanError{Code: "DEFAULT_CLASS_CHANGE_UNSUPPORTED", Message: "changing the cluster default StorageClass requires a separately designed workflow"}
	}
	cluster, err := p.dynamic.Resource(clusterGVR).Namespace(p.rookNamespace).Get(ctx, p.rookClusterName, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "CEPH_CLUSTER_NOT_FOUND", "configured CephCluster was not found", "configured CephCluster could not be inspected")
	}
	if !rookObjectReady(cluster) {
		return &PlanError{Code: "CEPH_CLUSTER_NOT_READY", Message: "configured CephCluster is not ready for StorageClass creation"}
	}
	clusterHealth, _, _ := unstructured.NestedString(cluster.Object, "status", "ceph", "health")
	if strings.Contains(strings.ToUpper(clusterHealth), "ERR") {
		return &PlanError{Code: "CEPH_HEALTH_ERR", Message: "Ceph HEALTH_ERR blocks StorageClass creation"}
	}
	if strings.Contains(strings.ToUpper(clusterHealth), "WARN") {
		plan.Warnings = append(plan.Warnings, "Ceph reports HEALTH_WARN. Health is rechecked immediately before creating the StorageClass.")
	}
	plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "ceph.rook.io/v1", Kind: "CephCluster", Namespace: p.rookNamespace, Name: cluster.GetName(), UID: string(cluster.GetUID()), ResourceVersion: cluster.GetResourceVersion()})
	mountOptions, mountErr := allowedMountOptions(request.Parameters)
	if mountErr != nil {
		return mountErr
	}
	if request.ActionID == "create-ceph-rbd-storageclass" {
		if err := validateAllowed(request.Parameters, "filesystemType", []string{"ext4", "xfs"}); err != nil {
			return err
		}
		if err := validateAllowed(request.Parameters, "imageFeatures", []string{"layering", "layering,fast-diff,object-map,deep-flatten,exclusive-lock"}); err != nil {
			return err
		}
		poolName := stringParameter(request.Parameters, "pool")
		if poolName == "" {
			return invalid("parameters.pool", "pool is required")
		}
		pool, getErr := p.dynamic.Resource(poolGVR).Namespace(p.rookNamespace).Get(ctx, poolName, metav1.GetOptions{})
		if getErr != nil {
			return classifyReadError(getErr, "POOL_NOT_FOUND", "selected CephBlockPool was not found", "selected CephBlockPool could not be inspected")
		}
		if !rookObjectReady(pool) {
			return &PlanError{Code: "POOL_NOT_READY", Message: "selected CephBlockPool is not ready"}
		}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "ceph.rook.io/v1", Kind: "CephBlockPool", Namespace: p.rookNamespace, Name: poolName, UID: string(pool.GetUID()), ResourceVersion: pool.GetResourceVersion()})
	} else {
		filesystemName := stringParameter(request.Parameters, "filesystem")
		if filesystemName == "" {
			return invalid("parameters.filesystem", "filesystem is required")
		}
		filesystem, getErr := p.dynamic.Resource(filesystemGVR).Namespace(p.rookNamespace).Get(ctx, filesystemName, metav1.GetOptions{})
		if getErr != nil {
			return classifyReadError(getErr, "FILESYSTEM_NOT_FOUND", "selected CephFilesystem was not found", "selected CephFilesystem could not be inspected")
		}
		if !rookObjectReady(filesystem) {
			return &PlanError{Code: "FILESYSTEM_NOT_READY", Message: "selected CephFilesystem is not ready"}
		}
		if group := stringParameter(request.Parameters, "subvolumeGroup"); group != "" {
			if messages := validation.IsDNS1123Subdomain(group); len(messages) > 0 {
				return invalid("parameters.subvolumeGroup", "subvolumeGroup must be a valid Kubernetes-style name")
			}
		}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "ceph.rook.io/v1", Kind: "CephFilesystem", Namespace: p.rookNamespace, Name: filesystemName, UID: string(filesystem.GetUID()), ResourceVersion: filesystem.GetResourceVersion()})
	}
	provisioner, parameters := p.rookProvisioner(request.ActionID, request.Parameters)
	manifest := map[string]any{"apiVersion": "storage.k8s.io/v1", "kind": "StorageClass", "metadata": map[string]any{"name": request.Target.Name, "labels": map[string]any{"app.kubernetes.io/managed-by": "highland"}}, "provisioner": provisioner, "parameters": parameters, "reclaimPolicy": allowedString(request.Parameters, "reclaimPolicy", []string{"Delete", "Retain"}, "Delete"), "volumeBindingMode": allowedString(request.Parameters, "volumeBindingMode", []string{"Immediate", "WaitForFirstConsumer"}, "Immediate"), "allowVolumeExpansion": boolParameter(request.Parameters, "allowVolumeExpansion", true)}
	if len(mountOptions) > 0 {
		manifest["mountOptions"] = mountOptions
	}
	plan.Resources = []PlannedResource{{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: request.Target.Name, Operation: "server-side-apply", Manifest: manifest}}
	plan.Checks = append(plan.Checks, Check{ID: "class-name", Status: "pass", Message: "StorageClass name is available"})
	return nil
}

func (p *Planner) planDeleteClass(ctx context.Context, plan *Plan, request Request) error {
	class, err := p.core.StorageV1().StorageClasses().Get(ctx, request.Target.Name, metav1.GetOptions{})
	if err != nil {
		return classifyReadError(err, "STORAGE_CLASS_NOT_FOUND", "StorageClass was not found", "StorageClass could not be inspected")
	}
	if class.Provisioner != p.rookNamespace+".rbd.csi.ceph.com" && class.Provisioner != p.rookNamespace+".cephfs.csi.ceph.com" {
		return &PlanError{Code: "PROVIDER_MISMATCH", Message: "StorageClass is not owned by the configured Rook/Ceph provider"}
	}
	claims, claimErr := p.core.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	volumes, volumeErr := p.core.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if claimErr != nil {
		return classifyDependencyError(claimErr, "could not prove StorageClass has no PVC dependencies")
	}
	if volumeErr != nil {
		return classifyDependencyError(volumeErr, "could not prove StorageClass has no PV dependencies")
	}
	dependencies := []ResourceTarget{}
	for _, claim := range claims.Items {
		if claim.Spec.StorageClassName != nil && *claim.Spec.StorageClassName == class.Name {
			dependencies = append(dependencies, ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: claim.Namespace, Name: claim.Name})
		}
	}
	for _, volume := range volumes.Items {
		if volume.Spec.StorageClassName == class.Name {
			dependencies = append(dependencies, ResourceTarget{Kind: "PersistentVolume", Name: volume.Name})
		}
	}
	if len(dependencies) > 0 {
		return &PlanError{Code: "DEPENDENCIES_EXIST", Message: fmt.Sprintf("StorageClass is referenced by %d PVC/PV objects", len(dependencies)), Details: map[string]any{"dependencies": dependencies}}
	}
	plan.Target.UID, plan.Target.ResourceVersion = string(class.UID), class.ResourceVersion
	plan.Resources = []PlannedResource{{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: class.Name, Operation: "delete"}}
	return nil
}

func (p *Planner) planPool(ctx context.Context, plan *Plan, request Request, deleting bool) error {
	namespace := request.Target.Namespace
	if namespace == "" {
		namespace = p.rookNamespace
		plan.Target.Namespace = namespace
	}
	if namespace != p.rookNamespace {
		return &PlanError{Code: "PROVIDER_SCOPE_MISMATCH", Message: "CephBlockPool target is outside the configured Rook namespace"}
	}
	resourceClient := p.dynamic.Resource(poolGVR).Namespace(namespace)
	if deleting {
		pool, err := resourceClient.Get(ctx, request.Target.Name, metav1.GetOptions{})
		if err != nil {
			return classifyReadError(err, "POOL_NOT_FOUND", "CephBlockPool was not found", "CephBlockPool could not be inspected")
		}
		classes, classErr := p.core.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
		if classErr != nil {
			return classifyDependencyError(classErr, "could not prove pool is unused")
		}
		for _, class := range classes.Items {
			if class.Parameters["pool"] == pool.GetName() && (class.Parameters["clusterID"] == namespace || class.Parameters["clusterID"] == p.rookClusterName) {
				return &PlanError{Code: "DEPENDENCIES_EXIST", Message: "CephBlockPool is referenced by a StorageClass", Details: map[string]any{"storageClass": class.Name}}
			}
		}
		if p.safety == nil {
			return &PlanError{Code: "CANNOT_PROVE_EMPTY", Message: "backend emptiness verifier is unavailable; pool deletion is blocked"}
		}
		empty, reason, verifyErr := p.safety.VerifyPoolEmpty(ctx, namespace, pool.GetName())
		if verifyErr != nil || !empty {
			return &PlanError{Code: "CANNOT_PROVE_EMPTY", Message: nonempty(reason, "could not prove CephBlockPool is empty")}
		}
		plan.Target.UID, plan.Target.ResourceVersion = string(pool.GetUID()), pool.GetResourceVersion()
		plan.Resources = []PlannedResource{{APIVersion: "ceph.rook.io/v1", Kind: "CephBlockPool", Namespace: namespace, Name: pool.GetName(), Operation: "delete"}}
		plan.BlastRadius = "one proven-empty Ceph block pool"
		return nil
	}
	if _, err := resourceClient.Get(ctx, request.Target.Name, metav1.GetOptions{}); err == nil {
		return &PlanError{Code: "ALREADY_EXISTS", Message: "CephBlockPool already exists"}
	} else if !apierrors.IsNotFound(err) {
		return classifyDependencyError(err, "could not prove the CephBlockPool name is available")
	}
	verifier, ok := p.safety.(PoolPostflightVerifier)
	if !ok {
		return &PlanError{Code: "POOL_POSTFLIGHT_UNAVAILABLE", Message: "fresh Ceph runtime verification is required before creating a pool", Retryable: true}
	}
	present, reason, verifyErr := verifier.VerifyPoolPresent(ctx, namespace, request.Target.Name)
	if verifyErr != nil {
		return &PlanError{Code: "POOL_POSTFLIGHT_UNAVAILABLE", Message: nonempty(reason, "fresh Ceph runtime verification is unavailable"), Retryable: true}
	}
	if present {
		return &PlanError{Code: "BACKEND_POOL_EXISTS", Message: "Ceph runtime already contains a pool with this name"}
	}
	cluster, clusterErr := p.dynamic.Resource(clusterGVR).Namespace(p.rookNamespace).Get(ctx, p.rookClusterName, metav1.GetOptions{})
	if clusterErr != nil {
		return &PlanError{Code: "CEPH_CLUSTER_NOT_FOUND", Message: "configured CephCluster was not found", Retryable: !apierrors.IsNotFound(clusterErr)}
	}
	clusterState, _, _ := unstructured.NestedString(cluster.Object, "status", "state")
	clusterHealth, _, _ := unstructured.NestedString(cluster.Object, "status", "ceph", "health")
	if strings.Contains(strings.ToUpper(clusterHealth), "ERR") || strings.EqualFold(clusterState, "Error") {
		return &PlanError{Code: "CEPH_HEALTH_ERR", Message: "Ceph HEALTH_ERR blocks pool creation"}
	}
	if !strings.EqualFold(clusterState, "Ready") && !strings.EqualFold(clusterState, "Created") {
		return &PlanError{Code: "CEPH_CLUSTER_NOT_READY", Message: "CephCluster is not ready for pool creation"}
	}
	if strings.Contains(strings.ToUpper(clusterHealth), "WARN") {
		plan.Warnings = append(plan.Warnings, "Ceph reports HEALTH_WARN. Health is rechecked immediately before applying the pool.")
	}
	plan.Dependencies = append(plan.Dependencies, ResourceTarget{APIVersion: "ceph.rook.io/v1", Kind: "CephCluster", Namespace: p.rookNamespace, Name: cluster.GetName(), UID: string(cluster.GetUID()), ResourceVersion: cluster.GetResourceVersion()})
	replicas := intParameter(request.Parameters, "replicatedSize", 3)
	if replicas < 2 || replicas > 9 {
		return invalid("parameters.replicatedSize", "replicatedSize must be between 2 and 9")
	}
	failureDomain := allowedString(request.Parameters, "failureDomain", []string{"host", "rack", "zone"}, "host")
	if err := validateAllowed(request.Parameters, "failureDomain", []string{"host", "rack", "zone"}); err != nil {
		return err
	}
	compression := allowedString(request.Parameters, "compressionMode", []string{"none", "passive", "aggressive", "force"}, "none")
	if err := validateAllowed(request.Parameters, "compressionMode", []string{"none", "passive", "aggressive", "force"}); err != nil {
		return err
	}
	if stringParameter(request.Parameters, "deviceClass") != "" {
		if err := validateAllowed(request.Parameters, "deviceClass", []string{"hdd", "ssd", "nvme"}); err != nil {
			return err
		}
	}
	nodes, nodeErr := p.core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if nodeErr != nil {
		return &PlanError{Code: "FAILURE_DOMAIN_CHECK_FAILED", Message: "available failure domains could not be inspected", Retryable: true}
	}
	domains := map[string]bool{}
	for _, node := range nodes.Items {
		value := node.Name
		if failureDomain == "zone" {
			value = node.Labels["topology.kubernetes.io/zone"]
		}
		if failureDomain == "rack" {
			value = node.Labels["topology.rook.io/rack"]
		}
		if value != "" {
			domains[value] = true
		}
	}
	if len(domains) < replicas {
		return &PlanError{Code: "INSUFFICIENT_FAILURE_DOMAINS", Message: fmt.Sprintf("replica size %d requires at least %d distinct %s failure domains; found %d", replicas, replicas, failureDomain, len(domains))}
	}
	manifest := map[string]any{"apiVersion": "ceph.rook.io/v1", "kind": "CephBlockPool", "metadata": map[string]any{"name": request.Target.Name, "namespace": namespace, "labels": map[string]any{"app.kubernetes.io/managed-by": "highland"}}, "spec": map[string]any{"replicated": map[string]any{"size": replicas, "requireSafeReplicaSize": true}, "failureDomain": failureDomain, "parameters": map[string]any{"compression_mode": compression}}}
	if deviceClass := stringParameter(request.Parameters, "deviceClass"); deviceClass != "" {
		manifest["spec"].(map[string]any)["deviceClass"] = deviceClass
	}
	plan.Resources = []PlannedResource{{APIVersion: "ceph.rook.io/v1", Kind: "CephBlockPool", Namespace: namespace, Name: request.Target.Name, Operation: "server-side-apply", Manifest: manifest}}
	plan.Checks = append(plan.Checks, Check{ID: "replica-safety", Status: "pass", Message: "safe replica size is required and no redundancy downgrade is permitted"})
	return nil
}

func (p *Planner) planLonghorn(ctx context.Context, plan *Plan, request Request) error {
	if p.longhorn == nil {
		return &PlanError{Code: "LONGHORN_UNAVAILABLE", Message: "the Longhorn manager integration is unavailable", Retryable: true}
	}
	plan.ProviderID = "longhorn"
	plan.BlastRadius = "one Longhorn resource"
	switch request.ActionID {
	case "longhorn-backup-target-configure":
		return p.planLonghornBackupTarget(ctx, plan, request)
	case "longhorn-backup-delete":
		return p.planLonghornBackupDelete(ctx, plan, request)
	case "longhorn-backup-restore":
		return p.planLonghornBackupRestore(ctx, plan, request)
	default:
		return p.planLonghornVolume(ctx, plan, request)
	}
}

func (p *Planner) planLonghornVolume(ctx context.Context, plan *Plan, request Request) error {
	volume, err := p.longhorn.Get(ctx, "volumes", request.Target.Name)
	if err != nil {
		return classifyLonghornReadError(err, "LONGHORN_VOLUME_NOT_FOUND", "Longhorn volume was not found", "Longhorn volume could not be inspected")
	}
	actionName := map[string]string{
		"longhorn-volume-attach":        "attach",
		"longhorn-volume-detach":        "detach",
		"longhorn-volume-replica-count": "updateReplicaCount",
		"longhorn-volume-backup":        "snapshotBackup",
		"longhorn-recurring-job-add":    "recurringJobAdd",
		"longhorn-recurring-job-remove": "recurringJobDelete",
		"longhorn-volume-salvage":       "salvage",
		"longhorn-engine-upgrade":       "engineUpgrade",
	}[request.ActionID]
	if actionName == "" || !longhornHasAction(volume, actionName) {
		return &PlanError{Code: "LONGHORN_ACTION_UNAVAILABLE", Message: actionName + " is not currently available for this Longhorn volume"}
	}
	plan.Target.UID = firstNonemptyString(longhornString(volume, "id"), request.Target.Name)
	plan.Target.ResourceVersion = longhornResourceVersion(volume)
	state := strings.ToLower(longhornString(volume, "state"))
	robustness := strings.ToLower(longhornString(volume, "robustness"))
	parameters := map[string]any{}
	switch request.ActionID {
	case "longhorn-volume-attach":
		if state != "detached" {
			return &PlanError{Code: "INVALID_VOLUME_STATE", Message: "Longhorn volume must be detached before attachment"}
		}
		hostID := stringParameter(request.Parameters, "hostId")
		if hostID == "" {
			return invalid("parameters.hostId", "target Longhorn node is required")
		}
		nodes, listErr := p.longhorn.List(ctx, "nodes")
		if listErr != nil {
			return &PlanError{Code: "LONGHORN_DEPENDENCY_CHECK_FAILED", Message: "Longhorn nodes could not be inspected", Retryable: true}
		}
		found := false
		for _, node := range nodes {
			if longhornString(node, "name") == hostID || longhornString(node, "id") == hostID {
				found = true
				plan.Dependencies = append(plan.Dependencies, ResourceTarget{Kind: "LonghornNode", Name: hostID})
				break
			}
		}
		if !found {
			return &PlanError{Code: "LONGHORN_NODE_NOT_FOUND", Message: "selected Longhorn node was not found"}
		}
		parameters = map[string]any{"hostId": hostID, "disableFrontend": false, "attachedBy": "", "attacherType": "", "attachmentID": ""}
		plan.Checks = append(plan.Checks, Check{ID: "target-node", Status: "pass", Message: "target Longhorn node exists"})
	case "longhorn-volume-detach":
		if state == "detached" {
			return &PlanError{Code: "INVALID_VOLUME_STATE", Message: "Longhorn volume is already detached"}
		}
		force := boolParameter(request.Parameters, "force", false)
		parameters = map[string]any{"forceDetach": force, "hostId": "", "attachmentID": ""}
		if force {
			plan.Warnings = append(plan.Warnings, "Forced detach can interrupt active I/O and must only be used after workload ownership is understood.")
		}
		appendLonghornWorkloadEvidence(plan, volume)
		plan.BlastRadius = "one attached Longhorn volume and its listed Kubernetes workloads"
	case "longhorn-volume-replica-count":
		replicaCount := intParameter(request.Parameters, "replicaCount", 0)
		if replicaCount < 1 || replicaCount > 20 {
			return invalid("parameters.replicaCount", "replicaCount must be between 1 and 20")
		}
		current := longhornInt(volume, "numberOfReplicas")
		if current == replicaCount {
			return &PlanError{Code: "NO_CHANGE", Message: "requested replica count already matches the volume"}
		}
		if replicaCount < current {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Replica count will decrease from %d to %d, reducing redundancy.", current, replicaCount))
		}
		parameters = map[string]any{"replicaCount": replicaCount}
		plan.Resources = []PlannedResource{longhornPlannedResource("LonghornVolume", request.Target.Name, "longhorn-action", "volumes", actionName, parameters, map[string]any{"numberOfReplicas": replicaCount})}
		plan.Checks = append(plan.Checks, Check{ID: "replica-safety", Status: "pass", Message: fmt.Sprintf("replica count changes from %d to %d", current, replicaCount)})
	case "longhorn-volume-backup":
		snapshotName := stringParameter(request.Parameters, "snapshotName")
		if snapshotName == "" {
			return invalid("parameters.snapshotName", "snapshotName is required")
		}
		snapshots, actionErr := p.longhorn.Action(ctx, "volumes", request.Target.Name, firstAvailableLonghornAction(volume, "snapshotList", "snapshotCRList"), map[string]any{})
		if actionErr != nil {
			return &PlanError{Code: "LONGHORN_SNAPSHOT_CHECK_FAILED", Message: "Longhorn snapshots could not be inspected", Retryable: true}
		}
		if !longhornCollectionContains(snapshots, snapshotName) {
			return &PlanError{Code: "LONGHORN_SNAPSHOT_NOT_FOUND", Message: "selected Longhorn snapshot was not found"}
		}
		targetName := stringParameter(request.Parameters, "backupTargetName")
		if targetName != "" {
			if _, targetErr := p.longhorn.Get(ctx, "backuptargets", targetName); targetErr != nil {
				return classifyLonghornReadError(targetErr, "LONGHORN_BACKUP_TARGET_NOT_FOUND", "Longhorn backup target was not found", "Longhorn backup target could not be inspected")
			}
			plan.Dependencies = append(plan.Dependencies, ResourceTarget{Kind: "LonghornBackupTarget", Name: targetName})
		}
		parameters = map[string]any{"name": snapshotName, "backupTargetName": targetName, "backupMode": allowedString(request.Parameters, "backupMode", []string{"full", "incremental"}, "incremental"), "labels": map[string]any{}}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{Kind: "LonghornSnapshot", Name: snapshotName})
	case "longhorn-recurring-job-add", "longhorn-recurring-job-remove":
		jobName := stringParameter(request.Parameters, "jobName")
		if jobName == "" {
			return invalid("parameters.jobName", "jobName is required")
		}
		if _, jobErr := p.longhorn.Get(ctx, "recurringjobs", jobName); jobErr != nil {
			return classifyLonghornReadError(jobErr, "LONGHORN_RECURRING_JOB_NOT_FOUND", "Longhorn recurring job was not found", "Longhorn recurring job could not be inspected")
		}
		parameters = map[string]any{"name": jobName, "isGroup": false}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{Kind: "LonghornRecurringJob", Name: jobName})
		if request.ActionID == "longhorn-recurring-job-remove" {
			plan.Warnings = append(plan.Warnings, "Removing this assignment stops future scheduled executions of the recurring job for this volume.")
		}
	case "longhorn-volume-salvage":
		if robustness != "faulted" && state != "faulted" {
			return &PlanError{Code: "INVALID_VOLUME_STATE", Message: "salvage is only allowed for a faulted Longhorn volume"}
		}
		replicas, replicaErr := stringListParameter(request.Parameters, "replicas")
		if replicaErr != nil || len(replicas) == 0 {
			return invalid("parameters.replicas", "at least one salvage replica is required")
		}
		available := longhornReplicaNames(volume)
		for _, replica := range replicas {
			if !available[replica] {
				return &PlanError{Code: "LONGHORN_REPLICA_NOT_FOUND", Message: "selected salvage replica does not belong to the volume", Details: map[string]any{"replica": replica}}
			}
		}
		parameters = map[string]any{"names": stringSliceAny(replicas)}
		plan.Warnings = append(plan.Warnings, "Salvage selects surviving replica data as authoritative and may discard newer data from other replicas.")
		plan.BlastRadius = "one faulted Longhorn volume and all of its replica data"
	case "longhorn-engine-upgrade":
		image := stringParameter(request.Parameters, "image")
		if image == "" {
			return invalid("parameters.image", "target engine image is required")
		}
		images, listErr := p.longhorn.List(ctx, "engineimages")
		if listErr != nil {
			return &PlanError{Code: "LONGHORN_ENGINE_IMAGE_CHECK_FAILED", Message: "Longhorn engine images could not be inspected", Retryable: true}
		}
		found := false
		for _, candidate := range images {
			if longhornString(candidate, "image") == image || longhornString(candidate, "name") == image {
				state := strings.ToLower(longhornString(candidate, "state"))
				if state != "" && state != "deployed" && state != "ready" {
					return &PlanError{Code: "LONGHORN_ENGINE_IMAGE_NOT_READY", Message: "selected Longhorn engine image is not deployed"}
				}
				found = true
				break
			}
		}
		if !found {
			return &PlanError{Code: "LONGHORN_ENGINE_IMAGE_NOT_FOUND", Message: "selected Longhorn engine image was not found"}
		}
		parameters = map[string]any{"image": image}
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{Kind: "LonghornEngineImage", Name: image})
		plan.Warnings = append(plan.Warnings, "Engine upgrades can temporarily interrupt volume availability and cannot always be downgraded safely.")
	}
	if len(plan.Resources) == 0 {
		plan.Resources = []PlannedResource{longhornPlannedResource("LonghornVolume", request.Target.Name, "longhorn-action", "volumes", actionName, parameters, nil)}
	}
	plan.Checks = append(plan.Checks,
		Check{ID: "provider-health", Status: "pass", Message: "Longhorn manager returned current volume state"},
		Check{ID: "volume-state", Status: "pass", Message: "volume state permits the requested workflow"},
		Check{ID: "action-available", Status: "pass", Message: "Longhorn currently advertises the requested action"},
	)
	return nil
}

func (p *Planner) planLonghornBackupTarget(ctx context.Context, plan *Plan, request Request) error {
	rawURL := stringParameter(request.Parameters, "url")
	if rawURL == "" {
		return invalid("parameters.url", "backup target URL is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || !map[string]bool{"s3": true, "nfs": true, "cifs": true, "azblob": true, "vfs": true}[strings.ToLower(parsed.Scheme)] {
		return invalid("parameters.url", "backup target URL must use s3, nfs, cifs, azblob, or vfs")
	}
	body := map[string]any{
		"name": request.Target.Name, "backupTargetURL": rawURL,
		"credentialSecret": stringParameter(request.Parameters, "credentialSecret"),
		"pollInterval":     firstNonemptyString(stringParameter(request.Parameters, "pollInterval"), "5m"),
	}
	existing, getErr := p.longhorn.Get(ctx, "backuptargets", request.Target.Name)
	operation := "longhorn-create"
	action := ""
	if getErr == nil {
		if !longhornHasAction(existing, "backupTargetUpdate") {
			return &PlanError{Code: "LONGHORN_ACTION_UNAVAILABLE", Message: "backup target update is not currently available"}
		}
		operation = "longhorn-action"
		action = "backupTargetUpdate"
		plan.Target.UID = firstNonemptyString(longhornString(existing, "id"), request.Target.Name)
		plan.Target.ResourceVersion = longhornResourceVersion(existing)
		plan.Warnings = append(plan.Warnings, "The existing backup target configuration will be replaced after confirmation.")
	} else if !isLonghornNotFound(getErr) {
		return &PlanError{Code: "LONGHORN_DEPENDENCY_CHECK_FAILED", Message: "existing backup target could not be inspected", Retryable: true}
	} else {
		plan.Target.UID = request.Target.Name
		plan.Target.ResourceVersion = "absent"
	}
	plan.Resources = []PlannedResource{longhornPlannedResource("LonghornBackupTarget", request.Target.Name, operation, "backuptargets", action, body, map[string]any{"backupTargetURL": rawURL})}
	plan.Checks = append(plan.Checks,
		Check{ID: "provider-health", Status: "pass", Message: "Longhorn manager is reachable"},
		Check{ID: "backup-url", Status: "pass", Message: "backup target URL uses a supported scheme"},
		Check{ID: "credential-reference", Status: "pass", Message: "only a Kubernetes credential Secret reference is stored"},
	)
	plan.BlastRadius = "one Longhorn backup target configuration"
	return nil
}

func (p *Planner) planLonghornBackupDelete(ctx context.Context, plan *Plan, request Request) error {
	backupVolume := stringParameter(request.Parameters, "backupVolume")
	if backupVolume == "" {
		return invalid("parameters.backupVolume", "backupVolume is required")
	}
	resource, err := p.longhorn.Get(ctx, "backupvolumes", backupVolume)
	if err != nil {
		return classifyLonghornReadError(err, "LONGHORN_BACKUP_VOLUME_NOT_FOUND", "Longhorn backup volume was not found", "Longhorn backup volume could not be inspected")
	}
	if !longhornHasAction(resource, "backupDelete") {
		return &PlanError{Code: "LONGHORN_ACTION_UNAVAILABLE", Message: "backup deletion is not currently available for this backup volume"}
	}
	backups, listErr := p.longhorn.Action(ctx, "backupvolumes", backupVolume, "backupList", map[string]any{})
	if listErr != nil {
		return &PlanError{Code: "LONGHORN_BACKUP_CHECK_FAILED", Message: "Longhorn backups could not be inspected", Retryable: true}
	}
	if !longhornCollectionContains(backups, request.Target.Name) {
		return &PlanError{Code: "LONGHORN_BACKUP_NOT_FOUND", Message: "selected Longhorn backup was not found"}
	}
	parameters := map[string]any{"name": request.Target.Name}
	plan.Target.UID = backupVolume + "/" + request.Target.Name
	plan.Target.ResourceVersion = longhornResourceVersion(backups)
	plan.Resources = []PlannedResource{longhornPlannedResource("LonghornBackup", backupVolume, "longhorn-action", "backupvolumes", "backupDelete", parameters, map[string]any{"backupAbsent": request.Target.Name})}
	plan.Dependencies = append(plan.Dependencies, ResourceTarget{Kind: "LonghornBackupVolume", Name: backupVolume})
	plan.Warnings = append(plan.Warnings, "Deleting a Longhorn-native backup permanently removes that recovery point from the backup target.")
	plan.BlastRadius = "one Longhorn-native backup recovery point"
	return nil
}

func (p *Planner) planLonghornBackupRestore(ctx context.Context, plan *Plan, request Request) error {
	backupVolume := stringParameter(request.Parameters, "backupVolume")
	backupName := stringParameter(request.Parameters, "backupName")
	if backupVolume == "" || backupName == "" {
		return invalid("parameters", "backupVolume and backupName are required")
	}
	if _, err := p.longhorn.Get(ctx, "volumes", request.Target.Name); err == nil {
		return &PlanError{Code: "ALREADY_EXISTS", Message: "a Longhorn volume with the restore target name already exists"}
	} else if !isLonghornNotFound(err) {
		return &PlanError{Code: "LONGHORN_DEPENDENCY_CHECK_FAILED", Message: "restore target name could not be checked", Retryable: true}
	}
	resource, err := p.longhorn.Get(ctx, "backupvolumes", backupVolume)
	if err != nil {
		return classifyLonghornReadError(err, "LONGHORN_BACKUP_VOLUME_NOT_FOUND", "Longhorn backup volume was not found", "Longhorn backup volume could not be inspected")
	}
	backups, listErr := p.longhorn.Action(ctx, "backupvolumes", backupVolume, "backupList", map[string]any{})
	if listErr != nil {
		return &PlanError{Code: "LONGHORN_BACKUP_CHECK_FAILED", Message: "Longhorn backups could not be inspected", Retryable: true}
	}
	if !longhornCollectionContains(backups, backupName) {
		return &PlanError{Code: "LONGHORN_BACKUP_NOT_FOUND", Message: "selected Longhorn backup was not found"}
	}
	replicas := intParameter(request.Parameters, "replicaCount", 3)
	if replicas < 1 || replicas > 20 {
		return invalid("parameters.replicaCount", "replicaCount must be between 1 and 20")
	}
	body := map[string]any{
		"name": request.Target.Name, "size": firstNonemptyString(stringParameter(request.Parameters, "size"), longhornString(resource, "size")),
		"numberOfReplicas": replicas, "frontend": "blockdev", "standby": boolParameter(request.Parameters, "standby", false),
		"fromBackup": "backup://" + backupVolume + "/" + backupName,
	}
	if body["standby"] == true {
		body["frontend"] = ""
	}
	plan.Target.UID = request.Target.Name
	plan.Target.ResourceVersion = "absent"
	plan.Resources = []PlannedResource{longhornPlannedResource("LonghornVolume", request.Target.Name, "longhorn-create", "volumes", "", body, map[string]any{"fromBackup": body["fromBackup"]})}
	plan.Dependencies = append(plan.Dependencies,
		ResourceTarget{Kind: "LonghornBackupVolume", Name: backupVolume},
		ResourceTarget{Kind: "LonghornBackup", Name: backupName},
	)
	plan.Warnings = append(plan.Warnings, "Restore creates a new Longhorn volume and begins asynchronous data recovery from the selected backup.")
	plan.BlastRadius = "one new Longhorn volume restored from one backup"
	return nil
}

func longhornPlannedResource(kind, name, operation, collection, action string, parameters, expected map[string]any) PlannedResource {
	manifest := map[string]any{"collection": collection}
	if action != "" {
		manifest["action"] = action
	}
	if parameters != nil {
		manifest["parameters"] = parameters
	}
	if expected != nil {
		manifest["expected"] = expected
	}
	return PlannedResource{APIVersion: "longhorn.io/v1", Kind: kind, Name: name, Operation: operation, Manifest: manifest}
}

func appendLonghornWorkloadEvidence(plan *Plan, volume map[string]any) {
	status, _ := volume["kubernetesStatus"].(map[string]any)
	workloads, _ := status["workloadsStatus"].([]any)
	for _, raw := range workloads {
		workload, _ := raw.(map[string]any)
		name := firstNonemptyString(longhornString(workload, "podName"), longhornString(workload, "workloadName"))
		if name == "" {
			continue
		}
		namespace := longhornString(status, "namespace")
		plan.Dependencies = append(plan.Dependencies, ResourceTarget{Kind: "Pod", Namespace: namespace, Name: name})
	}
	if len(plan.Dependencies) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d Kubernetes workload references are reported by Longhorn and may lose access during detach.", len(plan.Dependencies)))
	}
}

func firstAvailableLonghornAction(resource map[string]any, names ...string) string {
	for _, name := range names {
		if longhornHasAction(resource, name) {
			return name
		}
	}
	return names[0]
}

func longhornCollectionContains(response map[string]any, name string) bool {
	data, _ := response["data"].([]any)
	for _, raw := range data {
		item, _ := raw.(map[string]any)
		if firstNonemptyString(longhornString(item, "name"), longhornString(item, "backupName"), longhornString(item, "id")) == name {
			return true
		}
	}
	return false
}

func longhornReplicaNames(volume map[string]any) map[string]bool {
	result := map[string]bool{}
	replicas, _ := volume["replicas"].([]any)
	for _, raw := range replicas {
		replica, _ := raw.(map[string]any)
		if name := longhornString(replica, "name"); name != "" {
			result[name] = true
		}
	}
	return result
}

func stringListParameter(parameters map[string]any, key string) ([]string, error) {
	raw, ok := parameters[key].([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s must contain nonempty strings", key)
		}
		result = append(result, strings.TrimSpace(value))
	}
	return result, nil
}

func stringSliceAny(values []string) []any {
	result := make([]any, len(values))
	for index := range values {
		result[index] = values[index]
	}
	return result
}

func longhornResourceVersion(resource any) string {
	return hashValue(resource)
}

func classifyLonghornReadError(err error, notFoundCode, notFoundMessage, dependencyMessage string) error {
	if isLonghornNotFound(err) {
		return &PlanError{Code: notFoundCode, Message: notFoundMessage}
	}
	return &PlanError{Code: "LONGHORN_DEPENDENCY_CHECK_FAILED", Message: dependencyMessage, Retryable: true}
}

func isLonghornNotFound(err error) bool {
	var responseErr *LonghornHTTPError
	return errors.As(err, &responseErr) && responseErr.Status == 404
}

func firstNonemptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (p *Planner) rookProvisioner(actionID string, parameters map[string]any) (string, map[string]any) {
	result := map[string]any{"clusterID": p.rookNamespace}
	if actionID == "create-ceph-rbd-storageclass" {
		result["pool"] = stringParameter(parameters, "pool")
		result["imageFeatures"] = allowedString(parameters, "imageFeatures", []string{"layering", "layering,fast-diff,object-map,deep-flatten,exclusive-lock"}, "layering")
		if filesystem := stringParameter(parameters, "filesystemType"); filesystem != "" {
			result["csi.storage.k8s.io/fstype"] = filesystem
		}
		if boolParameter(parameters, "encrypted", false) {
			result["encrypted"] = "true"
		}
		result["csi.storage.k8s.io/provisioner-secret-name"] = "rook-csi-rbd-provisioner"
		result["csi.storage.k8s.io/provisioner-secret-namespace"] = p.rookNamespace
		result["csi.storage.k8s.io/node-stage-secret-name"] = "rook-csi-rbd-node"
		result["csi.storage.k8s.io/node-stage-secret-namespace"] = p.rookNamespace
		return p.rookNamespace + ".rbd.csi.ceph.com", result
	}
	result["fsName"] = stringParameter(parameters, "filesystem")
	result["pool"] = stringParameter(parameters, "pool")
	if group := stringParameter(parameters, "subvolumeGroup"); group != "" {
		result["subvolumeGroup"] = group
	}
	result["csi.storage.k8s.io/provisioner-secret-name"] = "rook-csi-cephfs-provisioner"
	result["csi.storage.k8s.io/provisioner-secret-namespace"] = p.rookNamespace
	result["csi.storage.k8s.io/node-stage-secret-name"] = "rook-csi-cephfs-node"
	result["csi.storage.k8s.io/node-stage-secret-namespace"] = p.rookNamespace
	return p.rookNamespace + ".cephfs.csi.ceph.com", result
}

type challengePayload struct {
	Requester     string         `json:"sub"`
	ActionID      string         `json:"action"`
	ProviderID    string         `json:"provider,omitempty"`
	Target        ResourceTarget `json:"target"`
	PlanHash      string         `json:"planHash"`
	PolicyVersion string         `json:"policyVersion,omitempty"`
	Expires       int64          `json:"exp"`
}

func (p *Planner) sign(payload challengePayload) string {
	encoded, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, p.secret)
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (p *Planner) verifyToken(token string) (challengePayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return challengePayload{}, &PlanError{Code: "CONFIRMATION_INVALID", Message: "confirmation challenge is invalid"}
	}
	mac := hmac.New(sha256.New, p.secret)
	_, _ = mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return challengePayload{}, &PlanError{Code: "CONFIRMATION_INVALID", Message: "confirmation challenge is invalid"}
	}
	encoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return challengePayload{}, &PlanError{Code: "CONFIRMATION_INVALID", Message: "confirmation challenge is invalid"}
	}
	var payload challengePayload
	if json.Unmarshal(encoded, &payload) != nil {
		return challengePayload{}, &PlanError{Code: "CONFIRMATION_INVALID", Message: "confirmation challenge is invalid"}
	}
	return payload, nil
}
func hashValue(value any) string {
	encoded, _ := json.Marshal(value)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
func invalid(field, message string) error {
	return &PlanError{Code: "INVALID_PARAMETER", Message: message, Details: map[string]any{"field": field}}
}
func classifyReadError(err error, notFoundCode, notFoundMessage, dependencyMessage string) error {
	if apierrors.IsNotFound(err) {
		return &PlanError{Code: notFoundCode, Message: notFoundMessage}
	}
	return classifyDependencyError(err, dependencyMessage)
}
func classifyDependencyError(err error, message string) error {
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return &PlanError{Code: "DEPENDENCY_PERMISSION_DENIED", Message: message + ": Kubernetes authorization denied the required read"}
	}
	return &PlanError{Code: "DEPENDENCY_CHECK_FAILED", Message: message, Retryable: true}
}
func stringParameter(parameters map[string]any, key string) string {
	value, _ := parameters[key].(string)
	return strings.TrimSpace(value)
}
func boolParameter(parameters map[string]any, key string, fallback bool) bool {
	value, ok := parameters[key].(bool)
	if !ok {
		return fallback
	}
	return value
}
func intParameter(parameters map[string]any, key string, fallback int) int {
	switch value := parameters[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	}
	return fallback
}
func allowedString(parameters map[string]any, key string, allowed []string, fallback string) string {
	value := stringParameter(parameters, key)
	if value == "" {
		return fallback
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return fallback
}
func validateAllowed(parameters map[string]any, key string, allowed []string) error {
	value := stringParameter(parameters, key)
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return invalid("parameters."+key, key+" is not an allowed value")
}

var actionParameterAllowlist = map[string]map[string]bool{
	"create-pvc":                       {"storageClass": true, "size": true, "accessModes": true, "volumeMode": true},
	"expand-pvc":                       {"size": true},
	"delete-pvc":                       {},
	"create-snapshot":                  {"sourceClaim": true, "snapshotClass": true},
	"delete-snapshot":                  {},
	"restore-snapshot":                 {"sourceSnapshot": true, "storageClass": true, "size": true, "accessModes": true, "volumeMode": true},
	"clone-pvc":                        {"sourceClaim": true, "storageClass": true, "size": true, "accessModes": true, "volumeMode": true},
	"create-ceph-rbd-storageclass":     {"pool": true, "reclaimPolicy": true, "volumeBindingMode": true, "allowVolumeExpansion": true, "default": true, "imageFeatures": true, "filesystemType": true, "encrypted": true, "mountOptions": true},
	"create-cephfs-storageclass":       {"filesystem": true, "pool": true, "subvolumeGroup": true, "reclaimPolicy": true, "volumeBindingMode": true, "allowVolumeExpansion": true, "default": true, "mountOptions": true},
	"delete-ceph-storageclass":         {},
	"create-ceph-blockpool":            {"replicatedSize": true, "failureDomain": true, "deviceClass": true, "compressionMode": true},
	"delete-ceph-blockpool":            {},
	"longhorn-volume-attach":           {"hostId": true},
	"longhorn-volume-detach":           {"force": true},
	"longhorn-volume-replica-count":    {"replicaCount": true},
	"longhorn-volume-backup":           {"snapshotName": true, "backupTargetName": true, "backupMode": true},
	"longhorn-recurring-job-add":       {"jobName": true},
	"longhorn-recurring-job-remove":    {"jobName": true},
	"longhorn-volume-salvage":          {"replicas": true},
	"longhorn-engine-upgrade":          {"image": true},
	"longhorn-backup-target-configure": {"url": true, "credentialSecret": true, "pollInterval": true},
	"longhorn-backup-delete":           {"backupVolume": true},
	"longhorn-backup-restore":          {"backupVolume": true, "backupName": true, "size": true, "replicaCount": true, "standby": true},
}

func validateActionParameters(actionID string, parameters map[string]any) error {
	allowed := actionParameterAllowlist[actionID]
	if len(parameters) > 32 {
		return invalid("parameters", "too many operation parameters")
	}
	for key, value := range parameters {
		if !allowed[key] {
			return invalid("parameters."+key, "parameter is not supported by this action")
		}
		switch typed := value.(type) {
		case string:
			if len(typed) > 2048 {
				return invalid("parameters."+key, "parameter exceeds the 2048 character limit")
			}
		case []any:
			if len(typed) > 16 {
				return invalid("parameters."+key, "parameter list exceeds 16 items")
			}
			for _, item := range typed {
				if text, ok := item.(string); !ok || len(text) > 256 {
					return invalid("parameters."+key, "parameter list must contain bounded strings")
				}
			}
		case bool, float64, int, json.Number:
		default:
			return invalid("parameters."+key, "nested or unsupported parameter values are not accepted")
		}
	}
	return nil
}

func validateBoolParameter(parameters map[string]any, key string) error {
	if value, exists := parameters[key]; exists {
		if _, ok := value.(bool); !ok {
			return invalid("parameters."+key, key+" must be a boolean")
		}
	}
	return nil
}

func allowedMountOptions(parameters map[string]any) ([]any, error) {
	raw, exists := parameters["mountOptions"]
	if !exists {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, invalid("parameters.mountOptions", "mountOptions must be an array")
	}
	allowed := map[string]bool{"discard": true, "noatime": true, "nodiratime": true}
	result := make([]any, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(string)
		if !ok || !allowed[item] {
			return nil, invalid("parameters.mountOptions", "mountOptions contains a value outside the safe allowlist")
		}
		result = append(result, item)
	}
	return result, nil
}
func rookObjectReady(object *unstructured.Unstructured) bool {
	if object == nil {
		return false
	}
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	if phase == "Ready" || phase == "Connected" || phase == "Created" {
		return true
	}
	state, _, _ := unstructured.NestedString(object.Object, "status", "state")
	if state == "Ready" || state == "Created" {
		return true
	}
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, raw := range conditions {
		if condition, ok := raw.(map[string]any); ok && fmt.Sprint(condition["type"]) == "Ready" && strings.EqualFold(fmt.Sprint(condition["status"]), "True") {
			return true
		}
	}
	return false
}
func accessModes(parameters map[string]any) ([]any, error) {
	raw, ok := parameters["accessModes"].([]any)
	if !ok || len(raw) == 0 {
		return []any{"ReadWriteOnce"}, nil
	}
	allowed := map[string]bool{"ReadWriteOnce": true, "ReadOnlyMany": true, "ReadWriteMany": true, "ReadWriteOncePod": true}
	result := make([]any, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || !allowed[value] {
			return nil, invalid("parameters.accessModes", "accessModes contains an unsupported value")
		}
		result = append(result, value)
	}
	return result, nil
}
func errorsIsNotFound(err error) bool { return errors.Is(err, storage.ErrNotFound) }
