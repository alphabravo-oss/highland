package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/highland-io/highland/apps/api/internal/audit"
)

type OperationObserver interface {
	OperationStarted(provider, action string)
	OperationFinished(provider, action, result string, duration time.Duration)
	OperationRetry(provider, reason string)
}

// ControllerStateObserver is optional so operation accounting implementations
// do not need to expose leader state. Prometheus uses it to distinguish an
// idle controller from a missing elected reconciler.
type ControllerStateObserver interface {
	OperationControllerLeader(bool)
}

type PostflightObserver interface {
	OperationPostflightMismatch(provider, kind string)
}

type Controller struct {
	core      kubernetes.Interface
	dynamic   dynamic.Interface
	store     *Store
	planner   *Planner
	namespace string
	observer  OperationObserver
	audit     *audit.Store
	now       func() time.Time
	active    sync.Map
}

func NewController(core kubernetes.Interface, dynamicClient dynamic.Interface, store *Store, planner *Planner, namespace string, observer OperationObserver, auditStore *audit.Store) (*Controller, error) {
	if core == nil || dynamicClient == nil || store == nil || planner == nil {
		return nil, fmt.Errorf("operation controller requires clients, store, and planner")
	}
	if namespace == "" {
		namespace = "highland-system"
	}
	return &Controller{core: core, dynamic: dynamicClient, store: store, planner: planner, namespace: namespace, observer: observer, audit: auditStore, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Controller) Start(ctx context.Context) {
	identity, _ := os.Hostname()
	if identity == "" {
		identity = fmt.Sprintf("highland-%d", time.Now().UnixNano())
	}
	lock := &resourcelock.LeaseLock{LeaseMeta: metav1.ObjectMeta{Name: "highland-storage-operations", Namespace: c.namespace}, Client: c.core.CoordinationV1(), LockConfig: resourcelock.ResourceLockConfig{Identity: identity}}
	go leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: lock, LeaseDuration: 30 * time.Second, RenewDeadline: 20 * time.Second, RetryPeriod: 5 * time.Second, ReleaseOnCancel: true, Name: "highland-storage-operations",
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				c.observeLeader(true)
				defer c.observeLeader(false)
				c.run(leaderCtx)
			},
			OnStoppedLeading: func() { c.observeLeader(false) },
		},
	})
}

func (c *Controller) run(ctx context.Context) {
	go c.garbageCollect(ctx)
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		operations, err := c.store.listBySelector(ctx, "")
		if err != nil {
			return
		}
		for index := range operations {
			if operations[index].Status.Phase == "Pending" || operations[index].Status.Phase == "Running" {
				_ = c.Reconcile(ctx, &operations[index])
			}
		}
	}, 2*time.Second)
}

func (c *Controller) garbageCollect(ctx context.Context) {
	collect := func() {
		if c.audit == nil || !c.audit.Durable() {
			// The StorageOperation CR is the durable audit record when no writable
			// append-only audit volume exists. Retain it instead of silently trading
			// durability for object-count bounds.
			return
		}
		terminalAudit, err := c.audit.DurableTerminalOperationIDs()
		if err != nil {
			return
		}
		_, _ = c.store.DeleteTerminalBeforeWhere(ctx, c.now().Add(-30*24*time.Hour), func(operation Operation) bool {
			return terminalAudit[operation.Name]
		})
	}
	// Run once on leadership acquisition so long-lived installations do not
	// need to wait another day after a failover, then retain the daily bound.
	collect()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}

