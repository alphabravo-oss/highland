package kube

import (
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func TestNewRejectsNilConfig(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil config error")
	}
}

func TestKubernetesClientEnvironmentBounds(t *testing.T) {
	t.Setenv("HIGHLAND_KUBE_QPS", "75.5")
	qps, err := envFloat32("HIGHLAND_KUBE_QPS", 0, defaultQPS, 1, 1000)
	if err != nil || qps != 75.5 {
		t.Fatalf("qps=%v err=%v", qps, err)
	}
	t.Setenv("HIGHLAND_KUBE_BURST", "0")
	if _, err := envInt("HIGHLAND_KUBE_BURST", 0, defaultBurst, 1, 5000); err == nil {
		t.Fatal("expected out-of-range burst error")
	}
	t.Setenv("HIGHLAND_KUBE_TIMEOUT", "45s")
	timeout, err := envDuration("HIGHLAND_KUBE_TIMEOUT", 0, defaultTimeout, time.Second, 5*time.Minute)
	if err != nil || timeout != 45*time.Second {
		t.Fatalf("timeout=%v err=%v", timeout, err)
	}
}

func TestBoundedUserAgent(t *testing.T) {
	if got := boundedUserAgent(""); got != defaultUserAgent {
		t.Fatalf("default user agent = %q", got)
	}
	if got := boundedUserAgent(strings.Repeat("x", 200)); len(got) != 128 {
		t.Fatalf("user agent length = %d", len(got))
	}
}

func TestNewBuildsAllClients(t *testing.T) {
	clients, err := New(&rest.Config{Host: "https://127.0.0.1:6443"})
	if err != nil {
		t.Fatal(err)
	}
	if clients.Core == nil || clients.Dynamic == nil || clients.Discovery == nil {
		t.Fatal("expected typed, dynamic, and discovery clients")
	}
}
