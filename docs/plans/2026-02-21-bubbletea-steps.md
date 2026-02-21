# Bubbletea Step Runner Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the current two-line StepRun/StepOK pattern with animated bubbletea spinners that resolve to checkmarks in place.

**Architecture:** Create `internal/tui/` with a reusable `StepRunner` model. Commands define steps as data and pass them to `tui.RunSteps()`. The runner handles spinner animation, sub-step messages via `tea.Println`, error handling, and TTY fallback. Interactive prompts (SSH TOFU, y/N confirmations, sudo) happen before the TUI starts.

**Tech Stack:** `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/bubbles` (spinner), existing `lipgloss` + `internal/ui`

---

### Task 1: Add bubbletea and bubbles dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add dependencies**

Run: `go get github.com/charmbracelet/bubbletea@latest github.com/charmbracelet/bubbles@latest`
Expected: Downloads packages, updates go.mod and go.sum

**Step 2: Verify the build still works**

Run: `go build ./...`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add bubbletea and bubbles"
```

---

### Task 2: Create `internal/tui/step.go` — StepRunner model

**Files:**
- Create: `internal/tui/step.go`

**Step 1: Create the step runner implementation**

```go
package tui

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/hopboxdev/hopbox/internal/ui"
)

// Step defines one unit of work in a step runner.
type Step struct {
	Title string
	Run   func(ctx context.Context, sub func(string)) error
}

// stepDoneMsg is sent when a step's Run function completes.
type stepDoneMsg struct {
	index int
	err   error
}

// subStepMsg is sent by a step's sub callback to update the spinner text.
// The previous message is printed with ✔ and the spinner shows the new one.
type subStepMsg struct {
	msg string
}

type stepRunner struct {
	ctx     context.Context
	cancel  context.CancelFunc
	steps   []Step
	current int
	spinner spinner.Model
	subMsg  string // current message shown next to spinner
	err     error
	program *tea.Program
}

func (m *stepRunner) Init() tea.Cmd {
	m.subMsg = m.steps[0].Title
	return tea.Batch(m.spinner.Tick, m.runStep(0))
}

func (m *stepRunner) runStep(idx int) tea.Cmd {
	step := m.steps[idx]
	return func() tea.Msg {
		sub := func(msg string) {
			m.program.Send(subStepMsg{msg: msg})
		}
		err := step.Run(m.ctx, sub)
		return stepDoneMsg{index: idx, err: err}
	}
}

func (m *stepRunner) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			m.err = context.Canceled
			return m, tea.Sequence(
				tea.Println("  "+ui.StepFail(m.subMsg)),
				tea.Quit,
			)
		}

	case subStepMsg:
		prev := m.subMsg
		m.subMsg = msg.msg
		if prev != "" {
			return m, tea.Println("  " + ui.StepOK(prev))
		}
		return m, nil

	case stepDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Sequence(
				tea.Println("  "+ui.StepFail(m.subMsg)),
				tea.Quit,
			)
		}
		printCmd := tea.Println("  " + ui.StepOK(m.subMsg))
		m.current++
		if m.current >= len(m.steps) {
			return m, tea.Sequence(printCmd, tea.Quit)
		}
		m.subMsg = m.steps[m.current].Title
		return m, tea.Batch(printCmd, m.runStep(m.current))

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *stepRunner) View() string {
	if m.current >= len(m.steps) || m.err != nil {
		return ""
	}
	return "  " + m.spinner.View() + " " + m.subMsg
}

// RunSteps executes steps sequentially with animated spinner progress.
// Each step shows a braille-dot spinner that resolves to ✔ on success
// or ✘ on failure. Sub-step messages update the spinner text in place.
// Falls back to plain output if stdout is not a TTY.
func RunSteps(ctx context.Context, steps []Step) error {
	if len(steps) == 0 {
		return nil
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runStepsPlain(ctx, steps)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ui.Yellow)

	m := &stepRunner{
		ctx:     ctx,
		cancel:  cancel,
		steps:   steps,
		spinner: s,
	}
	p := tea.NewProgram(m)
	m.program = p

	result, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	if r, ok := result.(*stepRunner); ok && r.err != nil {
		return r.err
	}
	return nil
}

