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
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// Dev roles are OFF by default and only seeded when HIGHLAND_DEV_ROLES is
	// explicitly true/1 (see auth.NewUserStoreFromEnv). We do NOT auto-enable
	// them based on running outside Kubernetes.
	users := auth.NewUserStoreFromEnv(cfg.BootstrapUsername, cfg.BootstrapPassword)

	var backend auth.SessionBackend = auth.NewMemoryBackend()
	if cfg.RedisAddr != "" {
		rb, err := auth.NewRedisBackend(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
		if err != nil {
			slog.Error("redis session backend failed", "err", err)
			os.Exit(1)
		}
		backend = rb
		slog.Info("session backend", "type", "redis", "addr", cfg.RedisAddr)
	} else {
		slog.Info("session backend", "type", "memory")
	}
	store := auth.NewStoreFromBackend(backend, cfg.SessionTTL)
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
	stream, err := longhorn.NewStreamProxy(cfg.ManagerURL)
	if err != nil {
		slog.Error("stream proxy init failed", "err", err)
		os.Exit(1)
	}

	auditStore := audit.NewStore(2000, cfg.AuditFile)
	k8sRunner := benchmark.NewK8sRunnerFromEnv()
	benchStore := benchmark.NewStore(k8sRunner)
	if k8sRunner != nil && k8sRunner.Available() {
		// Persist benchmark records as ConfigMaps (etcd) so they survive restarts.
		benchStore.SetPersister(benchmark.NewConfigMapPersister(k8sRunner.Clientset(), k8sRunner.Namespace()))
		benchStore.Load()
		slog.Info("benchmark mode", "mode", "kubernetes-job", "persistence", "configmap")
	} else {
		slog.Info("benchmark mode", "mode", "synthetic", "persistence", "memory")
	}
	scraper := metrics.NewScraper(cfg.ManagerURL, cfg.MetricsInterval, 60)
	scraper.Start()
	defer scraper.Stop()

	router := handlers.NewRouter(handlers.Deps{
		Cfg:         cfg,
		Auth:        authenticator,
		OIDC:        oidcProv,
		OIDCRuntime: oidcRuntime,
		Proxy:       lhProxy,
		Stream:      stream,
		Audit:       auditStore,
		Metrics:     scraper,
		Benchmarks:  benchStore,
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}
	slog.Info("highland-api stopped")
}
