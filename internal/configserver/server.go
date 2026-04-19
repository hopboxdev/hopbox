package configserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

const catalogCachePath = "/tmp/hopbox-catalog.json"

// Server is the config HTTP server. Bind address is always 127.0.0.1.
type Server struct {
	Port             int
	DevcontainerPath string
}

// ListenAndServe starts the HTTP server. Prints "LISTENING :<port>" to stdout
// once ready. Returns nil when the server shuts down cleanly (last WebSocket
// client disconnects). ctx cancellation forces immediate shutdown.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Load catalog in background; serve stale/empty in the meantime.
	var catalog atomic.Pointer[Catalog]
	catalogReady := make(chan struct{})
	go func() {
		cat, _ := LoadOrFetchCatalog(context.Background(), catalogCachePath)
		if cat != nil {
			catalog.Store(cat)
		}
		close(catalogReady)
	}()

	mux := http.NewServeMux()

	hb := NewHeartbeatManager(cancel, 5*time.Second)
	mux.Handle("/ws", hb.Handler())

	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(s.DevcontainerPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	mux.HandleFunc("/save", SaveHandler(s.DevcontainerPath))

	mux.HandleFunc("/catalog", func(w http.ResponseWriter, r *http.Request) {
		// Wait up to 100ms for catalog to be ready before returning empty.
		select {
		case <-catalogReady:
		case <-time.After(100 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "application/json")
		c := catalog.Load()
		if c == nil {
			c = &Catalog{Stale: true}
		}
		json.NewEncoder(w).Encode(c)
	})

	mux.HandleFunc("/catalog/refresh", func(w http.ResponseWriter, r *http.Request) {
		cat, err := FetchCatalog(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		catalog.Store(cat)
		_ = saveCatalogToDisk(cat, catalogCachePath)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cat)
	})

	// Static files — strip "static/" prefix so "/" serves index.html
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("embed static: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(subFS)))

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.Port),
		Handler: mux,
	}

	// Shutdown on context cancel (WebSocket heartbeat or parent cancellation).
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	fmt.Printf("LISTENING :%d\n", s.Port)

	err = srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
