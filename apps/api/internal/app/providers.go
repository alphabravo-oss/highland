package app

import (
	"context"
	"log/slog"

	"github.com/highland-io/highland/apps/api/internal/config"
	"github.com/highland-io/highland/apps/api/internal/kube"
	"github.com/highland-io/highland/apps/api/internal/observability"
	"github.com/highland-io/highland/apps/api/internal/policy"
	linstorprovider "github.com/highland-io/highland/apps/api/internal/providers/linstor"
	longhornprovider "github.com/highland-io/highland/apps/api/internal/providers/longhorn"
	openebsprovider "github.com/highland-io/highland/apps/api/internal/providers/openebs"
	"github.com/highland-io/highland/apps/api/internal/providers/rookceph"
	"github.com/highland-io/highland/apps/api/internal/storage"
	"github.com/highland-io/highland/apps/api/internal/watch"
	"k8s.io/client-go/dynamic"
)

// registerLonghorn builds and registers the Longhorn managed provider when enabled.
func registerLonghorn(cfg *config.Config, obs *observability.Metrics, registry *storage.Registry) (*longhornprovider.Adapter, error) {
	if !cfg.LonghornEnabled {
		return nil, nil
	}
	adapter, err := longhornprovider.New(longhornprovider.Config{
		ManagerURL: cfg.ManagerURL, Namespace: cfg.LonghornNamespace, Required: cfg.LonghornRequired,
		ScrapeInterval: cfg.MetricsInterval, Observer: obs,
	})
	if err != nil {
		return nil, providerErr("longhorn provider init failed", err)
	}
	adapter.Start()
	if registerErr := registry.Register(context.Background(), adapter); registerErr != nil {
		adapter.Stop()
		return nil, providerErr("longhorn provider registration failed", registerErr)
	}
	return adapter, nil
}

// registerOpenEBS builds and registers the OpenEBS provider when enabled.
func registerOpenEBS(cfg *config.Config, clients *kube.Clients, obs *observability.Metrics, registry *storage.Registry) error {
	if !cfg.OpenEBSEnabled {
		return nil
	}
	if clients == nil {
		return providerErr("OpenEBS provider requires Kubernetes connectivity", nil)
	}
	adapter, err := openebsprovider.New(openebsprovider.Config{
		ID: openebsprovider.ProviderID, Namespace: cfg.OpenEBSNamespace,
		Dynamic: clients.Dynamic, Discovery: clients.Discovery, Observer: obs,
	})
	if err != nil {
		return providerErr("OpenEBS provider init failed", err)
	}
	if registerErr := registry.Register(context.Background(), adapter); registerErr != nil {
		return providerErr("OpenEBS provider registration failed", registerErr)
	}
	return nil
}

// registerLinstor builds and registers the LINSTOR provider when enabled.
func registerLinstor(cfg *config.Config, clients *kube.Clients, obs *observability.Metrics, registry *storage.Registry) error {
	if !cfg.LinstorEnabled {
		return nil
	}
	if clients == nil {
		return providerErr("LINSTOR provider requires Kubernetes connectivity", nil)
	}
	client, err := linstorprovider.NewClient(linstorprovider.ClientConfig{
		URL: cfg.LinstorControllerURL, Token: cfg.LinstorAuthToken, CAFile: cfg.LinstorCAFile,
		InsecureSkipVerify: cfg.LinstorInsecureTLS, Timeout: cfg.LinstorTimeout,
	})
	if err != nil {
		return providerErr("LINSTOR client config invalid", err)
	}
	adapter, err := linstorprovider.New(linstorprovider.Config{
		ID: linstorprovider.ProviderID, Namespace: cfg.LinstorNamespace,
		Dynamic: clients.Dynamic, Client: client, Observer: obs,
	})
	if err != nil {
		return providerErr("LINSTOR provider init failed", err)
	}
	if registerErr := registry.Register(context.Background(), adapter); registerErr != nil {
		return providerErr("LINSTOR provider registration failed", registerErr)
	}
	return nil
}

// registerRookCeph builds, registers, and starts the Rook/Ceph provider when enabled.
func registerRookCeph(
	cfg *config.Config,
	clients *kube.Clients,
	obs *observability.Metrics,
	registry *storage.Registry,
	hub *watch.Hub,
	policyManager *policy.Manager,
	watchCtx context.Context,
	logger *slog.Logger,
) (*rookceph.Adapter, *rookceph.DashboardClient, error) {
	if !cfg.RookCephEnabled {
		return nil, nil, nil
	}
	if clients == nil {
		return nil, nil, providerErr("Rook/Ceph provider requires Kubernetes connectivity", nil)
	}
	dashboard, err := rookceph.NewDashboardClient(rookceph.DashboardConfig{
		URL: cfg.RookCephDashboardURL, Username: cfg.RookCephDashboardUsername, Password: cfg.RookCephDashboardPassword,
		CAFile: cfg.RookCephDashboardCAFile, InsecureSkipVerify: cfg.RookCephDashboardInsecureTLS,
	})
	if err != nil {
		return nil, nil, providerErr("Rook/Ceph dashboard config invalid", err)
	}
	prometheus, err := rookceph.NewPrometheusClient(cfg.RookCephPrometheusURL, nil)
	if err != nil {
		return nil, nil, providerErr("Rook/Ceph Prometheus config invalid", err)
	}
	adapter, err := rookceph.New(rookceph.Config{
		ID: "rook-ceph", Namespace: cfg.RookCephNamespace, ClusterName: cfg.RookCephClusterName,
		Dynamic: clients.Dynamic, Discovery: clients.Discovery, Dashboard: dashboard,
		DashboardPublicURL: cfg.RookCephDashboardPublicURL, Prometheus: prometheus, Publisher: hub, Observer: obs,
		WritesEnabled: cfg.StorageWritesEnabled && cfg.RookCephWritesEnabled,
		AllowStorageClassDelete: cfg.RookCephAllowStorageClassDelete, AllowPoolDelete: cfg.RookCephAllowPoolDelete,
		WritePolicy: func() (bool, bool, bool) {
			if policyManager == nil {
				return false, false, false
			}
			effective := policyManager.Snapshot().Effective
			return effective.AcceptNewOperations && effective.RookCephWrites, effective.AllowCephStorageClassDelete, effective.AllowCephPoolDelete
		},
	})
	if err != nil {
		return nil, nil, providerErr("Rook/Ceph provider init failed", err)
	}
	if registerErr := registry.Register(context.Background(), adapter); registerErr != nil {
		return nil, nil, providerErr("Rook/Ceph provider registration failed", registerErr)
	}
	if startErr := adapter.Start(watchCtx); startErr != nil {
		// CRD absence is a provider condition, not a process failure.
		logger.Warn("Rook/Ceph watches unavailable", "err", startErr)
	}
	return adapter, dashboard, nil
}

func kubeClientsDynamic(clients *kube.Clients) dynamic.Interface {
	if clients == nil {
		return nil
	}
	return clients.Dynamic
}
