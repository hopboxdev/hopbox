package configserver

import (
	"path/filepath"
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
