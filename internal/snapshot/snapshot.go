// Package snapshot wraps the restic CLI for workspace backup and restore.
package snapshot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Info describes a single restic snapshot.
type Info struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Paths    []string  `json:"paths"`
	Hostname string    `json:"hostname"`
	Tags     []string  `json:"tags,omitempty"`
}

// SummaryResult is returned by Create.
type SummaryResult struct {
	SnapshotID string `json:"snapshot_id"`
	FilesNew   int    `json:"files_new"`
	SizeAdded  int64  `json:"added_size"`
}

// Create runs `restic backup` for the given paths.
// target is the restic repository (e.g. "s3:s3.amazonaws.com/my-bucket/ws").
// env overrides are merged with the current process environment and should
// contain RESTIC_PASSWORD and any cloud credentials.
func Create(ctx context.Context, target string, paths []string, envOverrides map[string]string) (*SummaryResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no paths to back up")
	}
	args := append([]string{"backup", "--json"}, paths...)
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = mergeEnv(envOverrides)
	cmd.Env = append(cmd.Env, "RESTIC_REPOSITORY="+target)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("restic backup: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	// restic --json outputs one JSON object per line; the last non-empty line
	// is the summary.
	var summary SummaryResult
	scanner := bufio.NewScanner(bytes.NewReader(out))
	var lastLine string
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lastLine = line
		}
	}
	if lastLine != "" {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(lastLine), &raw); err == nil {
			if id, ok := raw["snapshot_id"]; ok {
				_ = json.Unmarshal(id, &summary.SnapshotID)
			}
		}
	}
	return &summary, nil
}

// List returns all snapshots in the repository.
func List(ctx context.Context, target string, envOverrides map[string]string) ([]Info, error) {
	cmd := exec.CommandContext(ctx, "restic", "snapshots", "--json")
	cmd.Env = mergeEnv(envOverrides)
	cmd.Env = append(cmd.Env, "RESTIC_REPOSITORY="+target)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("restic snapshots: %w", err)
	}

	var snaps []Info
	if err := json.Unmarshal(out, &snaps); err != nil {
		return nil, fmt.Errorf("parse snapshots: %w", err)
	}
	return snaps, nil
}

// Restore runs `restic restore <id> --target <restoreTo>`.
// restoreTo is typically "/" to restore files to their original absolute paths.
func Restore(ctx context.Context, target, id, restoreTo string, envOverrides map[string]string) error {
	if restoreTo == "" {
		restoreTo = "/"
	}
	cmd := exec.CommandContext(ctx, "restic", "restore", id, "--target", restoreTo)
	cmd.Env = mergeEnv(envOverrides)
	cmd.Env = append(cmd.Env, "RESTIC_REPOSITORY="+target)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic restore %q: %w\n%s", id, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Init initialises a new restic repository.
func Init(ctx context.Context, target string, envOverrides map[string]string) error {
	cmd := exec.CommandContext(ctx, "restic", "init")
	cmd.Env = mergeEnv(envOverrides)
	cmd.Env = append(cmd.Env, "RESTIC_REPOSITORY="+target)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restic init: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// mergeEnv returns the current process environment with overrides applied.
func mergeEnv(overrides map[string]string) []string {
	env := os.Environ()
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}
