package longhorn_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/handlers"
	"github.com/highland-io/highland/apps/api/internal/longhorn"
)

// Fixture manager implements the collection surface used by Phase 1 UI.
func fixtureManager() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		base := "http://" + r.Host
		path := r.URL.Path

		collection := func(resourceType string, data any) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":         "collection",
				"resourceType": resourceType,
				"data":         data,
				"links":        map[string]string{"self": base + path},
			})
		}

		switch {
		case path == "/v1/dashboard" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type": "dashboard",
				"volumes": map[string]int{
					"total": 1, "healthy": 1, "degraded": 0, "faulted": 0, "detached": 0,
				},
				"nodes": map[string]int{"total": 1, "ready": 1},
				"storage": map[string]int{
					"total": 100 * 1024 * 1024 * 1024, "used": 10 * 1024 * 1024 * 1024, "available": 90 * 1024 * 1024 * 1024,
				},
			})
		case path == "/v1/volumes" && r.Method == http.MethodGet:
			collection("volume", []any{
				map[string]any{
					"id": "vol-1", "type": "volume", "name": "pvc-db",
					"size": "10737418240", "state": "detached", "robustness": "healthy",
					"numberOfReplicas": 3, "dataEngine": "v1",
					"links": map[string]string{"self": base + "/v1/volumes/vol-1"},
					"actions": map[string]string{
						"attach":         base + "/v1/volumes/vol-1?action=attach",
						"snapshotCreate": base + "/v1/volumes/vol-1?action=snapshotCreate",
					},
				},
			})
		case path == "/v1/volumes" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			name, _ := body["name"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": name, "type": "volume", "name": name,
				"state": "detached", "robustness": "unknown",
				"links":   map[string]string{"self": base + "/v1/volumes/" + name},
				"actions": map[string]string{},
			})
		case strings.HasPrefix(path, "/v1/volumes/") && r.Method == http.MethodGet:
			name := strings.TrimPrefix(path, "/v1/volumes/")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": name, "type": "volume", "name": name,
				"size": "10737418240", "state": "detached", "robustness": "healthy",
				"numberOfReplicas": 3,
				"replicas": []any{
					map[string]any{"name": name + "-r-a", "hostId": "node-1", "running": true, "mode": "RW"},
				},
				"conditions": []any{
					map[string]any{"type": "Scheduled", "status": "True", "message": "Replica scheduling success"},
				},
				"links": map[string]string{"self": base + path},
				"actions": map[string]string{
					"attach": base + path + "?action=attach",
					"detach": base + path + "?action=detach",
				},
			})
		case strings.HasPrefix(path, "/v1/volumes/") && r.Method == http.MethodPost && r.URL.Query().Get("action") != "":
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "volume", "id": "vol-1", "name": "pvc-db", "state": "attached"})
		case path == "/v1/nodes" && r.Method == http.MethodGet:
			collection("node", []any{
				map[string]any{
					"id": "node-1", "type": "node", "name": "node-1", "address": "10.0.0.1",
					"allowScheduling": true,
					"conditions":      []any{map[string]any{"type": "Ready", "status": "True"}},
					"disks": map[string]any{
						"default-disk": map[string]any{
							"path": "/var/lib/longhorn", "allowScheduling": true,
							"storageAvailable": 90 * 1024 * 1024 * 1024,
							"storageMaximum":   100 * 1024 * 1024 * 1024,
							"storageScheduled": 10 * 1024 * 1024 * 1024,
							"tags":             []string{"ssd"},
						},
					},
					"tags":  []string{"worker"},
					"links": map[string]string{"self": base + "/v1/nodes/node-1"},
				},
			})
		case path == "/v1/settings" && r.Method == http.MethodGet:
			collection("setting", []any{
				map[string]any{
					"id": "default-replica-count", "type": "setting", "name": "default-replica-count",
					"value": "3",
					"definition": map[string]any{
						"displayName": "Default Replica Count",
						"category":    "general",
						"description": "Default number of replicas",
						"type":        "int",
					},
					"links": map[string]string{"self": base + "/v1/settings/default-replica-count"},
				},
			})
		case path == "/v1/backupvolumes":
			collection("backupVolume", []any{})
		case path == "/v1/backuptargets":
			collection("backupTarget", []any{})
		case path == "/v1/recurringjobs":
			collection("recurringJob", []any{})
		case path == "/v1/engineimages":
			collection("engineImage", []any{
				map[string]any{
					"id": "ei-default", "type": "engineImage", "name": "ei-default",
					"image": "longhornio/longhorn-engine:v1.12.0", "state": "ready", "default": true,
				},
			})
		case path == "/v1/backingimages", path == "/v1/instancemanagers", path == "/v1/orphans",
			path == "/v1/systembackups", path == "/v1/systemrestores", path == "/v1/supportbundles", path == "/v1/events":
			collection(strings.TrimPrefix(path, "/v1/"), []any{})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestPhase1CollectionsThroughProxy(t *testing.T) {
	upstream := fixtureManager()
	defer upstream.Close()

	_ = os.Setenv("HIGHLAND_DEV_ROLES", "1")
	cfg := &config.Config{
		ListenAddr:        ":0",
		ManagerURL:        upstream.URL,
		BootstrapUsername: "admin",
		BootstrapPassword: "highland",
		SessionTTL:        time.Hour,
		CookieName:        "highland_session",
		OIDCMock:          true,
		Version:           "test",
	}
	users := auth.NewUserStoreFromEnv(cfg.BootstrapUsername, cfg.BootstrapPassword)
	store := auth.NewStore(cfg.SessionTTL)
	authenticator := auth.NewAuthenticator(users, store)
	proxy, err := longhorn.NewProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	h := handlers.NewRouter(handlers.Deps{
		Cfg:   cfg,
		Auth:  authenticator,
		Proxy: proxy,
		Audit: nil,
	})

	// login
	loginReq := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"admin","password":"highland"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRR := httptest.NewRecorder()
	h.ServeHTTP(loginRR, loginReq)
	if loginRR.Code != 200 {
		t.Fatalf("login %d", loginRR.Code)
	}
	var cookie *http.Cookie
	for _, c := range loginRR.Result().Cookies() {
		if c.Name == "highland_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no cookie")
	}

	paths := []string{
		"/api/v1/lh/dashboard",
		"/api/v1/lh/volumes",
		"/api/v1/lh/volumes/vol-1",
		"/api/v1/lh/nodes",
		"/api/v1/lh/settings",
		"/api/v1/lh/backupvolumes",
		"/api/v1/lh/backuptargets",
		"/api/v1/lh/recurringjobs",
		"/api/v1/lh/engineimages",
		"/api/v1/lh/backingimages",
		"/api/v1/lh/instancemanagers",
		"/api/v1/lh/orphans",
		"/api/v1/lh/systembackups",
		"/api/v1/lh/supportbundles",
	}
	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s => %d body=%s", p, rr.Code, rr.Body.String())
		}
	}

	// create volume
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/lh/volumes", strings.NewReader(`{"name":"new-vol","size":"1073741824","numberOfReplicas":3}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(cookie)
	createRR := httptest.NewRecorder()
	h.ServeHTTP(createRR, createReq)
	if createRR.Code != 200 {
		t.Fatalf("create volume %d %s", createRR.Code, createRR.Body.String())
	}
	var created map[string]any
	if err := json.NewDecoder(createRR.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created["name"] != "new-vol" {
		t.Fatalf("created name %v", created["name"])
	}

	// action attach
	actionReq := httptest.NewRequest(http.MethodPost, "/api/v1/lh/volumes/vol-1?action=attach", strings.NewReader(`{"hostId":"node-1"}`))
	actionReq.Header.Set("Content-Type", "application/json")
	actionReq.AddCookie(cookie)
	actionRR := httptest.NewRecorder()
	h.ServeHTTP(actionRR, actionReq)
	if actionRR.Code != 200 {
		t.Fatalf("attach %d %s", actionRR.Code, actionRR.Body.String())
	}
}
