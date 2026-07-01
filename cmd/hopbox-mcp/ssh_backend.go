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

	mu      sync.Mutex
	order   []string
	boxes   map[string]*mcp.Box
	subs    map[int]func()
	nextSub int
	seq     int
}

func newSSHBackend(host string) *sshBackend {
	dir, err := os.MkdirTemp("", "hopbox-mcp")
	if err != nil {
		log.Fatal(err)
	}
	key := dir + "/id"
	if out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-q", "-f", key).CombinedOutput(); err != nil {
		log.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	return &sshBackend{host: host, keyFile: key, dir: dir, boxes: map[string]*mcp.Box{}, subs: map[int]func(){}}
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
	id, _ := b.newBox("")
	b.set(id, "done", "spawned")
	return id, nil
}

func (b *sshBackend) Delegate(_ context.Context, taskCmd string) (string, error) {
	id, name := b.newBox(taskCmd)
	go func() {
		out, err := b.run(name, taskCmd)
		if err != nil {
			b.set(id, "failed", strings.TrimSpace(out+" "+err.Error()))
			return
		}
		b.set(id, "done", strings.TrimSpace(out))
	}()
	return id, nil
}

func (b *sshBackend) newBox(taskCmd string) (id, name string) {
	b.mu.Lock()
	b.seq++
	id = fmt.Sprintf("b%03d", b.seq)
	name = "mcp-" + id
	b.boxes[id] = &mcp.Box{ID: id, Name: name, Task: taskCmd, State: "working", Updated: time.Now().Unix()}
	b.order = append(b.order, id)
	b.mu.Unlock()
	b.notify()
	return
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
