# Phase 5A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `hopbox expose` command and admin web UI with user/box management.

**Architecture:** The `hopbox expose` command is a thin CLI addition that reads hostname/port from the control socket's status response and prints SSH tunnel instructions. The admin web UI is a standalone HTTP server using Go's `net/http` with `html/template`, embedded templates via `//go:embed`, htmx for interactivity, and Tailwind CSS via CDN. It runs alongside the SSH server in `hopboxd`, protected by HTTP Basic Auth.

**Tech Stack:** Go, kong, net/http, html/template, embed, htmx, Tailwind CSS CDN

**Spec:** `docs/superpowers/specs/2026-04-09-hopbox-phase5a-design.md`

---

## Task 1: Config changes

**Files:** `internal/config/config.go`

Add `Hostname` field and `AdminConfig` struct to the config. Defaults: hostname empty, admin disabled, port 8080, username "admin", password empty.

### Changes to `internal/config/config.go`

Add the `AdminConfig` struct and new fields:

```go
type AdminConfig struct {
	Enabled  bool   `toml:"enabled"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

type Config struct {
	Port             int             `toml:"port"`
	Hostname         string          `toml:"hostname"`
	DataDir          string          `toml:"data_dir"`
	HostKeyPath      string          `toml:"host_key_path"`
	OpenRegistration bool            `toml:"open_registration"`
	IdleTimeoutHours int             `toml:"idle_timeout_hours"`
	Resources        ResourcesConfig `toml:"resources"`
	Admin            AdminConfig     `toml:"admin"`
}
```

Update `defaults()` to include the new fields:

```go
func defaults() Config {
	return Config{
		Port:             2222,
		Hostname:         "",
		DataDir:          "./data",
		HostKeyPath:      "",
		OpenRegistration: true,
		IdleTimeoutHours: 24,
		Resources: ResourcesConfig{
			CPUCores:  2,
			MemoryGB:  4,
			PidsLimit: 512,
		},
		Admin: AdminConfig{
			Enabled:  false,
			Port:     8080,
			Username: "admin",
			Password: "",
		},
	}
}
```

---

## Task 2: `hopbox expose` command

**Files:** `cmd/hopbox/main.go`

Add `ExposeCmd` to the kong CLI. It calls status to get hostname and SSH port, then prints the tunnel command.

### Changes to `cmd/hopbox/main.go`

Add `ExposeCmd` to the CLI struct:

```go
type CLI struct {
	Status  StatusCmd  `cmd:"" help:"Show box info."`
	Expose  ExposeCmd  `cmd:"" help:"Print SSH tunnel instructions for a port."`
	Destroy DestroyCmd `cmd:"" help:"Destroy this box."`
}

