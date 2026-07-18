package app

import (
	"context"
	"log/slog"
	"time"
)

// Close stops background work and the HTTP server in a deterministic order.
// It is safe to call more than once; subsequent calls are no-ops.
//
// Stop order:
//  1. cancel watch/informer context (hubs, inventory, policy, operations)
//  2. stop SSE hub (unblocks long-lived streams for Shutdown)
//  3. stop Longhorn metrics scraper
//  4. logout Ceph dashboard session
//  5. HTTP server graceful shutdown
//  6. close audit sink / login limiter when present
func (a *App) Close(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		a.closeErr = a.shutdown(ctx)
	})
	return a.closeErr
}

func (a *App) shutdown(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	logger := a.logger
	if logger == nil {
		logger = slog.Default()
	}

	if a.cancelWatch != nil {
		a.cancelWatch()
	}
	if a.hub != nil {
		a.hub.Stop()
	}
	if a.longhornAdapter != nil {
		a.longhornAdapter.Stop()
	}
	if a.rookCephDashboard != nil {
		if err := a.rookCephDashboard.Logout(ctx); err != nil {
			logger.Warn("Ceph Dashboard logout failed", "err", err)
		}
	}
	var shutdownErr error
	if a.server != nil {
		if err := a.server.Shutdown(ctx); err != nil {
			logger.Error("shutdown error", "err", err)
			shutdownErr = err
		}
	}
	if a.auditStore != nil {
		if err := a.auditStore.Close(ctx); err != nil {
			logger.Warn("audit close failed", "err", err)
			if shutdownErr == nil {
				shutdownErr = err
			}
		}
	}
	if a.limiter != nil {
		if err := a.limiter.Close(); err != nil {
			logger.Warn("limiter close failed", "err", err)
			if shutdownErr == nil {
				shutdownErr = err
			}
		}
	}
	if shutdownErr == nil {
		logger.Info("highland-api stopped")
	}
	return shutdownErr
}
