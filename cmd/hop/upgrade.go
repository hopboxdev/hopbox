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

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tui"
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

	doClient := !c.AgentOnly && !c.HelperOnly
	doHelper := !c.ClientOnly && !c.AgentOnly
	doAgent := !c.ClientOnly && !c.HelperOnly

	// Resolve target version.
	targetVersion := c.TargetVersion
	if !c.Local && targetVersion == "" {
		fmt.Println(ui.StepOK("Checking for latest release"))
		v, err := latestRelease(ctx)
		if err != nil {
			return fmt.Errorf("fetch latest release: %w", err)
		}
		targetVersion = v
		fmt.Println(ui.StepOK(fmt.Sprintf("Latest release: %s", targetVersion)))
	}

	if c.Local {
		fmt.Println(ui.StepOK("Upgrading from local builds (./dist/)"))
	}

	// Helper upgrade stays pre-TUI (needs sudo subprocess).
	if doHelper && runtime.GOOS == "darwin" {
		if err := c.upgradeHelper(ctx, targetVersion); err != nil {
			return fmt.Errorf("upgrade helper: %w", err)
		}
	}

	// SSH connect before TUI so passphrase prompts work.
	var sshClient *ssh.Client
	var agentCfg *hostconfig.HostConfig
	var agentHostName string
	if doAgent {
		var err error
		agentHostName, err = resolveHost(globals)
		if err == nil {
			agentCfg, err = hostconfig.Load(agentHostName)
			if err != nil {
				return fmt.Errorf("load host config: %w", err)
			}
			sshClient, err = setup.UpgradeAgentSSH(ctx, agentCfg)
			if err != nil {
				return fmt.Errorf("SSH connect for agent upgrade: %w", err)
			}
			defer func() { _ = sshClient.Close() }()
		}
	}

	// Client + Agent upgrades via TUI step runner.
	var steps []tui.Step
	if doClient {
		tv := targetVersion
		steps = append(steps, tui.Step{
			Title: "Upgrading client",
			Run: func(ctx context.Context, sub func(string)) error {
				return c.upgradeClientStep(ctx, tv, sub)
			},
		})
	}
	if doAgent && sshClient != nil {
		tv := targetVersion
		steps = append(steps, tui.Step{
			Title: "Upgrading agent",
			Run: func(ctx context.Context, sub func(string)) error {
				return c.upgradeAgentStepWithClient(ctx, sshClient, agentCfg, agentHostName, tv, sub)
			},
		})
	} else if doAgent && sshClient == nil && agentHostName == "" {
		// No host configured — show as skipped in the TUI.
		steps = append(steps, tui.Step{
			Title: "Upgrading agent",
			Run: func(ctx context.Context, sub func(string)) error {
				sub("Agent: skipped (no host configured)")
				return nil
			},
		})
	}

	if len(steps) > 0 {
		if err := tui.RunSteps(ctx, steps); err != nil {
			return err
		}
	}

	fmt.Println("\n" + ui.StepOK("Upgrade complete"))
	return nil
}

func (c *UpgradeCmd) upgradeClientStep(ctx context.Context, targetVersion string, sub func(string)) error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return err
	}

	if pm := version.DetectPackageManager(execPath); pm != "" {
		sub(fmt.Sprintf("Client: installed via %s — run your package manager to update", pm))
		return nil
	}

	if !c.Local && targetVersion == version.Version {
		sub(fmt.Sprintf("Client: already at %s", version.Version))
		return nil
	}

	if c.Local {
		paths := resolveLocalPaths("dist")
		data, err := os.ReadFile(paths.client)
		if err != nil {
			return fmt.Errorf("read %s: %w", paths.client, err)
		}
		sub("Client: upgrading from local build")
		if err := atomicReplace(execPath, data); err != nil {
			return err
		}
		sub("Client upgraded from local build")
		return nil
	}

	binName := fmt.Sprintf("hop_%s_%s_%s", targetVersion, runtime.GOOS, runtime.GOARCH)
	binURL := fmt.Sprintf("%s/v%s/%s", releaseBaseURL, targetVersion, binName)
	sub(fmt.Sprintf("Client: %s → %s", version.Version, targetVersion))
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
	} else {
		binName := fmt.Sprintf("hop-helper_%s_%s_%s", targetVersion, runtime.GOOS, runtime.GOARCH)
		binURL := fmt.Sprintf("%s/v%s/%s", releaseBaseURL, targetVersion, binName)
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

func (c *UpgradeCmd) upgradeAgentStepWithClient(ctx context.Context, client *ssh.Client, cfg *hostconfig.HostConfig, hostName, targetVersion string, sub func(string)) error {
	if state, err := tunnel.LoadState(hostName); err == nil && state != nil {
		sub(fmt.Sprintf("Agent (%s): tunnel running (PID %d), agent will restart", hostName, state.PID))
	}

	if c.Local {
		paths := resolveLocalPaths("dist")
		if err := os.Setenv("HOP_AGENT_BINARY", paths.agent); err != nil {
			return fmt.Errorf("set HOP_AGENT_BINARY: %w", err)
		}
	}

	agentVersion := targetVersion
	if c.Local {
		agentVersion = ""
	}

	sub(fmt.Sprintf("Agent (%s): upgrading", hostName))
	if err := setup.UpgradeAgentWithClient(ctx, client, cfg, os.Stdout, agentVersion, sub); err != nil {
		return err
	}
	sub(fmt.Sprintf("Agent (%s) upgraded", hostName))
	return nil
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