type ExposeCmd struct {
	Port int `arg:"" help:"Port to expose."`
}
```

Add the "expose" case to the switch in `main()`:

```go
func main() {
	var cli CLI
	ctx := kong.Parse(&cli, kong.Name("hopbox"), kong.Description("Hopbox dev environment CLI"))
	switch ctx.Command() {
	case "status":
		doStatus(cli.Status.JSON)
	case "expose <port>":
		doExpose(cli.Expose.Port)
	case "destroy":
		doDestroy()
	}
}
```

Add the `doExpose` function:

```go
func doExpose(port int) {
	resp, err := sendRequest(control.Request{Command: "status"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	hostname := resp.Data["hostname"]
	if hostname == "" {
		hostname = "<server>"
	}
	sshPort := resp.Data["ssh_port"]
	if sshPort == "" {
		sshPort = "2222"
	}
	user := resp.Data["user"]

	fmt.Printf("To access port %d from your machine, run:\n\n", port)
	fmt.Printf("  ssh -p %s -L %d:localhost:%d -N %s@%s\n\n", sshPort, port, port, user, hostname)
	fmt.Printf("Then open http://localhost:%d\n", port)
}
```

---

## Task 3: Pass hostname and SSH port to control socket

**Files:** `internal/control/handler.go`, `internal/gateway/server.go`, `internal/gateway/tunnel.go`

### Changes to `internal/control/handler.go`

Add `Hostname` and `SSHPort` fields to `BoxInfo`:

```go
// BoxInfo holds metadata about a box for the status command.
type BoxInfo struct {
	BoxName     string
	Username    string
	Shell       string
	Multiplexer string
	ContainerID string
	StartedAt   time.Time
	Hostname    string
	SSHPort     int
}
```

Include them in the status response Data map in `handleStatus`:

```go
func handleStatus(info BoxInfo) Response {
	uptime := time.Since(info.StartedAt).Truncate(time.Second).String()

	sshPort := "2222"
	if info.SSHPort > 0 {
		sshPort = fmt.Sprintf("%d", info.SSHPort)
	}

	return Response{
		OK: true,
		Data: map[string]string{
			"box":         info.BoxName,
			"user":        info.Username,
			"os":          fmt.Sprintf("Ubuntu 24.04 (%s)", runtime.GOARCH),
			"shell":       info.Shell,
			"multiplexer": info.Multiplexer,
			"uptime":      uptime,
			"hostname":    info.Hostname,
			"ssh_port":    sshPort,
		},
	}
}
```

### Changes to `internal/gateway/server.go`

Pass hostname and SSH port when constructing `BoxInfo` (around line 259):

```go
	boxInfo := control.BoxInfo{
		BoxName:     boxname,
		Username:    user.Username,
		Shell:       profile.Shell.Tool,
		Multiplexer: profile.Multiplexer.Tool,
		Hostname:    s.cfg.Hostname,
		SSHPort:     s.cfg.Port,
	}
```

### Changes to `internal/gateway/tunnel.go`

The `resolveContainerID` function does not have direct access to the config. We need to pass hostname and port through. Update the function signature and the `BoxInfo` construction.

First, update `resolveContainerID` to accept hostname and sshPort parameters:

```go
func resolveContainerID(sshCtx ssh.Context, mgr *containers.Manager, store *users.Store, dockerCli *client.Client, baseTag string, hostname string, sshPort int) (string, error) {
```

Then update the BoxInfo construction in `resolveContainerID` (around line 79):

```go
	boxInfo := control.BoxInfo{
		BoxName:  boxname,
		Username: user.Username,
		Hostname: hostname,
		SSHPort:  sshPort,
	}
	if profile != nil {
		boxInfo.Shell = profile.Shell.Tool
		boxInfo.Multiplexer = profile.Multiplexer.Tool
	}
```

Update all callers of `resolveContainerID` to pass `s.cfg.Hostname` and `s.cfg.Port`. The caller is in the direct-tcpip handler in `server.go`. Find the call to `resolveContainerID` and add the two new arguments:

```go
	containerID, err := resolveContainerID(ctx, s.manager, s.store, s.dockerCli, s.baseTag, s.cfg.Hostname, s.cfg.Port)
```

---

## Task 4: Admin server + basic auth

**Files:** `internal/admin/server.go` (new file)

Create the admin HTTP server with basic auth middleware, routes, and embedded templates.

### Create `internal/admin/server.go`

```go
package admin

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/users"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates *template.Template

func init() {
	templates = template.Must(template.New("").Funcs(template.FuncMap{
		"sub": func(a, b int) int { return a - b },
	}).ParseFS(templateFS, "templates/*.html"))
}

// AdminServer serves the admin web UI.
type AdminServer struct {
	cfg       *config.Config
	store     *users.Store
	manager   *containers.Manager
	dockerCli *client.Client
	httpSrv   *http.Server
}

// NewAdminServer creates a new admin HTTP server.
func NewAdminServer(cfg *config.Config, store *users.Store, mgr *containers.Manager, dockerCli *client.Client) *AdminServer {
	s := &AdminServer{
		cfg:       cfg,
		store:     store,
		manager:   mgr,
		dockerCli: dockerCli,
	}

	mux := http.NewServeMux()

	// Page routes
	mux.HandleFunc("GET /", s.handleDashboard)
	mux.HandleFunc("GET /users", s.handleUsers)
	mux.HandleFunc("GET /users/{username}/boxes", s.handleBoxes)
	mux.HandleFunc("GET /settings", s.handleSettings)

	// API routes (htmx actions)
	mux.HandleFunc("DELETE /api/users/{username}", s.handleDeleteUser)
	mux.HandleFunc("DELETE /api/users/{username}/boxes/{boxname}", s.handleDeleteBox)
	mux.HandleFunc("POST /api/users/{username}/boxes/{boxname}/stop", s.handleStopBox)
	mux.HandleFunc("PUT /api/settings/registration", s.handleToggleRegistration)

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Admin.Port),
		Handler: s.basicAuth(mux),
	}

	return s
}

// ListenAndServe starts the admin HTTP server.
func (s *AdminServer) ListenAndServe() error {
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully shuts down the admin HTTP server.
func (s *AdminServer) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

// basicAuth wraps a handler with HTTP Basic Auth.
func (s *AdminServer) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != s.cfg.Admin.Username || password != s.cfg.Admin.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="Hopbox Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// renderPage renders a full page template with the layout.
func (s *AdminServer) renderPage(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("[admin] template error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// userInfo is used for template rendering.
type userInfo struct {
	Username     string
	Fingerprint  string
	KeyType      string
	RegisteredAt time.Time
	BoxCount     int
}

// boxInfo is used for template rendering.
type boxInfo struct {
	Name            string
	Username        string
	ContainerStatus string
	Shell           string
	Multiplexer     string
}

// dashboardData holds data for the dashboard template.
type dashboardData struct {
	TotalUsers        int
	TotalBoxes        int
	RunningContainers int
	Hostname          string
	SSHPort           int
}

// usersData holds data for the users template.
type usersData struct {
	Users []userInfo
}

// boxesData holds data for the boxes template.
type boxesData struct {
	Username string
	Boxes    []boxInfo
}

// settingsData holds data for the settings template.
type settingsData struct {
	OpenRegistration bool
}

func (s *AdminServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	allUsers := s.store.ListAll()
	totalBoxes := 0
	for fp := range allUsers {
		userDir := fmt.Sprintf("%s/%s", s.store.Dir(), fp)
		boxes, _ := containers.ListBoxes(userDir)
		totalBoxes += len(boxes)
	}

	// Count running hopbox containers
	ctx := context.Background()
	runningContainers, err := s.dockerCli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "hopbox-"), filters.Arg("status", "running")),
	})
	runningCount := 0
	if err == nil {
		runningCount = len(runningContainers)
	}

	s.renderPage(w, "dashboard.html", dashboardData{
		TotalUsers:        len(allUsers),
		TotalBoxes:        totalBoxes,
		RunningContainers: runningCount,
		Hostname:          s.cfg.Hostname,
		SSHPort:           s.cfg.Port,
	})
}

