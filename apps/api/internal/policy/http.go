package policy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	appmw "github.com/highland-io/highland/apps/api/internal/middleware"
)

const challengeTTL = 5 * time.Minute

type PolicyStore interface {
	Enabled() bool
	Snapshot() Snapshot
	Update(context.Context, StoragePolicy, string, string, string) (*unstructured.Unstructured, error)
	WaitObserved(context.Context, int64) (Snapshot, error)
}

type Impact struct {
	ActionIDs                  []string `json:"actionIds"`
	Roles                      []string `json:"roles"`
	AddedPortableProviderIDs   []string `json:"addedPortableProviderIds"`
	RemovedPortableProviderIDs []string `json:"removedPortableProviderIds"`
}

type APIConfig struct {
	Store           PolicyStore
	Audit           audit.Sink
	Secret          []byte
	ClusterIdentity string
	ActiveCount     func(context.Context) (int, error)
	ImpactResolver  func(StoragePolicy, StoragePolicy) Impact
	Now             func() time.Time
	Observer        Observer
}

type API struct {
	store           PolicyStore
	audit           audit.Sink
	secret          []byte
	clusterIdentity string
	activeCount     func(context.Context) (int, error)
	impactResolver  func(StoragePolicy, StoragePolicy) Impact
	now             func() time.Time
	observer        Observer
}

type ChangeRequest struct {
	Policy          StoragePolicy `json:"policy"`
	ResourceVersion string        `json:"resourceVersion"`
	Confirmation    Confirmation  `json:"confirmation,omitempty"`
}

type Confirmation struct {
	Challenge          string `json:"challenge"`
	ClusterIdentity    string `json:"clusterIdentity,omitempty"`
	EnablePhrase       string `json:"enablePhrase,omitempty"`
	CephPoolPhrase     string `json:"cephPoolPhrase,omitempty"`
	ImpactAcknowledged bool   `json:"impactAcknowledged,omitempty"`
}

type Plan struct {
	Current               StoragePolicy `json:"current"`
	Requested             StoragePolicy `json:"requested"`
	Effective             StoragePolicy `json:"effective"`
	Ceiling               Ceiling       `json:"ceiling"`
	Conditions            []Condition   `json:"conditions"`
	ResourceVersion       string        `json:"resourceVersion"`
	PolicyGeneration      int64         `json:"policyGeneration"`
	Broadening            bool          `json:"broadening"`
	EnablesCephPoolDelete bool          `json:"enablesCephPoolDelete"`
	Impact                Impact        `json:"impact"`
	InFlightOperations    int           `json:"inFlightOperations"`
	ClusterIdentity       string        `json:"clusterIdentity"`
	Actor                 string        `json:"actor"`
	RequestID             string        `json:"requestId"`
	Hash                  string        `json:"hash"`
	Challenge             string        `json:"challenge"`
	ChallengeExpiresAt    time.Time     `json:"challengeExpiresAt"`
	ObservedAt            time.Time     `json:"observedAt"`
}

type challengePayload struct {
	Username          string        `json:"sub"`
	ClusterIdentity   string        `json:"cluster"`
	ResourceVersion   string        `json:"resourceVersion"`
	Requested         StoragePolicy `json:"requested"`
	PlanHash          string        `json:"planHash"`
	Broadening        bool          `json:"broadening"`
	EnablesPoolDelete bool          `json:"enablesPoolDelete"`
	Expires           int64         `json:"exp"`
}

