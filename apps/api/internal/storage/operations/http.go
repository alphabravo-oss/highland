package operations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	appmw "github.com/highland-io/highland/apps/api/internal/middleware"
	"github.com/highland-io/highland/apps/api/internal/storage"
)

type PreflightObserver interface{ PreflightDenied(provider, reason string) }

type APIConfig struct {
	Store                   *Store
	Planner                 *Planner
	Audit                   *audit.Store
	Observer                PreflightObserver
	WritesEnabled           bool
	CephWritesEnabled       bool
	AllowStorageClassDelete bool
	AllowPoolDelete         bool
	CephPoolVerified        bool
	CephVersionSafe         bool
	CephVersionCheck        func(context.Context) bool
}

type API struct {
	store                   *Store
	planner                 *Planner
	audit                   *audit.Store
	observer                PreflightObserver
	writesEnabled           bool
	cephWritesEnabled       bool
	allowStorageClassDelete bool
	allowPoolDelete         bool
	cephPoolVerified        bool
	cephVersionSafe         bool
	cephVersionCheck        func(context.Context) bool
}

func NewAPI(cfg APIConfig) *API {
	return &API{store: cfg.Store, planner: cfg.Planner, audit: cfg.Audit, observer: cfg.Observer, writesEnabled: cfg.WritesEnabled, cephWritesEnabled: cfg.CephWritesEnabled, allowStorageClassDelete: cfg.AllowStorageClassDelete, allowPoolDelete: cfg.AllowPoolDelete, cephPoolVerified: cfg.CephPoolVerified, cephVersionSafe: cfg.CephVersionSafe, cephVersionCheck: cfg.CephVersionCheck}
}

func (a *API) Mount(r chi.Router) {
	r.Get("/api/v1/storage/actions", a.ListActions)
	r.Get("/api/v1/storage/operations", a.ListOperations)
	r.Get("/api/v1/storage/operations/{operationId}", a.GetOperation)
	r.Post("/api/v1/storage/plans", a.CreatePlan)
	r.Post("/api/v1/storage/claims", a.submit("create-pvc", "", targetFromBody))
	r.Patch("/api/v1/storage/claims/{namespace}/{name}/size", a.submit("expand-pvc", "", targetFromPath("PersistentVolumeClaim")))
	r.Delete("/api/v1/storage/claims/{namespace}/{name}", a.submit("delete-pvc", "", targetFromPath("PersistentVolumeClaim")))
	r.Post("/api/v1/storage/snapshots", a.submit("create-snapshot", "", targetFromBody))
	r.Delete("/api/v1/storage/snapshots/{namespace}/{name}", a.submit("delete-snapshot", "", targetFromPath("VolumeSnapshot")))
	r.Post("/api/v1/storage/restores", a.submit("restore-snapshot", "", targetFromBody))
	r.Post("/api/v1/storage/clones", a.submit("clone-pvc", "", targetFromBody))
	r.Post("/api/v1/providers/{providerId}/ceph/block-pools", a.submit("create-ceph-blockpool", "rook-ceph", targetFromBody))
	r.Delete("/api/v1/providers/{providerId}/ceph/block-pools/{namespace}/{name}", a.submit("delete-ceph-blockpool", "rook-ceph", targetFromPath("CephBlockPool")))
	r.Post("/api/v1/providers/{providerId}/ceph/storage-classes", a.submit("create-ceph-rbd-storageclass", "rook-ceph", targetFromBody))
	r.Delete("/api/v1/providers/{providerId}/ceph/storage-classes/{name}", a.submit("delete-ceph-storageclass", "rook-ceph", targetFromPath("StorageClass")))
}

func (a *API) ListActions(w http.ResponseWriter, r *http.Request) {
	user, _ := appmw.UserFromContext(r.Context())
	result := []map[string]any{}
	type prerequisiteResult struct {
		available bool
		reason    string
	}
	prerequisites := map[string]prerequisiteResult{}
	for _, action := range Actions() {
		enabled := a.featureEnabled(r.Context(), action)
		prerequisiteKey := action.ID
		if strings.Contains(action.ID, "snapshot") {
			prerequisiteKey = "snapshot-api"
		}
		if action.ProviderKind == "rook-ceph" {
			prerequisiteKey = "rook-ceph"
		}
		prerequisite, cached := prerequisites[prerequisiteKey]
		if !cached {
			prerequisite.available, prerequisite.reason = a.planner.ActionPrerequisite(r.Context(), action)
			prerequisites[prerequisiteKey] = prerequisite
		}
		available := enabled && prerequisite.available && Authorize(action, user.Role) == nil
		reason := ""
		if !enabled {
			reason = a.unavailableReason(r.Context(), action)
		} else if !prerequisite.available {
			reason = prerequisite.reason
		} else if !available {
			reason = action.MinimumRole + " role is required"
		}
		result = append(result, map[string]any{"action": action, "enabled": enabled, "available": available, "unavailableReason": reason})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result, "writesEnabled": a.writesEnabled})
}