func (s *AdminServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	allUsers := s.store.ListAll()

	var userList []userInfo
	for fp, u := range allUsers {
		userDir := fmt.Sprintf("%s/%s", s.store.Dir(), fp)
		boxes, _ := containers.ListBoxes(userDir)
		userList = append(userList, userInfo{
			Username:     u.Username,
			Fingerprint:  fp,
			KeyType:      u.KeyType,
			RegisteredAt: u.RegisteredAt,
			BoxCount:     len(boxes),
		})
	}

	s.renderPage(w, "users.html", usersData{Users: userList})
}

func (s *AdminServer) handleBoxes(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")

	// Find the user's fingerprint
	allUsers := s.store.ListAll()
	var fp string
	for f, u := range allUsers {
		if u.Username == username {
			fp = f
			break
		}
	}
	if fp == "" {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	userDir := fmt.Sprintf("%s/%s", s.store.Dir(), fp)
	boxNames, _ := containers.ListBoxes(userDir)

	ctx := context.Background()
	var boxList []boxInfo
	for _, name := range boxNames {
		containerName := containers.ContainerName(username, name)
		status := "none"

		cl, err := s.dockerCli.ContainerList(ctx, container.ListOptions{
			All:     true,
			Filters: filters.NewArgs(filters.Arg("name", "^/"+containerName+"$")),
		})
		if err == nil && len(cl) > 0 {
			status = cl[0].State
		}

		boxList = append(boxList, boxInfo{
			Name:            name,
			Username:        username,
			ContainerStatus: status,
		})
	}

	s.renderPage(w, "boxes.html", boxesData{
		Username: username,
		Boxes:    boxList,
	})
}

func (s *AdminServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderPage(w, "settings.html", settingsData{
		OpenRegistration: s.cfg.OpenRegistration,
	})
}

func (s *AdminServer) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")

	allUsers := s.store.ListAll()
	var fp string
	for f, u := range allUsers {
		if u.Username == username {
			fp = f
			break
		}
	}
	if fp == "" {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Delete all boxes and containers for this user
	userDir := fmt.Sprintf("%s/%s", s.store.Dir(), fp)
	boxNames, _ := containers.ListBoxes(userDir)
	for _, boxname := range boxNames {
		boxDir := fmt.Sprintf("%s/boxes/%s", userDir, boxname)
		_ = s.manager.DestroyBox(context.Background(), username, boxname, boxDir)
	}

	// Delete user from store
	if err := s.store.Delete(fp); err != nil {
		log.Printf("[admin] failed to delete user %s: %v", username, err)
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	log.Printf("[admin] deleted user %s (fp=%s)", username, fp[:12])

	// Return empty string — htmx will remove the row
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, "")
}

func (s *AdminServer) handleDeleteBox(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	boxname := r.PathValue("boxname")

	allUsers := s.store.ListAll()
	var fp string
	for f, u := range allUsers {
		if u.Username == username {
			fp = f
			break
		}
	}
	if fp == "" {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	userDir := fmt.Sprintf("%s/%s", s.store.Dir(), fp)
	boxDir := fmt.Sprintf("%s/boxes/%s", userDir, boxname)
	if err := s.manager.DestroyBox(context.Background(), username, boxname, boxDir); err != nil {
		log.Printf("[admin] failed to delete box %s/%s: %v", username, boxname, err)
		http.Error(w, "Failed to delete box", http.StatusInternalServerError)
		return
	}

	log.Printf("[admin] deleted box %s/%s", username, boxname)
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, "")
}

func (s *AdminServer) handleStopBox(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	boxname := r.PathValue("boxname")

	containerName := containers.ContainerName(username, boxname)
	ctx := context.Background()

	cl, err := s.dockerCli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+containerName+"$")),
	})
	if err != nil || len(cl) == 0 {
		http.Error(w, "Container not found", http.StatusNotFound)
		return
	}

	if err := s.dockerCli.ContainerStop(ctx, cl[0].ID, container.StopOptions{}); err != nil {
		log.Printf("[admin] failed to stop container %s: %v", containerName, err)
		http.Error(w, "Failed to stop container", http.StatusInternalServerError)
		return
	}

	log.Printf("[admin] stopped container %s", containerName)

	// Return updated status badge
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, `<span class="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-medium text-gray-800">exited</span>`)
}

