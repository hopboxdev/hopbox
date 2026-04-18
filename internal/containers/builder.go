package containers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/hopboxdev/hopbox/internal/metrics"
)

// userImageTagFromFile computes the hopbox-owned image tag for a (username,
// boxname, devcontainer.json) triple. Pure function on the JSON content.
func userImageTagFromFile(username, boxname, devcontainerPath string) (string, error) {
	raw, err := ReadDevcontainer(devcontainerPath)
	if err != nil {
		return "", fmt.Errorf("read devcontainer: %w", err)
	}
	hash, err := CanonicalHash(raw)
	if err != nil {
		return "", fmt.Errorf("hash devcontainer: %w", err)
	}
	return fmt.Sprintf("hopbox-%s-%s:%s", username, boxname, hash), nil
}

// EnsureUserImage returns the image tag for a user's box, building it via the
// bundled builder container if it doesn't already exist. The image is derived
// from .devcontainer/devcontainer.json at the given path.
func EnsureUserImage(ctx context.Context, cli *client.Client, username, boxname, devcontainerPath string) (string, error) {
	tag, err := userImageTagFromFile(username, boxname, devcontainerPath)
	if err != nil {
		return "", err
	}

	if cli == nil {
		// Tag-only mode (used by unit tests).
		return tag, nil
	}

	// Cache hit: image already present locally.
	if exists, _ := imageExists(ctx, cli, tag); exists {
		return tag, nil
	}

	start := time.Now()
	defer func() {
		metrics.BuildDurationSeconds.Observe(time.Since(start).Seconds())
	}()
	slog.Info("building user image", "component", "builder", "tag", tag, "user", username, "box", boxname)

	workspaceDir := filepath.Dir(filepath.Dir(devcontainerPath))
	if _, err := os.Stat(filepath.Join(workspaceDir, ".devcontainer", "devcontainer.json")); err != nil {
		return "", fmt.Errorf("workspace layout: expected .devcontainer/devcontainer.json under %s: %w", workspaceDir, err)
	}

	if err := runBuilder(ctx, cli, workspaceDir, tag); err != nil {
		return "", fmt.Errorf("devcontainer build: %w", err)
	}
	return tag, nil
}

func imageExists(ctx context.Context, cli *client.Client, tag string) (bool, error) {
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, img := range images {
		for _, t := range img.RepoTags {
			if t == tag {
				return true, nil
			}
		}
	}
	return false, nil
}

// runBuilder spawns the builder image, mounts the workspace read-only plus
// the Docker socket, and runs `devcontainer build`. Blocks until the builder
// container exits. Returns an error if the exit code is non-zero.
func runBuilder(ctx context.Context, cli *client.Client, workspaceDir, imageTag string) error {
	cfg := &container.Config{
		Image: BuilderImage,
		Cmd: []string{
			"build",
			"--workspace-folder", "/workspace",
			"--image-name", imageTag,
			"--log-level", "info",
		},
	}
	host := &container.HostConfig{
		Binds: []string{
			"/var/run/docker.sock:/var/run/docker.sock",
			fmt.Sprintf("%s:/workspace:ro", workspaceDir),
		},
		AutoRemove: true,
	}

	resp, err := cli.ContainerCreate(ctx, cfg, host, nil, nil, "")
	if err != nil {
		return fmt.Errorf("create builder: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start builder: %w", err)
	}

	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("wait builder: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("builder exited with code %d", status.StatusCode)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
