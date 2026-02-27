# Control Plane Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build `hopbox-cp`, a Go API server with embedded React dashboard for managing the managed Hopbox product — GitHub OAuth, workspace CRUD (delegating to hostd via gRPC), host registry, and a minimal web dashboard.

**Architecture:** Single Go binary (`hopbox-cp`) serves a JSON API at `/api/*` and an embedded React SPA at `/*`. Connects to `hopbox-hostd` on bare metal via gRPC over mTLS. PostgreSQL for persistence. Separate private repo.

**Tech Stack:** Go (pgx, net/http, grpc), PostgreSQL, React 19 + shadcn/ui + TypeScript + Vite, buf for proto codegen.

**Design doc:** `docs/plans/2026-02-27-control-plane-design.md`

---

### Task 1: Repository scaffolding

Create the `hopbox-cp` repo with Go module, directory structure, and initial Makefile.

**Files:**
- Create: `~/Developer/hopbox-cp/go.mod`
- Create: `~/Developer/hopbox-cp/cmd/hopbox-cp/main.go`
- Create: `~/Developer/hopbox-cp/internal/cp/server.go`
- Create: `~/Developer/hopbox-cp/Makefile`
- Create: `~/Developer/hopbox-cp/.gitignore`
- Create: `~/Developer/hopbox-cp/CLAUDE.md`

**Step 1: Initialize repo and Go module**

```bash
mkdir -p ~/Developer/hopbox-cp
cd ~/Developer/hopbox-cp
git init
go mod init github.com/hopboxdev/hopbox-cp
```

**Step 2: Create directory structure**

```bash
mkdir -p cmd/hopbox-cp
mkdir -p internal/cp
mkdir -p internal/db
mkdir -p internal/auth
mkdir -p migrations
mkdir -p web
mkdir -p scripts
```

**Step 3: Write minimal main.go**

```go
// cmd/hopbox-cp/main.go
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
)

func main() {
	var (
		listenAddr = flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
		dbURL      = flag.String("db", "postgres://localhost:5432/hopbox_cp?sslmode=disable", "PostgreSQL connection URL")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, *listenAddr, *dbURL); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, listenAddr, dbURL string) error {
	log.Printf("hopbox-cp starting on %s", listenAddr)
	<-ctx.Done()
	return nil
}
```

**Step 4: Write Makefile**

```makefile
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w

DIST := dist

.PHONY: build clean

build: $(DIST)/hopbox-cp

$(DIST)/hopbox-cp: $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hopbox-cp/

$(DIST):
	mkdir -p $(DIST)

clean:
	rm -rf $(DIST)
```

**Step 5: Write .gitignore**

```
dist/
node_modules/
web/dist/
*.env
*.pem
*.key
```

**Step 6: Write CLAUDE.md**

```markdown
# hopbox-cp

Control plane for the managed Hopbox product.

## Build

make build

## Run locally

go run ./cmd/hopbox-cp/ --listen 127.0.0.1:8080 --db postgres://localhost:5432/hopbox_cp?sslmode=disable

## Conventions

- Error variables always named `err`, never suffixed
- File permissions: 0600 for sensitive files, 0700 for directories
- Graceful shutdown via context cancellation
- HTTP handlers use standard net/http ServeMux
- JSON responses via encoding/json
- Database queries via pgx (no ORM)
```

**Step 7: Verify build and commit**

```bash
go build ./cmd/hopbox-cp/
git add -A
git commit -m "init: scaffold hopbox-cp repo"
```

---

### Task 2: Database schema and connection

Set up PostgreSQL migrations and connection pool.

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/db_test.go`
- Create: `migrations/001_init.up.sql`
- Create: `migrations/001_init.down.sql`
- Create: `internal/db/migrate.go`
- Modify: `cmd/hopbox-cp/main.go`

**Step 1: Write the migration SQL**

```sql
-- migrations/001_init.up.sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    github_id  BIGINT NOT NULL UNIQUE,
    github_login TEXT NOT NULL,
    email      TEXT,
    avatar_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE hosts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    grpc_endpoint   TEXT NOT NULL,
    public_ip       TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'online',
    total_vcpus     INT NOT NULL DEFAULT 0,
    available_vcpus INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    host_id       UUID NOT NULL REFERENCES hosts(id),
    name          TEXT NOT NULL,
    state         TEXT NOT NULL DEFAULT 'creating',
    vcpus         INT NOT NULL,
    memory_mb     INT NOT NULL,
    disk_gb       INT NOT NULL,
    host_port     INT,
    vm_ip         TEXT,
    client_config JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, name)
);

CREATE INDEX idx_workspaces_user_id ON workspaces(user_id);
CREATE INDEX idx_workspaces_host_id ON workspaces(host_id);
```

```sql
-- migrations/001_init.down.sql
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS hosts;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
```

**Step 2: Write the database connection package**

```go
// internal/db/db.go
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgx connection pool.
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new database connection pool.
func New(ctx context.Context, connURL string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close closes the connection pool.
func (d *DB) Close() {
	d.Pool.Close()
}
```

**Step 3: Write the migration runner**

```go
// internal/db/migrate.go
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"
)

//go:embed ../migrations/*.sql
// NOTE: This won't work with ../migrations. Migrations will be embedded
// from the cmd package instead. See step below.

// RunMigrations executes all up migrations in order.
func (d *DB) RunMigrations(ctx context.Context, migrationsFS fs.FS) error {
	// Create migrations tracking table.
	_, err := d.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Find all .up.sql files.
	entries, err := fs.Glob(migrationsFS, "*.up.sql")
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}
	sort.Strings(entries)

	for _, entry := range entries {
		// Extract version number from filename (e.g., "001_init.up.sql" -> 1).
		parts := strings.SplitN(entry, "_", 2)
		var version int
		if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
			return fmt.Errorf("parse migration version %q: %w", entry, err)
		}

		// Check if already applied.
		var exists bool
		err := d.Pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)",
			version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if exists {
			continue
		}

		// Read and execute.
		sql, err := fs.ReadFile(migrationsFS, entry)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry, err)
		}

		if _, err := d.Pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("run migration %s: %w", entry, err)
		}

		if _, err := d.Pool.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)", version,
		); err != nil {
			return fmt.Errorf("record migration %d: %w", version, err)
		}

		log.Printf("applied migration %s", entry)
	}

	return nil
}
```

**Step 4: Wire database into main.go**

```go
// cmd/hopbox-cp/main.go
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"os"
	"os/signal"

	"github.com/hopboxdev/hopbox-cp/internal/db"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func main() {
	var (
		listenAddr = flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
		dbURL      = flag.String("db", "postgres://localhost:5432/hopbox_cp?sslmode=disable", "PostgreSQL connection URL")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx, *listenAddr, *dbURL); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, listenAddr, dbURL string) error {
	database, err := db.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer database.Close()

	migFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	if err := database.RunMigrations(ctx, migFS); err != nil {
		return err
	}

	log.Printf("hopbox-cp starting on %s", listenAddr)
	<-ctx.Done()
	return nil
}
```

Note: The `migrations/` directory must be at `cmd/hopbox-cp/migrations/` for the embed directive, OR use a relative symlink / copy step. Simpler: move the embed to a separate package or keep migrations at the top level and embed from main.

**Step 5: Install dependencies, verify build, commit**

```bash
go get github.com/jackc/pgx/v5
go mod tidy
go build ./cmd/hopbox-cp/
git add -A
git commit -m "feat: add database connection and migration runner"
```

---

### Task 3: Database queries (users, sessions, hosts, workspaces)

Write the CRUD query functions for all four tables.

**Files:**
- Create: `internal/db/users.go`
- Create: `internal/db/sessions.go`
- Create: `internal/db/hosts.go`
- Create: `internal/db/workspaces.go`

**Step 1: Write user queries**

```go
// internal/db/users.go
package db

import (
	"context"
	"fmt"
	"time"
)

