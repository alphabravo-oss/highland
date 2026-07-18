// Package app owns injectable process construction and lifecycle for highland-api.
//
// cmd/highland-api remains a thin binary: load config, install signal handling,
// Build, Run, and map errors to process exit codes. Build never calls os.Exit.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/auth"
	"github.com/highland-io/highland/apps/api/internal/benchmark"
	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/handlers"
	"github.com/highland-io/highland/apps/api/internal/kube"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/policy"
	longhornprovider "github.com/highland-io/highland/apps/api/internal/providers/longhorn"
	"github.com/highland-io/highland/apps/api/internal/providers/rookceph"
	"github.com/highland-io/highland/apps/api/internal/ratelimit"
	"github.com/highland-io/highland/apps/api/internal/storage"
	storageoperations "github.com/highland-io/highland/apps/api/internal/storage/operations"
	"github.com/highland-io/highland/apps/api/internal/watch"
	"k8s.io/client-go/kubernetes"
)

// Dependencies are injectable construction inputs. Nil fields use production defaults.
type Dependencies struct {
	// Cfg is required.
	Cfg *config.Config

	// Logger defaults to JSON slog on stdout when nil.
	Logger *slog.Logger
	// Metrics defaults to a new observability registry when nil.
	Metrics *observability.Metrics

	// When DisableKubeDiscovery is true, KubeClients is used as-is (may be nil)
	// and environment discovery is skipped. Tests use this to force offline mode.
	DisableKubeDiscovery bool
	KubeClients          *kube.Clients

	// Audit overrides automatic sink selection (Postgres / JSONL / memory).
	Audit audit.Sink
	// Limiter is optional. When set it is retained for lifecycle Close and
	// injected into the router.
	Limiter ratelimit.Limiter

	// SessionSecret, when non-empty, is used instead of cfg.SessionSecret /
	// ephemeral generation.
	SessionSecret []byte
	// SessionBackend overrides Redis/signed-cookie selection when non-nil.
	SessionBackend     auth.SessionBackend
	SessionBackendName string
}

// App is a fully wired highland-api process ready to serve HTTP.
type App struct {
	cfg    *config.Config
	logger *slog.Logger
	server *http.Server

	longhornAdapter   *longhornprovider.Adapter
	rookCephDashboard *rookceph.DashboardClient
	hub               *watch.Hub
	cancelWatch       context.CancelFunc
	auditStore        audit.Sink
	limiter           ratelimit.Limiter

	// Exported for characterization / integration tests without starting HTTP.
	Authenticator  *auth.Authenticator
	OIDCRuntime    *auth.OIDCRuntime
	StorageAPI     *storage.HTTPAPI
	PolicyManager  *policy.Manager
	SessionBackend string
	BenchmarkMode  string
	Handler        http.Handler

	closeOnce sync.Once
	closeErr  error
}

