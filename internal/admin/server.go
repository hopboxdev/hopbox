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
	tmpl, ok := pageTmpl[name]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
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