func (s *AdminServer) handleToggleRegistration(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Toggle the value
	enabled := r.FormValue("enabled") == "true"
	s.cfg.OpenRegistration = enabled

	log.Printf("[admin] registration toggled to %v (runtime only)", enabled)

	// Return the updated toggle fragment
	w.Header().Set("Content-Type", "text/html")
	if enabled {
		fmt.Fprint(w, registrationOnFragment)
	} else {
		fmt.Fprint(w, registrationOffFragment)
	}
}

var registrationOnFragment = strings.TrimSpace(`
<div id="registration-toggle" class="flex items-center gap-4">
    <span class="inline-flex items-center rounded-full bg-green-100 px-3 py-1 text-sm font-medium text-green-800">Open</span>
    <button hx-put="/api/settings/registration" hx-vals='{"enabled":"false"}' hx-target="#registration-toggle" hx-swap="outerHTML"
        class="rounded-md bg-red-600 px-3 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-red-500">
        Disable Registration
    </button>
</div>
`)

var registrationOffFragment = strings.TrimSpace(`
<div id="registration-toggle" class="flex items-center gap-4">
    <span class="inline-flex items-center rounded-full bg-red-100 px-3 py-1 text-sm font-medium text-red-800">Closed</span>
    <button hx-put="/api/settings/registration" hx-vals='{"enabled":"true"}' hx-target="#registration-toggle" hx-swap="outerHTML"
        class="rounded-md bg-green-600 px-3 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-green-500">
        Enable Registration
    </button>
</div>
`)
```

---

## Task 5: Admin templates

**Files:** `internal/admin/templates/layout.html`, `internal/admin/templates/dashboard.html`, `internal/admin/templates/users.html`, `internal/admin/templates/boxes.html`, `internal/admin/templates/settings.html` (all new)

### Create `internal/admin/templates/layout.html`

```html
{{define "layout"}}
<!DOCTYPE html>
<html lang="en" class="h-full bg-gray-50">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Hopbox Admin</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://unpkg.com/htmx.org@2.0.4"></script>
</head>
<body class="h-full" hx-headers='{"HX-Request": "true"}'>
    <div class="flex h-full">
        <!-- Sidebar -->
        <div class="flex w-64 flex-col bg-gray-900">
            <div class="flex h-16 items-center px-6">
                <span class="text-xl font-bold text-white">Hopbox</span>
                <span class="ml-2 text-sm text-gray-400">Admin</span>
            </div>
            <nav class="flex-1 space-y-1 px-3 py-4">
                <a href="/" class="flex items-center rounded-md px-3 py-2 text-sm font-medium text-gray-300 hover:bg-gray-800 hover:text-white">
                    <svg class="mr-3 h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z" />
                    </svg>
                    Dashboard
                </a>
                <a href="/users" class="flex items-center rounded-md px-3 py-2 text-sm font-medium text-gray-300 hover:bg-gray-800 hover:text-white">
                    <svg class="mr-3 h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0Zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0Z" />
                    </svg>
                    Users
                </a>
                <a href="/settings" class="flex items-center rounded-md px-3 py-2 text-sm font-medium text-gray-300 hover:bg-gray-800 hover:text-white">
                    <svg class="mr-3 h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z" />
                        <path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z" />
                    </svg>
                    Settings
                </a>
            </nav>
        </div>

        <!-- Main content -->
        <div class="flex-1 overflow-auto">
            <div class="p-8">
                {{template "content" .}}
            </div>
        </div>
    </div>
