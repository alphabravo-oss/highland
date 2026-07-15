package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/benchmark"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/handlers"
	"github.com/highland-io/highland/apps/api/internal/longhorn"
	"github.com/highland-io/highland/apps/api/internal/metrics"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Self-observability: Prometheus metrics for the BFF's own operation.
	obsMetrics := observability.New()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// Dev roles are OFF by default and only seeded when HIGHLAND_DEV_ROLES is
	// explicitly true/1 (see auth.NewUserStoreFromEnv). We do NOT auto-enable
	// them based on running outside Kubernetes.
	users := auth.NewUserStoreFromEnv(cfg.BootstrapUsername, cfg.BootstrapPassword)

	// Sessions are stateless HMAC-signed cookies (no server store) — they survive
	// restarts and work across replicas. A stable secret keeps tokens valid across
	// restarts; without one we generate an ephemeral secret (logins drop on restart).
	secret := []byte(cfg.SessionSecret)
	if len(secret) == 0 {
		gen, err := auth.RandomSecret(32)
		if err != nil {
			slog.Error("session secret generation failed", "err", err)
			os.Exit(1)
		}
		secret = gen
		slog.Warn("session backend", "type", "stateless", "secret", "ephemeral (set HIGHLAND_SESSION_SECRET to persist logins across restarts)")
	} else {
		slog.Info("session backend", "type", "stateless")
	}
	store := auth.NewStoreFromBackend(auth.NewTokenBackend(secret), cfg.SessionTTL)
	authenticator := auth.NewAuthenticator(users, store)

	oidcPath := os.Getenv("HIGHLAND_OIDC_CONFIG_FILE")
	oidcRuntime := auth.NewOIDCRuntime(authenticator, oidcPath)
	// Seed from env/Helm; file (if present) overlays for admin-persisted settings.
	oidcRuntime.SeedFromEnv(
		cfg.OIDCIssuer,
		cfg.OIDCClientID,
		cfg.OIDCClientSecret,
		cfg.OIDCRedirectURL,
		cfg.OIDCRoleClaim,
		cfg.OIDCEnabled() && cfg.OIDCIssuer != "",
	)
	if err := oidcRuntime.LoadFile(); err != nil {
		slog.Warn("OIDC config file load failed", "err", err, "path", oidcPath)
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := oidcRuntime.Init(ctx); err != nil {
			slog.Warn("OIDC init failed — local login still works; configure via Admin → SSO", "err", err)
		} else if oidcRuntime.IsReady() {
			slog.Info("OIDC enabled", "issuer", oidcRuntime.Public().IssuerURL)
		}
		cancel()
	}
	oidcProv := oidcRuntime.Provider()

	lhProxy, err := longhorn.NewProxy(cfg.ManagerURL)
	if err != nil {
		slog.Error("longhorn proxy init failed", "err", err)
		os.Exit(1)
	}
	lhProxy.SetMetrics(obsMetrics)
	stream, err := longhorn.NewStreamProxy(cfg.ManagerURL)
	if err != nil {
		slog.Error("stream proxy init failed", "err", err)
		os.Exit(1)
	}

	auditStore := audit.NewStore(2000, cfg.AuditFile)
	k8sRunner := benchmark.NewK8sRunnerFromEnv()
	benchStore := benchmark.NewStore(k8sRunner)
	benchmarkMode := "synthetic"
	var k8sClient kubernetes.Interface
	var hub *watch.Hub
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	if k8sRunner != nil && k8sRunner.Available() {
		// Persist benchmark records as ConfigMaps (etcd) so they survive restarts.
		benchStore.SetPersister(benchmark.NewConfigMapPersister(k8sRunner.Clientset(), k8sRunner.Namespace()))
		benchStore.Load()
		benchmarkMode = "kubernetes-job"
		k8sClient = k8sRunner.Clientset()
		slog.Info("benchmark mode", "mode", "kubernetes-job", "persistence", "configmap")
		// Real-time SSE: watch Longhorn CRDs via a dynamic client and fan out.
		if dyn, err := dynamic.NewForConfig(k8sRunner.RESTConfig()); err != nil {
			slog.Warn("realtime: dynamic client init failed; SSE disabled", "err", err)
		} else {
			hub = watch.NewHub(dyn, os.Getenv("HIGHLAND_LONGHORN_NAMESPACE"))
			hub.SetMetrics(obsMetrics)
			obsMetrics.RegisterSSEClientSource(hub.ClientCount)
			hub.Start(watchCtx)
		}
	} else {
		slog.Info("benchmark mode", "mode", "synthetic", "persistence", "memory")
	}
	scraper := metrics.NewScraper(cfg.ManagerURL, cfg.MetricsInterval, 60)
	scraper.Start()
	defer scraper.Stop()

	router := handlers.NewRouter(handlers.Deps{
		Cfg:               cfg,
		Auth:              authenticator,
		OIDC:              oidcProv,
		OIDCRuntime:       oidcRuntime,
		Proxy:             lhProxy,
		Stream:            stream,
		Audit:             auditStore,
		Metrics:           scraper,
		Benchmarks:        benchStore,
		K8s:               k8sClient,
		LonghornNamespace: os.Getenv("HIGHLAND_LONGHORN_NAMESPACE"),
		SessionBackend:    "stateless",
		BenchmarkMode:     benchmarkMode,
		// Share the exact session-signing key so CSRF tokens verify against it.
		SessionSecret: secret,
		WatchHub:      hub,
		Obs:           obsMetrics,
		Logger:        logger,
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("highland-api listening",
			"addr", cfg.ListenAddr,
			"manager", cfg.ManagerURL,
			"localAuth", cfg.LocalEnabled(),
			"oidc", oidcProv != nil,
			"oidcMock", cfg.OIDCMock,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	// Stop the informers, then close SSE client channels so the long-lived
	// stream handlers return — otherwise srv.Shutdown() blocks on them.
	cancelWatch()
	if hub != nil {
		hub.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}
	slog.Info("highland-api stopped")
}
