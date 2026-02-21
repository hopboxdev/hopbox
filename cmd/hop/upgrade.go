package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/ui"
	"github.com/hopboxdev/hopbox/internal/version"
)

// UpgradeCmd upgrades hop binaries (client, helper, agent).
type UpgradeCmd struct {
	TargetVersion string `name:"version" help:"Target version (e.g. 0.3.0). Default: latest release."`
	Local         bool   `help:"Use local dev builds from ./dist/." short:"l"`
	ClientOnly    bool   `help:"Only upgrade the hop client binary."`
	AgentOnly     bool   `help:"Only upgrade the agent on the remote host."`
	HelperOnly    bool   `help:"Only upgrade the helper daemon (macOS)."`
}

type localPaths struct {
	client string
	helper string
	agent  string
}

func resolveLocalPaths(distDir string) localPaths {
	return localPaths{
		client: filepath.Join(distDir, "hop"),
		helper: filepath.Join(distDir, "hop-helper"),
		agent:  filepath.Join(distDir, "hop-agent-linux"),
	}
}

const releaseBaseURL = "https://github.com/hopboxdev/hopbox/releases/download"

func (c *UpgradeCmd) Run(globals *CLI) error {
	ctx := context.Background()

	// Determine which components to upgrade.
	doClient := !c.AgentOnly && !c.HelperOnly
	doHelper := !c.ClientOnly && !c.AgentOnly
	doAgent := !c.ClientOnly && !c.HelperOnly

	// Resolve target version.
	targetVersion := c.TargetVersion
	if !c.Local && targetVersion == "" {
		fmt.Println(ui.StepRun("Checking for latest release"))
		v, err := latestRelease(ctx)
		if err != nil {
			return fmt.Errorf("fetch latest release: %w", err)
		}
		targetVersion = v
		fmt.Println(ui.StepOK(fmt.Sprintf("Latest release: %s", targetVersion)))
	}

	if c.Local {
		fmt.Println(ui.StepRun("Upgrading from local builds (./dist/)"))
	}

	// --- Client ---
	if doClient {
		if err := c.upgradeClient(ctx, targetVersion); err != nil {
			return fmt.Errorf("upgrade client: %w", err)
		}
	}

	// --- Helper (macOS only) ---
	if doHelper && runtime.GOOS == "darwin" {
		if err := c.upgradeHelper(ctx, targetVersion); err != nil {
			return fmt.Errorf("upgrade helper: %w", err)
		}
	}

	// --- Agent ---
	if doAgent {
		if err := c.upgradeAgent(ctx, globals, targetVersion); err != nil {
			return fmt.Errorf("upgrade agent: %w", err)
		}
	}

	fmt.Println("\n" + ui.StepOK("Upgrade complete"))
	return nil
}

func (c *UpgradeCmd) upgradeClient(ctx context.Context, targetVersion string) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return err
	}

	// Check package manager.
	if pm := version.DetectPackageManager(execPath); pm != "" {
		fmt.Println(ui.StepOK(fmt.Sprintf("Client: installed via %s — run your package manager to update", pm)))
		return nil
	}

	// Version check (skip for --local since versions are both "dev").
	if !c.Local && targetVersion == version.Version {
		fmt.Println(ui.StepOK(fmt.Sprintf("Client: already at %s", version.Version)))
		return nil
	}

	if c.Local {
		paths := resolveLocalPaths("dist")
		data, err := os.ReadFile(paths.client)
		if err != nil {
			return fmt.Errorf("read %s: %w", paths.client, err)
		}
		fmt.Println(ui.StepRun("Client: upgrading from local build"))
		if err := atomicReplace(execPath, data); err != nil {
			return err
		}
		fmt.Println(ui.StepOK(fmt.Sprintf("Client: upgraded (%s)", execPath)))
		return nil
	}

	binName := fmt.Sprintf("hop_%s_%s_%s", targetVersion, runtime.GOOS, runtime.GOARCH)
	binURL := fmt.Sprintf("%s/v%s/%s", releaseBaseURL, targetVersion, binName)
	fmt.Println(ui.StepRun(fmt.Sprintf("Client: %s → %s", version.Version, targetVersion)))
	data, err := setup.FetchURL(ctx, binURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := verifyChecksum(ctx, targetVersion, binName, data); err != nil {
		return err
	}
	if err := atomicReplace(execPath, data); err != nil {
		return err
	}
	fmt.Println(ui.StepOK(fmt.Sprintf("Client: upgraded (%s)", execPath)))
	return nil
}

