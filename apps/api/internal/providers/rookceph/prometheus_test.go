package rookceph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPrometheusUsesOnlyAllowlistedQueries(t *testing.T) {
	if _, err := NewPrometheusClient("https://reader:password@prometheus.example", nil); err == nil {
		t.Fatal("expected URL userinfo to be rejected")
	}
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Query().Get("query")] = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[1,"42"]}]}}`))
	}))
	defer server.Close()
	client, err := NewPrometheusClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil || len(snapshot.Values) != len(cephQueries) {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	for _, query := range cephQueries {
		if !seen[query] {
			t.Fatalf("missing allowlisted query %q", query)
		}
	}
	for query := range seen {
		if strings.Contains(query, "label_values") {
			t.Fatalf("unexpected unbounded query %q", query)
		}
	}
}

func TestPrometheusDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/metadata", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewPrometheusClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Snapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("redirect error=%v", err)
	}
	if redirected.Load() != 0 {
		t.Fatal("Prometheus client followed a redirect to another origin")
	}
}

func TestPrometheusReturnsFreshPartialSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Query().Get("query"), "ceph_health_status") {
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[1,"0"]}]}}`))
			return
		}
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewPrometheusClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil || snapshot.Stale || snapshot.Values["healthStatus"] != "0" {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
	if len(snapshot.Unavailable) != len(cephQueries)-1 {
		t.Fatalf("unavailable=%v", snapshot.Unavailable)
	}
	if snapshot.Failures != len(cephQueries)-1 || len(snapshot.QueryDurationSeconds) != len(cephQueries) || snapshot.LastSuccess.IsZero() {
		t.Fatalf("query observations were not recorded: %#v", snapshot)
	}
}