func (c *Controller) Reconcile(ctx context.Context, operation *Operation) error {
	if operation == nil {
		return nil
	}
	action, actionKnown := ActionByID(operation.Spec.ActionID)
	if !actionKnown {
		return c.fail(ctx, operation, "ACTION_NOT_SUPPORTED", "stored operation references an unsupported action")
	}
	if len(operation.Spec.Resources) != 1 {
		return c.fail(ctx, operation, "INVALID_OPERATION_SPEC", "stored operation must contain exactly one planned resource")
	}
	plan := Plan{Action: action, ProviderID: operation.Spec.ProviderID, Target: operation.Spec.Target, Resources: operation.Spec.Resources, Dependencies: operation.Spec.Dependencies, Hash: operation.Spec.PlanHash}
	if operation.Status.Phase == "Running" {
		c.observeStarted(operation)
	}
	readyToExecute := false
	if operation.Status.Phase == "Pending" {
		freshPlan, preflightErr := c.planner.Plan(ctx, operation.Spec.Requester, Request{ActionID: operation.Spec.ActionID, ProviderID: operation.Spec.ProviderID, Target: operation.Spec.Target, Parameters: operation.Spec.Parameters})
		if preflightErr != nil {
			if retryable(preflightErr) && operation.Status.Retries < 8 {
				return c.retry(ctx, operation, "PreflightRetrying", preflightErr)
			}
			return c.fail(ctx, operation, codeOf(preflightErr, "PREFLIGHT_FAILED"), preflightErr.Error())
		}
		if freshPlan.Hash != operation.Spec.PlanHash {
			return c.fail(ctx, operation, "STALE_PREFLIGHT", "dependencies or resource versions changed after approval")
		}
		plan = freshPlan
		now := c.now()
		operation.Status = Status{Phase: "Running", Step: "PreflightComplete", StartedAt: &now, LastAttemptAt: &now}
		updated, updateErr := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
		if updateErr != nil {
			return updateErr
		}
		operation = updated
		c.observeStarted(operation)
		c.auditOperation(operation, "storage_operation_execution_started", "ok", "reconciliation started")
		readyToExecute = true
	}

	if operation.Status.Phase == "Running" && operation.Status.Step != "WaitingForReconciliation" {
		if !readyToExecute {
			observed, done, failed, message, result, observeErr := c.mutationObservation(ctx, operation, plan)
			if observeErr != nil {
				if retryable(observeErr) && operation.Status.Retries < 8 {
					operation.Status.Retries++
					operation.Status.Diagnostics = sanitize(observeErr.Error())
					now := c.now()
					operation.Status.LastAttemptAt = &now
					_, err := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
					return err
				}
				return c.fail(ctx, operation, codeOf(observeErr, "RECOVERY_CHECK_FAILED"), observeErr.Error())
			}
			if failed {
				c.observePostflightMismatch(operation)
				return c.fail(ctx, operation, "RESOURCE_RECONCILIATION_FAILED", message)
			}
			if done {
				operation.Status.Result = result
				return c.succeed(ctx, operation, message)
			}
			if observed {
				operation.Status.Result = result
				operation.Status.Step = "WaitingForReconciliation"
				_, err := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
				return err
			}
			// No mutation is visible after takeover or a retryable error. Re-run
			// every authoritative check before attempting the idempotent write.
			freshPlan, preflightErr := c.planner.Plan(ctx, operation.Spec.Requester, Request{ActionID: operation.Spec.ActionID, ProviderID: operation.Spec.ProviderID, Target: operation.Spec.Target, Parameters: operation.Spec.Parameters})
			if preflightErr != nil {
				if retryable(preflightErr) && operation.Status.Retries < 8 {
					return c.retry(ctx, operation, "PreflightRetrying", preflightErr)
				}
				return c.fail(ctx, operation, codeOf(preflightErr, "PREFLIGHT_FAILED"), preflightErr.Error())
			}
			if freshPlan.Hash != operation.Spec.PlanHash {
				return c.fail(ctx, operation, "STALE_PREFLIGHT", "dependencies or resource versions changed after approval")
			}
			plan = freshPlan
		}
		result, pending, executeErr := c.execute(ctx, operation, plan)
		if executeErr != nil {
			if retryable(executeErr) && operation.Status.Retries < 8 {
				operation.Status.Retries++
				operation.Status.Step = "Retrying"
				operation.Status.Diagnostics = sanitize(executeErr.Error())
				now := c.now()
				operation.Status.LastAttemptAt = &now
				_, err := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
				if c.observer != nil {
					c.observer.OperationRetry(nonempty(operation.Spec.ProviderID, "kubernetes"), retryReason(executeErr))
				}
				return err
			}
			return c.fail(ctx, operation, codeOf(executeErr, "EXECUTION_FAILED"), executeErr.Error())
		}
		operation.Status.Result = result
		if !pending {
			return c.succeed(ctx, operation, "Applied")
		}
		operation.Status.Step = "WaitingForReconciliation"
		_, updateErr := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
		return updateErr
	}

	if operation.Status.StartedAt != nil && c.now().Sub(*operation.Status.StartedAt) > 30*time.Minute {
		return c.fail(ctx, operation, "OPERATION_TIMEOUT", "storage resource did not reach its expected state before timeout")
	}
	done, failed, message, inspectErr := c.inspect(ctx, operation, plan)
	if inspectErr != nil {
		if retryable(inspectErr) {
			operation.Status.Retries++
			operation.Status.Diagnostics = sanitize(inspectErr.Error())
			_, updateErr := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
			return updateErr
		}
		return c.fail(ctx, operation, "POSTFLIGHT_FAILED", inspectErr.Error())
	}
	if failed {
		c.observePostflightMismatch(operation)
		return c.fail(ctx, operation, "RESOURCE_RECONCILIATION_FAILED", message)
	}
	if done {
		return c.succeed(ctx, operation, message)
	}
	return nil
}

