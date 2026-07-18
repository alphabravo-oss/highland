package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// PostgresConfig configures the production durable audit sink (ADR-0004).
type PostgresConfig struct {
	DSN            string
	ConnectTimeout time.Duration
	// Required marks this sink as mandatory for privileged mutations.
	Required bool
}

// PostgresSink stores audit events in PostgreSQL with atomic inserts.
// The schema is created on first Connect when Migrate is true.
type PostgresSink struct {
	db       *sql.DB
	backend  string
	required bool
}

// OpenPostgres opens a Postgres audit sink. driverName should be "pgx" or "postgres"
// and the driver must already be registered by the caller (optional dependency).
func OpenPostgres(ctx context.Context, driverName string, cfg PostgresConfig) (*PostgresSink, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("%w: empty DSN", ErrUnavailable)
	}
	if driverName == "" {
		driverName = "pgx"
	}
	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	db, err := sql.Open(driverName, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %v", ErrUnavailable, err)
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(time.Hour)
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: ping: %v", ErrUnavailable, err)
	}
	s := &PostgresSink{db: db, backend: "postgres", required: cfg.Required}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *PostgresSink) migrate(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS highland_audit_events (
  id TEXT PRIMARY KEY,
  ts TIMESTAMPTZ NOT NULL,
  username TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  method TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL,
  source_ip TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  operation_id TEXT NOT NULL DEFAULT '',
  action_id TEXT NOT NULL DEFAULT '',
  provider_id TEXT NOT NULL DEFAULT '',
  provider_kind TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS highland_audit_ts_idx ON highland_audit_events (ts DESC);
CREATE INDEX IF NOT EXISTS highland_audit_action_idx ON highland_audit_events (action);
CREATE INDEX IF NOT EXISTS highland_audit_operation_idx ON highland_audit_events (operation_id);
`
	_, err := s.db.ExecContext(ctx, ddl)
	if err != nil {
		return fmt.Errorf("%w: migrate: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *PostgresSink) Append(ctx context.Context, e Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: nil postgres sink", ErrUnavailable)
	}
	if e.SchemaVersion == 0 {
		e.SchemaVersion = SchemaVersion
	}
	if err := ValidateEvent(e); err != nil {
		return err
	}
	e.Message = redactMessage(e.Message)
	if e.ID == "" {
		e.ID = fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102T150405.000000000"), time.Now().UnixNano())
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("%w: marshal: %v", ErrInvalidEvent, err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO highland_audit_events (
  id, ts, username, role, action, target, method, path, result, source_ip, message,
  operation_id, action_id, provider_id, provider_kind, payload
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (id) DO NOTHING
`, e.ID, e.Timestamp, e.Username, e.Role, e.Action, e.Target, e.Method, e.Path, e.Result, e.SourceIP, e.Message,
		e.OperationID, e.ActionID, e.ProviderID, e.ProviderKind, payload)
	if err != nil {
		return fmt.Errorf("%w: insert: %v", ErrUnavailable, err)
	}
	return nil
}

func (s *PostgresSink) List(ctx context.Context, query Query) (Page, error) {
	if s == nil || s.db == nil {
		return Page{}, fmt.Errorf("%w: nil postgres sink", ErrUnavailable)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT payload FROM highland_audit_events
WHERE ($1 = '' OR action = $1)
  AND ($2 = '' OR result = $2)
  AND ($3 = '' OR provider_id = $3)
  AND ($4 = '' OR operation_id = $4)
  AND ($5 = '' OR username = $5)
ORDER BY ts DESC, id DESC
LIMIT $6
`, query.Action, query.Result, query.ProviderID, query.OperationID, query.Username, limit)
	if err != nil {
		return Page{}, fmt.Errorf("%w: query: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return Page{}, err
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return Page{}, fmt.Errorf("%w: decode: %v", ErrInvalidEvent, err)
		}
		events = append(events, e)
	}
	return Page{Events: events}, rows.Err()
}

func (s *PostgresSink) Health(ctx context.Context) Health {
	h := Health{Backend: s.backend, Durable: true}
	if s == nil || s.db == nil {
		h.Status = "unavailable"
		return h
	}
	if err := s.db.PingContext(ctx); err != nil {
		h.Status = "unavailable"
		h.Message = "ping failed"
		return h
	}
	h.Status = "ok"
	return h
}

func (s *PostgresSink) Durable() bool { return true }

// DurableTerminalOperationIDs implements TerminalEvidence for operation GC.
func (s *PostgresSink) DurableTerminalOperationIDs() (map[string]bool, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: nil postgres sink", ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT operation_id FROM highland_audit_events
WHERE operation_id <> ''
  AND action IN ('storage_operation_succeeded','storage_operation_failed','storage_operation_cancelled')
`)
	if err != nil {
		return nil, fmt.Errorf("%w: terminal ids: %v", ErrUnavailable, err)
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (s *PostgresSink) Close(ctx context.Context) error {
	_ = ctx
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Ensure PostgresSink satisfies Sink and TerminalEvidence.
var (
	_ Sink             = (*PostgresSink)(nil)
	_ TerminalEvidence = (*PostgresSink)(nil)
)