type User struct {
	ID          string    `json:"id"`
	GitHubID    int64     `json:"github_id"`
	GitHubLogin string    `json:"github_login"`
	Email       string    `json:"email,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// UpsertUser creates or updates a user by GitHub ID. Returns the user.
func (d *DB) UpsertUser(ctx context.Context, githubID int64, login, email, avatarURL string) (*User, error) {
	var u User
	err := d.Pool.QueryRow(ctx, `
		INSERT INTO users (github_id, github_login, email, avatar_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (github_id) DO UPDATE SET
			github_login = EXCLUDED.github_login,
			email = EXCLUDED.email,
			avatar_url = EXCLUDED.avatar_url
		RETURNING id, github_id, github_login, email, avatar_url, created_at
	`, githubID, login, email, avatarURL).Scan(
		&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Email, &u.AvatarURL, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return &u, nil
}

// GetUser returns a user by ID.
func (d *DB) GetUser(ctx context.Context, id string) (*User, error) {
	var u User
	err := d.Pool.QueryRow(ctx, `
		SELECT id, github_id, github_login, email, avatar_url, created_at
		FROM users WHERE id = $1
	`, id).Scan(&u.ID, &u.GitHubID, &u.GitHubLogin, &u.Email, &u.AvatarURL, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}
```

**Step 2: Write session queries**

```go
// internal/db/sessions.go
package db

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

const sessionDuration = 30 * 24 * time.Hour // 30 days

// CreateSession creates a new session for a user. Returns the session token.
func (d *DB) CreateSession(ctx context.Context, userID string) (*Session, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	var s Session
	err = d.Pool.QueryRow(ctx, `
		INSERT INTO sessions (id, user_id, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, expires_at, created_at
	`, token, userID, time.Now().Add(sessionDuration)).Scan(
		&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &s, nil
}

// ValidateSession returns the session if valid and not expired.
func (d *DB) ValidateSession(ctx context.Context, token string) (*Session, error) {
	var s Session
	err := d.Pool.QueryRow(ctx, `
		SELECT id, user_id, expires_at, created_at
		FROM sessions WHERE id = $1 AND expires_at > now()
	`, token).Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("validate session: %w", err)
	}
	return &s, nil
}

// DeleteSession removes a session by ID.
func (d *DB) DeleteSession(ctx context.Context, token string) error {
	_, err := d.Pool.Exec(ctx, "DELETE FROM sessions WHERE id = $1", token)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
```

**Step 3: Write host queries**

```go
// internal/db/hosts.go
package db

import (
	"context"
	"fmt"
	"time"
)

type Host struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	GRPCEndpoint   string    `json:"grpc_endpoint"`
	PublicIP       string    `json:"public_ip"`
	Status         string    `json:"status"`
	TotalVCPUs     int       `json:"total_vcpus"`
	AvailableVCPUs int       `json:"available_vcpus"`
	CreatedAt      time.Time `json:"created_at"`
}

func (d *DB) CreateHost(ctx context.Context, name, grpcEndpoint, publicIP string) (*Host, error) {
	var h Host
	err := d.Pool.QueryRow(ctx, `
		INSERT INTO hosts (name, grpc_endpoint, public_ip)
		VALUES ($1, $2, $3)
		RETURNING id, name, grpc_endpoint, public_ip, status, total_vcpus, available_vcpus, created_at
	`, name, grpcEndpoint, publicIP).Scan(
		&h.ID, &h.Name, &h.GRPCEndpoint, &h.PublicIP, &h.Status,
		&h.TotalVCPUs, &h.AvailableVCPUs, &h.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}
	return &h, nil
}

func (d *DB) ListHosts(ctx context.Context) ([]Host, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, name, grpc_endpoint, public_ip, status, total_vcpus, available_vcpus, created_at
		FROM hosts ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	defer rows.Close()

	var hosts []Host
	for rows.Next() {
		var h Host
		if err := rows.Scan(&h.ID, &h.Name, &h.GRPCEndpoint, &h.PublicIP, &h.Status,
			&h.TotalVCPUs, &h.AvailableVCPUs, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan host: %w", err)
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (d *DB) GetHost(ctx context.Context, id string) (*Host, error) {
	var h Host
	err := d.Pool.QueryRow(ctx, `
		SELECT id, name, grpc_endpoint, public_ip, status, total_vcpus, available_vcpus, created_at
		FROM hosts WHERE id = $1
	`, id).Scan(&h.ID, &h.Name, &h.GRPCEndpoint, &h.PublicIP, &h.Status,
		&h.TotalVCPUs, &h.AvailableVCPUs, &h.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get host: %w", err)
	}
	return &h, nil
}

func (d *DB) DeleteHost(ctx context.Context, id string) error {
	_, err := d.Pool.Exec(ctx, "DELETE FROM hosts WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete host: %w", err)
	}
	return nil
}

func (d *DB) UpdateHostCapacity(ctx context.Context, id string, total, available int) error {
	_, err := d.Pool.Exec(ctx, `
		UPDATE hosts SET total_vcpus = $2, available_vcpus = $3 WHERE id = $1
	`, id, total, available)
	if err != nil {
		return fmt.Errorf("update host capacity: %w", err)
	}
	return nil
}
```

**Step 4: Write workspace queries**

```go
// internal/db/workspaces.go
package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type Workspace struct {
	ID           string          `json:"id"`
	UserID       string          `json:"user_id"`
	HostID       string          `json:"host_id"`
	Name         string          `json:"name"`
	State        string          `json:"state"`
	VCPUs        int             `json:"vcpus"`
	MemoryMB     int             `json:"memory_mb"`
	DiskGB       int             `json:"disk_gb"`
	HostPort     int             `json:"host_port,omitempty"`
	VMIP         string          `json:"vm_ip,omitempty"`
	ClientConfig json.RawMessage `json:"client_config,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type CreateWorkspaceParams struct {
	UserID   string
	HostID   string
	Name     string
	VCPUs    int
	MemoryMB int
	DiskGB   int
}

func (d *DB) CreateWorkspace(ctx context.Context, p CreateWorkspaceParams) (*Workspace, error) {
	var w Workspace
	err := d.Pool.QueryRow(ctx, `
		INSERT INTO workspaces (user_id, host_id, name, vcpus, memory_mb, disk_gb)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, host_id, name, state, vcpus, memory_mb, disk_gb,
		          host_port, vm_ip, client_config, created_at, updated_at
	`, p.UserID, p.HostID, p.Name, p.VCPUs, p.MemoryMB, p.DiskGB).Scan(
		&w.ID, &w.UserID, &w.HostID, &w.Name, &w.State, &w.VCPUs, &w.MemoryMB, &w.DiskGB,
		&w.HostPort, &w.VMIP, &w.ClientConfig, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	return &w, nil
}

func (d *DB) UpdateWorkspaceProvisioned(ctx context.Context, id string, state string, hostPort int, vmIP string, clientConfig json.RawMessage) error {
	_, err := d.Pool.Exec(ctx, `
		UPDATE workspaces
		SET state = $2, host_port = $3, vm_ip = $4, client_config = $5, updated_at = now()
		WHERE id = $1
	`, id, state, hostPort, vmIP, clientConfig)
	if err != nil {
		return fmt.Errorf("update workspace provisioned: %w", err)
	}
	return nil
}

func (d *DB) UpdateWorkspaceState(ctx context.Context, id, state string) error {
	_, err := d.Pool.Exec(ctx, `
		UPDATE workspaces SET state = $2, updated_at = now() WHERE id = $1
	`, id, state)
	if err != nil {
		return fmt.Errorf("update workspace state: %w", err)
	}
	return nil
}

func (d *DB) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	var w Workspace
	err := d.Pool.QueryRow(ctx, `
		SELECT id, user_id, host_id, name, state, vcpus, memory_mb, disk_gb,
		       host_port, vm_ip, client_config, created_at, updated_at
		FROM workspaces WHERE id = $1
	`, id).Scan(
		&w.ID, &w.UserID, &w.HostID, &w.Name, &w.State, &w.VCPUs, &w.MemoryMB, &w.DiskGB,
		&w.HostPort, &w.VMIP, &w.ClientConfig, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return &w, nil
}

func (d *DB) ListWorkspacesByUser(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT id, user_id, host_id, name, state, vcpus, memory_mb, disk_gb,
		       host_port, vm_ip, client_config, created_at, updated_at
		FROM workspaces WHERE user_id = $1 AND state != 'destroyed'
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.UserID, &w.HostID, &w.Name, &w.State,
			&w.VCPUs, &w.MemoryMB, &w.DiskGB, &w.HostPort, &w.VMIP,
			&w.ClientConfig, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		workspaces = append(workspaces, w)
	}
	return workspaces, nil
}

func (d *DB) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := d.Pool.Exec(ctx, `
		UPDATE workspaces SET state = 'destroyed', updated_at = now() WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}
```

**Step 5: Verify build and commit**

```bash
go mod tidy
go build ./...
git add -A
git commit -m "feat: add database CRUD for users, sessions, hosts, workspaces"
```

---

### Task 4: GitHub OAuth

Implement GitHub OAuth — device flow (for CLI) and web flow (for dashboard).

**Files:**
- Create: `internal/auth/github.go`
- Create: `internal/auth/github_test.go`

**Step 1: Write GitHub OAuth client**

```go
// internal/auth/github.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GitHubOAuth struct {
	ClientID     string
	ClientSecret string
	httpClient   *http.Client
}

func NewGitHubOAuth(clientID, clientSecret string) *GitHubOAuth {
	return &GitHubOAuth{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// DeviceFlowResponse is returned when initiating the device flow.
type DeviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// StartDeviceFlow initiates the GitHub device flow.
func (g *GitHubOAuth) StartDeviceFlow(ctx context.Context) (*DeviceFlowResponse, error) {
	data := url.Values{
		"client_id": {g.ClientID},
		"scope":     {"read:user user:email"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://github.com/login/device/code",
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device flow request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result DeviceFlowResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// PollDeviceFlow polls GitHub for the access token.
// Returns the access token or an error. Returns empty string if still pending.
func (g *GitHubOAuth) PollDeviceFlow(ctx context.Context, deviceCode string) (string, error) {
	data := url.Values{
		"client_id":   {g.ClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://github.com/login/oauth/access_token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("poll request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	switch result.Error {
	case "":
		return result.AccessToken, nil
	case "authorization_pending", "slow_down":
		return "", nil // Still waiting
	default:
		return "", fmt.Errorf("oauth error: %s", result.Error)
	}
}

// ExchangeCode exchanges an authorization code for an access token (web flow).
func (g *GitHubOAuth) ExchangeCode(ctx context.Context, code string) (string, error) {
	data := url.Values{
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://github.com/login/oauth/access_token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange code: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("oauth error: %s", result.Error)
	}
	return result.AccessToken, nil
}

// GitHubUser represents a GitHub user profile.
type GitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// GetUser fetches the authenticated GitHub user.
func (g *GitHubOAuth) GetUser(ctx context.Context, accessToken string) (*GitHubUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API error %d: %s", resp.StatusCode, string(body))
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &user, nil
}
```

**Step 2: Verify build and commit**

```bash
go mod tidy
go build ./...
git add -A
git commit -m "feat: add GitHub OAuth client (device + web flow)"
```

---

### Task 5: HTTP server and auth middleware

Set up the HTTP server with auth middleware, JSON helpers, and health endpoint.

**Files:**
- Create: `internal/cp/server.go`
- Create: `internal/cp/middleware.go`
- Create: `internal/cp/json.go`
- Modify: `cmd/hopbox-cp/main.go`

**Step 1: Write JSON helpers**

```go
// internal/cp/json.go
package cp

import (
	"encoding/json"
	"log"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20) // 1 MiB
	return json.NewDecoder(r.Body).Decode(v)
}
```

**Step 2: Write auth middleware**

```go
// internal/cp/middleware.go
package cp

import (
	"context"
	"net/http"
	"strings"

	"github.com/hopboxdev/hopbox-cp/internal/db"
)

type contextKey string

const userContextKey contextKey = "user"

// UserFromContext extracts the authenticated user from the request context.
func UserFromContext(ctx context.Context) *db.User {
	u, _ := ctx.Value(userContextKey).(*db.User)
	return u
}

// requireAuth is middleware that validates the session token and injects the user.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization token")
			return
		}

		session, err := s.db.ValidateSession(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}

		user, err := s.db.GetUser(r.Context(), session.UserID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not found")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

// requireAdmin wraps requireAuth and checks the admin allowlist.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if !s.isAdmin(user.GitHubID) {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next(w, r)
	})
}

func (s *Server) isAdmin(githubID int64) bool {
	for _, id := range s.adminGitHubIDs {
		if id == githubID {
			return true
		}
	}
	return false
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
```

**Step 3: Write the server struct and routes**

```go
// internal/cp/server.go
package cp

import (
	"log"
	"net/http"
	"time"

	"github.com/hopboxdev/hopbox-cp/internal/auth"
	"github.com/hopboxdev/hopbox-cp/internal/db"
)

type Server struct {
	db             *db.DB
	github         *auth.GitHubOAuth
	adminGitHubIDs []int64
	mux            *http.ServeMux
}

type ServerConfig struct {
	DB             *db.DB
	GitHub         *auth.GitHubOAuth
	AdminGitHubIDs []int64
}

func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		db:             cfg.DB,
		github:         cfg.GitHub,
		adminGitHubIDs: cfg.AdminGitHubIDs,
		mux:            http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Health
	s.mux.HandleFunc("GET /api/health", s.handleHealth)

	// Auth
	s.mux.HandleFunc("POST /api/auth/github", s.handleGitHubDeviceFlow)
	s.mux.HandleFunc("POST /api/auth/github/callback", s.handleGitHubCallback)
	s.mux.HandleFunc("POST /api/auth/github/web", s.handleGitHubWebFlow)
	s.mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))
	s.mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))

	// Workspaces
	s.mux.HandleFunc("POST /api/workspaces", s.requireAuth(s.handleCreateWorkspace))
	s.mux.HandleFunc("GET /api/workspaces", s.requireAuth(s.handleListWorkspaces))
	s.mux.HandleFunc("GET /api/workspaces/{id}", s.requireAuth(s.handleGetWorkspace))
	s.mux.HandleFunc("DELETE /api/workspaces/{id}", s.requireAuth(s.handleDeleteWorkspace))
	s.mux.HandleFunc("POST /api/workspaces/{id}/suspend", s.requireAuth(s.handleSuspendWorkspace))
	s.mux.HandleFunc("POST /api/workspaces/{id}/resume", s.requireAuth(s.handleResumeWorkspace))

	// Hosts (admin)
	s.mux.HandleFunc("GET /api/hosts", s.requireAdmin(s.handleListHosts))
	s.mux.HandleFunc("POST /api/hosts", s.requireAdmin(s.handleCreateHost))
	s.mux.HandleFunc("DELETE /api/hosts/{id}", s.requireAdmin(s.handleDeleteHost))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Logging middleware.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %dms", r.Method, r.URL.Path, sw.status, time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
```

**Step 4: Wire into main.go**

Update `cmd/hopbox-cp/main.go` to create the server and start listening.

```go
// cmd/hopbox-cp/main.go - updated run function
func run(ctx context.Context, listenAddr, dbURL string) error {
	database, err := db.New(ctx, dbURL)
	if err != nil {
		return err
	}
	defer database.Close()

	migFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	if err := database.RunMigrations(ctx, migFS); err != nil {
		return err
	}

	ghClientID := os.Getenv("GITHUB_CLIENT_ID")
	ghClientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	if ghClientID == "" {
		return fmt.Errorf("GITHUB_CLIENT_ID env var required")
	}

	github := auth.NewGitHubOAuth(ghClientID, ghClientSecret)

	// TODO: parse admin IDs from flag/env
	srv := cp.NewServer(cp.ServerConfig{
		DB:             database,
		GitHub:         github,
		AdminGitHubIDs: []int64{}, // Set from config
	})

	httpServer := &http.Server{
		Addr:    listenAddr,
		Handler: cp.LogRequests(srv),
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down HTTP server...")
		_ = httpServer.Shutdown(context.Background())
	}()

	log.Printf("hopbox-cp listening on %s", listenAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
```

**Step 5: Verify build and commit**

```bash
go mod tidy
go build ./...
git add -A
git commit -m "feat: add HTTP server with auth middleware and routing"
```

---

### Task 6: Auth handlers

Implement the GitHub OAuth endpoints (device flow + web flow + me + logout).

**Files:**
- Create: `internal/cp/auth_handlers.go`

**Step 1: Write auth handlers**

```go
// internal/cp/auth_handlers.go
package cp

import (
	"net/http"
)

func (s *Server) handleGitHubDeviceFlow(w http.ResponseWriter, r *http.Request) {
	result, err := s.github.StartDeviceFlow(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start device flow")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "device_code required")
		return
	}

	accessToken, err := s.github.PollDeviceFlow(r.Context(), req.DeviceCode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "oauth error")
		return
	}
	if accessToken == "" {
		// Still pending — client should retry.
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	}

	session, err := s.completeAuth(r.Context(), accessToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete auth")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": session.ID})
}

func (s *Server) handleGitHubWebFlow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code required")
		return
	}

	accessToken, err := s.github.ExchangeCode(r.Context(), req.Code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to exchange code")
		return
	}

	session, err := s.completeAuth(r.Context(), accessToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete auth")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": session.ID})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if err := s.db.DeleteSession(r.Context(), token); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to logout")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// completeAuth fetches the GitHub user, upserts them, and creates a session.
func (s *Server) completeAuth(ctx context.Context, accessToken string) (*db.Session, error) {
	ghUser, err := s.github.GetUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	user, err := s.db.UpsertUser(ctx, ghUser.ID, ghUser.Login, ghUser.Email, ghUser.AvatarURL)
	if err != nil {
		return nil, err
	}

	return s.db.CreateSession(ctx, user.ID)
}
```

**Step 2: Verify build and commit**

```bash
go build ./...
git add -A
git commit -m "feat: add GitHub OAuth auth handlers"
```

---

### Task 7: gRPC client to hostd with mTLS

Create a gRPC client that connects to hostd over mTLS.

**Files:**
- Create: `internal/cp/hostd_client.go`
- Modify: `internal/cp/server.go` (add hostd client to Server)
- Modify: `cmd/hopbox-cp/main.go` (add TLS flags)

**Step 1: Copy hostd proto into this repo**

The control plane needs the hostd proto to generate a gRPC client. Copy from the hopbox repo.

```bash
mkdir -p proto/hostd/v1
cp ~/Developer/hopbox/proto/hostd/v1/hostd.proto proto/hostd/v1/
cp ~/Developer/hopbox/buf.yaml .
cp ~/Developer/hopbox/buf.gen.yaml .
buf generate
```

**Step 2: Write the hostd gRPC client wrapper**

```go
// internal/cp/hostd_client.go
package cp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	pb "github.com/hopboxdev/hopbox-cp/gen/hostd/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// HostdClient wraps the gRPC connection to a hostd instance.
type HostdClient struct {
	conn   *grpc.ClientConn
	client pb.HostServiceClient
}

// HostdTLSConfig holds paths to mTLS certificates.
type HostdTLSConfig struct {
	CertFile string // Client certificate
	KeyFile  string // Client private key
	CAFile   string // CA certificate for verifying hostd
}

// NewHostdClient creates a gRPC client to hostd with mTLS.
func NewHostdClient(endpoint string, tlsCfg HostdTLSConfig) (*HostdClient, error) {
	creds, err := loadMTLSCredentials(tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("load TLS credentials: %w", err)
	}

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial hostd: %w", err)
	}

	return &HostdClient{
		conn:   conn,
		client: pb.NewHostServiceClient(conn),
	}, nil
}

// NewHostdClientInsecure creates a gRPC client without TLS (for development).
func NewHostdClientInsecure(endpoint string) (*HostdClient, error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("dial hostd: %w", err)
	}
	return &HostdClient{
		conn:   conn,
		client: pb.NewHostServiceClient(conn),
	}, nil
}

func (h *HostdClient) Close() error {
	return h.conn.Close()
}

func (h *HostdClient) Service() pb.HostServiceClient {
	return h.client
}

// CreateWorkspace calls hostd CreateWorkspace RPC.
func (h *HostdClient) CreateWorkspace(ctx context.Context, name, image string, vcpus, memMB, diskGB int) (*pb.CreateWorkspaceResponse, error) {
	return h.client.CreateWorkspace(ctx, &pb.CreateWorkspaceRequest{
		Name:     name,
		Image:    image,
		Vcpus:    int32(vcpus),
		MemoryMb: int32(memMB),
		DiskGb:   int32(diskGB),
	})
}

func (h *HostdClient) DestroyWorkspace(ctx context.Context, name string) error {
	_, err := h.client.DestroyWorkspace(ctx, &pb.DestroyWorkspaceRequest{Name: name})
	return err
}

func (h *HostdClient) SuspendWorkspace(ctx context.Context, name string) error {
	_, err := h.client.SuspendWorkspace(ctx, &pb.SuspendWorkspaceRequest{Name: name})
	return err
}

func (h *HostdClient) ResumeWorkspace(ctx context.Context, name string) (*pb.ResumeWorkspaceResponse, error) {
	return h.client.ResumeWorkspace(ctx, &pb.ResumeWorkspaceRequest{Name: name})
}

func (h *HostdClient) HostStatus(ctx context.Context) (*pb.HostStatusResponse, error) {
	return h.client.HostStatus(ctx, &pb.HostStatusRequest{})
}

func loadMTLSCredentials(cfg HostdTLSConfig) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	caCert, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA cert")
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
	}), nil
}
```

**Step 3: Add hostd client to Server struct**

Update `internal/cp/server.go` to include a map of host ID → hostd client.

```go
// Add to Server struct:
type Server struct {
	db             *db.DB
	github         *auth.GitHubOAuth
	adminGitHubIDs []int64
	hostdClients   map[string]*HostdClient // host ID → client
	hostdTLS       *HostdTLSConfig         // nil = insecure (dev mode)
	mux            *http.ServeMux
}

// Add to ServerConfig:
type ServerConfig struct {
	DB             *db.DB
	GitHub         *auth.GitHubOAuth
	AdminGitHubIDs []int64
	HostdTLS       *HostdTLSConfig // nil = insecure
}
```

Add a method to get or create a hostd client for a host:

```go
func (s *Server) getHostdClient(host *db.Host) (*HostdClient, error) {
	if client, ok := s.hostdClients[host.ID]; ok {
		return client, nil
	}

	var client *HostdClient
	var err error
	if s.hostdTLS != nil {
		client, err = NewHostdClient(host.GRPCEndpoint, *s.hostdTLS)
	} else {
		client, err = NewHostdClientInsecure(host.GRPCEndpoint)
	}
	if err != nil {
		return nil, err
	}

	s.hostdClients[host.ID] = client
	return client, nil
}
```

**Step 4: Add TLS flags to main.go**

```go
// Add to flag section:
hostdCert = flag.String("hostd-cert", "", "client TLS cert for hostd (mTLS)")
hostdKey  = flag.String("hostd-key", "", "client TLS key for hostd (mTLS)")
hostdCA   = flag.String("hostd-ca", "", "CA cert for verifying hostd (mTLS)")

// In run(), build TLS config:
var hostdTLS *cp.HostdTLSConfig
if *hostdCert != "" {
	hostdTLS = &cp.HostdTLSConfig{
		CertFile: *hostdCert,
		KeyFile:  *hostdKey,
		CAFile:   *hostdCA,
	}
}
```

**Step 5: Install dependencies, verify build, commit**

```bash
go get google.golang.org/grpc
go get google.golang.org/protobuf
go mod tidy
go build ./...
git add -A
git commit -m "feat: add gRPC client to hostd with mTLS support"
```

---

### Task 8: Workspace API handlers

Implement the workspace CRUD endpoints that delegate to hostd.

**Files:**
- Create: `internal/cp/workspace_handlers.go`

**Step 1: Write workspace handlers**

```go
// internal/cp/workspace_handlers.go
package cp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/hopboxdev/hopbox-cp/internal/db"
)

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())

	var req struct {
		Name     string `json:"name"`
		Image    string `json:"image"`
		VCPUs    int    `json:"vcpus"`
		MemoryMB int    `json:"memory_mb"`
		DiskGB   int    `json:"disk_gb"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	// Defaults.
	if req.VCPUs == 0 {
		req.VCPUs = 2
	}
	if req.MemoryMB == 0 {
		req.MemoryMB = 2048
	}
	if req.DiskGB == 0 {
		req.DiskGB = 10
	}

	// Pick a host (for now, just use the first online host).
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list hosts")
		return
	}
	var host *db.Host
	for i := range hosts {
		if hosts[i].Status == "online" {
			host = &hosts[i]
			break
		}
	}
	if host == nil {
		writeError(w, http.StatusServiceUnavailable, "no available hosts")
		return
	}

	// Create workspace record.
	ws, err := s.db.CreateWorkspace(r.Context(), db.CreateWorkspaceParams{
		UserID:   user.ID,
		HostID:   host.ID,
		Name:     req.Name,
		VCPUs:    req.VCPUs,
		MemoryMB: req.MemoryMB,
		DiskGB:   req.DiskGB,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace record")
		return
	}

	// Call hostd to create the VM.
	hostdClient, err := s.getHostdClient(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to connect to host")
		return
	}

	result, err := hostdClient.CreateWorkspace(r.Context(), req.Name, req.Image, req.VCPUs, req.MemoryMB, req.DiskGB)
	if err != nil {
		log.Printf("hostd CreateWorkspace failed: %v", err)
		_ = s.db.UpdateWorkspaceState(r.Context(), ws.ID, "failed")
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create VM: %v", err))
		return
	}

	// Store provisioning result.
	clientCfg, _ := json.Marshal(map[string]string{
		"name":            result.ClientConfig.Name,
		"endpoint":        result.ClientConfig.Endpoint,
		"private_key":     result.ClientConfig.PrivateKey,
		"peer_public_key": result.ClientConfig.PeerPublicKey,
		"tunnel_ip":       result.ClientConfig.TunnelIp,
		"agent_ip":        result.ClientConfig.AgentIp,
	})

	err = s.db.UpdateWorkspaceProvisioned(r.Context(), ws.ID, "running",
		int(result.Workspace.HostPort), result.Workspace.VmIp, clientCfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workspace")
		return
	}

	// Return workspace with client config.
	ws, _ = s.db.GetWorkspace(r.Context(), ws.ID)
	writeJSON(w, http.StatusCreated, ws)
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())

	workspaces, err := s.db.ListWorkspacesByUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}
	if workspaces == nil {
		workspaces = []db.Workspace{}
	}
	writeJSON(w, http.StatusOK, workspaces)
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	id := r.PathValue("id")

	ws, err := s.db.GetWorkspace(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if ws.UserID != user.ID && !s.isAdmin(user.GitHubID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	writeJSON(w, http.StatusOK, ws)
}

func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	id := r.PathValue("id")

	ws, err := s.db.GetWorkspace(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if ws.UserID != user.ID && !s.isAdmin(user.GitHubID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	// Get host for this workspace.
	host, err := s.db.GetHost(r.Context(), ws.HostID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get host")
		return
	}

	hostdClient, err := s.getHostdClient(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to connect to host")
		return
	}

	if err := hostdClient.DestroyWorkspace(r.Context(), ws.Name); err != nil {
		log.Printf("hostd DestroyWorkspace failed: %v", err)
	}

	if err := s.db.DeleteWorkspace(r.Context(), ws.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workspace")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "destroyed"})
}

func (s *Server) handleSuspendWorkspace(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	id := r.PathValue("id")

	ws, err := s.db.GetWorkspace(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if ws.UserID != user.ID && !s.isAdmin(user.GitHubID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	host, err := s.db.GetHost(r.Context(), ws.HostID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get host")
		return
	}

	hostdClient, err := s.getHostdClient(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to connect to host")
		return
	}

	if err := hostdClient.SuspendWorkspace(r.Context(), ws.Name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("suspend failed: %v", err))
		return
	}

	_ = s.db.UpdateWorkspaceState(r.Context(), ws.ID, "suspended")

	writeJSON(w, http.StatusOK, map[string]string{"status": "suspended"})
}

func (s *Server) handleResumeWorkspace(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	id := r.PathValue("id")

	ws, err := s.db.GetWorkspace(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	if ws.UserID != user.ID && !s.isAdmin(user.GitHubID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	host, err := s.db.GetHost(r.Context(), ws.HostID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get host")
		return
	}

	hostdClient, err := s.getHostdClient(host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to connect to host")
		return
	}

	if _, err := hostdClient.ResumeWorkspace(r.Context(), ws.Name); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("resume failed: %v", err))
		return
	}

	_ = s.db.UpdateWorkspaceState(r.Context(), ws.ID, "running")

	writeJSON(w, http.StatusOK, map[string]string{"status": "running"})
}
```

**Step 2: Verify build and commit**

```bash
go build ./...
git add -A
git commit -m "feat: add workspace CRUD handlers delegating to hostd"
```

---

### Task 9: Host API handlers

Implement the admin-only host registry endpoints.

**Files:**
- Create: `internal/cp/host_handlers.go`

**Step 1: Write host handlers**

```go
// internal/cp/host_handlers.go
package cp

import (
	"log"
	"net/http"
)

func (s *Server) handleListHosts(w http.ResponseWriter, r *http.Request) {
	hosts, err := s.db.ListHosts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list hosts")
		return
	}
	if hosts == nil {
		hosts = []db.Host{}
	}

	// Refresh capacity from hostd for each online host.
	for i := range hosts {
		if hosts[i].Status != "online" {
			continue
		}
		client, err := s.getHostdClient(&hosts[i])
		if err != nil {
			log.Printf("failed to connect to host %s: %v", hosts[i].Name, err)
			continue
		}
		status, err := client.HostStatus(r.Context())
		if err != nil {
			log.Printf("failed to get status from host %s: %v", hosts[i].Name, err)
			continue
		}
		hosts[i].TotalVCPUs = int(status.TotalVcpus)
		hosts[i].AvailableVCPUs = int(status.AvailableVcpus)
		_ = s.db.UpdateHostCapacity(r.Context(), hosts[i].ID, hosts[i].TotalVCPUs, hosts[i].AvailableVCPUs)
	}

	writeJSON(w, http.StatusOK, hosts)
}

func (s *Server) handleCreateHost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		GRPCEndpoint string `json:"grpc_endpoint"`
		PublicIP     string `json:"public_ip"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.GRPCEndpoint == "" || req.PublicIP == "" {
		writeError(w, http.StatusBadRequest, "name, grpc_endpoint, and public_ip required")
		return
	}

	host, err := s.db.CreateHost(r.Context(), req.Name, req.GRPCEndpoint, req.PublicIP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create host")
		return
	}

	writeJSON(w, http.StatusCreated, host)
}

func (s *Server) handleDeleteHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := s.db.DeleteHost(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete host")
		return
	}

	// Clean up cached client.
	if client, ok := s.hostdClients[id]; ok {
		_ = client.Close()
		delete(s.hostdClients, id)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
```

**Step 2: Fix import — add db package import to host_handlers.go**

The `db.Host` reference requires the import. Ensure `internal/db` is imported.

**Step 3: Verify build and commit**

```bash
go build ./...
git add -A
git commit -m "feat: add admin host registry handlers"
```

---

### Task 10: React frontend scaffolding

Set up the React + Vite + shadcn/ui + TypeScript frontend.

**Files:**
- Create: `web/` directory with Vite React project
- Create: `web/src/lib/api.ts` (API client)
- Create: `web/src/lib/auth.tsx` (auth context)

**Step 1: Scaffold Vite React project**

```bash
cd ~/Developer/hopbox-cp
npm create vite@latest web -- --template react-ts
cd web
npm install
npx shadcn@latest init -d
npx shadcn@latest add button card input label table badge dialog dropdown-menu
```

**Step 2: Write API client**

```typescript
// web/src/lib/api.ts
const API_BASE = "/api";

class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const token = localStorage.getItem("token");
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const resp = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  if (!resp.ok) {
    const data = await resp.json().catch(() => ({ error: "request failed" }));
    throw new ApiError(resp.status, data.error || "request failed");
  }

  return resp.json();
}

export const api = {
  // Auth
  startDeviceFlow: () => request<DeviceFlowResponse>("POST", "/auth/github"),
  pollDeviceFlow: (deviceCode: string) =>
    request<{ token?: string; status?: string }>("POST", "/auth/github/callback", { device_code: deviceCode }),
  webAuth: (code: string) =>
    request<{ token: string }>("POST", "/auth/github/web", { code }),
  me: () => request<User>("GET", "/auth/me"),
  logout: () => request<void>("POST", "/auth/logout"),

  // Workspaces
  listWorkspaces: () => request<Workspace[]>("GET", "/workspaces"),
  getWorkspace: (id: string) => request<Workspace>("GET", `/workspaces/${id}`),
  createWorkspace: (params: CreateWorkspaceParams) =>
    request<Workspace>("POST", "/workspaces", params),
  deleteWorkspace: (id: string) => request<void>("DELETE", `/workspaces/${id}`),
  suspendWorkspace: (id: string) => request<void>("POST", `/workspaces/${id}/suspend`),
  resumeWorkspace: (id: string) => request<void>("POST", `/workspaces/${id}/resume`),

  // Hosts (admin)
  listHosts: () => request<Host[]>("GET", "/hosts"),
  createHost: (params: CreateHostParams) => request<Host>("POST", "/hosts", params),
  deleteHost: (id: string) => request<void>("DELETE", `/hosts/${id}`),
};

// Types
export interface User {
  id: string;
  github_id: number;
  github_login: string;
  email?: string;
  avatar_url?: string;
}

export interface Workspace {
  id: string;
  user_id: string;
  host_id: string;
  name: string;
  state: string;
  vcpus: number;
  memory_mb: number;
  disk_gb: number;
  host_port?: number;
  vm_ip?: string;
  client_config?: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface Host {
  id: string;
  name: string;
  grpc_endpoint: string;
  public_ip: string;
  status: string;
  total_vcpus: number;
  available_vcpus: number;
}

export interface DeviceFlowResponse {
  device_code: string;
  user_code: string;
  verification_uri: string;
  expires_in: number;
  interval: number;
}

export interface CreateWorkspaceParams {
  name: string;
  image?: string;
  vcpus?: number;
  memory_mb?: number;
  disk_gb?: number;
}

export interface CreateHostParams {
  name: string;
  grpc_endpoint: string;
  public_ip: string;
}
```

**Step 3: Write auth context**

```typescript
// web/src/lib/auth.tsx
import { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { api, User } from "./api";

interface AuthContextType {
  user: User | null;
  loading: boolean;
  login: (token: string) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const token = localStorage.getItem("token");
    if (!token) {
      setLoading(false);
      return;
    }
    api.me()
      .then(setUser)
      .catch(() => localStorage.removeItem("token"))
      .finally(() => setLoading(false));
  }, []);

  const login = (token: string) => {
    localStorage.setItem("token", token);
    api.me().then(setUser);
  };

  const logout = () => {
    api.logout().catch(() => {});
    localStorage.removeItem("token");
    setUser(null);
  };

  return (
    <AuthContext.Provider value={{ user, loading, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
```

**Step 4: Configure Vite proxy for development**

```typescript
// web/vite.config.ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: scaffold React frontend with shadcn, API client, and auth"
```

---

### Task 11: Login page

Build the GitHub OAuth login page for the dashboard.

**Files:**
- Create: `web/src/pages/LoginPage.tsx`
- Modify: `web/src/App.tsx`

**Step 1: Write login page**

The dashboard uses the GitHub web flow (redirect-based). The login page redirects to GitHub, and on callback, exchanges the code for a session token.

```typescript
// web/src/pages/LoginPage.tsx
import { useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

const GITHUB_CLIENT_ID = import.meta.env.VITE_GITHUB_CLIENT_ID;

export function LoginPage() {
  const { login } = useAuth();

  useEffect(() => {
    // Handle OAuth callback.
    const params = new URLSearchParams(window.location.search);
    const code = params.get("code");
    if (code) {
      api.webAuth(code).then(({ token }) => {
        login(token);
        window.history.replaceState({}, "", "/");
      });
    }
  }, [login]);

  const handleLogin = () => {
    window.location.href =
      `https://github.com/login/oauth/authorize?client_id=${GITHUB_CLIENT_ID}&scope=read:user%20user:email`;
  };

  return (
    <div className="min-h-screen flex items-center justify-center">
      <Card className="w-[400px]">
        <CardHeader>
          <CardTitle>Hopbox</CardTitle>
        </CardHeader>
        <CardContent>
          <Button onClick={handleLogin} className="w-full">
            Sign in with GitHub
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
```

**Step 2: Write App.tsx with routing**

```typescript
// web/src/App.tsx
import { AuthProvider, useAuth } from "@/lib/auth";
import { LoginPage } from "@/pages/LoginPage";
import { WorkspacesPage } from "@/pages/WorkspacesPage";
import { HostsPage } from "@/pages/HostsPage";
import { useState } from "react";

function AppContent() {
  const { user, loading, logout } = useAuth();
  const [page, setPage] = useState<"workspaces" | "hosts">("workspaces");

  if (loading) return <div className="min-h-screen flex items-center justify-center">Loading...</div>;
  if (!user) return <LoginPage />;

  return (
    <div className="min-h-screen">
      <nav className="border-b px-6 py-3 flex items-center justify-between">
        <div className="flex items-center gap-4">
          <span className="font-bold">Hopbox</span>
          <button
            onClick={() => setPage("workspaces")}
            className={`text-sm ${page === "workspaces" ? "font-semibold" : "text-muted-foreground"}`}
          >
            Workspaces
          </button>
          <button
            onClick={() => setPage("hosts")}
            className={`text-sm ${page === "hosts" ? "font-semibold" : "text-muted-foreground"}`}
          >
            Hosts
          </button>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">{user.github_login}</span>
          <button onClick={logout} className="text-sm text-muted-foreground hover:text-foreground">
            Logout
          </button>
        </div>
      </nav>
      <main className="p-6">
        {page === "workspaces" ? <WorkspacesPage /> : <HostsPage />}
      </main>
    </div>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  );
}
```

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: add login page and app shell with nav"
```

---

### Task 12: Workspaces page

Build the workspaces table with create, suspend, resume, and destroy actions.

**Files:**
- Create: `web/src/pages/WorkspacesPage.tsx`

**Step 1: Write workspaces page**

```typescript
// web/src/pages/WorkspacesPage.tsx
import { useEffect, useState } from "react";
import { api, Workspace } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger,
} from "@/components/ui/dialog";

const stateBadge: Record<string, string> = {
  running: "bg-green-100 text-green-800",
  suspended: "bg-yellow-100 text-yellow-800",
  creating: "bg-blue-100 text-blue-800",
  destroyed: "bg-gray-100 text-gray-800",
  failed: "bg-red-100 text-red-800",
};

export function WorkspacesPage() {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);

  const refresh = () => {
    api.listWorkspaces().then(setWorkspaces).finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  const handleCreate = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    await api.createWorkspace({
      name: form.get("name") as string,
      image: (form.get("image") as string) || undefined,
      vcpus: Number(form.get("vcpus")) || undefined,
      memory_mb: Number(form.get("memory_mb")) || undefined,
      disk_gb: Number(form.get("disk_gb")) || undefined,
    });
    setCreateOpen(false);
    refresh();
  };

  const handleAction = async (id: string, action: "suspend" | "resume" | "destroy") => {
    if (action === "destroy" && !confirm("Destroy this workspace?")) return;
    if (action === "suspend") await api.suspendWorkspace(id);
    else if (action === "resume") await api.resumeWorkspace(id);
    else await api.deleteWorkspace(id);
    refresh();
  };

  if (loading) return <div>Loading...</div>;

  return (
    <div>
      <div className="flex justify-between items-center mb-4">
        <h1 className="text-xl font-semibold">Workspaces</h1>
        <Dialog open={createOpen} onOpenChange={setCreateOpen}>
          <DialogTrigger asChild>
            <Button>Create Workspace</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Workspace</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleCreate} className="space-y-4">
              <div><Label htmlFor="name">Name</Label><Input name="name" required /></div>
              <div><Label htmlFor="image">Image</Label><Input name="image" placeholder="ubuntu-dev" /></div>
              <div className="grid grid-cols-3 gap-2">
                <div><Label htmlFor="vcpus">vCPUs</Label><Input name="vcpus" type="number" placeholder="2" /></div>
                <div><Label htmlFor="memory_mb">Memory (MB)</Label><Input name="memory_mb" type="number" placeholder="2048" /></div>
                <div><Label htmlFor="disk_gb">Disk (GB)</Label><Input name="disk_gb" type="number" placeholder="10" /></div>
              </div>
              <Button type="submit" className="w-full">Create</Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>State</TableHead>
            <TableHead>vCPUs</TableHead>
            <TableHead>Memory</TableHead>
            <TableHead>Port</TableHead>
            <TableHead>Created</TableHead>
            <TableHead>Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {workspaces.map((ws) => (
            <>
              <TableRow key={ws.id} className="cursor-pointer" onClick={() => setExpanded(expanded === ws.id ? null : ws.id)}>
                <TableCell className="font-medium">{ws.name}</TableCell>
                <TableCell>
                  <Badge className={stateBadge[ws.state] || ""}>{ws.state}</Badge>
                </TableCell>
                <TableCell>{ws.vcpus}</TableCell>
                <TableCell>{ws.memory_mb} MB</TableCell>
                <TableCell>{ws.host_port || "—"}</TableCell>
                <TableCell>{new Date(ws.created_at).toLocaleDateString()}</TableCell>
                <TableCell className="space-x-1" onClick={(e) => e.stopPropagation()}>
                  {ws.state === "running" && (
                    <Button size="sm" variant="outline" onClick={() => handleAction(ws.id, "suspend")}>Suspend</Button>
                  )}
                  {ws.state === "suspended" && (
                    <Button size="sm" variant="outline" onClick={() => handleAction(ws.id, "resume")}>Resume</Button>
                  )}
                  {ws.state !== "destroyed" && (
                    <Button size="sm" variant="destructive" onClick={() => handleAction(ws.id, "destroy")}>Destroy</Button>
                  )}
                </TableCell>
              </TableRow>
              {expanded === ws.id && ws.client_config && (
                <TableRow key={`${ws.id}-config`}>
                  <TableCell colSpan={7}>
                    <pre className="bg-muted p-3 rounded text-xs">
{`# Connect to this workspace:
hop create ${ws.name} --managed --endpoint ${ws.client_config.endpoint}
hop up ${ws.name}`}
                    </pre>
                  </TableCell>
                </TableRow>
              )}
            </>
          ))}
          {workspaces.length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className="text-center text-muted-foreground">
                No workspaces yet. Create one to get started.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
```

**Step 2: Commit**

```bash
git add -A
git commit -m "feat: add workspaces page with create, suspend, resume, destroy"
```

---

### Task 13: Hosts page (admin)

Build the admin hosts table with add/remove actions.

**Files:**
- Create: `web/src/pages/HostsPage.tsx`

**Step 1: Write hosts page**

```typescript
// web/src/pages/HostsPage.tsx
import { useEffect, useState } from "react";
import { api, Host } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from "@/components/ui/table";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger,
} from "@/components/ui/dialog";

export function HostsPage() {
  const [hosts, setHosts] = useState<Host[]>([]);
  const [loading, setLoading] = useState(true);
  const [addOpen, setAddOpen] = useState(false);

  const refresh = () => {
    api.listHosts().then(setHosts).finally(() => setLoading(false));
  };

  useEffect(refresh, []);

  const handleAdd = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    await api.createHost({
      name: form.get("name") as string,
      grpc_endpoint: form.get("grpc_endpoint") as string,
      public_ip: form.get("public_ip") as string,
    });
    setAddOpen(false);
    refresh();
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Remove this host?")) return;
    await api.deleteHost(id);
    refresh();
  };

  if (loading) return <div>Loading...</div>;

  return (
    <div>
      <div className="flex justify-between items-center mb-4">
        <h1 className="text-xl font-semibold">Hosts</h1>
        <Dialog open={addOpen} onOpenChange={setAddOpen}>
          <DialogTrigger asChild>
            <Button>Add Host</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add Host</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleAdd} className="space-y-4">
              <div><Label htmlFor="name">Name</Label><Input name="name" required placeholder="bm-1" /></div>
              <div><Label htmlFor="grpc_endpoint">gRPC Endpoint</Label><Input name="grpc_endpoint" required placeholder="10.0.0.5:9090" /></div>
              <div><Label htmlFor="public_ip">Public IP</Label><Input name="public_ip" required placeholder="203.0.113.5" /></div>
              <Button type="submit" className="w-full">Add Host</Button>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Endpoint</TableHead>
            <TableHead>Public IP</TableHead>
            <TableHead>Capacity</TableHead>
            <TableHead>Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {hosts.map((host) => (
            <TableRow key={host.id}>
              <TableCell className="font-medium">{host.name}</TableCell>
              <TableCell>
                <Badge className={host.status === "online" ? "bg-green-100 text-green-800" : "bg-gray-100 text-gray-800"}>
                  {host.status}
                </Badge>
              </TableCell>
              <TableCell className="font-mono text-sm">{host.grpc_endpoint}</TableCell>
              <TableCell className="font-mono text-sm">{host.public_ip}</TableCell>
              <TableCell>{host.available_vcpus}/{host.total_vcpus} vCPUs</TableCell>
              <TableCell>
                <Button size="sm" variant="destructive" onClick={() => handleDelete(host.id)}>Remove</Button>
              </TableCell>
            </TableRow>
          ))}
          {hosts.length === 0 && (
            <TableRow>
              <TableCell colSpan={6} className="text-center text-muted-foreground">
                No hosts registered. Add one to get started.
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
```

**Step 2: Commit**

```bash
git add -A
git commit -m "feat: add admin hosts page"
```

---

### Task 14: Embed frontend in Go binary and serve SPA

Configure the Go binary to embed the built React app and serve it as a SPA.

**Files:**
- Create: `internal/cp/spa.go`
- Modify: `internal/cp/server.go` (add SPA handler)
- Modify: `Makefile` (add frontend build step)

**Step 1: Write SPA file server**

```go
// internal/cp/spa.go
package cp

import (
	"io/fs"
	"net/http"
	"strings"
)

// SPAHandler serves the embedded React SPA. It serves static files from the
// embedded filesystem and falls back to index.html for client-side routing.
func SPAHandler(webFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(webFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve SPA for API routes.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve static file.
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists.
		f, err := webFS.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fall back to index.html for SPA routing.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
```

**Step 2: Add SPA to server routes**

In `internal/cp/server.go`, add a `WebFS` field to `ServerConfig` and register the SPA handler as the fallback:

```go
// Add to ServerConfig:
WebFS fs.FS // Embedded React build output (nil in dev mode)

// Add to routes():
if s.webFS != nil {
	s.mux.Handle("/", SPAHandler(s.webFS))
}
```

**Step 3: Embed in main.go**

```go
// cmd/hopbox-cp/main.go

//go:embed web/dist/*
var webFS embed.FS

// In run():
webSubFS, err := fs.Sub(webFS, "web/dist")
if err != nil {
	return fmt.Errorf("web fs: %w", err)
}

srv := cp.NewServer(cp.ServerConfig{
	// ... existing fields
	WebFS: webSubFS,
})
```

Note: The `web/dist/` directory is created by `npm run build` in the `web/` directory. The embed directive requires the directory to exist at compile time. The Makefile handles building frontend before Go.

**Step 4: Update Makefile**

```makefile
.PHONY: build clean frontend

frontend:
	cd web && npm ci && npm run build
	mkdir -p cmd/hopbox-cp/web
	cp -r web/dist cmd/hopbox-cp/web/

build: frontend $(DIST)/hopbox-cp

$(DIST)/hopbox-cp: $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/hopbox-cp/

clean:
	rm -rf $(DIST) cmd/hopbox-cp/web
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: embed React SPA in Go binary with SPA fallback routing"
```

---

### Task 15: mTLS certificate generation script

Write a script to generate the CA, host cert, and control plane client cert.

**Files:**
- Create: `scripts/gen-certs.sh`

**Step 1: Write cert generation script**

```bash
#!/usr/bin/env bash
# scripts/gen-certs.sh — generate mTLS certificates for hopbox-cp ↔ hostd
set -euo pipefail

OUT="${1:-certs}"
mkdir -p "$OUT"

echo "=== Generating CA ==="
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout "$OUT/ca.key" -out "$OUT/ca.crt" \
  -days 3650 -nodes -subj "/CN=hopbox-ca"

echo "=== Generating hostd server cert ==="
openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout "$OUT/hostd.key" -out "$OUT/hostd.csr" \
  -nodes -subj "/CN=hopbox-hostd"
openssl x509 -req -in "$OUT/hostd.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" \
  -CAcreateserial -out "$OUT/hostd.crt" -days 3650
rm "$OUT/hostd.csr"

echo "=== Generating control plane client cert ==="
openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -keyout "$OUT/cp-client.key" -out "$OUT/cp-client.csr" \
  -nodes -subj "/CN=hopbox-cp"
openssl x509 -req -in "$OUT/cp-client.csr" -CA "$OUT/ca.crt" -CAkey "$OUT/ca.key" \
  -CAcreateserial -out "$OUT/cp-client.crt" -days 3650
rm "$OUT/cp-client.csr"

echo ""
echo "=== Certificates generated in $OUT/ ==="
echo ""
echo "On bare metal (hostd):"
echo "  --tls-cert $OUT/hostd.crt --tls-key $OUT/hostd.key --tls-ca $OUT/ca.crt"
echo ""
echo "On control plane VPS:"
echo "  --hostd-cert $OUT/cp-client.crt --hostd-key $OUT/cp-client.key --hostd-ca $OUT/ca.crt"
```

**Step 2: Make executable and commit**

```bash
chmod +x scripts/gen-certs.sh
git add -A
git commit -m "feat: add mTLS certificate generation script"
```

---

### Task 16: CLI changes — hop login, hop create, hop destroy (in hopbox repo)

Add managed product commands to the `hop` CLI in the hopbox repo.

**Files (in ~/Developer/hopbox):**
- Create: `internal/managed/client.go` (API client for control plane)
- Create: `internal/managed/auth.go` (auth token storage)
- Create: `cmd/hop/login.go`
- Create: `cmd/hop/create.go`
- Create: `cmd/hop/destroy.go`
- Modify: `cmd/hop/main.go` (register new commands)
- Modify: `internal/hostconfig/config.go` (add `Managed` field)

**Step 1: Write managed API client**

```go
// internal/managed/client.go
package managed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL    string
	Token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL:    baseURL,
		Token:      token,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) do(method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.BaseURL+"/api"+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var errResp struct{ Error string `json:"error"` }
		if json.Unmarshal(data, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}
	return data, nil
}

type DeviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
}

func (c *Client) StartDeviceFlow() (*DeviceFlowResponse, error) {
	data, err := c.do("POST", "/auth/github", nil)
	if err != nil {
		return nil, err
	}
	var r DeviceFlowResponse
	return &r, json.Unmarshal(data, &r)
}

func (c *Client) PollDeviceFlow(deviceCode string) (string, error) {
	data, err := c.do("POST", "/auth/github/callback", map[string]string{"device_code": deviceCode})
	if err != nil {
		return "", err
	}
	var r struct {
		Token  string `json:"token"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", err
	}
	if r.Status == "pending" {
		return "", nil
	}
	return r.Token, nil
}

type WorkspaceResponse struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	State        string            `json:"state"`
	ClientConfig map[string]string `json:"client_config"`
}

func (c *Client) CreateWorkspace(name string, vcpus, memMB, diskGB int) (*WorkspaceResponse, error) {
	data, err := c.do("POST", "/workspaces", map[string]any{
		"name":      name,
		"vcpus":     vcpus,
		"memory_mb": memMB,
		"disk_gb":   diskGB,
	})
	if err != nil {
		return nil, err
	}
	var r WorkspaceResponse
	return &r, json.Unmarshal(data, &r)
}

func (c *Client) DestroyWorkspace(id string) error {
	_, err := c.do("DELETE", "/workspaces/"+id, nil)
	return err
}
```

**Step 2: Write auth token storage**

```go
// internal/managed/auth.go
package managed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type AuthConfig struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

func AuthConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hopbox", "auth.json"), nil
}