func (c *Controller) observePostflightMismatch(operation *Operation) {
	if observer, ok := c.observer.(PostflightObserver); ok && operation != nil {
		observer.OperationPostflightMismatch(nonempty(operation.Spec.ProviderID, "kubernetes"), operation.Spec.Target.Kind)
	}
}

// mutationObservation distinguishes a mutation that completed before a leader
// crash from one that was never sent. It only adopts created objects carrying
// Highland's ownership label and never adopts a recreated update/delete target
// with a different UID.
func (c *Controller) mutationObservation(ctx context.Context, operation *Operation, plan Plan) (observed, done, failed bool, message string, result *ResultReference, err error) {
	if len(plan.Resources) != 1 {
		return false, false, false, "", nil, fmt.Errorf("operation plan must contain exactly one mutable resource")
	}
	resourcePlan := plan.Resources[0]
	gvr, namespaced, err := gvrFor(resourcePlan.APIVersion, resourcePlan.Kind)
	if err != nil {
		return false, false, false, "", nil, err
	}
	resourceClient := c.dynamic.Resource(gvr)
	var object *unstructured.Unstructured
	if namespaced {
		object, err = resourceClient.Namespace(resourcePlan.Namespace).Get(ctx, resourcePlan.Name, metav1.GetOptions{})
	} else {
		object, err = resourceClient.Get(ctx, resourcePlan.Name, metav1.GetOptions{})
	}
	if resourcePlan.Operation == "delete" {
		if apierrors.IsNotFound(err) {
			done, failed, message, err = c.verifyDeletedBackend(ctx, resourcePlan)
			return true, done, failed, message, &ResultReference{APIVersion: resourcePlan.APIVersion, Kind: resourcePlan.Kind, Namespace: resourcePlan.Namespace, Name: resourcePlan.Name, UID: plan.Target.UID}, err
		}
		if err != nil {
			return false, false, false, "", nil, err
		}
		if plan.Target.UID != "" && string(object.GetUID()) != plan.Target.UID {
			done, failed, message, err = c.verifyDeletedBackend(ctx, resourcePlan)
			return true, done, failed, message, &ResultReference{APIVersion: resourcePlan.APIVersion, Kind: resourcePlan.Kind, Namespace: resourcePlan.Namespace, Name: resourcePlan.Name, UID: plan.Target.UID}, err
		}
		if object.GetDeletionTimestamp() == nil {
			return false, false, false, "", nil, nil
		}
		return true, false, false, "WaitingForDeletion", &ResultReference{APIVersion: resourcePlan.APIVersion, Kind: resourcePlan.Kind, Namespace: resourcePlan.Namespace, Name: resourcePlan.Name, UID: plan.Target.UID}, nil
	}
	if apierrors.IsNotFound(err) {
		return false, false, false, "", nil, nil
	}
	if err != nil {
		return false, false, false, "", nil, err
	}
	if plan.Target.UID != "" && string(object.GetUID()) != plan.Target.UID {
		return false, false, false, "", nil, nil
	}
	if resourcePlan.Operation == "update" {
		requested, parseErr := resource.ParseQuantity(stringParameter(operation.Spec.Parameters, "size"))
		currentText, found, _ := unstructured.NestedString(object.Object, "spec", "resources", "requests", "storage")
		current, currentErr := resource.ParseQuantity(currentText)
		if parseErr != nil || !found || currentErr != nil || current.Cmp(requested) < 0 {
			return false, false, false, "", nil, nil
		}
	} else {
		managedBy, _, _ := unstructured.NestedString(object.Object, "metadata", "labels", "app.kubernetes.io/managed-by")
		if managedBy != "highland" {
			return false, false, false, "", nil, nil
		}
	}
	done, failed, message, err = c.inspect(ctx, operation, plan)
	result = &ResultReference{APIVersion: resourcePlan.APIVersion, Kind: resourcePlan.Kind, Namespace: resourcePlan.Namespace, Name: resourcePlan.Name, UID: string(object.GetUID())}
	return true, done, failed, message, result, err
}

