package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var OperationGVR = schema.GroupVersionResource{Group: "highland.io", Version: "v1alpha1", Resource: "storageoperations"}

type Store struct {
	dynamic   dynamic.Interface
	namespace string
}

func NewStore(client dynamic.Interface, namespace string) (*Store, error) {
	if client == nil {
		return nil, fmt.Errorf("storage operation store requires Kubernetes dynamic client")
	}
	if namespace == "" {
		namespace = "highland-system"
	}
	return &Store{dynamic: client, namespace: namespace}, nil
}

func (s *Store) Create(ctx context.Context, spec Spec) (*Operation, error) {
	requestedAt := spec.RequestedAt
	if requestedAt.IsZero() {
		requestedAt = time.Now().UTC()
		spec.RequestedAt = requestedAt
	}
	// A confirmation is valid for five minutes. Deriving the object name from
	// the approved plan and the same-sized time slot gives all API replicas one
	// Kubernetes-enforced idempotency key without preventing a later reviewed
	// retry after a terminal failure.
	idempotencyHash := spec.IdempotencyHash
	if len(idempotencyHash) < 24 {
		idempotencyHash = hashValue(map[string]any{"action": spec.ActionID, "provider": spec.ProviderID, "target": spec.Target, "parameters": spec.ParameterHash, "plan": spec.PlanHash, "slot": requestedAt.Unix() / 300})
	}
	operationName := "storage-" + idempotencyHash[:24]
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": APIVersion, "kind": Kind,
		"metadata": map[string]any{"name": operationName, "namespace": s.namespace, "labels": map[string]any{"app.kubernetes.io/managed-by": "highland", "highland.io/action": safeLabel(spec.ActionID), "highland.io/provider": safeLabel(nonempty(spec.ProviderID, "kubernetes")), "highland.io/request-hash": safeLabel(spec.ParameterHash), "highland.io/target-hash": targetHash(spec.Target)}},
		"spec":     mustMap(spec),
		"status":   map[string]any{"phase": "Pending", "step": "Queued"},
	}}
	created, err := s.dynamic.Resource(OperationGVR).Namespace(s.namespace).Create(ctx, object, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create StorageOperation: %w", err)
	}
	return decodeOperation(created)
}