func (c *UpgradeCmd) upgradeHelper(ctx context.Context, targetVersion string) error {
	helperClient := helper.NewClient()

	// Version check via helper daemon.
	if !c.Local && helperClient.IsReachable() {
		if hv, err := helperClient.Version(); err == nil && hv == targetVersion {
			fmt.Println(ui.StepOK(fmt.Sprintf("Helper: already at %s", hv)))
			return nil
		}
	}

	var data []byte
	var err error
	if c.Local {
		paths := resolveLocalPaths("dist")
		data, err = os.ReadFile(paths.helper)
		if err != nil {
			return fmt.Errorf("read %s: %w", paths.helper, err)
		}
		fmt.Println(ui.StepRun("Helper: upgrading from local build (requires sudo)"))
	} else {
		binName := fmt.Sprintf("hop-helper_%s_%s_%s", targetVersion, runtime.GOOS, runtime.GOARCH)
		binURL := fmt.Sprintf("%s/v%s/%s", releaseBaseURL, targetVersion, binName)
		fmt.Println(ui.StepRun(fmt.Sprintf("Helper: upgrading to %s (requires sudo)", targetVersion)))
		data, err = setup.FetchURL(ctx, binURL)
		if err != nil {
			return fmt.Errorf("download: %w", err)
		}
		if err := verifyChecksum(ctx, targetVersion, binName, data); err != nil {
			return err
		}
	}

	// Write to temp file, then sudo --install.
	tmp, err := os.CreateTemp("", "hop-helper-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := os.WriteFile(tmpPath, data, 0755); err != nil {
		return err
	}
	// WriteFile doesn't update permissions on an existing file (created by
	// CreateTemp with 0600), so chmod explicitly to make it executable.
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return err
	}

	cmd := exec.Command("sudo", tmpPath, "--install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helper --install: %w", err)
	}
	fmt.Println(ui.StepOK("Helper: upgraded"))
	return nil
}

func (c *UpgradeCmd) upgradeAgent(ctx context.Context, globals *CLI, targetVersion string) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		fmt.Println(ui.StepOK("Agent: skipped (no host configured)"))
		return nil
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

	// Warn if tunnel is running.
	if state, err := tunnel.LoadState(hostName); err == nil && state != nil {
		fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("tunnel is running (PID %d). The agent will restart", state.PID)))
	}

	if c.Local {
		paths := resolveLocalPaths("dist")
		if err := os.Setenv("HOP_AGENT_BINARY", paths.agent); err != nil {
			return fmt.Errorf("set HOP_AGENT_BINARY: %w", err)
		}
	}

	// Pass empty targetVersion for --local so UpgradeAgent skips the
	// version comparison (dev builds always re-upload).
	agentVersion := targetVersion
	if c.Local {
		agentVersion = ""
	}

	fmt.Println(ui.StepRun(fmt.Sprintf("Agent (%s): upgrading", hostName)))
	return setup.UpgradeAgent(ctx, cfg, os.Stdout, agentVersion)
}

// atomicReplace writes data to path atomically via rename.
func atomicReplace(path string, data []byte) error {
	tmpPath := path + ".new"
	if err := os.WriteFile(tmpPath, data, 0755); err != nil {
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s → %s: %w", tmpPath, path, err)
	}
	return nil
}

// verifyChecksum downloads checksums.txt for the given release version and
// verifies the SHA256 of data matches the expected value for binName.
func verifyChecksum(ctx context.Context, releaseVersion, binName string, data []byte) error {
	csURL := fmt.Sprintf("%s/v%s/checksums.txt", releaseBaseURL, releaseVersion)
	expected, err := setup.LookupChecksum(ctx, csURL, binName)
	if err != nil {
		return fmt.Errorf("checksum lookup: %w", err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", binName, actual, expected)
	}
	return nil
}

// latestRelease queries the GitHub API for the latest release tag and returns
// the version string (without "v" prefix).
func latestRelease(ctx context.Context) (string, error) {
	data, err := setup.FetchURL(ctx, "https://api.github.com/repos/hopboxdev/hopbox/releases/latest")
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("parse release JSON: %w", err)
	}
	v := strings.TrimPrefix(release.TagName, "v")
	if v == "" {
		return "", fmt.Errorf("no tag_name in release response")
	}
	return v, nil
}