func (c *Controller) retry(ctx context.Context, operation *Operation, step string, retryErr error) error {
	operation.Status.Retries++
	operation.Status.Step = step
	operation.Status.Diagnostics = sanitize(retryErr.Error())
	now := c.now()
	operation.Status.LastAttemptAt = &now
	_, err := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
	if c.observer != nil {
		c.observer.OperationRetry(nonempty(operation.Spec.ProviderID, "kubernetes"), retryReason(retryErr))
	}
	return err
}

func (c *Controller) execute(ctx context.Context, operation *Operation, plan Plan) (*ResultReference, bool, error) {
	if len(plan.Resources) != 1 {
		return nil, false, fmt.Errorf("operation plan must contain exactly one mutable resource")
	}
	resourcePlan := plan.Resources[0]
	switch resourcePlan.Operation {
	case "server-side-apply":
		gvr, namespaced, err := gvrFor(resourcePlan.APIVersion, resourcePlan.Kind)
		if err != nil {
			return nil, false, err
		}
		encoded, _ := json.Marshal(resourcePlan.Manifest)
		resourceClient := c.dynamic.Resource(gvr)
		var interfaceClient dynamic.ResourceInterface
		if namespaced {
			interfaceClient = resourceClient.Namespace(resourcePlan.Namespace)
		} else {
			interfaceClient = resourceClient
		}
		force := false
		if _, err := interfaceClient.Patch(ctx, resourcePlan.Name, k8stypes.ApplyPatchType, encoded, metav1.PatchOptions{FieldManager: "highland-storage", Force: &force, DryRun: []string{metav1.DryRunAll}}); err != nil {
			return nil, false, fmt.Errorf("server-side dry-run failed: %w", err)
		}
		applied, err := interfaceClient.Patch(ctx, resourcePlan.Name, k8stypes.ApplyPatchType, encoded, metav1.PatchOptions{FieldManager: "highland-storage", Force: &force})
		if err != nil {
			return nil, false, fmt.Errorf("server-side apply failed: %w", err)
		}
		immediate := resourcePlan.Kind == "StorageClass"
		return &ResultReference{APIVersion: resourcePlan.APIVersion, Kind: resourcePlan.Kind, Namespace: resourcePlan.Namespace, Name: resourcePlan.Name, UID: string(applied.GetUID())}, !immediate, nil
	case "update":
		claim, err := c.core.CoreV1().PersistentVolumeClaims(resourcePlan.Namespace).Get(ctx, resourcePlan.Name, metav1.GetOptions{})
		if err != nil {
			return nil, false, err
		}
		if string(claim.UID) != plan.Target.UID || claim.ResourceVersion != plan.Target.ResourceVersion {
			return nil, false, &PlanError{Code: "STALE_TARGET", Message: "PVC changed after approval"}
		}
		quantity, _ := resource.ParseQuantity(stringParameter(operation.Spec.Parameters, "size"))
		claim.Spec.Resources.Requests[corev1.ResourceStorage] = quantity
		updated, err := c.core.CoreV1().PersistentVolumeClaims(resourcePlan.Namespace).Update(ctx, claim, metav1.UpdateOptions{})
		if err != nil {
			return nil, false, err
		}
		return &ResultReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: updated.Namespace, Name: updated.Name, UID: string(updated.UID)}, true, nil
	case "delete":
		gvr, namespaced, err := gvrFor(resourcePlan.APIVersion, resourcePlan.Kind)
		if err != nil {
			return nil, false, err
		}
		uid := k8stypes.UID(plan.Target.UID)
		resourceVersion := plan.Target.ResourceVersion
		options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}}
		resourceClient := c.dynamic.Resource(gvr)
		var deleteErr error
		if namespaced {
			deleteErr = resourceClient.Namespace(resourcePlan.Namespace).Delete(ctx, resourcePlan.Name, options)
		} else {
			deleteErr = resourceClient.Delete(ctx, resourcePlan.Name, options)
		}
		if deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return nil, false, deleteErr
		}
		return &ResultReference{APIVersion: resourcePlan.APIVersion, Kind: resourcePlan.Kind, Namespace: resourcePlan.Namespace, Name: resourcePlan.Name, UID: plan.Target.UID}, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported planned operation %q", resourcePlan.Operation)
	}
}

