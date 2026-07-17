package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	appmw "github.com/highland-io/highland/apps/api/internal/middleware"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type policyStoreStub struct {
	mu       sync.Mutex
	enabled  bool
	snapshot Snapshot
}

func (s *policyStoreStub) Enabled() bool { return s.enabled }
func (s *policyStoreStub) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}
func (s *policyStoreStub) Update(_ context.Context, requested StoragePolicy, resourceVersion, username, requestID string) (*unstructured.Unstructured, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if resourceVersion != s.snapshot.ResourceVersion {
		return nil, ErrStale
	}
	s.snapshot.Requested = requested
	s.snapshot.Effective, s.snapshot.Conditions = Intersect(requested, s.snapshot.Ceiling, time.Now())
	s.snapshot.Generation++
	s.snapshot.ObservedGeneration = s.snapshot.Generation
	s.snapshot.ResourceVersion = "rv-next"
	s.snapshot.LastChange = ChangeMetadata{Username: username, RequestID: requestID, At: time.Now()}
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": Name, "generation": s.snapshot.Generation},
	}}, nil
}
func (s *policyStoreStub) WaitObserved(_ context.Context, _ int64) (Snapshot, error) {
	return s.Snapshot(), nil
}

func TestPolicyAPIAdminPlanApplyAndHistory(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Requested: StoragePolicy{}, Effective: StoragePolicy{},
		Ceiling: Ceiling{LonghornWrites: true}, Source: "runtime-policy",
		Generation: 1, ObservedGeneration: 1, ResourceVersion: "rv-1", ObservedAt: now,
	}}
	auditStore := audit.NewStore(100, "")
	api, err := NewAPI(APIConfig{
		Store: store, Audit: auditStore, Secret: bytes.Repeat([]byte("x"), 32),
		ClusterIdentity: "lab-cluster", Now: func() time.Time { return now },
		ImpactResolver: func(_, _ StoragePolicy) Impact {
			return Impact{ActionIDs: []string{"longhorn-volume-attach"}, Roles: []string{"operator"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	router, sessions := policyTestRouter(t, api)
	unauthenticated := policyRequest(t, router, nil, http.MethodGet, "/api/v1/admin/storage-policy", nil)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	viewerCookie := policySession(t, sessions, auth.User{Username: "viewer", Role: auth.RoleViewer})
	get := policyRequest(t, router, viewerCookie, http.MethodGet, "/api/v1/admin/storage-policy", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("viewer GET status=%d body=%s", get.Code, get.Body.String())
	}
	viewerHistory := policyRequest(t, router, viewerCookie, http.MethodGet, "/api/v1/admin/storage-policy/history", nil)
	if viewerHistory.Code != http.StatusForbidden {
		t.Fatalf("viewer history status=%d body=%s", viewerHistory.Code, viewerHistory.Body.String())
	}

	operatorCookie := policySession(t, sessions, auth.User{Username: "operator", Role: auth.RoleOperator})
	denied := policyRequest(t, router, operatorCookie, http.MethodPost, "/api/v1/admin/storage-policy/plans", ChangeRequest{
		Policy: StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}, ResourceVersion: "rv-1",
	})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("operator plan status=%d body=%s", denied.Code, denied.Body.String())
	}

	adminCookie := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	request := ChangeRequest{
		Policy: StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}, ResourceVersion: "rv-1",
	}
	planned := policyRequest(t, router, adminCookie, http.MethodPost, "/api/v1/admin/storage-policy/plans", request)
	if planned.Code != http.StatusOK {
		t.Fatalf("plan status=%d body=%s", planned.Code, planned.Body.String())
	}
	var plan Plan
	if err := json.Unmarshal(planned.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Broadening || plan.ClusterIdentity != "lab-cluster" || len(plan.Impact.ActionIDs) != 1 {
		t.Fatalf("plan=%+v", plan)
	}

	request.Confirmation = Confirmation{Challenge: plan.Challenge}
	mismatch := policyRequest(t, router, adminCookie, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("missing typed confirmation status=%d body=%s", mismatch.Code, mismatch.Body.String())
	}

	request.Confirmation = Confirmation{
		Challenge: plan.Challenge, ClusterIdentity: "lab-cluster",
		EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true,
	}
	applied := policyRequest(t, router, adminCookie, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if applied.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	if !store.Snapshot().Effective.LonghornWrites {
		t.Fatal("Longhorn policy was not enabled")
	}
	retried := policyRequest(t, router, adminCookie, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if retried.Code != http.StatusOK || !bytes.Contains(retried.Body.Bytes(), []byte(`"resourceVersion":"rv-next"`)) {
		t.Fatalf("idempotent retry status=%d body=%s", retried.Code, retried.Body.String())
	}

	history := policyRequest(t, router, adminCookie, http.MethodGet, "/api/v1/admin/storage-policy/history", nil)
	if history.Code != http.StatusOK || !bytes.Contains(history.Body.Bytes(), []byte("policy_change_applied")) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
}

func TestPolicyMutationRequiresCSRFWhenMountedWithProtection(t *testing.T) {
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Source: "runtime-policy", ResourceVersion: "rv-1",
	}}
	api, _ := NewAPI(APIConfig{
		Store: store, Secret: bytes.Repeat([]byte("c"), 32), ClusterIdentity: "lab",
	})
	router, sessions := policyTestRouter(t, api)
	protected := appmw.CSRF(bytes.Repeat([]byte("s"), 32), "csrf", false, time.Hour, observability.New())(router)
	admin := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	response := policyRequest(t, protected, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", ChangeRequest{
		Policy: StoragePolicy{}, ResourceVersion: "rv-1",
	})
	if response.Code != http.StatusForbidden || !bytes.Contains(response.Body.Bytes(), []byte("csrf token invalid")) {
		t.Fatalf("missing CSRF status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPolicyAPIRejectsCeilingStaleAndCrossUserChallenge(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Ceiling: Ceiling{LonghornWrites: true}, Source: "runtime-policy",
		Generation: 1, ObservedGeneration: 1, ResourceVersion: "rv-1", ObservedAt: now,
	}}
	api, _ := NewAPI(APIConfig{
		Store: store, Secret: bytes.Repeat([]byte("y"), 32),
		ClusterIdentity: "lab", Now: func() time.Time { return now },
	})
	router, sessions := policyTestRouter(t, api)
	admin := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})

	ceiling := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", ChangeRequest{
		Policy: StoragePolicy{AcceptNewOperations: true, RookCephWrites: true}, ResourceVersion: "rv-1",
	})
	if ceiling.Code != http.StatusConflict || !bytes.Contains(ceiling.Body.Bytes(), []byte("POLICY_PERMISSION_CEILING")) {
		t.Fatalf("ceiling status=%d body=%s", ceiling.Code, ceiling.Body.String())
	}
	stale := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", ChangeRequest{
		Policy: StoragePolicy{}, ResourceVersion: "old",
	})
	if stale.Code != http.StatusConflict || !bytes.Contains(stale.Body.Bytes(), []byte("POLICY_STALE")) {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}

	request := ChangeRequest{
		Policy: StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}, ResourceVersion: "rv-1",
	}
	planned := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", request)
	var plan Plan
	_ = json.Unmarshal(planned.Body.Bytes(), &plan)
	other := policySession(t, sessions, auth.User{Username: "other-admin", Role: auth.RoleAdmin})
	request.Confirmation = Confirmation{
		Challenge: plan.Challenge, ClusterIdentity: "lab",
		EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true,
	}
	replay := policyRequest(t, router, other, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if replay.Code != http.StatusConflict || !bytes.Contains(replay.Body.Bytes(), []byte("POLICY_CHALLENGE_INVALID")) {
		t.Fatalf("cross-user replay status=%d body=%s", replay.Code, replay.Body.String())
	}
}

