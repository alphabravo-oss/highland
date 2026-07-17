package linstor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientUsesFixedReadOnlyPathsAndBearerToken(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/v1/nodes" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q", got)
		}
		_, _ = w.Write([]byte(`[{"name":"satellite-a"}]`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{URL: server.URL, Token: "secret", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.List(context.Background(), "nodes")
	if err != nil || len(items) != 1 || items[0]["name"] != "satellite-a" {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if _, err := client.List(context.Background(), "../../secrets"); err == nil {
		t.Fatal("arbitrary path was accepted")
	}
}

func TestClientBoundsResponsesAndDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	redirectTargetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectTargetCalled = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/nodes" {
			http.Redirect(w, r, target.URL, http.StatusFound)
			return
		}
		_, _ = fmt.Fprint(w, strings.Repeat("x", maxResponseBytes+1))
	}))
	defer server.Close()
	client, _ := NewClient(ClientConfig{URL: server.URL, Timeout: time.Second})
	if _, err := client.List(context.Background(), "nodes"); err == nil || redirectTargetCalled {
		t.Fatalf("redirect err=%v targetCalled=%t", err, redirectTargetCalled)
	}
	if _, err := client.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversize err=%v", err)
	}
}

func TestNewClientRejectsUnsafeURL(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"file:///tmp/controller", "https://user:pass@example.test", "https://example.test?next=evil"} {
		if _, err := NewClient(ClientConfig{URL: value}); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestClientNormalizesWrappedScheduleAndRemoteLists(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/schedules" {
			_, _ = w.Write([]byte(`{"data":[{"schedule_name":"daily"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"s3_remotes":[{"remote_name":"archive"}],"linstor_remotes":[]}`))
	}))
	defer server.Close()
	client, _ := NewClient(ClientConfig{URL: server.URL})
	schedules, err := client.List(context.Background(), "schedules")
	if err != nil || len(schedules) != 1 || schedules[0]["schedule_name"] != "daily" {
		t.Fatalf("schedules=%#v err=%v", schedules, err)
	}
	remotes, err := client.List(context.Background(), "remotes")
	if err != nil || len(remotes) != 1 || remotes[0]["remote_type"] != "s3" {
		t.Fatalf("remotes=%#v err=%v", remotes, err)
	}
}

func TestClientRedactsUnauthorizedResponseAndCachesSuccess(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer correct-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"secret-upstream-detail"}`))
			return
		}
		_, _ = w.Write([]byte(`[{"name":"node-a"}]`))
	}))
	defer server.Close()

	bad, _ := NewClient(ClientConfig{URL: server.URL, Token: "wrong-token"})
	if _, err := bad.List(context.Background(), "nodes"); err == nil || strings.Contains(err.Error(), "secret-upstream-detail") || strings.Contains(err.Error(), "wrong-token") {
		t.Fatalf("unauthorized error was not bounded/redacted: %v", err)
	}
	good, _ := NewClient(ClientConfig{URL: server.URL, Token: "correct-token"})
	if _, err := good.List(context.Background(), "nodes"); err != nil {
		t.Fatal(err)
	}
	if _, err := good.List(context.Background(), "nodes"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 { // one rejected request plus one successful cached request
		t.Fatalf("requests=%d, want 2", requests)
	}
}