func (a *API) unavailableReason(ctx context.Context, action Action) string {
	if action.ProviderKind == "rook-ceph" && a.writesEnabled && a.cephWritesEnabled && !a.cephVersionSupported(ctx) {
		return "supported current/previous Rook and Ceph versions were not both detected"
	}
	if (action.ID == "create-ceph-blockpool" || action.ID == "delete-ceph-blockpool") && a.writesEnabled && a.cephWritesEnabled && !a.cephPoolVerified {
		return "fresh Ceph Dashboard pool verification is unavailable"
	}
	return action.FeatureFlag + " is disabled"
}

func (a *API) ListOperations(w http.ResponseWriter, r *http.Request) {
	if !a.available(w, r) {
		return
	}
	user, _ := appmw.UserFromContext(r.Context())
	filters := map[string]string{"provider": boundedQuery(r, "provider"), "action": boundedQuery(r, "action"), "state": boundedQuery(r, "state"), "user": boundedQuery(r, "user")}
	if user.Role != auth.RoleAdmin {
		filters["user"] = user.Username
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 500 {
			writeError(w, r, http.StatusBadRequest, "INVALID_PAGE", "limit must be between 1 and 500", false, nil)
			return
		}
		limit = parsed
	}
	operations, err := a.store.List(r.Context(), filters, limit)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "OPERATION_STORE_UNAVAILABLE", err.Error(), true, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": operations})
}

