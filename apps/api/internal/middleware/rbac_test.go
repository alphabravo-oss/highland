package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/observability"
)

func TestPolicyReadIsAuthenticatedButPolicyMutationAndHistoryRemainAdminOnly(t *testing.T) {
	handler := RequireRole(nil, observability.New())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	request := func(role auth.Role, method, path string) int {
		req := httptest.NewRequest(method, path, nil)
		req = req.WithContext(context.WithValue(req.Context(), userCtxKey, auth.User{Username: "test", Role: role}))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response.Code
	}
	if status := request(auth.RoleViewer, http.MethodGet, "/api/v1/admin/storage-policy"); status != http.StatusOK {
		t.Fatalf("viewer policy read status=%d", status)
	}
	if status := request(auth.RoleViewer, http.MethodGet, "/api/v1/admin/storage-policy/history"); status != http.StatusForbidden {
		t.Fatalf("viewer history status=%d", status)
	}
	if status := request(auth.RoleOperator, http.MethodPost, "/api/v1/admin/storage-policy/plans"); status != http.StatusForbidden {
		t.Fatalf("operator plan status=%d", status)
	}
	if status := request(auth.RoleAdmin, http.MethodPut, "/api/v1/admin/storage-policy"); status != http.StatusOK {
		t.Fatalf("admin apply status=%d", status)
	}
}