</body>
</html>
{{end}}
```

### Create `internal/admin/templates/dashboard.html`

```html
{{template "layout" .}}

{{define "content"}}
<div>
    <h1 class="text-2xl font-bold text-gray-900">Dashboard</h1>
    <p class="mt-1 text-sm text-gray-500">Server overview and statistics.</p>

    <div class="mt-6 grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
        <!-- Total Users -->
        <div class="overflow-hidden rounded-lg bg-white shadow">
            <div class="p-5">
                <div class="flex items-center">
                    <div class="flex-shrink-0">
                        <svg class="h-6 w-6 text-gray-400" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0Zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0Z" />
                        </svg>
                    </div>
                    <div class="ml-5 w-0 flex-1">
                        <dl>
                            <dt class="truncate text-sm font-medium text-gray-500">Total Users</dt>
                            <dd class="text-3xl font-semibold text-gray-900">{{.TotalUsers}}</dd>
                        </dl>
                    </div>
                </div>
            </div>
            <div class="bg-gray-50 px-5 py-3">
                <a href="/users" class="text-sm font-medium text-indigo-600 hover:text-indigo-500">View all</a>
            </div>
        </div>

        <!-- Total Boxes -->
        <div class="overflow-hidden rounded-lg bg-white shadow">
            <div class="p-5">
                <div class="flex items-center">
                    <div class="flex-shrink-0">
                        <svg class="h-6 w-6 text-gray-400" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" d="m21 7.5-9-5.25L3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9" />
                        </svg>
                    </div>
                    <div class="ml-5 w-0 flex-1">
                        <dl>
                            <dt class="truncate text-sm font-medium text-gray-500">Total Boxes</dt>
                            <dd class="text-3xl font-semibold text-gray-900">{{.TotalBoxes}}</dd>
                        </dl>
                    </div>
                </div>
            </div>
        </div>

        <!-- Running Containers -->
        <div class="overflow-hidden rounded-lg bg-white shadow">
            <div class="p-5">
                <div class="flex items-center">
                    <div class="flex-shrink-0">
                        <svg class="h-6 w-6 text-gray-400" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" d="M5.25 14.25h13.5m-13.5 0a3 3 0 0 1-3-3m3 3a3 3 0 1 0 0 6h13.5a3 3 0 1 0 0-6m-16.5-3a3 3 0 0 1 3-3h13.5a3 3 0 0 1 3 3m-19.5 0a4.5 4.5 0 0 1 .9-2.7L5.737 5.1a3.375 3.375 0 0 1 2.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 0 1 .9 2.7m0 0a3 3 0 0 1-3 3m0 3h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Zm-3 6h.008v.008h-.008v-.008Zm0-6h.008v.008h-.008v-.008Z" />
                        </svg>
                    </div>
                    <div class="ml-5 w-0 flex-1">
                        <dl>
                            <dt class="truncate text-sm font-medium text-gray-500">Running Containers</dt>
                            <dd class="text-3xl font-semibold text-gray-900">{{.RunningContainers}}</dd>
                        </dl>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <!-- Server Info -->
    <div class="mt-8">
        <h2 class="text-lg font-semibold text-gray-900">Server Info</h2>
        <div class="mt-4 overflow-hidden rounded-lg bg-white shadow">
            <dl class="divide-y divide-gray-200">
                <div class="px-6 py-4 sm:grid sm:grid-cols-3 sm:gap-4">
                    <dt class="text-sm font-medium text-gray-500">Hostname</dt>
                    <dd class="mt-1 text-sm text-gray-900 sm:col-span-2 sm:mt-0">{{if .Hostname}}{{.Hostname}}{{else}}<span class="text-gray-400">Not configured</span>{{end}}</dd>
                </div>
                <div class="px-6 py-4 sm:grid sm:grid-cols-3 sm:gap-4">
                    <dt class="text-sm font-medium text-gray-500">SSH Port</dt>
                    <dd class="mt-1 text-sm text-gray-900 sm:col-span-2 sm:mt-0">{{.SSHPort}}</dd>
                </div>
            </dl>
        </div>
    </div>
