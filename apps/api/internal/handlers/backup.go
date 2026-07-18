package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/middleware"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var dns1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// CreateBackupCredential POST /api/v1/backup-credential — creates (or updates) a
// Secret in the Longhorn namespace holding backup-target credentials (e.g. S3
// keys), so the backup-setup wizard can wire a backup target end-to-end without
// the admin hand-crafting a Kubernetes Secret. Admin-only; namespace-locked.
func (h *HighlandAPI) CreateBackupCredential(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFromContext(r.Context())
	if user.Role != auth.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin required"})
		return
	}
	if h.K8s == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no cluster access"})
		return
	}
	var body struct {
		Name string            `json:"name"`
		Data map[string]string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if !dns1123.MatchString(body.Name) || len(body.Name) > 253 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid secret name (lowercase dns-1123)"})
		return
	}
	if len(body.Data) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no credential data"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ns := h.longhornNamespace()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      body.Name,
			Namespace: ns,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "highland"},
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: body.Data,
	}
	secrets := h.K8s.CoreV1().Secrets(ns)
	existing, err := secrets.Get(ctx, body.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
	} else if err == nil {
		existing.StringData = body.Data
		_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if h.Audit != nil {
		_ = h.Audit.Append(r.Context(), audit.Event{
			Username: user.Username, Role: string(user.Role),
			Action: "backup_credential_create", Target: ns + "/" + body.Name,
			Method: r.Method, Path: r.URL.Path, Result: "ok", SourceIP: r.RemoteAddr,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": body.Name, "namespace": ns})
}