func (c *Controller) inspect(ctx context.Context, operation *Operation, plan Plan) (bool, bool, string, error) {
	resourcePlan := plan.Resources[0]
	gvr, namespaced, err := gvrFor(resourcePlan.APIVersion, resourcePlan.Kind)
	if err != nil {
		return false, false, "", err
	}
	resourceClient := c.dynamic.Resource(gvr)
	var object *unstructured.Unstructured
	if namespaced {
		object, err = resourceClient.Namespace(resourcePlan.Namespace).Get(ctx, resourcePlan.Name, metav1.GetOptions{})
	} else {
		object, err = resourceClient.Get(ctx, resourcePlan.Name, metav1.GetOptions{})
	}
	if resourcePlan.Operation == "delete" {
		if apierrors.IsNotFound(err) {
			return c.verifyDeletedBackend(ctx, resourcePlan)
		}
		if err != nil {
			return false, false, "", err
		}
		if plan.Target.UID != "" && string(object.GetUID()) != plan.Target.UID {
			// The approved UID is gone. Never wait on, mutate, or report the newly
			// recreated same-name object as the original target.
			return c.verifyDeletedBackend(ctx, resourcePlan)
		}
		return false, false, "WaitingForDeletion", nil
	}
	if err != nil {
		return false, false, "", err
	}
	switch resourcePlan.Kind {
	case "PersistentVolumeClaim":
		if operation.Spec.ActionID == "expand-pvc" {
			requested, _ := resource.ParseQuantity(stringParameter(operation.Spec.Parameters, "size"))
			capacity, found, _ := unstructured.NestedString(object.Object, "status", "capacity", "storage")
			if found {
				actual, parseErr := resource.ParseQuantity(capacity)
				if parseErr == nil && actual.Cmp(requested) >= 0 {
					return true, false, "ExpansionComplete", nil
				}
			}
			phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
			if phase == "Lost" {
				return false, true, "PVC entered Lost phase", nil
			}
			return false, false, "WaitingForExpansion", nil
		}
		phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
		if phase == "Bound" {
			return true, false, "ClaimBound", nil
		}
		if phase == "Lost" {
			return false, true, "PVC entered Lost phase", nil
		}
		return false, false, "WaitingForBinding", nil
	case "VolumeSnapshot":
		ready, _, _ := unstructured.NestedBool(object.Object, "status", "readyToUse")
		if ready {
			return true, false, "SnapshotReady", nil
		}
		if statusError, found, _ := unstructured.NestedMap(object.Object, "status", "error"); found {
			return false, true, sanitize(fmt.Sprint(statusError["message"])), nil
		}
		return false, false, "WaitingForSnapshot", nil
	case "CephBlockPool":
		phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
		if phase == "Ready" || phase == "Connected" || phase == "Created" {
			verifier, ok := c.planner.safety.(PoolPostflightVerifier)
			if !ok {
				return false, false, "", &PlanError{Code: "POOL_POSTFLIGHT_UNAVAILABLE", Message: "Ceph runtime verification is required before pool creation can succeed"}
			}
			present, reason, verifyErr := verifier.VerifyPoolPresent(ctx, resourcePlan.Namespace, resourcePlan.Name)
			if verifyErr != nil {
				return false, false, "", verifyErr
			}
			if !present {
				return false, true, nonempty(reason, "Rook reports ready but the Ceph runtime pool is absent"), nil
			}
			return true, false, "PoolReadyAndRuntimeVerified", nil
		}
		if strings.EqualFold(phase, "Failure") || strings.EqualFold(phase, "Error") {
			return false, true, "Rook reported pool failure", nil
		}
		return false, false, "WaitingForRook", nil
	default:
		return true, false, "Applied", nil
	}
}