</div>
{{end}}
```

### Create `internal/admin/templates/users.html`

```html
{{template "layout" .}}

{{define "content"}}
<div>
    <h1 class="text-2xl font-bold text-gray-900">Users</h1>
    <p class="mt-1 text-sm text-gray-500">All registered users and their boxes.</p>

    <div class="mt-6 overflow-hidden rounded-lg bg-white shadow">
        <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
                <tr>
                    <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Username</th>
                    <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Key Type</th>
                    <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Registered</th>
                    <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Boxes</th>
                    <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">Actions</th>
                </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 bg-white">
                {{range .Users}}
                <tr id="user-row-{{.Username}}">
                    <td class="whitespace-nowrap px-6 py-4 text-sm font-medium text-gray-900">
                        <a href="/users/{{.Username}}/boxes" class="text-indigo-600 hover:text-indigo-500">{{.Username}}</a>
                    </td>
                    <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500">{{.KeyType}}</td>
                    <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500">{{.RegisteredAt.Format "2006-01-02"}}</td>
                    <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500">{{.BoxCount}}</td>
                    <td class="whitespace-nowrap px-6 py-4 text-right text-sm">
                        <button hx-delete="/api/users/{{.Username}}" hx-target="#user-row-{{.Username}}" hx-swap="outerHTML"
                            hx-confirm="Are you sure you want to remove user '{{.Username}}'? This will delete all their boxes and containers."
                            class="rounded bg-red-50 px-2 py-1 text-xs font-semibold text-red-600 hover:bg-red-100">
                            Remove
                        </button>
                    </td>
                </tr>
                {{else}}
                <tr>
                    <td colspan="5" class="px-6 py-8 text-center text-sm text-gray-500">No users registered yet.</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</div>
{{end}}
```

### Create `internal/admin/templates/boxes.html`

```html
{{template "layout" .}}

{{define "content"}}
<div>
    <div class="flex items-center gap-3">
        <a href="/users" class="text-sm text-gray-500 hover:text-gray-700">&larr; Users</a>
        <h1 class="text-2xl font-bold text-gray-900">{{.Username}}'s Boxes</h1>
    </div>
    <p class="mt-1 text-sm text-gray-500">Manage boxes and containers for this user.</p>

    <div class="mt-6 overflow-hidden rounded-lg bg-white shadow">
        <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
                <tr>
                    <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Box Name</th>
                    <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Container Status</th>
                    <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">Actions</th>
                </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 bg-white">
                {{range .Boxes}}
                <tr id="box-row-{{.Username}}-{{.Name}}">
                    <td class="whitespace-nowrap px-6 py-4 text-sm font-medium text-gray-900">{{.Name}}</td>
                    <td class="whitespace-nowrap px-6 py-4 text-sm" id="status-{{.Username}}-{{.Name}}">
                        {{if eq .ContainerStatus "running"}}
                        <span class="inline-flex items-center rounded-full bg-green-100 px-2.5 py-0.5 text-xs font-medium text-green-800">running</span>
                        {{else if eq .ContainerStatus "exited"}}
                        <span class="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-medium text-gray-800">exited</span>
                        {{else}}
                        <span class="inline-flex items-center rounded-full bg-yellow-100 px-2.5 py-0.5 text-xs font-medium text-yellow-800">{{.ContainerStatus}}</span>
                        {{end}}
                    </td>
                    <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-2">
                        {{if eq .ContainerStatus "running"}}
                        <button hx-post="/api/users/{{.Username}}/boxes/{{.Name}}/stop" hx-target="#status-{{.Username}}-{{.Name}}" hx-swap="innerHTML"
                            hx-confirm="Stop the container for box '{{.Name}}'?"
                            class="rounded bg-yellow-50 px-2 py-1 text-xs font-semibold text-yellow-700 hover:bg-yellow-100">
                            Stop
                        </button>
                        {{end}}
                        <button hx-delete="/api/users/{{.Username}}/boxes/{{.Name}}" hx-target="#box-row-{{.Username}}-{{.Name}}" hx-swap="outerHTML"
                            hx-confirm="Are you sure you want to remove box '{{.Name}}'? This will delete the container and all box data."
                            class="rounded bg-red-50 px-2 py-1 text-xs font-semibold text-red-600 hover:bg-red-100">
                            Remove
                        </button>
                    </td>
                </tr>
                {{else}}
                <tr>
                    <td colspan="3" class="px-6 py-8 text-center text-sm text-gray-500">No boxes for this user.</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</div>
{{end}}
```

### Create `internal/admin/templates/settings.html`

```html
{{template "layout" .}}