func (a *API) GetOperation(w http.ResponseWriter, r *http.Request) {
	if !a.available(w, r) {
		return
	}
	operation, err := a.store.Get(r.Context(), chi.URLParam(r, "operationId"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, "OPERATION_NOT_FOUND", "storage operation was not found", false, nil)
		return
	}
	user, _ := appmw.UserFromContext(r.Context())
	if user.Role != auth.RoleAdmin && operation.Spec.Requester != user.Username {
		writeError(w, r, http.StatusForbidden, "OPERATION_FORBIDDEN", "operation belongs to another user", false, nil)
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (a *API) CreatePlan(w http.ResponseWriter, r *http.Request) {
	if !a.available(w, r) {
		return
	}
	var request Request
	if err := decodeRequest(r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false, nil)
		return
	}
	user, ok := appmw.UserFromContext(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", false, nil)
		return
	}
	action, ok := ActionByID(request.ActionID)
	if !ok {
		writeError(w, r, http.StatusNotFound, "ACTION_NOT_SUPPORTED", "storage action is not supported", false, nil)
		return
	}
	if err := a.authorize(r.Context(), action, user.Role); err != nil {
		a.deny(request.ProviderID, "authorization")
		a.auditEvent(user, r, action.ID, action.AuditAction+"_denied", request.ProviderID, request.Target, "", "", "denied", "authorization failed")
		writeError(w, r, http.StatusForbidden, "ACTION_FORBIDDEN", err.Error(), false, nil)
		return
	}
	plan, err := a.planner.Plan(r.Context(), user.Username, request)
	if err != nil {
		a.auditEvent(user, r, action.ID, action.AuditAction+"_denied", request.ProviderID, request.Target, "", "", "denied", codeOf(err, "PREFLIGHT_FAILED"))
		a.writePlanError(w, r, request.ProviderID, err)
		return
	}
	a.auditEvent(user, r, action.ID, action.AuditAction+"_plan", request.ProviderID, plan.Target, "", plan.Hash, "ok", "plan generated")
	writeJSON(w, http.StatusOK, plan)
}

type targetBuilder func(*http.Request, *Request)

func (a *API) submit(actionID, providerKind string, target targetBuilder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.available(w, r) {
			return
		}
		var request Request
		if r.Body != nil {
			if err := decodeRequest(r, &request); err != nil && !errors.Is(err, io.EOF) {
				writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", err.Error(), false, nil)
				return
			}
		}
		resolvedActionID := actionID
		if actionID == "create-ceph-rbd-storageclass" && request.ActionID == "create-cephfs-storageclass" {
			resolvedActionID = request.ActionID
		}
		request.ActionID = resolvedActionID
		if providerID := chi.URLParam(r, "providerId"); providerID != "" {
			request.ProviderID = providerID
		}
		if request.ProviderID == "" && providerKind != "" {
			request.ProviderID = providerKind
		}
		target(r, &request)
		if request.Confirmation.Challenge == "" {
			request.Confirmation.Challenge = r.Header.Get("X-Highland-Confirmation")
		}
		if request.Confirmation.TypedName == "" {
			request.Confirmation.TypedName = r.Header.Get("X-Highland-Typed-Name")
		}
		user, ok := appmw.UserFromContext(r.Context())
		if !ok {
			writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", false, nil)
			return
		}
		action, _ := ActionByID(resolvedActionID)
		if action.ProviderKind != "" && request.ProviderID != action.ProviderKind {
			a.deny(request.ProviderID, "provider_mismatch")
			a.auditEvent(user, r, action.ID, action.AuditAction+"_denied", request.ProviderID, request.Target, "", "", "denied", "provider mismatch")
			writeError(w, r, http.StatusBadRequest, "PROVIDER_MISMATCH", "provider route does not match the action's configured provider", false, nil)
			return
		}
		if err := a.authorize(r.Context(), action, user.Role); err != nil {
			a.deny(request.ProviderID, "authorization")
			a.auditEvent(user, r, action.ID, action.AuditAction+"_denied", request.ProviderID, request.Target, "", "", "denied", "authorization failed")
			writeError(w, r, http.StatusForbidden, "ACTION_FORBIDDEN", err.Error(), false, nil)
			return
		}
		plan, err := a.planner.Plan(r.Context(), user.Username, request)
		if err != nil {
			a.auditEvent(user, r, action.ID, action.AuditAction+"_denied", request.ProviderID, request.Target, "", "", "denied", codeOf(err, "PREFLIGHT_FAILED"))
			a.writePlanError(w, r, request.ProviderID, err)
			return
		}
		if err := a.planner.Verify(user.Username, request, plan); err != nil {
			a.auditEvent(user, r, action.ID, action.AuditAction+"_denied", request.ProviderID, plan.Target, "", plan.Hash, "denied", codeOf(err, "CONFIRMATION_INVALID"))
			a.writePlanError(w, r, request.ProviderID, err)
			return
		}
		spec := Spec{ActionID: resolvedActionID, ProviderID: request.ProviderID, Target: plan.Target, Parameters: request.Parameters, ParameterHash: hashValue(map[string]any{"action": resolvedActionID, "provider": request.ProviderID, "target": plan.Target, "parameters": request.Parameters}), PlanHash: plan.Hash, IdempotencyHash: hashValue(request.Confirmation.Challenge), Resources: plan.Resources, Dependencies: plan.Dependencies, Requester: user.Username, RequesterRole: string(user.Role), RequestedAt: time.Now().UTC()}
		if existing, findErr := a.store.FindEquivalent(r.Context(), spec); findErr == nil && existing != nil {
			writeJSON(w, http.StatusAccepted, map[string]any{"operation": existing, "duplicate": true})
			return
		}
		if active, findErr := a.store.FindActiveTarget(r.Context(), plan.Target); findErr == nil && active != nil {
			writeError(w, r, http.StatusConflict, "OPERATION_IN_PROGRESS", "another nonterminal storage operation already targets this resource", false, map[string]any{"operationId": active.Name})
			return
		}
		operation, err := a.store.Create(r.Context(), spec)
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				if existing, findErr := a.store.FindEquivalent(r.Context(), spec); findErr == nil && existing != nil {
					writeJSON(w, http.StatusAccepted, map[string]any{"operation": existing, "operationId": existing.Name, "duplicate": true})
					return
				}
			}
			writeError(w, r, http.StatusServiceUnavailable, "OPERATION_STORE_UNAVAILABLE", err.Error(), true, nil)
			return
		}
		a.auditEvent(user, r, action.ID, action.AuditAction+"_approved", request.ProviderID, plan.Target, operation.Name, plan.Hash, "ok", "operation approved")
		w.Header().Set("Location", "/api/v1/storage/operations/"+operation.Name)
		writeJSON(w, http.StatusAccepted, map[string]any{"operation": operation, "operationId": operation.Name})
	}
}