func (c *Controller) verifyDeletedBackend(ctx context.Context, resourcePlan PlannedResource) (bool, bool, string, error) {
	if resourcePlan.Kind != "CephBlockPool" {
		return true, false, "ResourceDeleted", nil
	}
	verifier, ok := c.planner.safety.(PoolPostflightVerifier)
	if !ok {
		return false, false, "", &PlanError{Code: "POOL_POSTFLIGHT_UNAVAILABLE", Message: "fresh Ceph runtime absence is required before pool deletion can succeed"}
	}
	present, reason, err := verifier.VerifyPoolPresent(ctx, resourcePlan.Namespace, resourcePlan.Name)
	if err != nil {
		return false, false, "", err
	}
	if present {
		return false, false, nonempty(reason, "Rook resource is gone but the Ceph runtime pool still exists"), nil
	}
	return true, false, "PoolDeletedAndRuntimeVerified", nil
}

func (c *Controller) succeed(ctx context.Context, operation *Operation, reason string) error {
	now := c.now()
	operation.Status.Phase, operation.Status.Step, operation.Status.FinishedAt, operation.Status.Diagnostics = "Succeeded", "Complete", &now, ""
	operation.Status.Conditions = append(operation.Status.Conditions, OperationCondition{Type: "Succeeded", Status: "True", Reason: reason, LastTransitionTime: now})
	_, err := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
	if c.observer != nil && c.observeFinished(operation) {
		started := operation.CreationTimestamp
		if operation.Status.StartedAt != nil {
			started = *operation.Status.StartedAt
		}
		c.observer.OperationFinished(nonempty(operation.Spec.ProviderID, "kubernetes"), operation.Spec.ActionID, "succeeded", now.Sub(started))
	}
	c.auditOperation(operation, "storage_operation_succeeded", "ok", reason)
	return err
}
func (c *Controller) fail(ctx context.Context, operation *Operation, code, message string) error {
	now := c.now()
	// A pending operation can fail its first fresh preflight before it ever
	// transitions to Running. Pair the terminal observation so counters remain
	// complete and the in-progress gauge cannot become negative.
	c.observeStarted(operation)
	operation.Status.Phase, operation.Status.Step, operation.Status.FinishedAt, operation.Status.ErrorCode, operation.Status.Diagnostics = "Failed", "Terminal", &now, code, sanitize(message)
	operation.Status.Conditions = append(operation.Status.Conditions, OperationCondition{Type: "Failed", Status: "True", Reason: code, Message: sanitize(message), LastTransitionTime: now})
	_, err := c.store.UpdateStatus(ctx, operation.Name, operation.Status)
	if c.observer != nil && c.observeFinished(operation) {
		c.observer.OperationFinished(nonempty(operation.Spec.ProviderID, "kubernetes"), operation.Spec.ActionID, "failed", now.Sub(operation.CreationTimestamp))
	}
	c.auditOperation(operation, "storage_operation_failed", "error", code+": "+sanitize(message))
	return err
}