func LoadAuth() (*AuthConfig, error) {
	path, err := AuthConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("not logged in (run 'hop login' first)")
	}
	var cfg AuthConfig
	return &cfg, json.Unmarshal(data, &cfg)
}

func SaveAuth(cfg *AuthConfig) error {
	path, err := AuthConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
```

**Step 3: Write hop login command**

```go
// cmd/hop/login.go
type LoginCmd struct {
	Endpoint string `required:"" help:"Control plane URL (e.g. https://cp.hopbox.dev)"`
}

func (c *LoginCmd) Run() error {
	client := managed.NewClient(c.Endpoint, "")

	flow, err := client.StartDeviceFlow()
	if err != nil {
		return fmt.Errorf("start login: %w", err)
	}

	fmt.Printf("Open %s and enter code: %s\n", flow.VerificationURI, flow.UserCode)

	interval := flow.Interval
	if interval == 0 {
		interval = 5
	}

	for {
		time.Sleep(time.Duration(interval) * time.Second)
		token, err := client.PollDeviceFlow(flow.DeviceCode)
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
		if token != "" {
			if err := managed.SaveAuth(&managed.AuthConfig{
				Endpoint: c.Endpoint,
				Token:    token,
			}); err != nil {
				return err
			}
			fmt.Println("Logged in successfully.")
			return nil
		}
	}
}
```

**Step 4: Write hop create and hop destroy commands**

```go
// cmd/hop/create.go
type CreateCmd struct {
	Name     string `arg:"" help:"Workspace name"`
	VCPUs    int    `default:"2" help:"Number of vCPUs"`
	MemoryMB int    `name:"memory" default:"2048" help:"Memory in MB"`
	DiskGB   int    `name:"disk" default:"10" help:"Disk in GB"`
}

func (c *CreateCmd) Run() error {
	auth, err := managed.LoadAuth()
	if err != nil {
		return err
	}

	client := managed.NewClient(auth.Endpoint, auth.Token)
	ws, err := client.CreateWorkspace(c.Name, c.VCPUs, c.MemoryMB, c.DiskGB)
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	// Save as host config with managed flag.
	cfg := &hostconfig.HostConfig{
		Name:          ws.Name,
		Endpoint:      ws.ClientConfig["endpoint"],
		PrivateKey:    ws.ClientConfig["private_key"],
		PeerPublicKey: ws.ClientConfig["peer_public_key"],
		TunnelIP:      ws.ClientConfig["tunnel_ip"],
		AgentIP:       ws.ClientConfig["agent_ip"],
		Managed:       true,
		ManagedID:     ws.ID,
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save host config: %w", err)
	}

	fmt.Printf("Workspace %q created. Run: hop up %s\n", ws.Name, ws.Name)
	return nil
}
```

```go
// cmd/hop/destroy.go — similar pattern, calls client.DestroyWorkspace
```

**Step 5: Add Managed field to HostConfig**

```go
// internal/hostconfig/config.go — add fields:
Managed   bool   `yaml:"managed,omitempty"`
ManagedID string `yaml:"managed_id,omitempty"`
```

**Step 6: Register commands in main.go CLI struct**

```go
// cmd/hop/main.go — add to CLI struct:
Login   LoginCmd   `cmd:"" help:"Log in to managed Hopbox."`
Create  CreateCmd  `cmd:"" help:"Create a managed workspace."`
Destroy DestroyCmd `cmd:"" help:"Destroy a managed workspace."`
```

**Step 7: Verify build and commit**

```bash
go mod tidy
go build ./cmd/hop/
git add -A
git commit -m "feat: add hop login, create, destroy for managed workspaces"
```

---

### Task 17: Update hop up for managed mode

Modify `hop up` to skip SSH bootstrap when connecting to a managed workspace.

**Files (in ~/Developer/hopbox):**
- Modify: `cmd/hop/up.go`

**Step 1: Add managed mode detection**

In the `hop up` command, after loading the host config, check `cfg.Managed`. If true, skip the SSH setup phase and go straight to WireGuard tunnel establishment.

```go
// In UpCmd.Run(), after loading host config:
if cfg.Managed {
	// Skip SSH bootstrap — keys are already provisioned by the control plane.
	// Go directly to tunnel setup.
	log.Printf("Connecting to managed workspace %q...", cfg.Name)
}
```

The exact modification depends on how `up.go` currently structures the SSH vs tunnel phases. The key change: when `cfg.Managed == true`, skip any SSH connection attempts and use the pre-provisioned WireGuard config directly.

**Step 2: Verify build and commit**

```bash
go build ./cmd/hop/
git add -A
git commit -m "feat: skip SSH bootstrap for managed workspaces in hop up"
```

---

### Task 18: Add mTLS support to hostd

Update hostd to accept TLS flags and require mTLS for gRPC connections.

**Files (in ~/Developer/hopbox):**
- Modify: `cmd/hopbox-hostd/main.go`

**Step 1: Add TLS flags and server configuration**

```go
// Add flags:
tlsCert = flag.String("tls-cert", "", "TLS certificate for gRPC server")
tlsKey  = flag.String("tls-key", "", "TLS private key for gRPC server")
tlsCA   = flag.String("tls-ca", "", "CA certificate for client verification (mTLS)")

// In run(), configure gRPC server with TLS:
var grpcServer *grpc.Server
if *tlsCert != "" {
	creds, err := loadServerTLS(*tlsCert, *tlsKey, *tlsCA)
	if err != nil {
		return fmt.Errorf("load TLS: %w", err)
	}
	grpcServer = grpc.NewServer(grpc.Creds(creds))
} else {
	grpcServer = grpc.NewServer()
}
```

```go
func loadServerTLS(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert: %w", err)
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to add CA cert")
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}), nil
}
```

**Step 2: Verify build and commit**

```bash
GOOS=linux go build ./cmd/hopbox-hostd/
git add -A
git commit -m "feat: add mTLS support to hostd gRPC server"
```
