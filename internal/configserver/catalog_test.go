package configserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCatalogDiskCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	cat := &Catalog{
		Features: []Feature{
			{ID: "go", Name: "Go", Description: "Go toolchain", Publisher: "devcontainers", OCIRef: "ghcr.io/devcontainers/features/go:1"},
		},
		FetchedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := saveCatalogToDisk(cat, path); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := loadCatalogFromDisk(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(got.Features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(got.Features))
	}
	if got.Features[0].ID != "go" {
		t.Errorf("expected go, got %s", got.Features[0].ID)
	}
	if !got.FetchedAt.Equal(cat.FetchedAt) {
		t.Errorf("FetchedAt mismatch: %v vs %v", got.FetchedAt, cat.FetchedAt)
	}
}

func TestCatalogDiskCache_Missing(t *testing.T) {
	_, err := loadCatalogFromDisk(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestCatalogIsFresh(t *testing.T) {
	fresh := &Catalog{FetchedAt: time.Now().Add(-1 * time.Hour)}
	if !catalogIsFresh(fresh, 24*time.Hour) {
		t.Error("1h old catalog should be fresh with 24h TTL")
	}

	stale := &Catalog{FetchedAt: time.Now().Add(-25 * time.Hour)}
	if catalogIsFresh(stale, 24*time.Hour) {
		t.Error("25h old catalog should be stale with 24h TTL")
	}
}

func TestFetchCatalog_FromStubs(t *testing.T) {
	// Stub GHCR registry
	ghcr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			json.NewEncoder(w).Encode(map[string]string{"token": "testtoken"})
		case strings.Contains(r.URL.Path, "/manifests/"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"layers": []map[string]interface{}{
					{
						"mediaType": "application/vnd.devcontainers",
						"digest":    "sha256:abc123",
						"size":      100,
					},
				},
			})
		case strings.Contains(r.URL.Path, "/blobs/"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"features": []map[string]interface{}{
					{
						"id":          "go",
						"name":        "Go",
						"description": "Go language toolchain",
					},
				},
			})
		}
	}))
	defer ghcr.Close()

	// Stub collection-index.yml server
	// The ociReference value is the host:port/path without scheme — fetchCollectionFeatures splits on first "/"
	ghcrHostPort := strings.TrimPrefix(ghcr.URL, "http://")
	colIdx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "- ociReference: %s/devcontainers/features\n", ghcrHostPort)
	}))
	defer colIdx.Close()

	cat, err := fetchCatalogFrom(context.Background(), colIdx.URL+"/collection-index.yml", ghcr.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(cat.Features) == 0 {
		t.Fatal("expected at least one feature")
	}
	found := false
	for _, f := range cat.Features {
		if f.ID == "go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected feature 'go', got %v", cat.Features)
	}
}

func TestLoadOrFetchCatalog_UsesDiskWhenFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")

	cat := &Catalog{
		Features:  []Feature{{ID: "rust", Name: "Rust", OCIRef: "ghcr.io/devcontainers/features/rust:1"}},
		FetchedAt: time.Now().Add(-1 * time.Hour),
	}
	if err := saveCatalogToDisk(cat, path); err != nil {
		t.Fatal(err)
	}

	called := false
	fetchFn := func(ctx context.Context) (*Catalog, error) {
		called = true
		return nil, fmt.Errorf("should not call fetch")
	}

	got, err := loadOrFetchWith(context.Background(), path, fetchFn)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if called {
		t.Error("fetch called despite fresh cache")
	}
	if len(got.Features) != 1 || got.Features[0].ID != "rust" {
		t.Errorf("unexpected features: %v", got.Features)
	}
}