func NewAPI(cfg APIConfig) (*API, error) {
	if cfg.Store == nil {
		return nil, errors.New("policy store is required")
	}
	if len(cfg.Secret) < 32 {
		return nil, errors.New("policy challenge secret must contain at least 32 bytes")
	}
	if strings.TrimSpace(cfg.ClusterIdentity) == "" {
		return nil, errors.New("cluster identity is required")
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &API{
		store: cfg.Store, audit: cfg.Audit, secret: cfg.Secret,
		clusterIdentity: cfg.ClusterIdentity, activeCount: cfg.ActiveCount,
		impactResolver: cfg.ImpactResolver, now: now, observer: cfg.Observer,
	}, nil
}

func (a *API) Mount(r chi.Router) {
	r.Get("/api/v1/admin/storage-policy", a.Get)
	r.Post("/api/v1/admin/storage-policy/plans", a.CreatePlan)
	r.Put("/api/v1/admin/storage-policy", a.Apply)
	r.Get("/api/v1/admin/storage-policy/history", a.History)
}

func (a *API) Get(w http.ResponseWriter, r *http.Request) {
	snapshot := a.store.Snapshot()
	active, err := a.countActive(r.Context())
	if err != nil {
		writePolicyError(w, r, http.StatusServiceUnavailable, "POLICY_STORE_UNAVAILABLE", "active storage operations could not be counted", true, nil)
		return
	}
	writePolicyJSON(w, http.StatusOK, map[string]any{
		"requested": snapshot.Requested, "effective": snapshot.Effective,
		"ceiling": snapshot.Ceiling, "conditions": snapshot.Conditions,
		"source": snapshot.Source, "generation": snapshot.Generation,
		"resourceVersion":    snapshot.ResourceVersion,
		"observedGeneration": snapshot.ObservedGeneration,
		"inFlightOperations": active, "lastChange": snapshot.LastChange,
		"meta": map[string]any{
			"observedAt": snapshot.ObservedAt, "stale": snapshot.Stale,
			"partial": snapshot.Partial, "requestId": chimw.GetReqID(r.Context()),
		},
	})
}

func (a *API) CreatePlan(w http.ResponseWriter, r *http.Request) {
	user, ok := requirePolicyAdmin(w, r)
	if !ok {
		return
	}
	request, ok := decodeChangeRequest(w, r)
	if !ok {
		return
	}
	plan, err := a.plan(r.Context(), user.Username, request)
	if err != nil {
		a.auditEvent(user, r, "policy_change_plan_denied", "denied", err.Error(), "", request.Policy)
		a.writeError(w, r, err)
		return
	}
	a.auditEvent(user, r, "policy_change_plan", "ok", "storage policy change planned", plan.Hash, request.Policy)
	writePolicyJSON(w, http.StatusOK, plan)
}

func (a *API) Apply(w http.ResponseWriter, r *http.Request) {
	user, ok := requirePolicyAdmin(w, r)
	if !ok {
		return
	}
	request, ok := decodeChangeRequest(w, r)
	if !ok {
		return
	}
	plan, err := a.plan(r.Context(), user.Username, request)
	if err != nil {
		if errors.Is(err, ErrStale) {
			if snapshot, replayed := a.idempotentReplay(user.Username, request); replayed {
				a.auditEvent(user, r, "policy_change_idempotent", "ok", "exact policy retry returned the already-observed state", "", request.Policy)
				if a.observer != nil {
					a.observer.PolicyUpdate("ok")
				}
				a.writeSnapshot(w, r, snapshot)
				return
			}
		}
		if a.observer != nil {
			a.observer.PolicyUpdate(policyUpdateResult(err))
		}
		a.auditEvent(user, r, "policy_change_denied", "denied", err.Error(), "", request.Policy)
		a.writeError(w, r, err)
		return
	}
	if err := a.verify(user.Username, request, plan); err != nil {
		if a.observer != nil {
			a.observer.PolicyUpdate("denied")
		}
		a.auditEvent(user, r, "policy_change_denied", "denied", err.Error(), plan.Hash, request.Policy)
		a.writeError(w, r, err)
		return
	}
	// Required pre-mutation audit when the sink is durable (ADR-0004 DEC-3).
	if a.audit != nil && a.audit.Durable() {
		admit := audit.Event{
			Username: user.Username, Role: string(user.Role),
			Action: "policy_change_admit", Target: "HighlandPolicy/highland",
			Method: r.Method, Path: r.URL.Path, Result: "ok", SourceIP: r.RemoteAddr,
			Message: "pre-mutation policy admission", PlanHash: plan.Hash,
			CorrelationID: chimw.GetReqID(r.Context()), HTTPCorrelationID: chimw.GetReqID(r.Context()),
		}
		if err := audit.RequireAppend(r.Context(), a.audit, admit); err != nil {
			if a.observer != nil {
				a.observer.PolicyUpdate("error")
			}
			writePolicyError(w, r, http.StatusServiceUnavailable, "AUDIT_REQUIRED_UNAVAILABLE",
				"policy mutation blocked because required audit admission failed", true, nil)
			return
		}
	}
	updated, err := a.store.Update(r.Context(), request.Policy, request.ResourceVersion, user.Username, chimw.GetReqID(r.Context()))
	if err != nil {
		if a.observer != nil {
			a.observer.PolicyUpdate(policyUpdateResult(err))
		}
		a.auditEvent(user, r, "policy_change_denied", "denied", err.Error(), plan.Hash, request.Policy)
		a.writeError(w, r, err)
		return
	}
	waitCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	snapshot, err := a.store.WaitObserved(waitCtx, updated.GetGeneration())
	if err != nil {
		if a.observer != nil {
			a.observer.PolicyUpdate("error")
		}
		a.auditEvent(user, r, "policy_change_error", "error", "updated policy was not observed before timeout", plan.Hash, request.Policy)
		writePolicyError(w, r, http.StatusServiceUnavailable, "POLICY_NOT_OBSERVED", "policy was persisted but the effective generation was not observed before timeout", true, map[string]any{"generation": updated.GetGeneration()})
		return
	}
	a.auditEvent(user, r, "policy_change_applied", "ok", "storage policy changed", plan.Hash, request.Policy, plan.Current)
	if a.observer != nil {
		a.observer.PolicyUpdate("ok")
	}
	a.writeSnapshot(w, r, snapshot)
}

func policyUpdateResult(err error) string {
	if errors.Is(err, ErrStale) || apierrors.IsConflict(err) {
		return "conflict"
	}
	var requestErr *requestError
	var ceilingErr *CeilingError
	if errors.As(err, &requestErr) || errors.As(err, &ceilingErr) || errors.Is(err, ErrControlDisabled) {
		return "denied"
	}
	return "error"
}

func (a *API) History(w http.ResponseWriter, r *http.Request) {
	if _, ok := requirePolicyAdmin(w, r); !ok {
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 500 {
			writePolicyError(w, r, http.StatusBadRequest, "POLICY_INVALID", "limit must be between 1 and 500", false, nil)
			return
		}
		limit = value
	}
	result := []audit.Event{}
	if a.audit != nil {
		for _, event := range audit.ListRecent(r.Context(), a.audit, 2000) {
			if strings.HasPrefix(event.Action, "policy_change_") {
				result = append(result, event)
				if len(result) == limit {
					break
				}
			}
		}
	}
	writePolicyJSON(w, http.StatusOK, map[string]any{
		"data": result, "page": map[string]any{"limit": limit, "total": len(result)},
		"meta": map[string]any{"observedAt": a.now(), "stale": false, "partial": false, "requestId": chimw.GetReqID(r.Context())},
	})
}

func (a *API) plan(ctx context.Context, username string, request ChangeRequest) (Plan, error) {
	if !a.store.Enabled() {
		return Plan{}, ErrControlDisabled
	}
	request.Policy = Normalize(request.Policy)
	if err := Validate(request.Policy); err != nil {
		return Plan{}, &requestError{Code: "POLICY_INVALID", Message: err.Error(), Status: http.StatusBadRequest}
	}
	snapshot := a.store.Snapshot()
	if snapshot.Source != "runtime-policy" || snapshot.Stale {
		return Plan{}, &requestError{Code: "POLICY_STORE_UNAVAILABLE", Message: "runtime policy is not currently authoritative", Status: http.StatusServiceUnavailable, Retryable: true}
	}
	if request.ResourceVersion == "" || request.ResourceVersion != snapshot.ResourceVersion {
		return Plan{}, ErrStale
	}
	if violations := CeilingViolations(request.Policy, snapshot.Ceiling); len(violations) > 0 {
		return Plan{}, &CeilingError{Capabilities: violations}
	}
	effective, conditions := Intersect(request.Policy, snapshot.Ceiling, a.now())
	active, err := a.countActive(ctx)
	if err != nil {
		return Plan{}, &requestError{Code: "POLICY_STORE_UNAVAILABLE", Message: "active storage operations could not be counted", Status: http.StatusServiceUnavailable, Retryable: true}
	}
	impact := Impact{}
	if a.impactResolver != nil {
		impact = a.impactResolver(snapshot.Effective, effective)
	}
	sort.Strings(impact.ActionIDs)
	sort.Strings(impact.Roles)
	sort.Strings(impact.AddedPortableProviderIDs)
	sort.Strings(impact.RemovedPortableProviderIDs)
	broadening := broadens(snapshot.Effective, effective)
	enablesPoolDelete := !snapshot.Effective.AllowCephPoolDelete && effective.AllowCephPoolDelete
	expires := a.now().Add(challengeTTL)
	plan := Plan{
		Current: snapshot.Effective, Requested: request.Policy, Effective: effective,
		Ceiling: snapshot.Ceiling, Conditions: conditions,
		ResourceVersion: snapshot.ResourceVersion, PolicyGeneration: snapshot.Generation,
		Broadening: broadening, EnablesCephPoolDelete: enablesPoolDelete,
		Impact: impact, InFlightOperations: active, ClusterIdentity: a.clusterIdentity,
		Actor: username, RequestID: chimw.GetReqID(ctx), ChallengeExpiresAt: expires, ObservedAt: a.now(),
	}
	plan.Hash = hashPlan(plan)
	plan.Challenge = a.sign(challengePayload{
		Username: username, ClusterIdentity: a.clusterIdentity,
		ResourceVersion: snapshot.ResourceVersion, Requested: request.Policy,
		PlanHash: plan.Hash, Broadening: plan.Broadening,
		EnablesPoolDelete: plan.EnablesCephPoolDelete, Expires: expires.Unix(),
	})
	return plan, nil
}

func (a *API) idempotentReplay(username string, request ChangeRequest) (Snapshot, bool) {
	snapshot := a.store.Snapshot()
	request.Policy = Normalize(request.Policy)
	if snapshot.Source != "runtime-policy" || snapshot.Stale || !Equal(snapshot.Requested, request.Policy) ||
		snapshot.LastChange.Username != username || request.Confirmation.Challenge == "" {
		return Snapshot{}, false
	}
	payload, err := a.verifyToken(request.Confirmation.Challenge)
	if err != nil || payload.Username != username || payload.ClusterIdentity != a.clusterIdentity ||
		payload.ResourceVersion != request.ResourceVersion || !Equal(payload.Requested, request.Policy) ||
		a.now().Unix() > payload.Expires {
		return Snapshot{}, false
	}
	if payload.Broadening &&
		(!request.Confirmation.ImpactAcknowledged ||
			request.Confirmation.ClusterIdentity != a.clusterIdentity ||
			request.Confirmation.EnablePhrase != "ENABLE STORAGE CHANGES") {
		return Snapshot{}, false
	}
	if payload.EnablesPoolDelete && request.Confirmation.CephPoolPhrase != "ENABLE CEPH POOL DELETE" {
		return Snapshot{}, false
	}
	return snapshot, true
}

func (a *API) writeSnapshot(w http.ResponseWriter, r *http.Request, snapshot Snapshot) {
	active, _ := a.countActive(r.Context())
	writePolicyJSON(w, http.StatusOK, map[string]any{
		"requested": snapshot.Requested, "effective": snapshot.Effective,
		"ceiling": snapshot.Ceiling, "conditions": snapshot.Conditions,
		"source":     snapshot.Source,
		"generation": snapshot.Generation, "resourceVersion": snapshot.ResourceVersion,
		"observedGeneration": snapshot.ObservedGeneration, "inFlightOperations": active,
		"lastChange": snapshot.LastChange,
		"meta": map[string]any{
			"observedAt": snapshot.ObservedAt, "stale": snapshot.Stale,
			"partial": snapshot.Partial, "requestId": chimw.GetReqID(r.Context()),
		},
	})
}

func (a *API) verify(username string, request ChangeRequest, plan Plan) error {
	request.Policy = Normalize(request.Policy)
	if request.Confirmation.Challenge == "" {
		return &requestError{Code: "POLICY_CONFIRMATION_REQUIRED", Message: "a current server-generated policy challenge is required", Status: http.StatusBadRequest}
	}
	payload, err := a.verifyToken(request.Confirmation.Challenge)
	if err != nil {
		return err
	}
	if payload.Username != username || payload.ClusterIdentity != a.clusterIdentity || payload.ResourceVersion != plan.ResourceVersion || payload.PlanHash != plan.Hash || !Equal(payload.Requested, request.Policy) {
		return &requestError{Code: "POLICY_CHALLENGE_INVALID", Message: "policy confirmation does not match the current plan", Status: http.StatusConflict}
	}
	if a.now().Unix() > payload.Expires {
		return &requestError{Code: "POLICY_CHALLENGE_EXPIRED", Message: "policy confirmation challenge has expired", Status: http.StatusConflict}
	}
	if plan.Broadening {
		if !request.Confirmation.ImpactAcknowledged || request.Confirmation.ClusterIdentity != a.clusterIdentity || request.Confirmation.EnablePhrase != "ENABLE STORAGE CHANGES" {
			return &requestError{Code: "POLICY_CONFIRMATION_MISMATCH", Message: "impact acknowledgement, exact cluster identity, and enable phrase are required", Status: http.StatusBadRequest}
		}
	}
	if plan.EnablesCephPoolDelete && request.Confirmation.CephPoolPhrase != "ENABLE CEPH POOL DELETE" {
		return &requestError{Code: "POLICY_CONFIRMATION_MISMATCH", Message: "the Ceph pool deletion phrase is required", Status: http.StatusBadRequest}
	}
	return nil
}

func (a *API) countActive(ctx context.Context) (int, error) {
	if a.activeCount == nil {
		return 0, nil
	}
	return a.activeCount(ctx)
}

func (a *API) sign(payload challengePayload) string {
	encoded, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *API) verifyToken(token string) (challengePayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return challengePayload{}, &requestError{Code: "POLICY_CHALLENGE_INVALID", Message: "policy challenge is invalid", Status: http.StatusBadRequest}
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(parts[0]))
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return challengePayload{}, &requestError{Code: "POLICY_CHALLENGE_INVALID", Message: "policy challenge is invalid", Status: http.StatusBadRequest}
	}
	encoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return challengePayload{}, &requestError{Code: "POLICY_CHALLENGE_INVALID", Message: "policy challenge is invalid", Status: http.StatusBadRequest}
	}
	var payload challengePayload
	if json.Unmarshal(encoded, &payload) != nil {
		return challengePayload{}, &requestError{Code: "POLICY_CHALLENGE_INVALID", Message: "policy challenge is invalid", Status: http.StatusBadRequest}
	}
	return payload, nil
}

