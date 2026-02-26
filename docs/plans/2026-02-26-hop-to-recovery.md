# `hop to` Error Recovery Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When `hop to` fails at any phase, clean up the target host config and exit with a clear phase-specific error message so the user can re-run from scratch.

**Architecture:** Add a defer-based cleanup function and phase tracking variable to the existing `ToCmd.Run()` method. On failure after bootstrap, delete `~/.config/hopbox/hosts/<target>.yaml`. Wrap `RunPhases` error with phase-specific context. No new files, no new abstractions.

**Tech Stack:** Go, existing `hostconfig.Delete()`, existing TUI runner.

---

### Task 1: Add client-side cleanup on failure

**Files:**
- Modify: `cmd/hop/to.go:32-164`

**Step 1: Add cleanup tracking and defer**

After the `var targetCfg` declaration (line 74), add a `targetConfigSaved` bool. Add a deferred cleanup function after the phase declarations that deletes the target host config if the migration didn't complete successfully.

Replace lines 72-74:

```go
	// Shared state across steps.
	var snapID string
	var targetCfg *hostconfig.HostConfig
```

With:

```go
	// Shared state across steps.
	var snapID string
	var targetCfg *hostconfig.HostConfig
	var targetConfigSaved bool
	var migrationDone bool

	defer func() {
		if !migrationDone && targetConfigSaved {
			if err := hostconfig.Delete(c.Target); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to clean up host config for %q: %v\n", c.Target, err)
			}
		}
	}()
```

**Step 2: Set `targetConfigSaved` after bootstrap**

In the bootstrap step handler (line 95-104), set `targetConfigSaved = true` after the successful bootstrap call. Replace lines 97-103:

```go
				var err error
				targetCfg, err = setup.BootstrapWithClient(ctx, sshClient, capturedKey, bootstrapOpts, os.Stdout)
				if err != nil {
					return fmt.Errorf("bootstrap %s: %w", c.Target, err)
				}
				send(tui.StepEvent{Message: fmt.Sprintf("%s bootstrapped", c.Target)})
				return nil
```

With:

```go
				var err error
				targetCfg, err = setup.BootstrapWithClient(ctx, sshClient, capturedKey, bootstrapOpts, os.Stdout)
				if err != nil {
					return fmt.Errorf("bootstrap %s: %w", c.Target, err)
				}
				targetConfigSaved = true
				send(tui.StepEvent{Message: fmt.Sprintf("%s bootstrapped", c.Target)})
				return nil
```

**Step 3: Set `migrationDone` on success**

Replace lines 157-163:

```go
	if err := tui.RunPhases(ctx, "hop to "+c.Target, phases); err != nil {
		return err
	}

	fmt.Println("\n" + ui.StepOK(fmt.Sprintf("Migration complete. Default host set to %q", c.Target)))
	fmt.Printf("Run 'hop up' to connect to %s.\n", c.Target)
	return nil
```

With:

```go
	if err := tui.RunPhases(ctx, "hop to "+c.Target, phases); err != nil {
		return err
	}

	migrationDone = true
	fmt.Println("\n" + ui.StepOK(fmt.Sprintf("Migration complete. Default host set to %q", c.Target)))
	fmt.Printf("Run 'hop up' to connect to %s.\n", c.Target)
	return nil
```

**Step 4: Verify it compiles**

Run: `go build ./cmd/hop/...`
Expected: no errors

**Step 5: Commit**

```bash
git add cmd/hop/to.go
git commit -m "feat: clean up target host config on hop to failure"
```

---

### Task 2: Add phase-specific error messages

**Files:**
- Modify: `cmd/hop/to.go`

**Step 1: Add recovery hints to each phase's error messages**

Update the snapshot error (line 81) from:

```go
					return fmt.Errorf("create snapshot on %s: %w", sourceHost, err)
```

To:

```go
					return fmt.Errorf("create snapshot on %s: %w\n\nNo changes were made. Re-run: hop to %s -a %s -u %s", sourceHost, err, c.Target, c.Addr, c.User)
```

Update the bootstrap error (line 100) from:

```go
					return fmt.Errorf("bootstrap %s: %w", c.Target, err)
```

To:

```go
					return fmt.Errorf("bootstrap %s: %w\n\nPartial agent install may exist on %s and will be overwritten on retry.\nRe-run: hop to %s -a %s -u %s", c.Target, err, c.Target, c.Target, c.Addr, c.User)
```

Update the restore section (lines 139-142). Replace:

```go
				if _, err := rpcclient.CallWithClient(agentClient, targetCfg.AgentIP, "snap.restore", map[string]string{"id": snapID}); err != nil {
					fmt.Fprintf(os.Stderr, "\nRestore failed. To retry manually:\n")
					fmt.Fprintf(os.Stderr, "  hop snap restore %s --host %s\n", snapID, c.Target)
					return fmt.Errorf("restore on %s: %w", c.Target, err)
				}
```

With:

```go
				if _, err := rpcclient.CallWithClient(agentClient, targetCfg.AgentIP, "snap.restore", map[string]string{"id": snapID}); err != nil {
					return fmt.Errorf("restore on %s: %w\n\nSnapshot %s exists and %s is bootstrapped.\nRe-run: hop to %s -a %s -u %s", c.Target, err, snapID, c.Target, c.Target, c.Addr, c.User)
				}
```

**Step 2: Verify it compiles**

Run: `go build ./cmd/hop/...`
Expected: no errors

**Step 3: Commit**

```bash
git add cmd/hop/to.go
git commit -m "feat: add phase-specific error messages to hop to"
```

---

### Task 3: Update ROADMAP.md

**Files:**
- Modify: `ROADMAP.md`

**Step 1: Mark `hop to` error recovery as complete**

Change: `- [ ] \`hop to\` error recovery — rollback or resume on mid-migration failure`
To: `- [x] \`hop to\` error recovery — client-side cleanup and idempotent retry on failure`

**Step 2: Commit**

```bash
git add ROADMAP.md
git commit -m "docs: mark hop to error recovery as complete in roadmap"
```

---

### Summary of files touched

| File | Action |
|------|--------|
| `cmd/hop/to.go` | Modify — add cleanup defer, phase tracking, error messages |
| `ROADMAP.md` | Modify — check off error recovery item |
