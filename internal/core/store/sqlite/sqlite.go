// Package sqlite is the M1 StateStore (pure-Go modernc driver; hand-written SQL).
package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hopboxdev/hopbox/internal/core/store"
	"github.com/hopboxdev/hopbox/internal/core/workspace"
)

//go:embed schema.sql
var schema string

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // sqlite: serialize writers; simplest correct M1 choice
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Lightweight migration for pre-M3 databases: add the ingress columns if a
	// table created by an older schema lacks them. ADD COLUMN on an existing
	// column errors with "duplicate column name" — tolerate that.
	for _, col := range []string{
		"ALTER TABLE workspaces ADD COLUMN ingress_spec TEXT NOT NULL DEFAULT '[]'",
		"ALTER TABLE workspaces ADD COLUMN endpoints TEXT NOT NULL DEFAULT '[]'",
	} {
		if _, err := db.Exec(col); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return "[]"
	}
	return string(b)
}

func (s *Store) Close() error { return s.db.Close() }

const ts = time.RFC3339Nano

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) CreateWorkspace(ctx context.Context, w *workspace.Workspace) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workspaces
		(id,tenant_id,owner,name,image_ref,mem_mb,phase,instance_ref,home_mount,
		 bootstrap_token,agent_connected,message,ingress_spec,endpoints,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.TenantID, w.Owner, w.Name, w.ImageRef, w.MemMB, string(w.Phase),
		w.InstanceRef, w.HomeMount, w.BootstrapToken, b2i(w.AgentConnected), w.Message,
		marshalJSON(w.Ingress), marshalJSON(w.Endpoints),
		w.CreatedAt.Format(ts), w.UpdatedAt.Format(ts))
	return err
}

const cols = `id,tenant_id,owner,name,image_ref,mem_mb,phase,instance_ref,home_mount,
	bootstrap_token,agent_connected,message,ingress_spec,endpoints,created_at,updated_at`

func scan(row interface{ Scan(...any) error }) (*workspace.Workspace, error) {
	var w workspace.Workspace
	var phase string
	var connected int
	var ingressJSON, endpointsJSON string
	var created, updated string
	if err := row.Scan(&w.ID, &w.TenantID, &w.Owner, &w.Name, &w.ImageRef, &w.MemMB,
		&phase, &w.InstanceRef, &w.HomeMount, &w.BootstrapToken, &connected, &w.Message,
		&ingressJSON, &endpointsJSON, &created, &updated); err != nil {
		return nil, err
	}
	w.Phase = workspace.Phase(phase)
	w.AgentConnected = connected != 0
	if err := json.Unmarshal([]byte(ingressJSON), &w.Ingress); err != nil {
		return nil, fmt.Errorf("parse ingress_spec %q: %w", ingressJSON, err)
	}
	if err := json.Unmarshal([]byte(endpointsJSON), &w.Endpoints); err != nil {
		return nil, fmt.Errorf("parse endpoints %q: %w", endpointsJSON, err)
	}
	var err error
	if w.CreatedAt, err = time.Parse(ts, created); err != nil {
		return nil, fmt.Errorf("parse created_at %q: %w", created, err)
	}
	if w.UpdatedAt, err = time.Parse(ts, updated); err != nil {
		return nil, fmt.Errorf("parse updated_at %q: %w", updated, err)
	}
	return &w, nil
}

func (s *Store) one(ctx context.Context, where string, args ...any) (*workspace.Workspace, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM workspaces WHERE `+where, args...)
	w, err := scan(row)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	return w, err
}

func (s *Store) GetWorkspace(ctx context.Context, tenantID, id string) (*workspace.Workspace, error) {
	return s.one(ctx, "tenant_id=? AND id=?", tenantID, id)
}
func (s *Store) GetByName(ctx context.Context, tenantID, name string) (*workspace.Workspace, error) {
	return s.one(ctx, "tenant_id=? AND name=?", tenantID, name)
}
func (s *Store) GetByToken(ctx context.Context, token string) (*workspace.Workspace, error) {
	if token == "" {
		return nil, store.ErrNotFound
	}
	return s.one(ctx, "bootstrap_token=?", token)
}

func (s *Store) list(ctx context.Context, where string, args ...any) ([]*workspace.Workspace, error) {
	q := `SELECT ` + cols + ` FROM workspaces`
	if where != "" {
		q += ` WHERE ` + where
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*workspace.Workspace
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) ListWorkspaces(ctx context.Context, tenantID string) ([]*workspace.Workspace, error) {
	return s.list(ctx, "tenant_id=?", tenantID)
}
func (s *Store) ListAll(ctx context.Context) ([]*workspace.Workspace, error) {
	return s.list(ctx, "")
}

func (s *Store) UpdateWorkspace(ctx context.Context, w *workspace.Workspace) error {
	w.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE workspaces SET
		  image_ref=?, mem_mb=?, phase=?, instance_ref=?, home_mount=?,
		  bootstrap_token=?, agent_connected=?, message=?, ingress_spec=?, endpoints=?, updated_at=?
		WHERE tenant_id=? AND id=?`,
		w.ImageRef, w.MemMB, string(w.Phase), w.InstanceRef, w.HomeMount,
		w.BootstrapToken, b2i(w.AgentConnected), w.Message,
		marshalJSON(w.Ingress), marshalJSON(w.Endpoints), w.UpdatedAt.Format(ts),
		w.TenantID, w.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteWorkspace(ctx context.Context, tenantID, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workspaces WHERE tenant_id=? AND id=?`, tenantID, id)
	return err
}