func (c *Controller) observeStarted(operation *Operation) {
	if c.observer == nil || operation == nil {
		return
	}
	if _, loaded := c.active.LoadOrStore(operation.Name, struct{}{}); !loaded {
		c.observer.OperationStarted(nonempty(operation.Spec.ProviderID, "kubernetes"), operation.Spec.ActionID)
	}
}

func (c *Controller) observeFinished(operation *Operation) bool {
	if operation == nil {
		return false
	}
	_, loaded := c.active.LoadAndDelete(operation.Name)
	return loaded
}

func (c *Controller) observeLeader(leader bool) {
	if observer, ok := c.observer.(ControllerStateObserver); ok {
		observer.OperationControllerLeader(leader)
	}
}

func (c *Controller) auditOperation(operation *Operation, action, result, message string) {
	if c.audit == nil || operation == nil {
		return
	}
	target := operation.Spec.Target
	actionDefinition, _ := ActionByID(operation.Spec.ActionID)
	c.audit.Append(audit.Event{Username: operation.Spec.Requester, Role: operation.Spec.RequesterRole, Action: action, ActionID: operation.Spec.ActionID, ProviderID: operation.Spec.ProviderID, ProviderKind: nonempty(actionDefinition.ProviderKind, "kubernetes"), OperationID: operation.Name, Target: target.Namespace + "/" + target.Name, TargetKind: target.Kind, TargetNamespace: target.Namespace, TargetName: target.Name, TargetUID: target.UID, KubernetesCorrelationID: target.UID, PlanHash: operation.Spec.PlanHash, Result: result, Message: sanitize(message)})
}

func gvrFor(apiVersion, kind string) (schema.GroupVersionResource, bool, error) {
	switch apiVersion + "/" + kind {
	case "v1/PersistentVolumeClaim":
		return schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, true, nil
	case "snapshot.storage.k8s.io/v1/VolumeSnapshot":
		return snapshotGVR, true, nil
	case "storage.k8s.io/v1/StorageClass":
		return schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, false, nil
	case "ceph.rook.io/v1/CephBlockPool":
		return poolGVR, true, nil
	default:
		return schema.GroupVersionResource{}, false, fmt.Errorf("unsupported resource %s %s", apiVersion, kind)
	}
}
func retryable(err error) bool {
	var planError *PlanError
	if errors.As(err, &planError) && planError.Retryable {
		return true
	}
	message := strings.ToLower(err.Error())
	return apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err) || strings.Contains(message, "connection reset") || strings.Contains(message, "request failed") || strings.Contains(message, "circuit is open") || strings.Contains(message, "temporarily unavailable") || strings.Contains(message, "timeout")
}
func retryReason(err error) string {
	if apierrors.IsConflict(err) {
		return "conflict"
	}
	if apierrors.IsTooManyRequests(err) {
		return "throttled"
	}
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) {
		return "timeout"
	}
	return "transient"
}
func codeOf(err error, fallback string) string {
	var planError *PlanError
	if errors.As(err, &planError) {
		return planError.Code
	}
	return fallback
}
func sanitize(message string) string {
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 1000 {
		message = message[:1000]
	}
	for _, marker := range []string{"Bearer ", "password", "token"} {
		if index := strings.Index(strings.ToLower(message), strings.ToLower(marker)); index >= 0 {
			message = message[:index] + "[redacted]"
		}
	}
	return message
}
