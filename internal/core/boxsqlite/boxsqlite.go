// Package boxsqlite is a persistent, box-native box.Store backed by sqlite
// (pure-Go modernc driver). It stores box.Box rows directly — no workspace /
// dev-env coupling — so a box-only daemon (boxd) survives restarts without
// pulling in the dev-env layer.
package boxsqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

const schema = `
CREATE TABLE IF NOT EXISTS boxes (
  id              TEXT PRIMARY KEY,
  tenant_id       TEXT NOT NULL,
  owner           TEXT NOT NULL,
  name            TEXT NOT NULL,
  image_ref       TEXT NOT NULL,
  backend         TEXT NOT NULL DEFAULT '',
  mem_mb          INTEGER NOT NULL DEFAULT 0,
  cpu_millis      INTEGER NOT NULL DEFAULT 0,
  ephemeral       INTEGER NOT NULL DEFAULT 0,
  grace_ns        INTEGER NOT NULL DEFAULT 0,
  max_ttl_ns      INTEGER NOT NULL DEFAULT 0,
  deadline        TEXT NOT NULL DEFAULT '',
  phase           TEXT NOT NULL,
  instance_ref    TEXT NOT NULL DEFAULT '',
  ip              TEXT NOT NULL DEFAULT '',
  bootstrap_token TEXT NOT NULL DEFAULT '',
  agent_connected INTEGER NOT NULL DEFAULT 0,
  attached        INTEGER NOT NULL DEFAULT 0,
  message         TEXT NOT NULL DEFAULT '',
  load            REAL NOT NULL DEFAULT 0,
  last_active     TEXT NOT NULL DEFAULT '',
  auto_suspend     INTEGER NOT NULL DEFAULT 0,
  keep_alive_until TEXT NOT NULL DEFAULT '',
  idle_timeout_ns  INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL,
  updated_at      TEXT NOT NULL,
  UNIQUE(tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_box_token ON boxes(bootstrap_token);
CREATE INDEX IF NOT EXISTS idx_box_ip ON boxes(ip);`

// Store is a sqlite-backed box.Store.
type Store struct{ db *sql.DB }

var _ box.Store = (*Store)(nil)

// Open opens (and migrates) the box database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open box db: %w", err)
	}
	db.SetMaxOpenConns(1) // serialize writers (simplest correct choice)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("box schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const ts = time.RFC3339Nano

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

const cols = `id,tenant_id,owner,name,image_ref,backend,mem_mb,cpu_millis,ephemeral,grace_ns,
	max_ttl_ns,deadline,phase,instance_ref,ip,bootstrap_token,agent_connected,attached,message,load,last_active,auto_suspend,keep_alive_until,idle_timeout_ns,created_at,updated_at`

func scan(row interface{ Scan(...any) error }) (*box.Box, error) {
	var b box.Box
	var backend, deadline, phase, lastActive, keepAlive, created, updated string
	var load float64
	var autoSuspend int
	var idleTO int64
	var memMB, cpu, graceNS, maxTTLNS int64
	var ephemeral, connected, attached int
	if err := row.Scan(&b.ID, &b.TenantID, &b.Owner, &b.Name, &b.ImageRef, &backend, &memMB, &cpu,
		&ephemeral, &graceNS, &maxTTLNS, &deadline, &phase, &b.InstanceRef, &b.IP, &b.BootstrapToken,
		&connected, &attached, &b.Message, &load, &lastActive, &autoSuspend, &keepAlive, &idleTO, &created, &updated); err != nil {
		return nil, err
	}
	b.Backend, b.MemMB, b.CPUMillis = backend, memMB, cpu
	b.Ephemeral = ephemeral != 0
	b.Grace, b.MaxTTL = time.Duration(graceNS), time.Duration(maxTTLNS)
	b.Phase = box.Phase(phase)
	b.AgentConnected = connected != 0
	b.Attached = attached != 0
	b.Load = load
	if lastActive != "" {
		la, err := time.Parse(ts, lastActive)
		if err != nil {
			return nil, fmt.Errorf("parse last_active %q: %w", lastActive, err)
		}
		b.LastActive = la
	}
	b.AutoSuspend = autoSuspend != 0
	if keepAlive != "" {
		ka, err := time.Parse(ts, keepAlive)
		if err != nil {
			return nil, fmt.Errorf("parse keep_alive_until %q: %w", keepAlive, err)
		}
		b.KeepAliveUntil = ka
	}
	b.IdleTimeoutOverride = time.Duration(idleTO)
	if deadline != "" {
		d, err := time.Parse(ts, deadline)
		if err != nil {
			return nil, fmt.Errorf("parse deadline %q: %w", deadline, err)
		}
		b.Deadline = &d
	}
	var err error
	if b.CreatedAt, err = time.Parse(ts, created); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if b.UpdatedAt, err = time.Parse(ts, updated); err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return &b, nil
}

