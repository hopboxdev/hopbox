# ClientTunnel Ready Channel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix a data race in `ClientTunnel` where `tun.DialContext` can be called before `tun.Start` finishes assigning `t.tnet`, by adding a `Ready()` channel that closes once the tunnel netstack is initialised.

**Architecture:** Add a `ready chan struct{}` field to `ClientTunnel`. Close it inside `Start` after `t.tnet` is assigned. Expose it via `Ready() <-chan struct{}`. Update `up.go` and `to.go` to `select` on `tun.Ready()` (plus the error channel) before constructing the `agentClient`. This mirrors the `ServerTunnel.Ready()` pattern already in the codebase.

**Tech Stack:** Go, `internal/tunnel`, `cmd/hop/up.go`, `cmd/hop/to.go`

---

### Task 1: Add `Ready()` channel to `ClientTunnel`

**Files:**
- Modify: `internal/tunnel/client.go`
- Test: `internal/tunnel/client_test.go`

This task fixes the data race. The race: `Start` assigns `t.tnet` in a goroutine; `DialContext` reads `t.tnet` from the main goroutine without synchronisation. Fix: close a channel after `t.tnet = tnet`; callers select on that channel before calling `DialContext`.

**Step 1: Write the failing test**

Add this test to `internal/tunnel/client_test.go` (the file already exists; append after the last test):

```go
func TestClientTunnelReady(t *testing.T) {
	kp, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}
	peerKP, err := wgkey.Generate()
	if err != nil {
		t.Fatal(err)
	}

	cfg := tunnel.Config{
		PrivateKey:          kp.PrivateKeyHex(),
		PeerPublicKey:       peerKP.PublicKeyHex(),
		LocalIP:             "10.99.0.1/24",
		PeerIP:              "10.99.0.2/32",
		Endpoint:            "127.0.0.1:51820", // unreachable — that's fine
		ListenPort:          0,
		MTU:                 tunnel.DefaultMTU,
		PersistentKeepalive: 0,
	}
	tun := tunnel.NewClientTunnel(cfg)

	// Ready channel must not be closed before Start is called.
	select {
	case <-tun.Ready():
		t.Fatal("Ready() closed before Start was called")
	default:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = tun.Start(ctx) }()

	select {
	case <-tun.Ready():
		// success — t.tnet is now safely assigned
	case <-time.After(3 * time.Second):
		t.Fatal("Ready() was not closed within 3s of Start")
	}
}
```

**Step 2: Run the test to verify it fails**

```bash
go test ./internal/tunnel/... -run TestClientTunnelReady -v
```

Expected: FAIL — `tunnel.ClientTunnel` has no `Ready()` method yet.

**Step 3: Add `ready` field and `Ready()` method to `ClientTunnel`**

In `internal/tunnel/client.go`:

1. Add `ready chan struct{}` to the struct:

```go
type ClientTunnel struct {
	cfg      Config
	dev      *device.Device
	tnet     *netstack.Net
	stopOnce sync.Once
	ready    chan struct{}
}
```

2. Initialise it in `NewClientTunnel`:

```go
func NewClientTunnel(cfg Config) *ClientTunnel {
	return &ClientTunnel{cfg: cfg, ready: make(chan struct{})}
}
```

3. Close it in `Start` immediately after `t.tnet = tnet` (line 61), before `<-ctx.Done()`:

```go
	t.dev = dev
	t.tnet = tnet
	close(t.ready) // signal: DialContext is now safe to call

	// Wait for context cancellation
	<-ctx.Done()
	t.Stop()
	return nil
```

4. Add the `Ready()` method after `Stop()`:

```go
// Ready returns a channel that is closed once the tunnel netstack is
// initialised and DialContext is safe to call.
func (t *ClientTunnel) Ready() <-chan struct{} {
	return t.ready
}
```

**Step 4: Run the test to verify it passes**

```bash
go test ./internal/tunnel/... -run TestClientTunnelReady -v
```

Expected: PASS.

**Step 5: Run the full test suite**

```bash
go test ./...
```

Expected: all pass.

**Step 6: Run with race detector**

```bash
go test -race ./internal/tunnel/... -run TestClientTunnelReady -v
```

Expected: PASS with no race conditions reported.

**Step 7: Commit**

```bash
git add internal/tunnel/client.go internal/tunnel/client_test.go
git commit -m "fix: add Ready() channel to ClientTunnel to eliminate t.tnet data race"
```

---

### Task 2: Wait on `tun.Ready()` in `up.go`

**Files:**
- Modify: `cmd/hop/up.go`

Currently `up.go` starts the tunnel goroutine then proceeds to build the `agentClient` without waiting for the netstack to be ready. Add a `select` on `tun.Ready()` right after the goroutine start.

**Step 1: Locate the goroutine in `up.go`**

Find this block (around line 63–66):

```go
	tunnelErr := make(chan error, 1)

	go func() {
		tunnelErr <- tun.Start(ctx)
	}()
```

**Step 2: Add the `Ready()` wait immediately after the goroutine**

Insert these lines right after the closing `}()`:

```go
	// Wait for the tunnel netstack to be ready before using DialContext.
	// Without this, DialContext races against Start's assignment of t.tnet.
	select {
	case <-tun.Ready():
	case err := <-tunnelErr:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}
```

**Step 3: Build**

```bash
go build ./cmd/hop/...
```

Expected: success.

**Step 4: Run the full test suite**

```bash
go test ./...
```

Expected: all pass.

**Step 5: Commit**

```bash
git add cmd/hop/up.go
git commit -m "fix: wait on tun.Ready() before using DialContext in hop up"
```

---

### Task 3: Wait on `tun.Ready()` in `to.go`

**Files:**
- Modify: `cmd/hop/to.go`

Same fix as Task 2 but for `to.go`. Currently the goroutine discards the error; capture it so we can select on it.

**Step 1: Locate the goroutine in `to.go`**

Find this block in the Step 3/4 section:

```go
	tunCtx, tunCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer tunCancel()
	go func() { _ = tun.Start(tunCtx) }()
```

**Step 2: Replace the goroutine and add the `Ready()` wait**

Replace those three lines with:

```go
	tunCtx, tunCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer tunCancel()
	tunErr := make(chan error, 1)
	go func() { tunErr <- tun.Start(tunCtx) }()

	// Wait for the tunnel netstack to be ready before using DialContext.
	select {
	case <-tun.Ready():
	case err := <-tunErr:
		return fmt.Errorf("tunnel failed to start: %w", err)
	case <-tunCtx.Done():
		return fmt.Errorf("tunnel start timed out")
	}
```

**Step 3: Build**

```bash
go build ./cmd/hop/...
```

Expected: success.

**Step 4: Run the full test suite with race detector**

```bash
go test -race ./...
```

Expected: all pass, no races reported.

**Step 5: Lint**

```bash
golangci-lint run ./...
```

Expected: no issues. Fix any that appear before committing.

**Step 6: Commit**

```bash
git add cmd/hop/to.go
git commit -m "fix: wait on tun.Ready() before using DialContext in hop to"
```