func (a *API) auditEvent(user auth.User, r *http.Request, action, result, message, planHash string, requested StoragePolicy, before ...StoragePolicy) {
	if a.audit == nil {
		return
	}
	previous := a.store.Snapshot().Effective
	if len(before) > 0 {
		previous = before[0]
	}
	effective, _ := Intersect(Normalize(requested), a.store.Snapshot().Ceiling, a.now())
	encoded, _ := json.Marshal(map[string]any{
		"message": message, "before": Normalize(previous), "requested": Normalize(requested), "effective": effective,
	})
	_ = a.audit.Append(r.Context(), audit.Event{
		Username: user.Username, Role: string(user.Role), Action: action,
		Target: "HighlandPolicy/highland", Method: r.Method, Path: r.URL.Path,
		Result: result, SourceIP: r.RemoteAddr,
		Message: string(encoded), PlanHash: planHash,
		CorrelationID: chimw.GetReqID(r.Context()), HTTPCorrelationID: chimw.GetReqID(r.Context()),
	})
}

func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var requestErr *requestError
	var ceilingErr *CeilingError
	switch {
	case errors.Is(err, ErrControlDisabled):
		writePolicyError(w, r, http.StatusConflict, "POLICY_CONTROL_DISABLED", err.Error(), false, nil)
	case errors.Is(err, ErrStale):
		writePolicyError(w, r, http.StatusConflict, "POLICY_STALE", err.Error(), false, nil)
	case errors.As(err, &ceilingErr):
		writePolicyError(w, r, http.StatusConflict, "POLICY_PERMISSION_CEILING", ceilingErr.Error(), false, map[string]any{"capabilities": ceilingErr.Capabilities})
	case errors.As(err, &requestErr):
		writePolicyError(w, r, requestErr.Status, requestErr.Code, requestErr.Message, requestErr.Retryable, nil)
	case apierrors.IsConflict(err):
		writePolicyError(w, r, http.StatusConflict, "POLICY_UPDATE_CONFLICT", "policy changed concurrently; generate a new plan", false, nil)
	default:
		writePolicyError(w, r, http.StatusServiceUnavailable, "POLICY_STORE_UNAVAILABLE", err.Error(), true, nil)
	}
}