func (s *Store) one(ctx context.Context, where string, args ...any) (*box.Box, error) {
	b, err := scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM boxes WHERE `+where, args...))
	if err == sql.ErrNoRows {
		return nil, box.ErrNotFound
	}
	return b, err
}

func (s *Store) Get(ctx context.Context, _ /*tenant*/, id string) (*box.Box, error) {
	return s.one(ctx, "id=?", id)
}
func (s *Store) GetByName(ctx context.Context, tenant, name string) (*box.Box, error) {
	return s.one(ctx, "tenant_id=? AND name=?", tenant, name)
}

// GetByToken backs the agent hub's resolver (not part of box.Store).
func (s *Store) GetByToken(ctx context.Context, token string) (*box.Box, error) {
	if token == "" {
		return nil, box.ErrNotFound
	}
	return s.one(ctx, "bootstrap_token=?", token)
}

// List returns all boxes when tenant is "" (the reconciler sweep), else by tenant.
func (s *Store) List(ctx context.Context, tenant string) ([]*box.Box, error) {
	where, args := "1=1", []any(nil)
	if tenant != "" {
		where, args = "tenant_id=?", []any{tenant}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM boxes WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*box.Box
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func deadlineStr(b *box.Box) string {
	if b.Deadline == nil {
		return ""
	}
	return b.Deadline.Format(ts)
}

func lastActiveStr(b *box.Box) string {
	if b.LastActive.IsZero() {
		return ""
	}
	return b.LastActive.Format(ts)
}

func keepAliveStr(b *box.Box) string {
	if b.KeepAliveUntil.IsZero() {
		return ""
	}
	return b.KeepAliveUntil.Format(ts)
}

func (s *Store) Create(ctx context.Context, b *box.Box) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO boxes (`+cols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.TenantID, b.Owner, b.Name, b.ImageRef, b.Backend, b.MemMB, b.CPUMillis,
		b2i(b.Ephemeral), int64(b.Grace), int64(b.MaxTTL), deadlineStr(b), string(b.Phase),
		b.InstanceRef, b.IP, b.BootstrapToken, b2i(b.AgentConnected), b2i(b.Attached), b.Message, b.Load, lastActiveStr(b), b2i(b.AutoSuspend), keepAliveStr(b), int64(b.IdleTimeoutOverride),
		b.CreatedAt.Format(ts), b.UpdatedAt.Format(ts))
	return err
}

func (s *Store) Update(ctx context.Context, b *box.Box) error {
	b.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE boxes SET
		  image_ref=?, backend=?, mem_mb=?, cpu_millis=?, ephemeral=?, grace_ns=?, max_ttl_ns=?,
		  deadline=?, phase=?, instance_ref=?, ip=?, bootstrap_token=?, agent_connected=?, attached=?,
		  message=?, load=?, last_active=?, auto_suspend=?, keep_alive_until=?, idle_timeout_ns=?, updated_at=?
		WHERE id=?`,
		b.ImageRef, b.Backend, b.MemMB, b.CPUMillis, b2i(b.Ephemeral), int64(b.Grace), int64(b.MaxTTL),
		deadlineStr(b), string(b.Phase), b.InstanceRef, b.IP, b.BootstrapToken, b2i(b.AgentConnected),
		b2i(b.Attached), b.Message, b.Load, lastActiveStr(b), b2i(b.AutoSuspend), keepAliveStr(b), int64(b.IdleTimeoutOverride), b.UpdatedAt.Format(ts), b.ID)
	return err
}

func (s *Store) Delete(ctx context.Context, _ /*tenant*/, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM boxes WHERE id=?`, id)
	return err
}

// GetByIP resolves a box by its network IP — backs the metadata API's
// identify-by-source-IP. Not part of box.Store.
func (s *Store) GetByIP(ctx context.Context, ip string) (*box.Box, error) {
	if ip == "" {
		return nil, box.ErrNotFound
	}
	return s.one(ctx, "ip=?", ip)
}
