package configserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v2"
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

const collectionIndexURL = "https://raw.githubusercontent.com/devcontainers/devcontainers.github.io/gh-pages/_data/collection-index.yml"
const ghcrBaseURL = "https://ghcr.io"

type collectionEntry struct {
	OCIReference string `yaml:"ociReference"`
}

type collectionIndex []collectionEntry

type ociManifest struct {
	Layers []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

type devcontainerCollection struct {
	Features []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"features"`
}

// FetchCatalog fetches the full feature catalog from upstream GHCR sources.
func FetchCatalog(ctx context.Context) (*Catalog, error) {
	return fetchCatalogFrom(ctx, collectionIndexURL, ghcrBaseURL)
}

func fetchCatalogFrom(ctx context.Context, indexURL, registryBase string) (*Catalog, error) {
	resp, err := httpGet(ctx, indexURL)
	if err != nil {
		return nil, fmt.Errorf("fetch collection index: %w", err)
	}
	defer resp.Body.Close()

	var idx collectionIndex
	if err := yaml.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return nil, fmt.Errorf("parse collection index: %w", err)
	}

	var all []Feature
	for _, col := range idx {
		ref := col.OCIReference
		if ref == "" {
			continue
		}
		features, err := fetchCollectionFeatures(ctx, ref, registryBase)
		if err != nil {
			// Non-fatal: skip broken namespaces
			continue
		}
		all = append(all, features...)
	}

	return &Catalog{Features: all, FetchedAt: time.Now().UTC()}, nil
}

// fetchCollectionFeatures fetches features for one OCI reference namespace.
// ociRef is like "ghcr.io/devcontainers/features" or "host:port/devcontainers/features".
func fetchCollectionFeatures(ctx context.Context, ociRef, registryBase string) ([]Feature, error) {
	parts := strings.SplitN(ociRef, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid ociRef: %s", ociRef)
	}
	registry := parts[0]
	repoPath := parts[1]
	publisher := strings.Split(repoPath, "/")[0]

	base := "https://" + registry
	if registryBase != ghcrBaseURL {
		base = registryBase
		registry = strings.TrimPrefix(strings.TrimPrefix(registryBase, "https://"), "http://")
	}

	// Get anonymous bearer token
	tokenURL := fmt.Sprintf("%s/token?scope=repository:%s:pull&service=%s", base, repoPath, registry)
	tokenResp, err := httpGet(ctx, tokenURL)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	defer tokenResp.Body.Close()
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenBody); err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	// Fetch manifest
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/devcontainer-collection:latest", base, repoPath)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	req.Header.Set("Authorization", "Bearer "+tokenBody.Token)
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	mResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer mResp.Body.Close()

	var manifest ociManifest
	if err := json.NewDecoder(mResp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	var digest string
	for _, layer := range manifest.Layers {
		if layer.MediaType == "application/vnd.devcontainers" {
			digest = layer.Digest
			break
		}
	}
	if digest == "" {
		return nil, fmt.Errorf("no devcontainers layer in manifest for %s", ociRef)
	}

	// Fetch collection blob
	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", base, repoPath, digest)
	blobReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
	blobReq.Header.Set("Authorization", "Bearer "+tokenBody.Token)
	blobResp, err := http.DefaultClient.Do(blobReq)
	if err != nil {
		return nil, fmt.Errorf("fetch blob: %w", err)
	}
	defer blobResp.Body.Close()

	var col devcontainerCollection
	if err := json.NewDecoder(blobResp.Body).Decode(&col); err != nil {
		return nil, fmt.Errorf("parse collection: %w", err)
	}

	features := make([]Feature, 0, len(col.Features))
	for _, f := range col.Features {
		features = append(features, Feature{
			ID:          f.ID,
			Name:        f.Name,
			Description: f.Description,
			Publisher:   publisher,
			OCIRef:      fmt.Sprintf("%s/%s:1", ociRef, f.ID),
		})
	}
	return features, nil
}

// LoadOrFetchCatalog returns a cached catalog if fresh, otherwise fetches from upstream.
func LoadOrFetchCatalog(ctx context.Context, cachePath string) (*Catalog, error) {
	return loadOrFetchWith(ctx, cachePath, FetchCatalog)
}

func loadOrFetchWith(ctx context.Context, cachePath string, fetchFn func(context.Context) (*Catalog, error)) (*Catalog, error) {
	cached, err := loadCatalogFromDisk(cachePath)
	if err == nil && catalogIsFresh(cached, catalogTTL) {
		return cached, nil
	}

	cat, err := fetchFn(ctx)
	if err != nil {
		if cached != nil {
			cached.Stale = true
			return cached, nil
		}
		return &Catalog{Stale: true}, nil
	}

	_ = saveCatalogToDisk(cat, cachePath)
	return cat, nil
}

var catalogHTTPClient = &http.Client{Timeout: 15 * time.Second}

func httpGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return catalogHTTPClient.Do(req)
}