// runStepsPlain runs steps without animation (non-TTY fallback).
func runStepsPlain(ctx context.Context, steps []Step) error {
	for _, step := range steps {
		lastMsg := step.Title
		sub := func(msg string) {
			fmt.Println("  " + ui.StepOK(lastMsg))
			lastMsg = msg
		}
		err := step.Run(ctx, sub)
		if err != nil {
			fmt.Println("  " + ui.StepFail(lastMsg))
			return err
		}
		fmt.Println("  " + ui.StepOK(lastMsg))
	}
	return nil
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/tui/...`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add internal/tui/step.go
git commit -m "feat: add bubbletea step runner model"
```

---

### Task 3: Test StepRunner

**Files:**
- Create: `internal/tui/step_test.go`

**Step 1: Write tests for model state transitions and plain fallback**

```go
package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestStepRunnerInit(t *testing.T) {
	steps := []Step{
		{Title: "first step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
		{Title: "second step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		spinner: spinner.New(),
	}
	m.Init()
	if m.subMsg != "first step" {
		t.Errorf("Init: subMsg = %q, want %q", m.subMsg, "first step")
	}
	if m.current != 0 {
		t.Errorf("Init: current = %d, want 0", m.current)
	}
}

func TestStepRunnerSubStep(t *testing.T) {
	steps := []Step{
		{Title: "main step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "main step",
		spinner: spinner.New(),
	}
	model, _ := m.Update(subStepMsg{msg: "doing work"})
	r := model.(*stepRunner)
	if r.subMsg != "doing work" {
		t.Errorf("subMsg = %q, want %q", r.subMsg, "doing work")
	}
}

func TestStepRunnerStepDone(t *testing.T) {
	steps := []Step{
		{Title: "step 1", Run: func(ctx context.Context, sub func(string)) error { return nil }},
		{Title: "step 2", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "step 1",
		spinner: spinner.New(),
	}
	model, _ := m.Update(stepDoneMsg{index: 0, err: nil})
	r := model.(*stepRunner)
	if r.current != 1 {
		t.Errorf("current = %d, want 1", r.current)
	}
	if r.subMsg != "step 2" {
		t.Errorf("subMsg = %q, want %q", r.subMsg, "step 2")
	}
}

func TestStepRunnerLastStepDone(t *testing.T) {
	steps := []Step{
		{Title: "only step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "only step",
		spinner: spinner.New(),
	}
	model, _ := m.Update(stepDoneMsg{index: 0, err: nil})
	r := model.(*stepRunner)
	if r.current != 1 {
		t.Errorf("current = %d, want 1", r.current)
	}
	if r.err != nil {
		t.Errorf("err = %v, want nil", r.err)
	}
}

func TestStepRunnerStepError(t *testing.T) {
	steps := []Step{
		{Title: "failing step", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "failing step",
		spinner: spinner.New(),
	}
	testErr := errors.New("something broke")
	model, _ := m.Update(stepDoneMsg{index: 0, err: testErr})
	r := model.(*stepRunner)
	if r.err != testErr {
		t.Errorf("err = %v, want %v", r.err, testErr)
	}
}

func TestStepRunnerViewShowsSpinner(t *testing.T) {
	steps := []Step{
		{Title: "running", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		subMsg:  "running",
		spinner: spinner.New(),
	}
	view := m.View()
	if !strings.Contains(view, "running") {
		t.Errorf("View = %q, want to contain %q", view, "running")
	}
}

func TestStepRunnerViewEmptyWhenDone(t *testing.T) {
	steps := []Step{
		{Title: "done", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	m := &stepRunner{
		ctx:     context.Background(),
		cancel:  func() {},
		steps:   steps,
		current: 1, // past the last step
		spinner: spinner.New(),
	}
	if view := m.View(); view != "" {
		t.Errorf("View = %q, want empty", view)
	}
}

func TestRunStepsPlain(t *testing.T) {
	var order []string
	steps := []Step{
		{Title: "step A", Run: func(ctx context.Context, sub func(string)) error {
			order = append(order, "A")
			return nil
		}},
		{Title: "step B", Run: func(ctx context.Context, sub func(string)) error {
			order = append(order, "B")
			return nil
		}},
	}
	err := runStepsPlain(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Errorf("order = %v, want [A B]", order)
	}
}

func TestRunStepsPlainError(t *testing.T) {
	testErr := errors.New("fail")
	steps := []Step{
		{Title: "ok", Run: func(ctx context.Context, sub func(string)) error { return nil }},
		{Title: "bad", Run: func(ctx context.Context, sub func(string)) error { return testErr }},
		{Title: "skip", Run: func(ctx context.Context, sub func(string)) error { return nil }},
	}
	err := runStepsPlain(context.Background(), steps)
	if !errors.Is(err, testErr) {
		t.Errorf("err = %v, want %v", err, testErr)
	}
}

func TestRunStepsPlainSubSteps(t *testing.T) {
	steps := []Step{
		{Title: "main", Run: func(ctx context.Context, sub func(string)) error {
			sub("sub-a")
			sub("sub-b")
			return nil
		}},
	}
	err := runStepsPlain(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Run the tests**

Run: `go test ./internal/tui/... -v`
Expected: All tests PASS

**Step 3: Commit**

```bash
git add internal/tui/step_test.go
git commit -m "test: add step runner model tests"
```

---

### Task 4: Refactor Bootstrap — split SSH connect from setup

**Files:**
- Modify: `internal/setup/bootstrap.go`

The current `Bootstrap` function does SSH connect (with TOFU) and everything else in one call. For the TUI, the TOFU prompt must happen before bubbletea starts. Split into two phases.

**Step 1: Extract SSH connect with TOFU into a new exported function**

Add this new function ABOVE the existing `Bootstrap` function (around line 56):

```go
// SSHConnectTOFU establishes an SSH connection to a new host using
// trust-on-first-use key verification. It prompts the user to confirm
// the server's host key fingerprint. Returns the connected client and
// the captured host key for later use.
func SSHConnectTOFU(ctx context.Context, opts Options, out io.Writer) (*ssh.Client, ssh.PublicKey, error) {
	if opts.SSHPort == 0 {
		opts.SSHPort = 22
	}
	if opts.SSHUser == "" {
		opts.SSHUser = "root"
	}

	confirmIn := opts.ConfirmReader
	if confirmIn == nil {
		confirmIn = os.Stdin
	}

	var capturedKey ssh.PublicKey
	captureCallback := func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		capturedKey = key
		fp := ssh.FingerprintSHA256(key)
		_, _ = fmt.Fprintf(out, "The authenticity of host %q cannot be established.\n", hostname)
		_, _ = fmt.Fprintf(out, "%s key fingerprint is %s.\n", key.Type(), fp)
		_, _ = fmt.Fprintf(out, "Are you sure you want to continue connecting (yes/no)? ")
		scanner := bufio.NewScanner(confirmIn)
		if scanner.Scan() {
			if strings.ToLower(strings.TrimSpace(scanner.Text())) == "yes" {
				return nil
			}
		}
		return fmt.Errorf("host key rejected by user")
	}

	client, err := sshConnect(ctx, opts, captureCallback)
	if err != nil {
		return nil, nil, fmt.Errorf("SSH connect: %w", err)
	}
	return client, capturedKey, nil
}
```

**Step 2: Create BootstrapWithClient that takes an already-connected SSH client**

Add this function after `SSHConnectTOFU`:

```go
// BootstrapWithClient performs bootstrap using an already-connected SSH client.
// Use SSHConnectTOFU to establish the connection first.
func BootstrapWithClient(ctx context.Context, client *ssh.Client, capturedKey ssh.PublicKey, opts Options, out io.Writer) (*hostconfig.HostConfig, error) {
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if opts.OnStep != nil {
			opts.OnStep(msg)
		} else {
			_, _ = fmt.Fprintln(out, msg)
		}
	}

	logf("Installing hop-agent...")
	if err := installAgent(ctx, client, out, version.Version); err != nil {
		return nil, fmt.Errorf("install agent: %w", err)
	}

	logf("Generating server WireGuard keys...")
	if _, err := runRemote(client, "sudo hop-agent setup"); err != nil {
		return nil, fmt.Errorf("hop-agent setup (phase 1): %w", err)
	}

	pubKeyLine, err := runRemote(client, "sudo grep '^public=' /etc/hopbox/agent.key")
	if err != nil {
		return nil, fmt.Errorf("read agent public key: %w", err)
	}
	serverPubKeyB64 := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(pubKeyLine), "public="))
	if _, err := wgkey.KeyB64ToHex(serverPubKeyB64); err != nil {
		return nil, fmt.Errorf("agent key file contains invalid public key %q: %w", serverPubKeyB64, err)
	}

	logf("Generating client WireGuard keys...")
	clientKP, err := wgkey.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate client keys: %w", err)
	}

	logf("Sending client public key to agent...")
	_, err = runRemote(client, "sudo hop-agent setup --client-pubkey="+clientKP.PublicKeyBase64())
	if err != nil {
		return nil, fmt.Errorf("hop-agent setup (phase 2): %w", err)
	}

	logf("Restarting hop-agent service...")
	_, err = runRemote(client, "sudo systemctl enable hop-agent && sudo systemctl restart hop-agent")
	if err != nil {
		logf("Warning: systemctl failed (non-systemd host?): %v", err)
	}

	cfg := &hostconfig.HostConfig{
		Name:          opts.Name,
		Endpoint:      net.JoinHostPort(opts.SSHHost, strconv.Itoa(tunnel.DefaultPort)),
		PrivateKey:    clientKP.PrivateKeyBase64(),
		PeerPublicKey: serverPubKeyB64,
		TunnelIP:      tunnel.ClientIP + "/24",
		AgentIP:       tunnel.ServerIP,
		SSHUser:       opts.SSHUser,
		SSHHost:       opts.SSHHost,
		SSHPort:       opts.SSHPort,
		SSHKeyPath:    opts.SSHKeyPath,
		SSHHostKey:    MarshalHostKey(capturedKey),
	}
	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("save host config: %w", err)
	}

	logf("Host config saved. Bootstrap complete.")
	return cfg, nil
}
```

**Step 3: Update existing Bootstrap to delegate to the new functions**

Replace the current `Bootstrap` function body with:

```go
func Bootstrap(ctx context.Context, opts Options, out io.Writer) (*hostconfig.HostConfig, error) {
	client, capturedKey, err := SSHConnectTOFU(ctx, opts, out)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	return BootstrapWithClient(ctx, client, capturedKey, opts, out)
}
```

**Step 4: Run the existing bootstrap test to verify backward compatibility**

Run: `go test ./internal/setup/... -v`
Expected: All tests PASS (existing tests use `Bootstrap`, which now delegates)

**Step 5: Run full build**

Run: `go build ./...`
Expected: Compiles without errors

**Step 6: Commit**

```bash
git add internal/setup/bootstrap.go
git commit -m "refactor: split Bootstrap into SSHConnectTOFU + BootstrapWithClient"
```

---

### Task 5: Add OnStep callback to UpgradeAgent

**Files:**
- Modify: `internal/setup/upgrade.go`

**Step 1: Add onStep parameter to UpgradeAgent**

Change the function signature on line 16 from:

```go
func UpgradeAgent(ctx context.Context, cfg *hostconfig.HostConfig, out io.Writer, clientVersion string) error {
```

to:

```go
func UpgradeAgent(ctx context.Context, cfg *hostconfig.HostConfig, out io.Writer, clientVersion string, onStep func(string)) error {
```

**Step 2: Update the logf function on line 17-19 to use onStep**

Replace:

```go
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, format+"\n", args...)
	}
