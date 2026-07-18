package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Status GET /api/v1/status — consolidated versions, component health, and
// runtime facts for the About/Status page.
func (h *HighlandAPI) Status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	k8sVersion := ""
	longhornVersion := ""
	managerReachable := false
	if h.K8s != nil {
		if v, err := h.K8s.Discovery().ServerVersion(); err == nil {
			k8sVersion = v.GitVersion
		}
		if h.LonghornEnabled {
			longhornVersion = longhornManagerVersion(ctx, h.K8s, h.longhornNamespace())
		}
	}
	if h.LonghornEnabled {
		managerReachable = h.managerReachable(ctx)
	}

	scrapeErr := ""
	if h.Metrics != nil {
		scrapeErr = h.Metrics.LastError()
	}

	resp := map[string]any{
		"highland": map[string]any{
			"version":        orUnknown(h.Version),
			"sessionBackend": orUnknown(h.SessionBackend),
			"benchmarkMode":  orUnknown(h.BenchmarkMode),
		},
		"longhorn": map[string]any{
			"enabled":    h.LonghornEnabled,
			"version":    orUnknown(longhornVersion),
			"namespace":  h.longhornNamespace(),
			"managerUrl": h.ManagerURL,
			"reachable":  managerReachable,
			"supported":  []string{"1.12.x", "1.11.x"},
		},
		"kubernetes": map[string]any{
			"version": orUnknown(k8sVersion),
		},
		"compatibility": map[string]any{
			"releaseLine": "0.3.x-storage-preview",
			"lastUpdated": "2026-07-18",
			"kubernetes":  map[string]any{"minimum": "1.34", "maximum": "1.36"},
			"providers": map[string]any{
				"longhorn":    map[string]any{"stage": "managed", "tested": "1.11.x – 1.12.x"},
				"rook-ceph":   map[string]any{"stage": "preview", "tested": "Rook 1.19.6 / 1.20.2 · Ceph 19.2.3 / 20.2.1"},
				"openebs":     map[string]any{"stage": "preview", "tested": "OpenEBS 4.5.1"},
				"linstor":     map[string]any{"stage": "preview", "tested": "Piraeus 2.10.8 · LINSTOR 1.33.3 · CSI 1.11.3"},
				"generic-csi": map[string]any{"stage": "detected", "tested": "Kubernetes inventory contract"},
			},
		},
		"components": map[string]any{
			"api":            "ok",
			"managerProxy":   componentStatus(h.LonghornEnabled, managerReachable),
			"metricsScraper": componentStatus(h.LonghornEnabled, scrapeErr == ""),
			"scrapeError":    scrapeErr,
		},
		"vendor": map[string]any{
			"name":    "AlphaBravo",
			"url":     "https://alphabravo.io",
			"tagline": "Highland is an enterprise storage operations manager for Kubernetes CSI providers, developed by AlphaBravo.",
		},
	}
	if h.Storage != nil {
		resp["storage"] = h.Storage.Status(r.Context())
	}
	if h.Policy != nil {
		snapshot := h.Policy.Snapshot()
		resp["storagePolicy"] = map[string]any{
			"source": snapshot.Source, "effective": snapshot.Effective,
			"generation": snapshot.Generation, "observedGeneration": snapshot.ObservedGeneration,
			"observedAt": snapshot.ObservedAt, "stale": snapshot.Stale, "partial": snapshot.Partial,
			"conditions": snapshot.Conditions,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func componentStatus(enabled, healthy bool) string {
	if !enabled {
		return "disabled"
	}
	return boolStatus(healthy)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "error"
}

// longhornNamespace derives the Longhorn namespace: explicit value if set,
// else the second DNS label of the manager URL host (…-backend.<ns>.svc…).
func (h *HighlandAPI) longhornNamespace() string {
	if h.LonghornNamespace != "" {
		return h.LonghornNamespace
	}
	if u, err := url.Parse(h.ManagerURL); err == nil {
		parts := strings.Split(u.Hostname(), ".")
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return "longhorn-system"
}

// managerReachable does a fast bounded GET against the Longhorn manager.
func (h *HighlandAPI) managerReachable(ctx context.Context) bool {
	if h.ManagerURL == "" {
		return false
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, strings.TrimRight(h.ManagerURL, "/")+"/v1", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// longhornManagerVersion reads the longhorn-manager image tag from its DaemonSet.
func longhornManagerVersion(ctx context.Context, client kubernetes.Interface, ns string) string {
	ds, err := client.AppsV1().DaemonSets(ns).Get(ctx, "longhorn-manager", metav1.GetOptions{})
	if err != nil {
		return ""
	}
	for _, c := range ds.Spec.Template.Spec.Containers {
		if i := strings.LastIndex(c.Image, ":"); i >= 0 && i+1 < len(c.Image) {
			return c.Image[i+1:]
		}
	}
	return ""
}