type requestError struct {
	Code      string
	Message   string
	Status    int
	Retryable bool
}

func (e *requestError) Error() string { return e.Message }

func requirePolicyAdmin(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := appmw.UserFromContext(r.Context())
	if !ok {
		writePolicyError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", false, nil)
		return auth.User{}, false
	}
	if user.Role != auth.RoleAdmin {
		writePolicyError(w, r, http.StatusForbidden, "POLICY_FORBIDDEN", "admin role is required to change storage policy", false, nil)
		return auth.User{}, false
	}
	return user, true
}

func decodeChangeRequest(w http.ResponseWriter, r *http.Request) (ChangeRequest, bool) {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	var request ChangeRequest
	if err := decoder.Decode(&request); err != nil {
		writePolicyError(w, r, http.StatusBadRequest, "POLICY_INVALID", err.Error(), false, nil)
		return ChangeRequest{}, false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writePolicyError(w, r, http.StatusBadRequest, "POLICY_INVALID", "request must contain one JSON object", false, nil)
		return ChangeRequest{}, false
	}
	return request, true
}

func broadens(current, requested StoragePolicy) bool {
	current, requested = Normalize(current), Normalize(requested)
	return (!current.AcceptNewOperations && requested.AcceptNewOperations) ||
		(!current.PortableKubernetesWrites && requested.PortableKubernetesWrites) ||
		providerScopeBroadens(current.PortableKubernetesProviderIDs, requested.PortableKubernetesProviderIDs) ||
		(!current.LonghornWrites && requested.LonghornWrites) ||
		(!current.RookCephWrites && requested.RookCephWrites) ||
		(!current.AllowCephStorageClassDelete && requested.AllowCephStorageClassDelete) ||
		(!current.AllowCephPoolDelete && requested.AllowCephPoolDelete)
}

