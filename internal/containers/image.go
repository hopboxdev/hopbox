package containers

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/moby/go-archive"
)

const baseImageRepo = "hopbox-base"

// HashTemplates computes a SHA256 hash of all files in the templates directory.
func HashTemplates(templatesDir string) (string, error) {
	h := sha256.New()
	var paths []string

	err := filepath.Walk(templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(templatesDir, path)
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Strings(paths)

	for _, rel := range paths {
		fmt.Fprintf(h, "file:%s\n", rel)
		data, err := os.ReadFile(filepath.Join(templatesDir, rel))
		if err != nil {
			return "", err
		}
		h.Write(data)
	}

	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// BaseImageTag returns the full image tag for the given hash.
func BaseImageTag(hash string) string {
	return fmt.Sprintf("%s:%s", baseImageRepo, hash)
}

// EnsureBaseImage checks if the base image exists for the given template hash.
// If not, it builds it from the templates directory.
func EnsureBaseImage(ctx context.Context, cli *client.Client, templatesDir string) (string, error) {
	hash, err := HashTemplates(templatesDir)
	if err != nil {
		return "", fmt.Errorf("hash templates: %w", err)
	}

	tag := BaseImageTag(hash)

	// Check if image already exists
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list images: %w", err)
	}
	for _, img := range images {
		for _, t := range img.RepoTags {
			if t == tag {
				return tag, nil
			}
		}
	}

	// Build the image
	buildCtx, err := archive.TarWithOptions(templatesDir, &archive.TarOptions{})
	if err != nil {
		return "", fmt.Errorf("create build context: %w", err)
	}
	defer buildCtx.Close()

	resp, err := cli.ImageBuild(ctx, buildCtx, build.ImageBuildOptions{
		Dockerfile: "base-devcontainer/Dockerfile",
		Tags:       []string{tag},
		Remove:     true,
	})
	if err != nil {
		return "", fmt.Errorf("build image: %w", err)
	}
	defer resp.Body.Close()

	// Parse build output, check for errors
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Stream != "" {
			fmt.Fprint(os.Stderr, msg.Stream)
		}
		if msg.Error != "" {
			return "", fmt.Errorf("build failed: %s", msg.Error)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read build output: %w", err)
	}

	return tag, nil
}
