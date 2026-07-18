package app

import (
	"context"
	"log/slog"
	"strings"

	"github.com/highland-io/highland/apps/api/internal/audit"
	"github.com/highland-io/highland/apps/api/internal/config"
)

// buildAuditSink selects Postgres (multi-replica durable), JSONL (single-replica
// durable), or memory. Injected deps.Audit wins when non-nil.
func buildAuditSink(ctx context.Context, deps Dependencies, cfg *config.Config, logger *slog.Logger) (audit.Sink, error) {
	if deps.Audit != nil {
		return deps.Audit, nil
	}
	dsn := strings.TrimSpace(cfg.AuditPostgresDSN)
	if dsn == "" {
		dsn = strings.TrimSpace(envOrLocal("HIGHLAND_AUDIT_POSTGRES_DSN", ""))
	}
	if dsn != "" {
		sink, err := audit.OpenPostgres(ctx, "pgx", audit.PostgresConfig{
			DSN:      dsn,
			Required: cfg.RequireAuditDurable,
		})
		if err != nil {
			return nil, dependencyErr("durable postgres audit sink unavailable", err)
		}
		if logger != nil {
			logger.Info("audit sink selected", "backend", "postgres", "durable", true)
		}
		return sink, nil
	}
	// Memory ring with optional single-replica JSONL file.
	return audit.NewStore(2000, cfg.AuditFile), nil
}