```

with:

```go
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if onStep != nil {
			onStep(msg)
		} else {
			_, _ = fmt.Fprintln(out, msg)
		}
	}
```

**Step 3: Fix the caller in cmd/hop/upgrade.go**

In `cmd/hop/upgrade.go`, line 243, update the call:

Replace:

```go
	if err := setup.UpgradeAgent(ctx, cfg, os.Stdout, agentVersion); err != nil {
```

with:

```go
	if err := setup.UpgradeAgent(ctx, cfg, os.Stdout, agentVersion, nil); err != nil {
```

Passing `nil` preserves the current behavior (falls back to `fmt.Fprintln`).

**Step 4: Build to verify**

Run: `go build ./...`
Expected: Compiles

**Step 5: Run tests**

Run: `go test ./internal/setup/... -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/setup/upgrade.go cmd/hop/upgrade.go
git commit -m "feat: add onStep callback to UpgradeAgent"
```

---

### Task 6: Convert `hop setup` to use tui.RunSteps

**Files:**
- Modify: `cmd/hop/setup.go`

**Step 1: Rewrite SetupCmd.Run to use the split Bootstrap + TUI**

Replace the `Run` method with:

```go
func (c *SetupCmd) Run() error {
	opts := setup.Options{
		Name:       c.Name,
		SSHHost:    c.Addr,
		SSHPort:    c.Port,
		SSHUser:    c.User,
		SSHKeyPath: c.SSHKey,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// SSH connect with TOFU happens before the TUI (interactive prompt).
	client, capturedKey, err := setup.SSHConnectTOFU(ctx, opts, os.Stdout)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	// Bootstrap via TUI step runner.
	var cfg *hostconfig.HostConfig
	steps := []tui.Step{
		{Title: "Setting up " + c.Name, Run: func(ctx context.Context, sub func(string)) error {
			opts.OnStep = sub
			var err error
			cfg, err = setup.BootstrapWithClient(ctx, client, capturedKey, opts, os.Stdout)
			return err
		}},
	}
	if err := tui.RunSteps(ctx, steps); err != nil {
		return err
	}

	// Auto-set as default host if no default is configured yet.
	if gcfg, err := hostconfig.LoadGlobalConfig(); err == nil && gcfg.DefaultHost == "" {
		if err := hostconfig.SetDefaultHost(c.Name); err == nil {
			fmt.Printf("Default host set to %q.\n", c.Name)
		}
	}

	// Install privileged helper if not already present.
	if !helper.IsInstalled() {
		fmt.Println("\nHopbox needs to install a system helper for tunnel networking.")
		fmt.Print("This requires sudo. Proceed? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() && strings.ToLower(strings.TrimSpace(scanner.Text())) == "y" {
			helperBin, err := findHelperBinary()
			if err != nil {
				return fmt.Errorf("find helper binary: %w", err)
			}
			cmd := exec.Command("sudo", helperBin, "--install")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Stdin = os.Stdin
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("install helper: %w", err)
			}
			fmt.Println("Helper installed.")
		} else {
			fmt.Println("Skipped helper installation. hop up will not work without it.")
		}
	}

	return nil
}
```

**Step 2: Update imports**

Replace the imports block with:

```go
import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hopboxdev/hopbox/internal/helper"
	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tui"
)
```

Remove `"github.com/hopboxdev/hopbox/internal/ui"` — no longer used directly in this file.

**Step 3: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 4: Commit**

```bash
git add cmd/hop/setup.go
git commit -m "feat: use bubbletea step runner in hop setup"
```

---

### Task 7: Convert `hop upgrade` to use tui.RunSteps

**Files:**
- Modify: `cmd/hop/upgrade.go`

The helper upgrade stays pre-TUI (needs sudo subprocess). Client and agent upgrades use the step runner.

**Step 1: Extract upgradeClient and upgradeAgent into step-compatible functions**

Add two new methods on UpgradeCmd that accept a `sub` callback. These wrap the existing logic but report progress via `sub` instead of `fmt.Println`:

```go
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

func (c *UpgradeCmd) upgradeAgentStep(ctx context.Context, globals *CLI, targetVersion string, sub func(string)) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		sub("Agent: skipped (no host configured)")
		return nil
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config: %w", err)
	}

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
	if err := setup.UpgradeAgent(ctx, cfg, os.Stdout, agentVersion, sub); err != nil {
		return err
	}
	return nil
}
```

**Step 2: Rewrite the Run method to use RunSteps**

Replace the `Run` method with:

```go
func (c *UpgradeCmd) Run(globals *CLI) error {
	ctx := context.Background()

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
		fmt.Println(ui.StepOK("Upgrading from local builds (./dist/)"))
	}

	// Helper upgrade stays pre-TUI (needs sudo subprocess).
	if doHelper && runtime.GOOS == "darwin" {
		if err := c.upgradeHelper(ctx, targetVersion); err != nil {
			return fmt.Errorf("upgrade helper: %w", err)
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
	if doAgent {
		tv := targetVersion
		g := globals
		steps = append(steps, tui.Step{
			Title: "Upgrading agent",
			Run: func(ctx context.Context, sub func(string)) error {
				return c.upgradeAgentStep(ctx, g, tv, sub)
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
```

**Step 3: Update imports**

Add `"github.com/hopboxdev/hopbox/internal/tui"` to imports. Keep existing imports that are still used by `upgradeHelper` and helpers.

**Step 4: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 5: Commit**

```bash
git add cmd/hop/upgrade.go
git commit -m "feat: use bubbletea step runner in hop upgrade"
```

---

### Task 8: Convert `hop to` to use tui.RunSteps

**Files:**
- Modify: `cmd/hop/to.go`

The confirmation prompt stays pre-TUI. The four migration steps use the step runner. The step runner produces the ✔/✘ output; group headers (Step 1/4, etc.) are printed before each step via the `sub` callback.

**Step 1: Rewrite ToCmd.Run**

Replace the `Run` method with:

```go
func (c *ToCmd) Run(globals *CLI) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	sourceHost, err := resolveHost(globals)
	if err != nil {
		return fmt.Errorf("source host: %w", err)
	}
	if c.Target == sourceHost {
		return fmt.Errorf("target host must differ from source host")
	}

	// Confirmation prompt (before TUI).
	fmt.Printf("Migrate workspace from %s → %s (%s)?\n", sourceHost, c.Target, c.Addr)
	fmt.Println("  1. Create snapshot on", sourceHost)
	fmt.Println("  2. Bootstrap", c.Target, "via SSH")
	fmt.Println("  3. Restore snapshot on", c.Target)
	fmt.Println("  4. Set", c.Target, "as default host")
	fmt.Print("\nProceed? [y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() || strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
		fmt.Println("Aborted.")
		return nil
	}
	fmt.Println()

	// SSH connect with TOFU for bootstrap target (before TUI).
	bootstrapOpts := setup.Options{
		Name:       c.Target,
		SSHHost:    c.Addr,
		SSHPort:    c.Port,
		SSHUser:    c.User,
		SSHKeyPath: c.SSHKey,
	}
	sshClient, capturedKey, err := setup.SSHConnectTOFU(ctx, bootstrapOpts, os.Stdout)
	if err != nil {
		return fmt.Errorf("SSH connect to %s: %w", c.Target, err)
	}
	defer func() { _ = sshClient.Close() }()

	// Shared state across steps.
	var snapID string
	var targetCfg *hostconfig.HostConfig

	steps := []tui.Step{
		// Step 1: Snapshot source.
		{Title: fmt.Sprintf("Creating snapshot on %s", sourceHost), Run: func(ctx context.Context, sub func(string)) error {
			snapResult, err := rpcclient.Call(sourceHost, "snap.create", nil)
			if err != nil {
				return fmt.Errorf("create snapshot on %s: %w", sourceHost, err)
			}
			var snap struct {
				SnapshotID string `json:"snapshot_id"`
			}
			if err := json.Unmarshal(snapResult, &snap); err != nil || snap.SnapshotID == "" {
				return fmt.Errorf("could not parse snapshot ID from response: %s", string(snapResult))
			}
			snapID = snap.SnapshotID
			sub(fmt.Sprintf("Snapshot %s created", snapID))
			return nil
		}},

		// Step 2: Bootstrap target.
		{Title: fmt.Sprintf("Bootstrapping %s", c.Target), Run: func(ctx context.Context, sub func(string)) error {
			bootstrapOpts.OnStep = sub
			var err error
			targetCfg, err = setup.BootstrapWithClient(ctx, sshClient, capturedKey, bootstrapOpts, os.Stdout)
			if err != nil {
				return fmt.Errorf("bootstrap %s: %w", c.Target, err)
			}
			return nil
		}},

		// Step 3: Restore via temporary WireGuard tunnel.
		{Title: fmt.Sprintf("Restoring snapshot on %s", c.Target), Run: func(ctx context.Context, sub func(string)) error {
			sub(fmt.Sprintf("Connecting to %s", c.Target))
			tunCfg, err := targetCfg.ToTunnelConfig()
			if err != nil {
				return fmt.Errorf("build tunnel config: %w", err)
			}
			tun := tunnel.NewClientTunnel(tunCfg)

			tunCtx, tunCancel := context.WithTimeout(ctx, 5*time.Minute)
			defer tunCancel()
			tunErr := make(chan error, 1)
			go func() { tunErr <- tun.Start(tunCtx) }()

			select {
			case <-tun.Ready():
			case err := <-tunErr:
				return fmt.Errorf("tunnel failed to start: %w", err)
			case <-tunCtx.Done():
				return fmt.Errorf("tunnel start timed out")
			}

			agentClient := &http.Client{
				Timeout:   agentClientTimeout,
				Transport: &http.Transport{DialContext: tun.DialContext},
			}
			agentURL := fmt.Sprintf("http://%s:%d/health", targetCfg.AgentIP, tunnel.AgentAPIPort)
			if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
				return fmt.Errorf("target agent unreachable after bootstrap: %w", err)
			}
			sub(fmt.Sprintf("Connected to %s", c.Target))

			sub(fmt.Sprintf("Restoring snapshot %s", snapID))
			if _, err := rpcclient.CallWithClient(agentClient, targetCfg.AgentIP, "snap.restore", map[string]string{"id": snapID}); err != nil {
				fmt.Fprintf(os.Stderr, "\nRestore failed. To retry manually:\n")
				fmt.Fprintf(os.Stderr, "  hop snap restore %s --host %s\n", snapID, c.Target)
				return fmt.Errorf("restore on %s: %w", c.Target, err)
			}
			return nil
		}},

		// Step 4: Switch default host.
		{Title: fmt.Sprintf("Setting default host to %q", c.Target), Run: func(ctx context.Context, sub func(string)) error {
			if err := hostconfig.SetDefaultHost(c.Target); err != nil {
				return fmt.Errorf("set default host: %w", err)
			}
			return nil
		}},
	}

	if err := tui.RunSteps(ctx, steps); err != nil {
		return err
	}

	fmt.Println("\n" + ui.StepOK(fmt.Sprintf("Migration complete. Default host set to %q", c.Target)))
	fmt.Printf("Run 'hop up' to connect to %s.\n", c.Target)
	return nil
}
```

**Step 2: Update imports**

```go
import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/rpcclient"
	"github.com/hopboxdev/hopbox/internal/setup"
	"github.com/hopboxdev/hopbox/internal/tui"
	"github.com/hopboxdev/hopbox/internal/tunnel"
	"github.com/hopboxdev/hopbox/internal/ui"
)
```

**Step 3: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 4: Commit**

```bash
git add cmd/hop/to.go
git commit -m "feat: use bubbletea step runner in hop to"
```

---

### Task 9: Convert `hop up` to use tui.RunSteps (partial)

**Files:**
- Modify: `cmd/hop/up.go`

The tunnel bring-up (TUN creation, configuration, host entry) stays imperative because it manages OS resources with deferred cleanup. The application-level steps (probe agent, sync manifest, install packages) use the step runner. Monitoring stays as plain output.

**Step 1: Rewrite the application setup portion to use RunSteps**

Replace the section from the agent probe through to "Tunnel up" (approximately lines 147-225) with a RunSteps call. Keep everything before (tunnel setup) and after (monitoring) as-is.

The new `Run` method structure:

```go
func (c *UpCmd) Run(globals *CLI) error {
	hostName, err := resolveHost(globals)
	if err != nil {
		return err
	}

	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return fmt.Errorf("load host config %q: %w", hostName, err)
	}

	if existing, _ := tunnel.LoadState(hostName); existing != nil {
		return fmt.Errorf("tunnel to %q is already running (PID %d); press Ctrl-C in that session to stop it first", hostName, existing.PID)
	}

	tunCfg, err := cfg.ToTunnelConfig()
	if err != nil {
		return fmt.Errorf("convert tunnel config: %w", err)
	}

	helperClient := helper.NewClient()
	if !helperClient.IsReachable() {
		return fmt.Errorf("hopbox helper is not running; install with 'sudo hop-helper --install' or re-run 'hop setup'")
	}

	tunFile, ifName, err := helperClient.CreateTUN(tunCfg.MTU)
	if err != nil {
		return fmt.Errorf("create TUN device: %w", err)
	}

	tun := tunnel.NewKernelTunnel(tunCfg, tunFile, ifName)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	fmt.Println(ui.StepRun(fmt.Sprintf("Bringing up tunnel to %s (%s)", cfg.Name, cfg.Endpoint)))

	tunnelErr := make(chan error, 1)
	go func() {
		tunnelErr <- tun.Start(ctx)
	}()

	select {
	case <-tun.Ready():
	case err := <-tunnelErr:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	localIP := strings.TrimSuffix(tunCfg.LocalIP, "/24")
	peerIP := strings.TrimSuffix(tunCfg.PeerIP, "/32")
	if err := helperClient.ConfigureTUN(tun.InterfaceName(), localIP, peerIP); err != nil {
		tun.Stop()
		return fmt.Errorf("configure TUN: %w", err)
	}
	defer func() { _ = helperClient.CleanupTUN(tun.InterfaceName()) }()

	hostname := cfg.Name + ".hop"
	if err := helperClient.AddHost(peerIP, hostname); err != nil {
		tun.Stop()
		return fmt.Errorf("add host entry: %w", err)
	}
	defer func() { _ = helperClient.RemoveHost(hostname) }()

	fmt.Println(ui.StepOK(fmt.Sprintf("Interface %s up, %s → %s", tun.InterfaceName(), localIP, hostname)))

	// Load workspace manifest.
	wsPath := c.Workspace
	if wsPath == "" {
		wsPath = "hopbox.yaml"
	}
	var ws *manifest.Workspace
	if _, err := os.Stat(wsPath); err == nil {
		ws, err = manifest.Parse(wsPath)
		if err != nil {
			return fmt.Errorf("parse manifest: %w", err)
		}
	}

	// Application setup steps via TUI runner.
	agentClient := &http.Client{Timeout: agentClientTimeout}
	agentURL := fmt.Sprintf("http://%s:%d/health", hostname, tunnel.AgentAPIPort)

	var steps []tui.Step
	steps = append(steps, tui.Step{
		Title: fmt.Sprintf("Probing agent at %s", agentURL),
		Run: func(ctx context.Context, sub func(string)) error {
			if err := probeAgent(ctx, agentURL, agentProbeTimeout, agentClient); err != nil {
				return fmt.Errorf("agent probe failed: %w", err)
			}
			// Check agent version.
			if resp, err := agentClient.Get(agentURL); err == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				var health map[string]any
				if json.Unmarshal(body, &health) == nil {
					if agentVer, ok := health["version"].(string); ok && agentVer != version.Version {
						_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf(
							"agent version %q differs from client %q — run 'hop upgrade' to sync",
							agentVer, version.Version)))
					}
				}
			}
			sub("Agent is up")
			return nil
		},
	})

	if ws != nil {
		steps = append(steps, tui.Step{
			Title: fmt.Sprintf("Loading workspace: %s", ws.Name),
			Run: func(ctx context.Context, sub func(string)) error {
				rawManifest, err := os.ReadFile(wsPath)
				if err != nil {
					return nil // non-fatal
				}
				if _, err := rpcclient.Call(hostName, "workspace.sync", map[string]string{"yaml": string(rawManifest)}); err != nil {
					_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("manifest sync failed: %v", err)))
				} else {
					sub("Manifest synced")
				}
				return nil
			},
		})
	}

	if ws != nil && len(ws.Packages) > 0 {
		steps = append(steps, tui.Step{
			Title: fmt.Sprintf("Installing %d package(s)", len(ws.Packages)),
			Run: func(ctx context.Context, sub func(string)) error {
				pkgs := make([]map[string]string, 0, len(ws.Packages))
				for _, p := range ws.Packages {
					pkgs = append(pkgs, map[string]string{
						"name":    p.Name,
						"backend": p.Backend,
						"version": p.Version,
					})
				}
				if _, err := rpcclient.Call(hostName, "packages.install", map[string]any{"packages": pkgs}); err != nil {
					_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("package installation failed: %v", err)))
				} else {
					sub("Packages installed")
				}
				return nil
			},
		})
	}

	if len(steps) > 0 {
		if err := tui.RunSteps(ctx, steps); err != nil {
			return err
		}
	}

	// Start bridges (after RunSteps, before monitoring).
	var bridges []bridge.Bridge
	if ws != nil {
		for _, b := range ws.Bridges {
			switch b.Type {
			case "clipboard":
				br := bridge.NewClipboardBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("clipboard bridge error: %v", err)))
					}
				}(br)
			case "cdp":
				br := bridge.NewCDPBridge("127.0.0.1")
				bridges = append(bridges, br)
				go func(br bridge.Bridge) {
					if err := br.Start(ctx); err != nil && ctx.Err() == nil {
						_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("CDP bridge error: %v", err)))
					}
				}(br)
			}
		}
	}

	// Write tunnel state.
	state := &tunnel.TunnelState{
		PID:         os.Getpid(),
		Host:        hostName,
		Hostname:    hostname,
		Interface:   tun.InterfaceName(),
		StartedAt:   time.Now(),
		Connected:   true,
		LastHealthy: time.Now(),
	}
	if err := tunnel.WriteState(state); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("write tunnel state: %v", err)))
	}
	defer func() { _ = tunnel.RemoveState(hostName) }()

	if globals.Verbose {
		for _, br := range bridges {
			fmt.Println(br.Status())
		}
	}

	fmt.Println(ui.StepOK("Tunnel up. Press Ctrl-C to stop"))

	// Monitoring phase (plain output).
	monitor := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: agentURL,
		Client:    agentClient,
		OnStateChange: func(evt tunnel.ConnEvent) {
			switch evt.State {
			case tunnel.ConnStateDisconnected:
				fmt.Printf("\n[%s] Agent unreachable — waiting for reconnection...\n",
					evt.At.Format("15:04:05"))
				state.Connected = false
			case tunnel.ConnStateConnected:
				fmt.Printf("[%s] Agent reconnected (was down for %s)\n",
					evt.At.Format("15:04:05"), evt.Duration.Round(time.Second))
				state.Connected = true
				state.LastHealthy = evt.At
			}
			if err := tunnel.WriteState(state); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("update tunnel state: %v", err)))
			}
		},
		OnHealthy: func(t time.Time) {
			state.LastHealthy = t
			if err := tunnel.WriteState(state); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, ui.Warn(fmt.Sprintf("update tunnel state: %v", err)))
			}
		},
	})
	go monitor.Run(ctx)

	select {
	case <-ctx.Done():
		fmt.Println("\n" + ui.StepRun("Shutting down..."))
	case err := <-tunnelErr:
		if err != nil {
			return fmt.Errorf("tunnel error: %w", err)
		}
	}

	return nil
}
```

**Step 2: Update imports**

Add `"github.com/hopboxdev/hopbox/internal/tui"` to the imports. Keep all existing imports.

**Step 3: Build to verify**

Run: `go build ./cmd/hop/...`
Expected: Compiles

**Step 4: Commit**

```bash
git add cmd/hop/up.go
git commit -m "feat: use bubbletea step runner in hop up"
```

---

### Task 10: Final verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests PASS

**Step 2: Run linter**

Run: `golangci-lint run`
Expected: No new issues

**Step 3: Build all binaries**

Run: `make build`
Expected: All binaries compile

**Step 4: Verify no unused imports**

Run: `go build ./...`
Expected: No "imported and not used" errors

**Step 5: Verify the TTY fallback path works**

Run: `echo "" | go run ./cmd/hop/... version`
Expected: Prints version without TUI (stdout is piped, so not a TTY)