func providerScopeBroadens(current, requested []string) bool {
	if len(current) == 1 && current[0] == "*" {
		return false
	}
	if len(requested) == 1 && requested[0] == "*" {
		return !(len(current) == 1 && current[0] == "*")
	}
	existing := map[string]struct{}{}
	for _, providerID := range current {
		existing[providerID] = struct{}{}
	}
	for _, providerID := range requested {
		if _, ok := existing[providerID]; !ok {
			return true
		}
	}
	return false
}

func hashPlan(plan Plan) string {
	encoded, _ := json.Marshal(map[string]any{
		"current": plan.Current, "requested": plan.Requested, "effective": plan.Effective,
		"ceiling": plan.Ceiling, "resourceVersion": plan.ResourceVersion,
		"policyGeneration": plan.PolicyGeneration, "broadening": plan.Broadening,
		"enablesCephPoolDelete": plan.EnablesCephPoolDelete,
		"impact":                plan.Impact, "clusterIdentity": plan.ClusterIdentity, "actor": plan.Actor,
	})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func writePolicyJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writePolicyError(w http.ResponseWriter, r *http.Request, status int, code, message string, retryable bool, details map[string]any) {
	writePolicyJSON(w, status, map[string]any{"error": map[string]any{
		"code": code, "message": message, "retryable": retryable,
		"details": details, "requestId": chimw.GetReqID(r.Context()),
	}})
}
