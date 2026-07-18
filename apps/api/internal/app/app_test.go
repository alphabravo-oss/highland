package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/highland-io/highland/apps/api/internal/app"
	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/ratelimit"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// minimalConfig is a hand-built config that does not require Kubernetes or Longhorn.
func minimalConfig() *config.Config {
	return &config.Config{
		ListenAddr:                 ":0",
		BootstrapUsername:          "admin",
		BootstrapPassword:          "highland-test-password",
		SessionTTL:                 time.Hour,
		SessionSecret:              "0123456789abcdef0123456789abcdef", // 32 bytes
		CookieName:                 "highland_session",
		AuthMode:                   config.AuthModeLocal,
		LocalAlways:                true,
		Version:                    "0.0.0-test",
		AllowedOrigins:             []string{"http://localhost:5173"},
		ClusterIdentity:            "test",
		StorageEnabled:             false,
		StorageScopeMode:           "cluster",
		LonghornEnabled:            false,
		KubernetesBenchmarkEnabled: false,
		CSRFEnabled:                true,
		CSRFCookieName:             "highland_csrf",
		LoginRateLimitEnabled:      true,
		LoginMaxFailuresUser:       5,
		LoginMaxFailuresIP:         15,
		LoginLockoutBase:           time.Minute,
		LoginLockoutMax:            15 * time.Minute,
		LoginFailureWindow:         15 * time.Minute,
		LoginMaxEntries:            1000,
	}
}

func isolateEnv(t *testing.T) {
	t.Helper()
	// Build still reads a few process env vars; clear those that force cluster deps.
	t.Setenv("HIGHLAND_IDENTITY_SECRET", "")
	t.Setenv("HIGHLAND_OIDC_CONFIG_FILE", "")
	t.Setenv("HIGHLAND_NAMESPACE", "")
	t.Setenv("HIGHLAND_DEV_ROLES", "")
	t.Setenv("HIGHLAND_REDIS_ADDR", "")
}

func TestBuildNilConfig(t *testing.T) {
	// Not parallel: isolateEnv mutates process env for this test.
	isolateEnv(t)
	_, err := app.Build(context.Background(), app.Dependencies{Logger: testLogger()})
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !app.IsKind(err, app.KindConfig) {
		t.Fatalf("expected KindConfig, got %v", err)
	}
}

func TestBuildStorageDisabledNilKube(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	application, err := app.Build(context.Background(), app.Dependencies{
		Cfg:                  cfg,
		Logger:               testLogger(),
		DisableKubeDiscovery: true,
		KubeClients:          nil,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = application.Close(ctx)
	})
	if application.Handler == nil {
		t.Fatal("expected HTTP handler")
	}
	if application.Authenticator == nil {
		t.Fatal("expected authenticator")
	}
	if application.SessionBackend != "signed-cookie" {
		t.Fatalf("session backend = %q, want signed-cookie", application.SessionBackend)
	}
	if application.BenchmarkMode != "disabled" {
		t.Fatalf("benchmark mode = %q, want disabled", application.BenchmarkMode)
	}
}

func TestBuildWithInjectedAuditAndLimiter(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	auditStore := audit.NewStore(100, "")
	limiter := ratelimit.New(ratelimit.Options{
		Enabled:         true,
		MaxFailuresUser: 3,
		MaxFailuresIP:   10,
		LockoutBase:     time.Second,
		LockoutMax:      time.Minute,
		FailureWindow:   time.Minute,
		MaxEntries:      100,
	})

	application, err := app.Build(context.Background(), app.Dependencies{
		Cfg:                  cfg,
		Logger:               testLogger(),
		DisableKubeDiscovery: true,
		Audit:                auditStore,
		Limiter:              limiter,
		SessionSecret:        []byte("0123456789abcdef0123456789abcdef"),
		SessionBackend:       auth.NewTokenBackend([]byte("0123456789abcdef0123456789abcdef")),
		SessionBackendName:   "test-token",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = application.Close(ctx)
	})
	if application.SessionBackend != "test-token" {
		t.Fatalf("session backend = %q, want test-token", application.SessionBackend)
	}
	// Injected audit is used by the router path (characterization: no panic / build ok).
	if application.Handler == nil {
		t.Fatal("expected handler with injected audit/limiter")
	}
}