// Build constructs the application graph. It never calls os.Exit; failures are typed *Error values.
// On failure, any resources already started are closed before returning.
func Build(ctx context.Context, deps Dependencies) (*App, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if deps.Cfg == nil {
		return nil, configErr("config is required", nil)
	}
	cfg := deps.Cfg

	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	obsMetrics := deps.Metrics
	if obsMetrics == nil {
		obsMetrics = observability.New()
	}

	a := &App{cfg: cfg, logger: logger, limiter: deps.Limiter}
	ok := false
	defer func() {
		if !ok {
			_ = a.Close(context.Background())
		}
	}()

	// Dev roles are OFF by default and only seeded when HIGHLAND_DEV_ROLES is
	// explicitly true/1 (see auth.NewUserStoreFromEnv). We do NOT auto-enable
	// them based on running outside Kubernetes.
	users := auth.NewUserStoreFromEnv(cfg.BootstrapUsername, cfg.BootstrapPassword)

	// Signed cookies are the default replica-safe session backend. Redis is used
	// when configured for centrally revocable sessions.
	secret := deps.SessionSecret
	if len(secret) == 0 {
		secret = []byte(cfg.SessionSecret)
	}
	if len(secret) == 0 {
		gen, err := auth.RandomSecret(32)
		if err != nil {
			return nil, dependencyErr("session secret generation failed", err)
		}
		secret = gen
		logger.Warn("session signing key", "persistence", "ephemeral", "impact", "logins are invalidated on restart")
	}

	sessionBackend := deps.SessionBackend
	sessionBackendName := deps.SessionBackendName
	if sessionBackend == nil {
		sessionBackend = auth.NewTokenBackend(secret)
		sessionBackendName = "signed-cookie"
		if cfg.RedisAddr != "" {
			redisBackend, redisErr := auth.NewRedisBackend(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
			if redisErr != nil {
				return nil, dependencyErr("Redis session backend unavailable", redisErr)
			}
			sessionBackend = redisBackend
			sessionBackendName = "redis"
		}
	}
	if sessionBackendName == "" {
		sessionBackendName = "injected"
	}
	logger.Info("session backend", "type", sessionBackendName)
	store := auth.NewStoreFromBackend(sessionBackend, cfg.SessionTTL)
	authenticator := auth.NewAuthenticator(users, store)
	a.Authenticator = authenticator
	a.SessionBackend = sessionBackendName

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
		logger.Warn("OIDC config file load failed", "err", err, "path", oidcPath)
	}
	{
		oidcCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := oidcRuntime.Init(oidcCtx); err != nil {
			logger.Warn("OIDC init failed — local login still works; configure via Admin → SSO", "err", err)
		} else if oidcRuntime.IsReady() {
			logger.Info("OIDC enabled", "issuer", oidcRuntime.Public().IssuerURL)
		}
		cancel()
	}
	oidcProv := oidcRuntime.Provider()
	a.OIDCRuntime = oidcRuntime

	storageRegistry := storage.NewRegistry()
	longhornAdapter, err := registerLonghorn(cfg, obsMetrics, storageRegistry)
	if err != nil {
		return nil, err
	}
	a.longhornAdapter = longhornAdapter
	var lhProxy = longhornAdapter.Proxy()
	var stream = longhornAdapter.Stream()

	auditStore, auditErr := buildAuditSink(ctx, deps, cfg, logger)
	if auditErr != nil {
		return nil, auditErr
	}
	a.auditStore = auditStore
	if cfg.RequireAuditDurable && !auditStore.Durable() {
		return nil, dependencyErr("durable audit is required (HIGHLAND_AUDIT_REQUIRED) but the audit sink is not durable", nil)
	}
	logger.Info("audit sink", "backend", auditStore.Health(ctx).Backend, "durable", auditStore.Durable())

	// Login limiter: injected, Redis shared (HA), or process-local memory.
	if a.limiter == nil {
		if addr := strings.TrimSpace(os.Getenv("HIGHLAND_LOGIN_LIMITER_REDIS_ADDR")); addr != "" {
			failOpen := envBoolLocal("HIGHLAND_LOGIN_LIMITER_FAIL_OPEN", false)
			policy := ratelimit.OutageFailClosed
			if failOpen {
				policy = ratelimit.OutageFailOpen
				logger.Warn("login limiter fail-open is enabled; brute-force protection weakens when Redis is down")
			}
			prefix := envOrLocal("HIGHLAND_LOGIN_LIMITER_KEY_PREFIX", "highland:"+cfg.ClusterIdentity+":login")
			rl, rlErr := ratelimit.NewRedis(ratelimit.RedisOptions{
				Options: ratelimit.Options{
					Enabled:         cfg.LoginRateLimitEnabled,
					MaxFailuresUser: cfg.LoginMaxFailuresUser,
					MaxFailuresIP:   cfg.LoginMaxFailuresIP,
					LockoutBase:     cfg.LoginLockoutBase,
					LockoutMax:      cfg.LoginLockoutMax,
					FailureWindow:   cfg.LoginFailureWindow,
					MaxEntries:      cfg.LoginMaxEntries,
				},
				Addr:         addr,
				Password:     os.Getenv("HIGHLAND_LOGIN_LIMITER_REDIS_PASSWORD"),
				DB:           cfg.RedisDB,
				KeyPrefix:    prefix,
				UsernameSalt: cfg.ClusterIdentity,
				OutagePolicy: policy,
			})
			if rlErr != nil {
				return nil, dependencyErr("shared login limiter Redis unavailable", rlErr)
			}
			a.limiter = rl
			logger.Info("login limiter", "backend", "redis", "outagePolicy", string(policy))
		} else {
			a.limiter = ratelimit.New(ratelimit.Options{
				Enabled:         cfg.LoginRateLimitEnabled,
				MaxFailuresUser: cfg.LoginMaxFailuresUser,
				MaxFailuresIP:   cfg.LoginMaxFailuresIP,
				LockoutBase:     cfg.LoginLockoutBase,
				LockoutMax:      cfg.LoginLockoutMax,
				FailureWindow:   cfg.LoginFailureWindow,
				MaxEntries:      cfg.LoginMaxEntries,
			})
			logger.Info("login limiter", "backend", "memory")
		}
	}

	var kubeClients *kube.Clients
	if deps.DisableKubeDiscovery {
		kubeClients = deps.KubeClients
	} else if deps.KubeClients != nil {
		kubeClients = deps.KubeClients
	} else {
		if clients, clientErr := kube.NewFromEnvironment(); clientErr != nil {
			logger.Warn("kubernetes client unavailable", "err", clientErr)
		} else {
			kubeClients = clients
		}
	}

	var k8sRunner *benchmark.K8sRunner
	if kubeClients != nil && cfg.KubernetesBenchmarkEnabled {
		k8sRunner = benchmark.NewK8sRunner(kubeClients.Core, kubeClients.RESTConfig)
	}
	benchStore := benchmark.NewStore(k8sRunner)
	benchmarkMode := "disabled"
	var k8sClient kubernetes.Interface
	var hub *watch.Hub
	runtimeNamespace := os.Getenv("HIGHLAND_NAMESPACE")
	if runtimeNamespace == "" {
		runtimeNamespace = "highland-system"
	}
	// Background watches are independent of the Build caller's deadline so a
	// short construction timeout cannot tear them down. Close/Run cancel them.
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	a.cancelWatch = cancelWatch

	if kubeClients != nil {
		k8sClient = kubeClients.Core
		if identitySecret := os.Getenv("HIGHLAND_IDENTITY_SECRET"); identitySecret != "" {
			identityPersistence, identityErr := auth.NewKubernetesIdentityPersistence(k8sClient, runtimeNamespace, identitySecret, secret)
			if identityErr != nil {
				return nil, dependencyErr("identity persistence configuration failed", identityErr)
			}
			identityCtx, cancelIdentity := context.WithTimeout(ctx, 10*time.Second)
			identityErr = users.ConfigurePersistence(identityCtx, identityPersistence)
			cancelIdentity()
			if identityErr != nil {
				return nil, dependencyErr("identity persistence initialization failed", identityErr)
			}
			users.StartSync(watchCtx, 2*time.Second)
			logger.Info("identity persistence", "type", "kubernetes-secret", "secret", identitySecret)
		}
		if cfg.LonghornEnabled {
			hub = watch.NewHub(kubeClients.Dynamic, cfg.LonghornNamespace)
		} else {
			hub = watch.NewStorageHub(kubeClients.Dynamic)
		}
		hub.SetMetrics(obsMetrics)
		obsMetrics.RegisterSSEClientSource(hub.ClientCount)
		hub.Start(watchCtx)
		a.hub = hub
		benchStore.SetPublisher(hub)
	} else if os.Getenv("HIGHLAND_IDENTITY_SECRET") != "" {
		return nil, dependencyErr("identity persistence requested but Kubernetes client is unavailable", nil)
	}

	staticPolicy := policy.StoragePolicy{
		AcceptNewOperations:         cfg.StorageWritesEnabled,
		PortableKubernetesWrites:    cfg.StorageWritesEnabled,
		LonghornWrites:              cfg.StorageWritesEnabled,
		RookCephWrites:              cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled,
		AllowCephStorageClassDelete: cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled && cfg.RookCephAllowStorageClassDelete,
		AllowCephPoolDelete:         cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled && cfg.RookCephAllowPoolDelete,
	}
	policyCeiling := policy.Ceiling{
		PortableKubernetesWrites:    cfg.StorageWritesEnabled,
		LonghornWrites:              cfg.StorageWritesEnabled,
		RookCephWrites:              cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled,
		AllowCephStorageClassDelete: cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled && cfg.RookCephAllowStorageClassDelete,
		AllowCephPoolDelete:         cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled && cfg.RookCephAllowPoolDelete,
	}
	if cfg.AdminPolicyControlEnabled {
		policyCeiling = policy.Ceiling{
			PortableKubernetesWrites:    cfg.PolicyCeilingPortableWrites,
			LonghornWrites:              cfg.PolicyCeilingLonghornWrites,
			RookCephWrites:              cfg.PolicyCeilingRookCephWrites,
			AllowCephStorageClassDelete: cfg.PolicyCeilingCephSCDelete,
			AllowCephPoolDelete:         cfg.PolicyCeilingCephPoolDelete,
		}
	}
	policyManager, err := policy.NewManager(policy.Config{
		Dynamic: kubeClientsDynamic(kubeClients), Namespace: runtimeNamespace,
		Enabled: cfg.AdminPolicyControlEnabled, Ceiling: policyCeiling,
		StaticRequested: staticPolicy, Publisher: hub, Observer: obsMetrics,
	})
	if err != nil {
		return nil, initErr("storage policy manager init failed", err)
	}
	a.PolicyManager = policyManager
	policyManager.SetOnChange(storageRegistry.InvalidateDescriptors)
	if startErr := policyManager.Start(watchCtx); startErr != nil {
		logger.Warn("runtime storage policy unavailable; writes fail closed", "err", startErr)
	}
	if k8sRunner != nil && k8sRunner.Available() {
		// Persist benchmark records as ConfigMaps (etcd) so they survive restarts.
		benchStore.SetPersister(benchmark.NewConfigMapPersister(k8sRunner.Clientset(), k8sRunner.Namespace()))
		benchStore.Load()
		benchmarkMode = "kubernetes-job"
		logger.Info("benchmark mode", "mode", "kubernetes-job", "persistence", "configmap")
	} else if cfg.KubernetesBenchmarkEnabled {
		benchmarkMode = "unavailable"
		logger.Warn("benchmark mode", "mode", benchmarkMode, "reason", "Kubernetes client unavailable")
	} else {
		logger.Info("benchmark mode", "mode", benchmarkMode, "reason", "Kubernetes Job/PVC workflow disabled")
	}
	a.BenchmarkMode = benchmarkMode

	if err := registerOpenEBS(cfg, kubeClients, obsMetrics, storageRegistry); err != nil {
		return nil, err
	}
	if err := registerLinstor(cfg, kubeClients, obsMetrics, storageRegistry); err != nil {
		return nil, err
	}
	rookCephAdapter, rookCephDashboard, err := registerRookCeph(cfg, kubeClients, obsMetrics, storageRegistry, hub, policyManager, watchCtx, logger)
	if err != nil {
		return nil, err
	}
	a.rookCephDashboard = rookCephDashboard

	// Warm managed provider descriptors before serving navigation requests.
	// Discovered generic CSI drivers are merged asynchronously after inventory
	// sync; the shell can use this managed snapshot immediately.
	descriptorContext, cancelDescriptors := context.WithTimeout(ctx, 4*time.Second)
	_, _, _ = storageRegistry.DescriptorSnapshot(descriptorContext, nil)
	cancelDescriptors()
	if k8sRunner != nil {
		k8sRunner.SetProviderResolver(storageRegistry.ResolveDriver)
	}
	var storageInventory *storage.Inventory
	if cfg.StorageEnabled && kubeClients != nil {
		inventory, inventoryErr := storage.NewInventory(
			kubeClients.Core,
			kubeClients.Dynamic,
			kubeClients.Discovery,
			storageRegistry,
			storage.NewScope(cfg.StorageScopeMode, cfg.StorageNamespaces),
		)
		if inventoryErr != nil {
			logger.Warn("storage inventory init failed", "err", inventoryErr)
		} else {
			storageInventory = inventory
			storageInventory.SetObserver(obsMetrics)
			if hub != nil {
				storageInventory.SetPublisher(hub)
			}
			storageInventory.Start(watchCtx)
		}
	}
	storageAPI := storage.NewHTTPAPI(storageInventory, storageRegistry)
	storageAPI.SetObserver(obsMetrics)
	storageAPI.SetContextSources(nil, func(limit int) []storage.ContextAuditRecord {
		events := audit.ListRecent(context.Background(), auditStore, limit)
		result := make([]storage.ContextAuditRecord, 0, len(events))
		for _, event := range events {
			result = append(result, storage.ContextAuditRecord{
				ID: event.ID, ProviderID: event.ProviderID, Action: event.Action,
				Result: event.Result, Message: event.Message, OperationID: event.OperationID,
				TargetKind: event.TargetKind, Namespace: event.TargetNamespace,
				TargetName: event.TargetName, TargetUID: event.TargetUID, ObservedAt: event.Timestamp,
			})
		}
		return result
	})
	a.StorageAPI = storageAPI

	var storageOperationsAPI *storageoperations.API
	var operationStore *storageoperations.Store
	if cfg.StorageEnabled && kubeClients != nil {
		operationNamespace := runtimeNamespace
		var storeErr error
		operationStore, storeErr = storageoperations.NewStore(kubeClients.Dynamic, operationNamespace)
		if storeErr == nil {
			operationStore.SetPublisher(hub)
		}
		var operationSafety storageoperations.SafetyVerifier
		if rookCephAdapter != nil {
			operationSafety = rookCephAdapter
		}
		operationPlanner, plannerErr := storageoperations.NewPlanner(storageoperations.PlannerConfig{
			Core: kubeClients.Core, Dynamic: kubeClients.Dynamic, Scope: storage.NewScope(cfg.StorageScopeMode, cfg.StorageNamespaces),
			Secret: secret, RookNamespace: cfg.RookCephNamespace, RookClusterName: cfg.RookCephClusterName,
			Safety: operationSafety, PlanDryRun: true, ImpactAnalyzer: storageAPI.ImpactAnalyzer(),
			ProviderForDriver: storageRegistry.ResolveDriver, RequireImpactAnalysis: true,
			Longhorn: storageoperations.NewLonghornManagerClient(cfg.ManagerURL),
			PolicyVersion: func() string {
				if policyManager == nil {
					return "static"
				}
				snapshot := policyManager.Snapshot()
				return fmt.Sprintf("%s:%s:%d", snapshot.Source, snapshot.ResourceVersion, snapshot.ObservedGeneration)
			},
		})
		if storeErr != nil || plannerErr != nil {
			logger.Warn("durable storage operations unavailable", "storeErr", storeErr, "plannerErr", plannerErr)
		} else {
			storageAPI.SetContextSources(func(opCtx context.Context, limit int) ([]storage.ContextOperationRecord, error) {
				operations, listErr := operationStore.List(opCtx, map[string]string{}, limit)
				if listErr != nil {
					return nil, listErr
				}
				result := make([]storage.ContextOperationRecord, 0, len(operations))
				for _, operation := range operations {
					observedAt := operation.CreationTimestamp
					if operation.Status.LastAttemptAt != nil {
						observedAt = *operation.Status.LastAttemptAt
					}
					if operation.Status.FinishedAt != nil {
						observedAt = *operation.Status.FinishedAt
					}
					message := operation.Status.Diagnostics
					if message == "" && len(operation.Status.Conditions) > 0 {
						message = operation.Status.Conditions[len(operation.Status.Conditions)-1].Message
					}
					result = append(result, storage.ContextOperationRecord{
						ID: operation.Name, ProviderID: operation.Spec.ProviderID,
						ActionID: operation.Spec.ActionID, Phase: operation.Status.Phase,
						TargetKind: operation.Spec.Target.Kind, Namespace: operation.Spec.Target.Namespace,
						TargetName: operation.Spec.Target.Name, TargetUID: operation.Spec.Target.UID,
						RequestedAt: operation.Spec.RequestedAt, ObservedAt: observedAt, Message: message,
					})
				}
				return result, nil
			}, func(limit int) []storage.ContextAuditRecord {
				events := audit.ListRecent(context.Background(), auditStore, limit)
				result := make([]storage.ContextAuditRecord, 0, len(events))
				for _, event := range events {
					result = append(result, storage.ContextAuditRecord{
						ID: event.ID, ProviderID: event.ProviderID, Action: event.Action,
						Result: event.Result, Message: event.Message, OperationID: event.OperationID,
						TargetKind: event.TargetKind, Namespace: event.TargetNamespace,
						TargetName: event.TargetName, TargetUID: event.TargetUID, ObservedAt: event.Timestamp,
					})
				}
				return result
			})
			operationController, controllerErr := storageoperations.NewController(kubeClients.Core, kubeClients.Dynamic, operationStore, operationPlanner, operationNamespace, obsMetrics, auditStore)
			if controllerErr != nil {
				logger.Warn("storage operation controller unavailable", "err", controllerErr)
			} else if cfg.StorageWritesEnabled || cfg.StorageOperationRecoveryEnabled || (cfg.AdminPolicyControlEnabled && cfg.AdminPolicyInstallWriterRBAC) {
				operationController.Start(watchCtx)
			}
			var cephVersionCheck func(context.Context) bool
			if rookCephAdapter != nil {
				cephVersionCheck = rookCephAdapter.WriteSupported
			}
			storageOperationsAPI = storageoperations.NewAPI(storageoperations.APIConfig{
				Store: operationStore, Planner: operationPlanner, Audit: auditStore, Observer: obsMetrics,
				WritesEnabled: cfg.StorageWritesEnabled, CephWritesEnabled: cfg.RookCephWritesEnabled,
				AllowStorageClassDelete: cfg.RookCephAllowStorageClassDelete, AllowPoolDelete: cfg.RookCephAllowPoolDelete,
				CephPoolVerified: rookCephAdapter != nil && rookCephAdapter.PoolVerificationAvailable(),
				CephVersionCheck: cephVersionCheck, Policy: policyManager,
			})
		}
	}
	var storagePolicyAPI *policy.API
	if policyManager != nil {
		storagePolicyAPI, err = policy.NewAPI(policy.APIConfig{
			Store: policyManager, Audit: auditStore, Secret: secret,
			ClusterIdentity: cfg.ClusterIdentity, ImpactResolver: storageoperations.PolicyImpact,
			Observer: obsMetrics,
			ActiveCount: func(activeCtx context.Context) (int, error) {
				if operationStore == nil {
					return 0, nil
				}
				operations, listErr := operationStore.List(activeCtx, map[string]string{}, 500)
				if listErr != nil {
					return 0, listErr
				}
				active := 0
				for _, operation := range operations {
					switch operation.Status.Phase {
					case "Succeeded", "Failed", "Cancelled":
					default:
						active++
					}
				}
				return active, nil
			},
		})
		if err != nil {
			return nil, initErr("storage policy API init failed", err)
		}
	}

	router := handlers.NewRouter(handlers.Deps{
		Cfg:               cfg,
		Auth:              authenticator,
		OIDC:              oidcProv,
		OIDCRuntime:       oidcRuntime,
		Proxy:             lhProxy,
		Stream:            stream,
		Audit:             auditStore,
		Metrics:           longhornAdapter.Scraper(),
		Benchmarks:        benchStore,
		K8s:               k8sClient,
		LonghornNamespace: cfg.LonghornNamespace,
		Storage:           storageAPI,
		StorageOperations: storageOperationsAPI,
		StoragePolicy:     storagePolicyAPI,
		PolicySnapshot:    policyManager,
		SessionBackend:    sessionBackendName,
		BenchmarkMode:     benchmarkMode,
		// Share the exact session-signing key so CSRF tokens verify against it.
		SessionSecret: secret,
		WatchHub:      hub,
		Obs:           obsMetrics,
		Logger:        logger,
		Limiter:       a.limiter,
	})
	a.Handler = router

	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	a.server = &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ok = true
	return a, nil
}

// Run serves HTTP until ctx is cancelled, then performs graceful shutdown.
// Server listen failures (other than ErrServerClosed) are returned.
func (a *App) Run(ctx context.Context) error {
	if a == nil || a.server == nil {
		return initErr("application is not constructed", nil)
	}
	if ctx == nil {
		return configErr("run context is required", nil)
	}

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("highland-api listening",
			"addr", a.server.Addr,
			"manager", a.cfg.ManagerURL,
			"localAuth", a.cfg.LocalEnabled(),
			"oidc", a.OIDCRuntime != nil && a.OIDCRuntime.Provider() != nil,
			"oidcMock", a.cfg.OIDCMock,
		)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.Close(shutdownCtx); err != nil {
			return err
		}
		// Drain server goroutine.
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
		return nil
	case err := <-errCh:
		// Listen failed; still release resources.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(shutdownCtx)
		if err != nil {
			return initErr("server error", err)
		}
		return nil
	}
}

// ServerAddr returns the configured listen address (useful in tests).
func (a *App) ServerAddr() string {
	if a == nil || a.server == nil {
		return ""
	}
	return a.server.Addr
}