func targetFromBody(_ *http.Request, request *Request) {
	if request.Target.Kind == "" {
		request.Target.Kind = targetKindForAction(request.ActionID)
	}
}
func targetFromPath(kind string) targetBuilder {
	return func(r *http.Request, request *Request) {
		request.Target.Kind = kind
		if namespace := chi.URLParam(r, "namespace"); namespace != "" {
			request.Target.Namespace = namespace
		}
		request.Target.Name = chi.URLParam(r, "name")
	}
}

func (a *API) authorize(ctx context.Context, action Action, role auth.Role) error {
	if !a.featureEnabled(ctx, action) {
		return fmt.Errorf("%s is disabled", action.FeatureFlag)
	}
	return Authorize(action, role)
}
func (a *API) featureEnabled(ctx context.Context, action Action) bool {
	switch action.FeatureFlag {
	case "storage.writes.enabled":
		return a.writesEnabled
	case "providers.rookCeph.writes.enabled":
		if !a.cephVersionSupported(ctx) {
			return false
		}
		if action.ID == "create-ceph-blockpool" {
			return a.writesEnabled && a.cephWritesEnabled && a.cephPoolVerified
		}
		return a.writesEnabled && a.cephWritesEnabled
	case "providers.rookCeph.writes.allowPoolDelete":
		return a.writesEnabled && a.cephWritesEnabled && a.allowPoolDelete && a.cephPoolVerified && a.cephVersionSupported(ctx)
	case "providers.rookCeph.writes.allowStorageClassDelete":
		return a.writesEnabled && a.cephWritesEnabled && a.allowStorageClassDelete && a.cephVersionSupported(ctx)
	default:
		return false
	}
}

func (a *API) cephVersionSupported(ctx context.Context) bool {
	if a.cephVersionCheck != nil {
		return a.cephVersionCheck(ctx)
	}
	return a.cephVersionSafe
}
func (a *API) available(w http.ResponseWriter, r *http.Request) bool {
	if a == nil || a.store == nil || a.planner == nil {
		writeError(w, r, http.StatusServiceUnavailable, "OPERATIONS_UNAVAILABLE", "durable storage operations are not configured", true, nil)
		return false
	}
	return true
}
func (a *API) writePlanError(w http.ResponseWriter, r *http.Request, provider string, err error) {
	var planError *PlanError
	if errors.As(err, &planError) {
		a.deny(provider, planError.Code)
		status := http.StatusConflict
		if planError.Retryable {
			status = http.StatusServiceUnavailable
		}
		writeError(w, r, status, planError.Code, planError.Message, planError.Retryable, planError.Details)
		return
	}
	a.deny(provider, "preflight_failed")
	writeError(w, r, http.StatusServiceUnavailable, "PREFLIGHT_FAILED", err.Error(), true, nil)
}
func (a *API) deny(provider, reason string) {
	if a.observer != nil {
		a.observer.PreflightDenied(nonempty(provider, "kubernetes"), reason)
	}
}
func (a *API) auditEvent(user auth.User, r *http.Request, actionID, action, providerID string, target ResourceTarget, operationID, planHash, result, message string) {
	if a.audit == nil {
		return
	}
	actionDefinition, _ := ActionByID(actionID)
	requestID := chimw.GetReqID(r.Context())
	a.audit.Append(audit.Event{Username: user.Username, Role: string(user.Role), Action: action, ActionID: actionID, ProviderID: providerID, ProviderKind: nonempty(actionDefinition.ProviderKind, "kubernetes"), OperationID: operationID, Target: target.Namespace + "/" + target.Name, TargetKind: target.Kind, TargetNamespace: target.Namespace, TargetName: target.Name, TargetUID: target.UID, PlanHash: planHash, CorrelationID: requestID, HTTPCorrelationID: requestID, KubernetesCorrelationID: target.UID, Method: r.Method, Path: r.URL.Path, Result: result, SourceIP: r.RemoteAddr, Message: sanitize(message)})
}

func decodeRequest(r *http.Request, target *Request) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, (1<<20)+1))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("request must contain one JSON object smaller than 1 MiB")
	}
	return nil
}
func boundedQuery(r *http.Request, key string) string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if len(value) > 128 {
		return value[:128]
	}
	return value
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool, details map[string]any) {
	requestID := ""
	if r != nil {
		requestID = chimw.GetReqID(r.Context())
	}
	writeJSON(w, status, storage.ErrorEnvelope{Error: storage.APIError{Code: code, Message: sanitize(message), Details: details, Retryable: retryable, RequestID: requestID}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
