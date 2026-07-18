package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/handlers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCephDashboardCredentialRevealIsAdminOnlyAuditedAndNoStore(t *testing.T) {
	deps := testDeps(t, "http://manager.example:9500")
	deps.Cfg.RookCephNamespace = "rook-ceph"
	deps.Cfg.RookCephCredentialRevealEnabled = true
	deps.Cfg.RookCephDashboardAdminUsername = "admin"
	deps.Cfg.RookCephDashboardAdminSecret = "rook-ceph-dashboard-password"
	deps.K8s = fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rook-ceph-dashboard-password", Namespace: "rook-ceph"},
		Data:       map[string][]byte{"password": []byte("ceph-test-password")},
	})
	router := handlers.NewRouter(deps)

	adminCookie := loginCookie(t, router, "admin", "highland")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/rook-ceph/dashboard-credential/reveal", nil)
	req.AddCookie(adminCookie)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin reveal: %d %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatalf("cache control = %q", rr.Header().Get("Cache-Control"))
	}
	var credential map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&credential); err != nil {
		t.Fatal(err)
	}
	if credential["username"] != "admin" || credential["password"] != "ceph-test-password" {
		t.Fatalf("credential = %#v", credential)
	}
	events := audit.ListRecent(context.Background(), deps.Audit, 10)
	found := false
	for _, event := range events {
		if event.Action == "ceph_dashboard_credential_reveal" && event.Result == "ok" {
			found = true
			if event.Message == "ceph-test-password" {
				t.Fatal("password leaked into audit event")
			}
		}
	}
	if !found {
		t.Fatalf("reveal audit event missing: %#v", events)
	}

	operatorCookie := loginCookie(t, router, "operator", "operator")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/rook-ceph/dashboard-credential/reveal", nil)
	req.AddCookie(operatorCookie)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("operator reveal = %d, want 403", rr.Code)
	}
}

func TestCephDashboardCredentialRevealDisabled(t *testing.T) {
	deps := testDeps(t, "http://manager.example:9500")
	deps.Cfg.RookCephNamespace = "rook-ceph"
	deps.Cfg.RookCephDashboardAdminUsername = "admin"
	deps.Cfg.RookCephDashboardAdminSecret = "rook-ceph-dashboard-password"
	deps.K8s = fake.NewSimpleClientset()
	router := handlers.NewRouter(deps)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/providers/rook-ceph/dashboard-credential/reveal", nil)
	req.AddCookie(loginCookie(t, router, "admin", "highland"))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("disabled reveal = %d, want 404", rr.Code)
	}
}