func TestPolicyAPIRejectsExpiredTamperedAndUnknownConfirmation(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Ceiling: Ceiling{LonghornWrites: true}, Source: "runtime-policy",
		Generation: 1, ObservedGeneration: 1, ResourceVersion: "rv-1", ObservedAt: now,
	}}
	api, _ := NewAPI(APIConfig{
		Store: store, Secret: bytes.Repeat([]byte("z"), 32),
		ClusterIdentity: "lab", Now: func() time.Time { return now },
	})
	router, sessions := policyTestRouter(t, api)
	admin := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	request := ChangeRequest{
		Policy: StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}, ResourceVersion: "rv-1",
	}
	planned := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", request)
	var plan Plan
	if err := json.Unmarshal(planned.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	request.Confirmation = Confirmation{
		Challenge: plan.Challenge + "tampered", ClusterIdentity: "lab",
		EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true,
	}
	tampered := policyRequest(t, router, admin, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if tampered.Code != http.StatusBadRequest || !bytes.Contains(tampered.Body.Bytes(), []byte("POLICY_CHALLENGE_INVALID")) {
		t.Fatalf("tampered status=%d body=%s", tampered.Code, tampered.Body.String())
	}
	request.Confirmation.Challenge = plan.Challenge
	now = now.Add(challengeTTL + time.Second)
	expired := policyRequest(t, router, admin, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if expired.Code != http.StatusConflict || !bytes.Contains(expired.Body.Bytes(), []byte("POLICY_CHALLENGE_EXPIRED")) {
		t.Fatalf("expired status=%d body=%s", expired.Code, expired.Body.String())
	}

	unknownBody := []byte(`{"policy":{},"resourceVersion":"rv-1","unexpected":true}`)
	raw := httptest.NewRequest(http.MethodPost, "/api/v1/admin/storage-policy/plans", bytes.NewReader(unknownBody))
	raw.AddCookie(admin)
	raw.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, raw)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte("POLICY_INVALID")) {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPolicyAPICephPoolDeleteRequiresIndependentPhrase(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Ceiling: Ceiling{RookCephWrites: true, AllowCephPoolDelete: true}, Source: "runtime-policy",
		Generation: 1, ObservedGeneration: 1, ResourceVersion: "rv-1", ObservedAt: now,
	}}
	api, _ := NewAPI(APIConfig{
		Store: store, Secret: bytes.Repeat([]byte("p"), 32),
		ClusterIdentity: "ceph-lab", Now: func() time.Time { return now },
	})
	router, sessions := policyTestRouter(t, api)
	admin := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	request := ChangeRequest{
		Policy: StoragePolicy{
			AcceptNewOperations: true, RookCephWrites: true, AllowCephPoolDelete: true,
		},
		ResourceVersion: "rv-1",
	}
	planned := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", request)
	var plan Plan
	_ = json.Unmarshal(planned.Body.Bytes(), &plan)
	request.Confirmation = Confirmation{
		Challenge: plan.Challenge, ClusterIdentity: "ceph-lab",
		EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true,
	}
	missing := policyRequest(t, router, admin, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if missing.Code != http.StatusBadRequest || !bytes.Contains(missing.Body.Bytes(), []byte("POLICY_CONFIRMATION_MISMATCH")) {
		t.Fatalf("missing Ceph phrase status=%d body=%s", missing.Code, missing.Body.String())
	}
	request.Confirmation.CephPoolPhrase = "ENABLE CEPH POOL DELETE"
	applied := policyRequest(t, router, admin, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if applied.Code != http.StatusOK {
		t.Fatalf("Ceph apply status=%d body=%s", applied.Code, applied.Body.String())
	}
}

func TestPolicyAPIControlDisabled(t *testing.T) {
	store := &policyStoreStub{enabled: false, snapshot: Snapshot{Source: "static-helm"}}
	api, _ := NewAPI(APIConfig{
		Store: store, Secret: bytes.Repeat([]byte("d"), 32), ClusterIdentity: "lab",
	})
	router, sessions := policyTestRouter(t, api)
	admin := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	response := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", ChangeRequest{})
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("POLICY_CONTROL_DISABLED")) {
		t.Fatalf("disabled status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPolicyAPIConcurrentConflictingUpdatesHaveOneWinner(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Ceiling: Ceiling{PortableKubernetesWrites: true, LonghornWrites: true},
		Source:  "runtime-policy", Generation: 1, ObservedGeneration: 1,
		ResourceVersion: "rv-1", ObservedAt: now,
	}}
	api, _ := NewAPI(APIConfig{
		Store: store, Secret: bytes.Repeat([]byte("q"), 32),
		ClusterIdentity: "lab", Now: func() time.Time { return now },
	})
	router, sessions := policyTestRouter(t, api)
	admin := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	requests := []ChangeRequest{
		{Policy: StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}, ResourceVersion: "rv-1"},
		{Policy: StoragePolicy{AcceptNewOperations: true, PortableKubernetesWrites: true}, ResourceVersion: "rv-1"},
	}
	for index := range requests {
		planned := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", requests[index])
		var plan Plan
		if err := json.Unmarshal(planned.Body.Bytes(), &plan); err != nil {
			t.Fatal(err)
		}
		requests[index].Confirmation = Confirmation{
			Challenge: plan.Challenge, ClusterIdentity: "lab",
			EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true,
		}
	}
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	for _, change := range requests {
		change := change
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			encoded, _ := json.Marshal(change)
			request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/storage-policy", bytes.NewReader(encoded))
			request.AddCookie(admin)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			statuses <- response.Code
		}()
	}
	close(start)
	workers.Wait()
	close(statuses)
	ok, conflict := 0, 0
	for status := range statuses {
		if status == http.StatusOK {
			ok++
		}
		if status == http.StatusConflict {
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent statuses: ok=%d conflict=%d", ok, conflict)
	}
}