{{define "content"}}
<div>
    <h1 class="text-2xl font-bold text-gray-900">Settings</h1>
    <p class="mt-1 text-sm text-gray-500">Runtime server settings. Changes do not persist to the config file.</p>

    <div class="mt-6 overflow-hidden rounded-lg bg-white shadow">
        <div class="px-6 py-5">
            <h3 class="text-base font-semibold text-gray-900">Open Registration</h3>
            <p class="mt-1 text-sm text-gray-500">When enabled, new users can register by SSH-ing in with an unrecognized key.</p>
            <div class="mt-4">
                {{if .OpenRegistration}}
                <div id="registration-toggle" class="flex items-center gap-4">
                    <span class="inline-flex items-center rounded-full bg-green-100 px-3 py-1 text-sm font-medium text-green-800">Open</span>
                    <button hx-put="/api/settings/registration" hx-vals='{"enabled":"false"}' hx-target="#registration-toggle" hx-swap="outerHTML"
                        class="rounded-md bg-red-600 px-3 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-red-500">
                        Disable Registration
                    </button>
                </div>
                {{else}}
                <div id="registration-toggle" class="flex items-center gap-4">
                    <span class="inline-flex items-center rounded-full bg-red-100 px-3 py-1 text-sm font-medium text-red-800">Closed</span>
                    <button hx-put="/api/settings/registration" hx-vals='{"enabled":"true"}' hx-target="#registration-toggle" hx-swap="outerHTML"
                        class="rounded-md bg-green-600 px-3 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-green-500">
                        Enable Registration
                    </button>
                </div>
                {{end}}
            </div>
        </div>
    </div>
</div>
{{end}}
```

---

## Task 6: Store methods

**Files:** `internal/users/store.go`

The admin server needs two new methods on the `Store`:

### Add `ListAll` method

```go
// ListAll returns a copy of all users keyed by fingerprint.
func (s *Store) ListAll() map[string]User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]User, len(s.users))
	for fp, u := range s.users {
		out[fp] = u
	}
	return out
}
```

### Add `Delete` method

```go
// Delete removes a user by fingerprint from the in-memory map and deletes their directory.
func (s *Store) Delete(fp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.users[fp]; !ok {
		return fmt.Errorf("user not found: %s", fp)
	}

	userDir := filepath.Join(s.dir, fp)
	if err := os.RemoveAll(userDir); err != nil {
		return fmt.Errorf("remove user dir: %w", err)
	}

	delete(s.users, fp)
	return nil
}
```

---

## Task 7: Wire admin into `cmd/hopboxd/main.go`

**Files:** `cmd/hopboxd/main.go`

Start the admin HTTP server in a goroutine alongside the SSH server. Log the admin URL on startup.

### Changes to `cmd/hopboxd/main.go`

Add the import:

```go
import (
	// ... existing imports ...
	"github.com/hopboxdev/hopbox/internal/admin"
)
```

After the container manager initialization and before the SSH server starts, add:

```go
	// Start admin web UI if enabled
	if cfg.Admin.Enabled {
		if cfg.Admin.Password == "" {
			log.Fatal("admin.password must be set when admin is enabled")
		}
		adminSrv := admin.NewAdminServer(&cfg, store, mgr, cli)
		go func() {
			log.Printf("admin UI: http://0.0.0.0:%d (user: %s)", cfg.Admin.Port, cfg.Admin.Username)
			if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("admin server error: %v", err)
			}
		}()
	}
