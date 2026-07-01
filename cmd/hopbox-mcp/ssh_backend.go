package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/hopboxdev/hopbox/internal/mcp"
)

// sshBackend is a standalone mcp.Backend: it tracks a fleet in memory and runs
// delegated tasks in real boxes over `ssh <name>@host`. Used for the standalone
// server + the demo (the daemon uses the engine backend instead).
type sshBackend struct {
	host, keyFile, dir string

	mu       sync.Mutex
	order    []string
	boxes    map[string]*mcp.Box
	keys     map[string]string // fleet.apply key -> box id
	surfaces *mcp.Surfaces
	subs     map[int]func()
	nextSub  int
	seq      int
}

func (b *sshBackend) RenderSurface(name, html string) string       { return b.surfaces.Render(name, html) }
func (b *sshBackend) SurfaceEvents(name string) []mcp.SurfaceEvent { return b.surfaces.Events(name) }

func newSSHBackend(host string) *sshBackend {
	dir, err := os.MkdirTemp("", "hopbox-mcp")
	if err != nil {
		log.Fatal(err)
	}
	key := dir + "/id"
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-q", "-f", key).CombinedOutput(); err != nil {
		log.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	b := &sshBackend{host: host, keyFile: key, dir: dir,
		boxes: map[string]*mcp.Box{}, keys: map[string]string{}, subs: map[int]func(){}}
	b.surfaces = mcp.NewSurfaces("http://localhost", b.notify)
	return b
}

func (b *sshBackend) cleanup() { _ = os.RemoveAll(b.dir) }

func (b *sshBackend) Fleet(context.Context) []mcp.Box {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]mcp.Box, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, *b.boxes[id])
	}
	return out
}

func (b *sshBackend) Spawn(_ context.Context, _ string) (string, error) {
	id, _ := b.newBox("", "")
	b.set(id, "done", "spawned")
	return id, nil
}

func (b *sshBackend) Delegate(_ context.Context, taskCmd string) (string, error) {
	id, name := b.newBox("", taskCmd)
	b.runAsync(id, name, taskCmd)
	return id, nil
}

func (b *sshBackend) Apply(_ context.Context, spec []mcp.SpecBox) ([]string, error) {
	b.mu.Lock()
	live := map[string]bool{}
	for _, id := range b.order {
		live[id] = true
	}
	b.mu.Unlock()
	var created []string
	for _, sb := range spec {
		if sb.Key == "" {
			continue
		}
		b.mu.Lock()
		existing, ok := b.keys[sb.Key]
		b.mu.Unlock()
		if ok && live[existing] {
			continue
		}
		id, name := b.newBox("fleet-"+sb.Key, sb.Task)
		b.runAsync(id, name, sb.Task)
		b.mu.Lock()
		b.keys[sb.Key] = id
		b.mu.Unlock()
		created = append(created, id)
	}
	return created, nil
}

func (b *sshBackend) runAsync(id, name, taskCmd string) {
	go func() {
		out, err := b.run(name, taskCmd)
		if err != nil {
			b.set(id, "failed", strings.TrimSpace(out+" "+err.Error()))
			return
		}
		b.set(id, "done", strings.TrimSpace(out))
	}()
}

func (b *sshBackend) newBox(name, taskCmd string) (id, boxName string) {
	b.mu.Lock()
	b.seq++
	id = fmt.Sprintf("b%03d", b.seq)
	if name == "" {
		name = "mcp-" + id
	}
	b.boxes[id] = &mcp.Box{ID: id, Name: name, Task: taskCmd, State: "working", Updated: time.Now().Unix()}
	b.order = append(b.order, id)
	b.mu.Unlock()
	b.notify()
	return id, name
}

func (b *sshBackend) run(name, taskCmd string) (string, error) {
	args := []string{"-i", b.keyFile,
		"-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/dev/null", "-o", "ConnectTimeout=90", "-o", "LogLevel=ERROR",
		name + "@" + b.host, taskCmd}
	out, err := exec.Command("ssh", args...).CombinedOutput()
	return string(out), err
}

func (b *sshBackend) set(id, state, result string) {
	b.mu.Lock()
	if bx := b.boxes[id]; bx != nil {
		bx.State, bx.Result, bx.Updated = state, result, time.Now().Unix()
	}
	b.mu.Unlock()
	b.notify()
}

func (b *sshBackend) OnChange(fn func()) (cancel func()) {
	b.mu.Lock()
	id := b.nextSub
	b.nextSub++
	b.subs[id] = fn
	b.mu.Unlock()
	return func() { b.mu.Lock(); delete(b.subs, id); b.mu.Unlock() }
}

func (b *sshBackend) notify() {
	b.mu.Lock()
	fns := make([]func(), 0, len(b.subs))
	for _, fn := range b.subs {
		fns = append(fns, fn)
	}
	b.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}