func (s *Store) Get(ctx context.Context, name string) (*Operation, error) {
	object, err := s.dynamic.Resource(OperationGVR).Namespace(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return decodeOperation(object)
}

func (s *Store) List(ctx context.Context, filters map[string]string, limit int) ([]Operation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	operations, err := s.listBySelector(ctx, "")
	if err != nil {
		return nil, err
	}
	result := make([]Operation, 0, len(operations))
	for index := range operations {
		operation := &operations[index]
		if value := filters["provider"]; value != "" && operation.Spec.ProviderID != value {
			continue
		}
		if value := filters["action"]; value != "" && operation.Spec.ActionID != value {
			continue
		}
		if value := filters["state"]; value != "" && !stringsEqualFold(operation.Status.Phase, value) {
			continue
		}
		if value := filters["user"]; value != "" && operation.Spec.Requester != value {
			continue
		}
		result = append(result, *operation)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreationTimestamp.After(result[j].CreationTimestamp) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *Store) FindEquivalent(ctx context.Context, spec Spec) (*Operation, error) {
	operations, err := s.listBySelector(ctx, labels.Set{
		"highland.io/action":       safeLabel(spec.ActionID),
		"highland.io/provider":     safeLabel(nonempty(spec.ProviderID, "kubernetes")),
		"highland.io/request-hash": safeLabel(spec.ParameterHash),
		"highland.io/target-hash":  targetHash(spec.Target),
	}.AsSelector().String())
	if err != nil {
		return nil, err
	}
	for index := range operations {
		operation := &operations[index]
		if operation.Spec.ParameterHash == spec.ParameterHash && operation.Spec.Target.Namespace == spec.Target.Namespace && operation.Spec.Target.Name == spec.Target.Name && operation.Status.Phase != "Succeeded" && operation.Status.Phase != "Failed" && operation.Status.Phase != "Cancelled" {
			return operation, nil
		}
	}
	return nil, nil
}

func (s *Store) FindActiveTarget(ctx context.Context, target ResourceTarget) (*Operation, error) {
	operations, err := s.listBySelector(ctx, labels.Set{"highland.io/target-hash": targetHash(target)}.AsSelector().String())
	if err != nil {
		return nil, err
	}
	for index := range operations {
		operation := &operations[index]
		terminal := operation.Status.Phase == "Succeeded" || operation.Status.Phase == "Failed" || operation.Status.Phase == "Cancelled"
		if !terminal && operation.Spec.Target.Kind == target.Kind && operation.Spec.Target.Namespace == target.Namespace && operation.Spec.Target.Name == target.Name {
			return operation, nil
		}
	}
	return nil, nil
}

func (s *Store) listBySelector(ctx context.Context, selector string) ([]Operation, error) {
	result := []Operation{}
	continuation := ""
	for {
		list, err := s.dynamic.Resource(OperationGVR).Namespace(s.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 500, Continue: continuation})
		if err != nil {
			return nil, fmt.Errorf("list StorageOperations: %w", err)
		}
		for index := range list.Items {
			operation, decodeErr := decodeOperation(&list.Items[index])
			if decodeErr == nil {
				result = append(result, *operation)
			}
		}
		continuation = list.GetContinue()
		if continuation == "" {
			break
		}
	}
	return result, nil
}

func (s *Store) UpdateStatus(ctx context.Context, name string, status Status) (*Operation, error) {
	resource := s.dynamic.Resource(OperationGVR).Namespace(s.namespace)
	current, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	current.Object["status"] = mustMap(status)
	updated, err := resource.UpdateStatus(ctx, current, metav1.UpdateOptions{})
	if err != nil {
		// Some test/API servers without a status subresource still preserve the
		// same durable semantics through a normal resource update.
		updated, err = resource.Update(ctx, current, metav1.UpdateOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("update StorageOperation status: %w", err)
	}
	return decodeOperation(updated)
}

func (s *Store) DeleteTerminalBefore(ctx context.Context, cutoff time.Time) (int, error) {
	return s.DeleteTerminalBeforeWhere(ctx, cutoff, nil)
}

func (s *Store) DeleteTerminalBeforeWhere(ctx context.Context, cutoff time.Time, allowed func(Operation) bool) (int, error) {
	operations, err := s.listBySelector(ctx, "")
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, operation := range operations {
		if operation.CreationTimestamp.After(cutoff) || (operation.Status.Phase != "Succeeded" && operation.Status.Phase != "Failed" && operation.Status.Phase != "Cancelled") {
			continue
		}
		if allowed != nil && !allowed(operation) {
			continue
		}
		uid := k8stypes.UID(operation.UID)
		if err := s.dynamic.Resource(OperationGVR).Namespace(s.namespace).Delete(ctx, operation.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

func decodeOperation(object *unstructured.Unstructured) (*Operation, error) {
	encoded, err := json.Marshal(object.Object)
	if err != nil {
		return nil, err
	}
	var wire struct {
		Spec   Spec   `json:"spec"`
		Status Status `json:"status"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return nil, err
	}
	return &Operation{APIVersion: object.GetAPIVersion(), Kind: object.GetKind(), Name: object.GetName(), UID: string(object.GetUID()), ResourceVersion: object.GetResourceVersion(), CreationTimestamp: object.GetCreationTimestamp().Time, Spec: wire.Spec, Status: wire.Status}, nil
}

func mustMap(value any) map[string]any {
	encoded, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(encoded, &result)
	return result
}
func safeLabel(value string) string {
	value = stringsMapLabel(value)
	if len(value) > 63 {
		value = value[:63]
	}
	return value
}
func stringsMapLabel(value string) string {
	result := make([]rune, 0, len(value))
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			result = append(result, char)
		}
	}
	if len(result) == 0 {
		return "unknown"
	}
	return string(result)
}
func stringsEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		a, b := left[index], right[index]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
func targetHash(target ResourceTarget) string {
	hash := hashValue(map[string]string{"kind": target.Kind, "namespace": target.Namespace, "name": target.Name})
	return hash[:32]
}
func nonempty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
