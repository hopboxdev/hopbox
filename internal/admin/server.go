package admin

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/hopboxdev/hopbox/internal/config"
	"github.com/hopboxdev/hopbox/internal/containers"
	"github.com/hopboxdev/hopbox/internal/users"
)

//go:embed templates/*.html
var templateFS embed.FS

var pageTmpl map[string]*template.Template

var funcMap = template.FuncMap{
	"sub": func(a, b int) int { return a - b },
}

func init() {
	pageTmpl = make(map[string]*template.Template)
	pages := []string{"dashboard.html", "users.html", "boxes.html", "settings.html"}
	for _, page := range pages {
		pageTmpl[page] = template.Must(
			template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/layout.html", "templates/"+page),
		)
	}
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

	// Authenticated routes live on their own sub-mux.
	authed := http.NewServeMux()
	authed.HandleFunc("GET /", s.handleDashboard)
	authed.HandleFunc("GET /users", s.handleUsers)
	authed.HandleFunc("GET /users/{username}/boxes", s.handleBoxes)
	authed.HandleFunc("GET /settings", s.handleSettings)
	authed.HandleFunc("DELETE /api/users/{username}", s.handleDeleteUser)
	authed.HandleFunc("DELETE /api/users/{username}/boxes/{boxname}", s.handleDeleteBox)
	authed.HandleFunc("POST /api/users/{username}/boxes/{boxname}/stop", s.handleStopBox)
	authed.HandleFunc("PUT /api/settings/registration", s.handleToggleRegistration)

	// Top-level mux: public routes direct, everything else via basicAuth(authed).
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", s.handleHealthz)
	root.Handle("GET /metrics", promhttp.Handler())
	root.Handle("/", s.basicAuth(authed))

	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Admin.Port),
		Handler: root,
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

// handleHealthz checks Docker reachability and returns JSON health status.
func (s *AdminServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if _, err := s.dockerCli.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"unhealthy","docker":"unreachable","error":%q}`, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"healthy","docker":"reachable"}`)
}

// renderPage renders a full page template with the layout.
func (s *AdminServer) renderPage(w http.ResponseWriter, name string, data any) {
	tmpl, ok := pageTmpl[name]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("template error", "component", "admin", "err", err)
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
		slog.Error("failed to delete user", "component", "admin", "user", username, "err", err)
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	slog.Info("deleted user", "component", "admin", "user", username, "fp", fp[:12])

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
		slog.Error("failed to delete box", "component", "admin", "user", username, "box", boxname, "err", err)
		http.Error(w, "Failed to delete box", http.StatusInternalServerError)
		return
	}

	slog.Info("deleted box", "component", "admin", "user", username, "box", boxname)
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
		slog.Error("failed to stop container", "component", "admin", "container", containerName, "err", err)
		http.Error(w, "Failed to stop container", http.StatusInternalServerError)
		return
	}

	slog.Info("stopped container", "component", "admin", "container", containerName)

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

	slog.Info("registration toggled (runtime only)", "component", "admin", "enabled", enabled)

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
