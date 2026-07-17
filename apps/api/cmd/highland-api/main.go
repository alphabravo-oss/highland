package main

import (
	"context"
	"fmt"
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
	"github.com/highland-io/highland/apps/api/internal/kube"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/policy"
	linstorprovider "github.com/highland-io/highland/apps/api/internal/providers/linstor"
	longhornprovider "github.com/highland-io/highland/apps/api/internal/providers/longhorn"
	openebsprovider "github.com/highland-io/highland/apps/api/internal/providers/openebs"
	"github.com/highland-io/highland/apps/api/internal/providers/rookceph"
	"github.com/highland-io/highland/apps/api/internal/storage"
	storageoperations "github.com/highland-io/highland/apps/api/internal/storage/operations"
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

	var longhornAdapter *longhornprovider.Adapter
	if cfg.LonghornEnabled {
		longhornAdapter, err = longhornprovider.New(longhornprovider.Config{
			ManagerURL: cfg.ManagerURL, Namespace: cfg.LonghornNamespace, Required: cfg.LonghornRequired,
			ScrapeInterval: cfg.MetricsInterval, Observer: obsMetrics,
		})
		if err != nil {
			slog.Error("longhorn provider init failed", "err", err)
			os.Exit(1)
		}
		longhornAdapter.Start()
		defer longhornAdapter.Stop()
	}
	var lhProxy = longhornAdapter.Proxy()
	var stream = longhornAdapter.Stream()

	auditStore := audit.NewStore(2000, cfg.AuditFile)
	var kubeClients *kube.Clients
	if clients, clientErr := kube.NewFromEnvironment(); clientErr != nil {
		slog.Warn("kubernetes client unavailable", "err", clientErr)
	} else {
		kubeClients = clients
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
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	if kubeClients != nil {
		k8sClient = kubeClients.Core
		if cfg.LonghornEnabled {
			hub = watch.NewHub(kubeClients.Dynamic, cfg.LonghornNamespace)
		} else {
			hub = watch.NewStorageHub(kubeClients.Dynamic)
		}
		hub.SetMetrics(obsMetrics)
		obsMetrics.RegisterSSEClientSource(hub.ClientCount)
		hub.Start(watchCtx)
		benchStore.SetPublisher(hub)
	}
	storageRegistry := storage.NewRegistry()
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
	var policyManager *policy.Manager
	policyManager, err = policy.NewManager(policy.Config{
		Dynamic: kubeClientsDynamic(kubeClients), Namespace: runtimeNamespace,
		Enabled: cfg.AdminPolicyControlEnabled, Ceiling: policyCeiling,
		StaticRequested: staticPolicy, Publisher: hub, Observer: obsMetrics,
	})
	if err != nil {
		slog.Error("storage policy manager init failed", "err", err)
		os.Exit(1)
	}
	policyManager.SetOnChange(storageRegistry.InvalidateDescriptors)
	if startErr := policyManager.Start(watchCtx); startErr != nil {
		slog.Warn("runtime storage policy unavailable; writes fail closed", "err", startErr)
	}
	if k8sRunner != nil && k8sRunner.Available() {
		// Persist benchmark records as ConfigMaps (etcd) so they survive restarts.
		benchStore.SetPersister(benchmark.NewConfigMapPersister(k8sRunner.Clientset(), k8sRunner.Namespace()))
		benchStore.Load()
		benchmarkMode = "kubernetes-job"
		slog.Info("benchmark mode", "mode", "kubernetes-job", "persistence", "configmap")
	} else if cfg.KubernetesBenchmarkEnabled {
		benchmarkMode = "unavailable"
		slog.Warn("benchmark mode", "mode", benchmarkMode, "reason", "Kubernetes client unavailable")
	} else {
		slog.Info("benchmark mode", "mode", benchmarkMode, "reason", "Kubernetes Job/PVC workflow disabled")
	}
	if longhornAdapter != nil {
		if registerErr := storageRegistry.Register(context.Background(), longhornAdapter); registerErr != nil {
			slog.Error("longhorn provider registration failed", "err", registerErr)
			os.Exit(1)
		}
	}
	if cfg.OpenEBSEnabled {
		if kubeClients == nil {
			slog.Error("OpenEBS provider requires Kubernetes connectivity")
			os.Exit(1)
		}
		openEBSAdapter, providerErr := openebsprovider.New(openebsprovider.Config{
			ID: openebsprovider.ProviderID, Namespace: cfg.OpenEBSNamespace,
			Dynamic: kubeClients.Dynamic, Discovery: kubeClients.Discovery, Observer: obsMetrics,
		})
		if providerErr != nil {
			slog.Error("OpenEBS provider init failed", "err", providerErr)
			os.Exit(1)
		}
		if registerErr := storageRegistry.Register(context.Background(), openEBSAdapter); registerErr != nil {
			slog.Error("OpenEBS provider registration failed", "err", registerErr)
			os.Exit(1)
		}
	}
	if cfg.LinstorEnabled {
		if kubeClients == nil {
			slog.Error("LINSTOR provider requires Kubernetes connectivity")
			os.Exit(1)
		}
		linstorClient, clientErr := linstorprovider.NewClient(linstorprovider.ClientConfig{
			URL: cfg.LinstorControllerURL, Token: cfg.LinstorAuthToken, CAFile: cfg.LinstorCAFile,
			InsecureSkipVerify: cfg.LinstorInsecureTLS, Timeout: cfg.LinstorTimeout,
		})
		if clientErr != nil {
			slog.Error("LINSTOR client config invalid", "err", clientErr)
			os.Exit(1)
		}
		linstorAdapter, providerErr := linstorprovider.New(linstorprovider.Config{
			ID: linstorprovider.ProviderID, Namespace: cfg.LinstorNamespace,
			Dynamic: kubeClients.Dynamic, Client: linstorClient, Observer: obsMetrics,
		})
		if providerErr != nil {
			slog.Error("LINSTOR provider init failed", "err", providerErr)
			os.Exit(1)
		}
		if registerErr := storageRegistry.Register(context.Background(), linstorAdapter); registerErr != nil {
			slog.Error("LINSTOR provider registration failed", "err", registerErr)
			os.Exit(1)
		}
	}
	var rookCephAdapter *rookceph.Adapter
	var rookCephDashboard *rookceph.DashboardClient
	if cfg.RookCephEnabled {
		if kubeClients == nil {
			slog.Error("Rook/Ceph provider requires Kubernetes connectivity")
			os.Exit(1)
		}
		dashboard, dashboardErr := rookceph.NewDashboardClient(rookceph.DashboardConfig{
			URL: cfg.RookCephDashboardURL, Username: cfg.RookCephDashboardUsername, Password: cfg.RookCephDashboardPassword,
			CAFile: cfg.RookCephDashboardCAFile, InsecureSkipVerify: cfg.RookCephDashboardInsecureTLS,
		})
		if dashboardErr != nil {
			slog.Error("Rook/Ceph dashboard config invalid", "err", dashboardErr)
			os.Exit(1)
		}
		rookCephDashboard = dashboard
		prometheus, prometheusErr := rookceph.NewPrometheusClient(cfg.RookCephPrometheusURL, nil)
		if prometheusErr != nil {
			slog.Error("Rook/Ceph Prometheus config invalid", "err", prometheusErr)
			os.Exit(1)
		}
		rookCephAdapter, err = rookceph.New(rookceph.Config{
			ID: "rook-ceph", Namespace: cfg.RookCephNamespace, ClusterName: cfg.RookCephClusterName,
			Dynamic: kubeClients.Dynamic, Discovery: kubeClients.Discovery, Dashboard: dashboard, DashboardPublicURL: cfg.RookCephDashboardPublicURL, Prometheus: prometheus, Publisher: hub, Observer: obsMetrics,
			WritesEnabled: cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled, AllowStorageClassDelete: cfg.RookCephAllowStorageClassDelete, AllowPoolDelete: cfg.RookCephAllowPoolDelete,
			WritePolicy: func() (bool, bool, bool) {
				if policyManager == nil {
					return false, false, false
				}
				effective := policyManager.Snapshot().Effective
				return effective.AcceptNewOperations && effective.RookCephWrites, effective.AllowCephStorageClassDelete, effective.AllowCephPoolDelete
			},
		})
		if err != nil {
			slog.Error("Rook/Ceph provider init failed", "err", err)
			os.Exit(1)
		}
		if registerErr := storageRegistry.Register(context.Background(), rookCephAdapter); registerErr != nil {
			slog.Error("Rook/Ceph provider registration failed", "err", registerErr)
			os.Exit(1)
		}
		if startErr := rookCephAdapter.Start(watchCtx); startErr != nil {
			// CRD absence is a provider condition, not a process failure.
			slog.Warn("Rook/Ceph watches unavailable", "err", startErr)
		}
	}
	// Warm managed provider descriptors before serving navigation requests.
	// Discovered generic CSI drivers are merged asynchronously after inventory
	// sync; the shell can use this managed snapshot immediately.
	descriptorContext, cancelDescriptors := context.WithTimeout(context.Background(), 4*time.Second)
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
			slog.Warn("storage inventory init failed", "err", inventoryErr)
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
		events := auditStore.List(limit)
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
			slog.Warn("durable storage operations unavailable", "storeErr", storeErr, "plannerErr", plannerErr)
		} else {
			storageAPI.SetContextSources(func(ctx context.Context, limit int) ([]storage.ContextOperationRecord, error) {
				operations, err := operationStore.List(ctx, map[string]string{}, limit)
				if err != nil {
					return nil, err
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
				events := auditStore.List(limit)
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
				slog.Warn("storage operation controller unavailable", "err", controllerErr)
			} else if cfg.StorageWritesEnabled || cfg.StorageOperationRecoveryEnabled || (cfg.AdminPolicyControlEnabled && cfg.AdminPolicyInstallWriterRBAC) {
				operationController.Start(watchCtx)
			}
			var cephVersionCheck func(context.Context) bool
			if rookCephAdapter != nil {
				cephVersionCheck = rookCephAdapter.WriteSupported
			}
			storageOperationsAPI = storageoperations.NewAPI(storageoperations.APIConfig{Store: operationStore, Planner: operationPlanner, Audit: auditStore, Observer: obsMetrics, WritesEnabled: cfg.StorageWritesEnabled, CephWritesEnabled: cfg.RookCephWritesEnabled, AllowStorageClassDelete: cfg.RookCephAllowStorageClassDelete, AllowPoolDelete: cfg.RookCephAllowPoolDelete, CephPoolVerified: rookCephAdapter != nil && rookCephAdapter.PoolVerificationAvailable(), CephVersionCheck: cephVersionCheck, Policy: policyManager})
		}
	}
	var storagePolicyAPI *policy.API
	if policyManager != nil {
		storagePolicyAPI, err = policy.NewAPI(policy.APIConfig{
			Store: policyManager, Audit: auditStore, Secret: secret,
			ClusterIdentity: cfg.ClusterIdentity, ImpactResolver: storageoperations.PolicyImpact,
			Observer: obsMetrics,
			ActiveCount: func(ctx context.Context) (int, error) {
				if operationStore == nil {
					return 0, nil
				}
				operations, listErr := operationStore.List(ctx, map[string]string{}, 500)
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
			slog.Error("storage policy API init failed", "err", err)
			os.Exit(1)
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
	if rookCephDashboard != nil {
		if err := rookCephDashboard.Logout(ctx); err != nil {
			slog.Warn("Ceph Dashboard logout failed", "err", err)
		}
	}
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}
	slog.Info("highland-api stopped")
}

func kubeClientsDynamic(clients *kube.Clients) dynamic.Interface {
	if clients == nil {
		return nil
	}
	return clients.Dynamic
}