func TestPolicyAPIProviderScopeIsNormalizedAndBoundToChallenge(t *testing.T) {
	now := time.Date(2026, 7, 17, 1, 0, 0, 0, time.UTC)
	current := StoragePolicy{
		AcceptNewOperations: true, PortableKubernetesWrites: true,
		PortableKubernetesProviderIDs: []string{"longhorn"},
	}
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Requested: current, Effective: current,
		Ceiling: Ceiling{PortableKubernetesWrites: true}, Source: "runtime-policy",
		Generation: 3, ObservedGeneration: 3, ResourceVersion: "rv-3", ObservedAt: now,
	}}
	api, _ := NewAPI(APIConfig{
		Store: store, Secret: bytes.Repeat([]byte("p"), 32), ClusterIdentity: "lab",
		Now: func() time.Time { return now },
		ImpactResolver: func(before, after StoragePolicy) Impact {
			return Impact{ActionIDs: []string{}, Roles: []string{}, AddedPortableProviderIDs: []string{"rook-ceph"}, RemovedPortableProviderIDs: []string{}}
		},
	})
	router, sessions := policyTestRouter(t, api)
	admin := policySession(t, sessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	request := ChangeRequest{Policy: StoragePolicy{
		AcceptNewOperations: true, PortableKubernetesWrites: true,
		PortableKubernetesProviderIDs: []string{"rook-ceph", "longhorn", "rook-ceph"},
	}, ResourceVersion: "rv-3"}
	planned := policyRequest(t, router, admin, http.MethodPost, "/api/v1/admin/storage-policy/plans", request)
	if planned.Code != http.StatusOK {
		t.Fatalf("plan status=%d body=%s", planned.Code, planned.Body.String())
	}
	var plan Plan
	if err := json.Unmarshal(planned.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Broadening || len(plan.Requested.PortableKubernetesProviderIDs) != 2 || plan.Requested.PortableKubernetesProviderIDs[0] != "longhorn" || plan.Impact.AddedPortableProviderIDs[0] != "rook-ceph" {
		t.Fatalf("provider-scoped plan=%#v", plan)
	}
	request.Policy.PortableKubernetesProviderIDs = []string{"longhorn", "openebs"}
	request.Confirmation = Confirmation{Challenge: plan.Challenge, ClusterIdentity: "lab", EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true}
	tampered := policyRequest(t, router, admin, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if tampered.Code != http.StatusConflict || !bytes.Contains(tampered.Body.Bytes(), []byte("POLICY_CHALLENGE_INVALID")) {
		t.Fatalf("provider-list tamper status=%d body=%s", tampered.Code, tampered.Body.String())
	}
	request.Policy.PortableKubernetesProviderIDs = []string{"rook-ceph", "longhorn"}
	reordered := policyRequest(t, router, admin, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if reordered.Code != http.StatusOK {
		t.Fatalf("semantically identical reordered scope status=%d body=%s", reordered.Code, reordered.Body.String())
	}
}

func TestPolicyAPIRejectsCrossClusterChallenge(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	store := &policyStoreStub{enabled: true, snapshot: Snapshot{
		Ceiling: Ceiling{LonghornWrites: true}, Source: "runtime-policy",
		Generation: 1, ObservedGeneration: 1, ResourceVersion: "rv-1", ObservedAt: now,
	}}
	secret := bytes.Repeat([]byte("k"), 32)
	sourceAPI, _ := NewAPI(APIConfig{Store: store, Secret: secret, ClusterIdentity: "cluster-a", Now: func() time.Time { return now }})
	sourceRouter, sourceSessions := policyTestRouter(t, sourceAPI)
	sourceAdmin := policySession(t, sourceSessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	request := ChangeRequest{
		Policy: StoragePolicy{AcceptNewOperations: true, LonghornWrites: true}, ResourceVersion: "rv-1",
	}
	planned := policyRequest(t, sourceRouter, sourceAdmin, http.MethodPost, "/api/v1/admin/storage-policy/plans", request)
	var plan Plan
	_ = json.Unmarshal(planned.Body.Bytes(), &plan)

	targetAPI, _ := NewAPI(APIConfig{Store: store, Secret: secret, ClusterIdentity: "cluster-b", Now: func() time.Time { return now }})
	targetRouter, targetSessions := policyTestRouter(t, targetAPI)
	targetAdmin := policySession(t, targetSessions, auth.User{Username: "admin", Role: auth.RoleAdmin})
	request.Confirmation = Confirmation{
		Challenge: plan.Challenge, ClusterIdentity: "cluster-b",
		EnablePhrase: "ENABLE STORAGE CHANGES", ImpactAcknowledged: true,
	}
	response := policyRequest(t, targetRouter, targetAdmin, http.MethodPut, "/api/v1/admin/storage-policy", request)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("POLICY_CHALLENGE_INVALID")) {
		t.Fatalf("cross-cluster status=%d body=%s", response.Code, response.Body.String())
	}
}

func FuzzPolicyRequestAndChallengeDecoding(f *testing.F) {
	f.Add([]byte(`{"policy":{},"resourceVersion":"1"}`), "invalid")
	api, _ := NewAPI(APIConfig{
		Store: &policyStoreStub{}, Secret: bytes.Repeat([]byte("f"), 32), ClusterIdentity: "fuzz",
	})
	f.Fuzz(func(t *testing.T, body []byte, token string) {
		request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		response := httptest.NewRecorder()
		_, _ = decodeChangeRequest(response, request)
		_, _ = api.verifyToken(token)
	})
}

func policyTestRouter(t *testing.T, api *API) (*chi.Mux, *auth.Store) {
	t.Helper()
	sessions := auth.NewStoreFromBackend(auth.NewMemoryBackend(), time.Hour)
	router := chi.NewRouter()
	router.Use(chimw.RequestID)
	router.Use(appmw.SessionAuth(sessions, "session", observability.New()))
	api.Mount(router)
	return router, sessions
}

func policySession(t *testing.T, sessions *auth.Store, user auth.User) *http.Cookie {
	t.Helper()
	id, err := sessions.Create(user)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "session", Value: id}
}

func policyRequest(t *testing.T, router http.Handler, cookie *http.Cookie, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		encoded, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