func TestBuildRequireAuditDurableFailsWithoutDurableSink(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	cfg.RequireAuditDurable = true
	// Memory audit without file path is not durable.
	_, err := app.Build(context.Background(), app.Dependencies{
		Cfg:                  cfg,
		Logger:               testLogger(),
		DisableKubeDiscovery: true,
		Audit:                audit.NewStore(10, ""),
	})
	if err == nil {
		t.Fatal("expected durable audit requirement failure")
	}
	if !app.IsKind(err, app.KindDependency) {
		t.Fatalf("expected KindDependency, got %v", err)
	}
}

func TestBuildRequireAuditDurableWithJSONL(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	cfg.RequireAuditDurable = true
	path := t.TempDir() + "/audit.jsonl"
	application, err := app.Build(context.Background(), app.Dependencies{
		Cfg:                  cfg,
		Logger:               testLogger(),
		DisableKubeDiscovery: true,
		Audit:                audit.NewStore(10, path),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := application.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBuildOpenEBSRequiresKubernetes(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	cfg.OpenEBSEnabled = true
	_, err := app.Build(context.Background(), app.Dependencies{
		Cfg:                  cfg,
		Logger:               testLogger(),
		DisableKubeDiscovery: true,
		KubeClients:          nil,
	})
	if err == nil {
		t.Fatal("expected OpenEBS without kube to fail")
	}
	if !app.IsKind(err, app.KindProvider) {
		t.Fatalf("expected KindProvider, got %v", err)
	}
}

func TestBuildLonghornEnabledWithoutManagerURLFails(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	cfg.LonghornEnabled = true
	cfg.ManagerURL = "" // invalid
	_, err := app.Build(context.Background(), app.Dependencies{
		Cfg:                  cfg,
		Logger:               testLogger(),
		DisableKubeDiscovery: true,
	})
	if err == nil {
		t.Fatal("expected longhorn init failure")
	}
	if !app.IsKind(err, app.KindProvider) {
		t.Fatalf("expected KindProvider, got %v", err)
	}
}

func TestBuildLonghornEnabledSucceedsOffline(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	cfg.LonghornEnabled = true
	cfg.LonghornRequired = false
	cfg.ManagerURL = "http://127.0.0.1:9500"
	cfg.LonghornNamespace = "longhorn-system"
	cfg.MetricsInterval = time.Hour // avoid aggressive scrape in test process
	application, err := app.Build(context.Background(), app.Dependencies{
		Cfg:                  cfg,
		Logger:               testLogger(),
		DisableKubeDiscovery: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := application.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestErrorIsKindConfig(t *testing.T) {
	isolateEnv(t)
	_, err := app.Build(context.Background(), app.Dependencies{})
	if err == nil || !app.IsKind(err, app.KindConfig) {
		t.Fatalf("unexpected: %v", err)
	}
	var typed *app.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *app.Error, got %T", err)
	}
}


func TestBuildPostgresAuditDSNUnreachable(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	cfg.AuditPostgresDSN = "postgres://invalid:invalid@127.0.0.1:1/highland?connect_timeout=1"
	cfg.RequireAuditDurable = true
	_, err := app.Build(context.Background(), app.Dependencies{
		Cfg: cfg, Logger: testLogger(), DisableKubeDiscovery: true,
	})
	if err == nil {
		t.Fatal("expected postgres audit open failure")
	}
	if !app.IsKind(err, app.KindDependency) {
		t.Fatalf("expected KindDependency, got %v", err)
	}
}

func TestBuildInjectedSharedAuditSink(t *testing.T) {
	isolateEnv(t)
	cfg := minimalConfig()
	cfg.RequireAuditDurable = true
	shared := audit.NewSharedMemorySink()
	application, err := app.Build(context.Background(), app.Dependencies{
		Cfg: cfg, Logger: testLogger(), DisableKubeDiscovery: true, Audit: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = application.Close(context.Background())
}
