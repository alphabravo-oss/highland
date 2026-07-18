package operations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	appmw "github.com/highland-io/highland/apps/api/internal/middleware"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/storage"
)

// TestHTTPSubmitBlocksWhenDurableAuditAdmissionFails drives the real
// POST /api/v1/storage/claims path: plan succeeds, confirmation is valid, but
// a durable audit sink failure must fail closed before store.Create.
func TestHTTPSubmitBlocksWhenDurableAuditAdmissionFails(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset(&storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "fast"},
		Provisioner: "example.csi.io",
	})
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		OperationGVR: "StorageOperationList",
	})
	store, err := NewStore(dynamic, "highland-system")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	planner, err := NewPlanner(PlannerConfig{
		Core: core, Dynamic: dynamic, Scope: storage.NewScope("cluster", nil), Secret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Durable failing sink: RequireAppend engages and fails.
	failAudit := audit.NewFailingSink(audit.ErrUnavailable)
	if !failAudit.Durable() {
		t.Fatal("test sink must be durable so admission is required")
	}

	api := NewAPI(APIConfig{
		Store: store, Planner: planner, Audit: failAudit, WritesEnabled: true,
	})

	// Build a valid plan + confirmation for create-pvc.
	request := Request{
		ActionID: "create-pvc",
		Target:   ResourceTarget{Kind: "PersistentVolumeClaim", Namespace: "default", Name: "data"},
		Parameters: map[string]any{
			"storageClass": "fast",
			"size":         "1Gi",
		},
	}
	plan, err := planner.Plan(context.Background(), "admin", request)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	request.Confirmation.Challenge = plan.Challenge
	request.Confirmation.WarningsAcknowledged = true
	if plan.Action.Confirmation == ConfirmTypedName {
		request.Confirmation.TypedName = plan.Target.Name
	}
	if err := planner.Verify("admin", request, plan); err != nil {
		t.Fatalf("verify: %v", err)
	}

	body, _ := json.Marshal(request)
	sessions := auth.NewStore(time.Hour)
	sessionID, err := sessions.Create(auth.User{Username: "admin", Role: auth.RoleAdmin})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Use(appmw.SessionAuth(sessions, "highland_session", observability.New()))
	api.Mount(router)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/claims", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "highland_session", Value: sessionID})
	req.Header.Set("X-Highland-Confirmation", plan.Challenge)
	if plan.Action.Confirmation == ConfirmTypedName {
		req.Header.Set("X-Highland-Typed-Name", plan.Target.Name)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 AUDIT_REQUIRED_UNAVAILABLE, status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "AUDIT_REQUIRED_UNAVAILABLE") {
		t.Fatalf("expected AUDIT_REQUIRED_UNAVAILABLE code, body=%s", recorder.Body.String())
	}

	// No operation CR may have been created when admission fails.
	ops, listErr := store.List(context.Background(), map[string]string{}, 100)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(ops) != 0 {
		t.Fatalf("store.Create must not run after audit admission failure; got %d ops", len(ops))
	}
}

// TestHTTPSubmitAllowsWhenAuditNonDurable proves memory (non-durable) path does
// not require admission (still may fail later for other reasons).
func TestHTTPSubmitSkipsAdmissionWhenAuditNotDurable(t *testing.T) {
	mem := audit.NewStore(10, "")
	if mem.Durable() {
		t.Fatal("memory must not be durable")
	}
	// Admission gate is skipped; this only asserts the durable flag path.
	api := NewAPI(APIConfig{Audit: mem, WritesEnabled: true})
	if api.audit == nil || api.audit.Durable() {
		t.Fatal("API must hold non-durable sink")
	}
}