```

Add `"net/http"` to the imports.

The full updated `main()` function:

```go
func main() {
	configPath := flag.String("config", "", "path to config.toml (default: ./config.toml)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log.Printf("config: port=%d data_dir=%s registration=%v idle_timeout=%dh resources=[cpu=%d mem=%dGB pids=%d]",
		cfg.Port, cfg.DataDir, cfg.OpenRegistration, cfg.IdleTimeoutHours,
		cfg.Resources.CPUCores, cfg.Resources.MemoryGB, cfg.Resources.PidsLimit)

	// Resolve data dir to absolute path (Docker bind mounts require absolute paths)
	cfg.DataDir, err = filepath.Abs(cfg.DataDir)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}

	// Ensure data directory exists
	usersDir := filepath.Join(cfg.DataDir, "users")
	if err := os.MkdirAll(usersDir, 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	// Initialize Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	// Verify Docker is reachable
	ctx := context.Background()
	if _, err := cli.Ping(ctx); err != nil {
		log.Fatalf("cannot reach Docker daemon: %v", err)
	}

	// Ensure base image is built
	templatesDir := findTemplatesDir()
	imageTag, err := containers.EnsureBaseImage(ctx, cli, templatesDir)
	if err != nil {
		log.Fatalf("ensure base image: %v", err)
	}
	log.Printf("using base image: %s", imageTag)

	// Initialize user store
	store := users.NewStore(usersDir)

	// Initialize container manager
	mgr := containers.NewManager(cli, cfg)

	// Start admin web UI if enabled
	if cfg.Admin.Enabled {
		if cfg.Admin.Password == "" {
			log.Fatal("admin.password must be set when admin is enabled")
		}
		adminSrv := admin.NewAdminServer(&cfg, store, mgr, cli)
		go func() {
			log.Printf("admin UI: http://0.0.0.0:%d (user: %s)", cfg.Admin.Port, cfg.Admin.Username)
			if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("admin server error: %v", err)
			}
		}()
	}

	// Start SSH server
	srv, err := gateway.NewServer(cfg, store, mgr, cli, imageTag)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		mgr.Shutdown()
		srv.Close()
		log.Println("shutdown complete")
	}()

	if err := srv.ListenAndServe(); err != nil {
		log.Printf("server stopped: %v", err)
	}
}
```

---

## Task 8: Smoke test

Manual testing checklist for verifying the implementation.

### `hopbox expose` testing

- [ ] Build the `hopbox` binary: `go build -o hopbox ./cmd/hopbox`
- [ ] Configure `hostname = "dev.example.com"` in `config.toml`
- [ ] SSH into a box and run `hopbox expose 3000`
- [ ] Verify output shows: `ssh -p 2222 -L 3000:localhost:3000 -N <user>@dev.example.com`
- [ ] Test with no hostname configured — should show `<server>` placeholder
- [ ] Test `hopbox expose 8080` — verify port number is correct in output
- [ ] Verify `hopbox status` still works and now includes `hostname` and `ssh_port` fields in JSON output

### Admin UI testing

- [ ] Add to `config.toml`:
  ```toml
  hostname = "dev.example.com"

  [admin]
  enabled = true
  port = 8080
  username = "admin"
  password = "testpass"
  ```
- [ ] Start `hopboxd` and verify log line: `admin UI: http://0.0.0.0:8080 (user: admin)`
- [ ] Open `http://localhost:8080` in browser — should get Basic Auth prompt
- [ ] Enter wrong credentials — should get 401
- [ ] Enter correct credentials — should see dashboard
- [ ] Dashboard: verify user count, box count, running container count are correct
- [ ] Dashboard: verify hostname and SSH port are displayed
- [ ] Navigate to Users page — verify table shows all registered users with correct box counts
- [ ] Click a username — should navigate to that user's boxes page
- [ ] Boxes page: verify container status badges (running/exited/none)
- [ ] Click Stop on a running container — verify status changes to "exited" without page reload
- [ ] Click Remove on a box — verify confirmation dialog, then row disappears
- [ ] Go back to Users — click Remove on a user — verify confirmation dialog, then row disappears
- [ ] Navigate to Settings — verify registration toggle matches config
- [ ] Click Disable Registration — verify toggle changes without page reload
- [ ] Click Enable Registration — verify toggle changes back
- [ ] Test with `admin.enabled = false` — verify admin server does not start
- [ ] Test with `admin.enabled = true` and empty password — verify `hopboxd` exits with fatal error
