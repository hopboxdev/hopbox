package configserver

import (
	"encoding/json"
	"os"
	"time"
)

const catalogTTL = 24 * time.Hour

// Feature is a single devcontainer feature entry from the catalog.
type Feature struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Publisher   string `json:"publisher"`
	OCIRef      string `json:"ociRef"`
}

// Catalog holds the full feature list and cache metadata.
type Catalog struct {
	Features  []Feature `json:"features"`
	FetchedAt time.Time `json:"fetchedAt"`
	Stale     bool      `json:"stale,omitempty"`
}

func saveCatalogToDisk(c *Catalog, path string) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadCatalogFromDisk(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func catalogIsFresh(c *Catalog, ttl time.Duration) bool {
	return time.Since(c.FetchedAt) < ttl
}
