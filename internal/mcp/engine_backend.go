package mcp

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/hopboxdev/hopbox/internal/agentproto"
	"github.com/hopboxdev/hopbox/internal/core/box"
)

// execHub is the slice of the agent hub the backend needs (avoids importing it).
type execHub interface {
	Connected(workspaceID string) bool
	OpenExec(workspaceID string, cmd []string) (io.ReadWriteCloser, error)
}

// EngineBackend backs the protocol with a live box.Engine + agent hub, so
// hopbox://fleet is the real reconciler state and box.delegate spawns real boxes.
type EngineBackend struct {
	engine *box.Engine
	hub    execHub
	owner  string

	mu      sync.Mutex
	tasks   map[string]*task // box id -> delegated task overlay (state + captured output)
	subs    map[int]func()
	nextSub int
}

type task struct{ task, state, result string }

// NewEngineBackend wires the backend to a daemon's engine + hub.
func NewEngineBackend(engine *box.Engine, hub execHub) *EngineBackend {
	b := &EngineBackend{engine: engine, hub: hub, owner: "mcp", tasks: map[string]*task{}, subs: map[int]func(){}}
	go b.watch()
	return b
}

func (b *EngineBackend) Fleet(ctx context.Context) []Box {
	boxes, _ := b.engine.List(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Box, 0, len(boxes))
	for _, bx := range boxes {
		v := Box{ID: bx.ID, Name: bx.Name, Image: bx.ImageRef, State: string(bx.Phase), IP: bx.IP, Updated: bx.UpdatedAt.Unix()}
		if t := b.tasks[bx.ID]; t != nil {
			v.Task, v.Result = t.task, t.result
			if t.state != "" {
				v.State = t.state
			}
		}
		out = append(out, v)
	}
	return out
}

func (b *EngineBackend) Spawn(ctx context.Context, name string) (string, error) {
	if name == "" {
		name = fmt.Sprintf("mcp-%d", time.Now().UnixNano()%1e6)
	}
	bx, release, err := b.engine.Attach(ctx, b.owner, name)
	if err != nil {
		return "", err
	}
	release() // detach; an ephemeral box then reaps after its grace window
	b.notify()
	return bx.ID, nil
}

func (b *EngineBackend) Delegate(ctx context.Context, taskCmd string) (string, error) {
	name := fmt.Sprintf("mcp-%d", time.Now().UnixNano()%1e6)
	bx, release, err := b.engine.Attach(ctx, b.owner, name)
	if err != nil {
		return "", err
	}
	b.setTask(bx.ID, taskCmd, "working", "")
	go b.runTask(bx.ID, taskCmd, release)
	return bx.ID, nil
}

func (b *EngineBackend) runTask(id, taskCmd string, release func()) {
	defer release() // hold the session until the task finishes, then let it reap
	deadline := time.Now().Add(90 * time.Second)
	for !b.hub.Connected(id) {
		if ph, _, ok := b.engine.State(context.Background(), id); ok && ph == box.PhaseFailed {
			b.setTask(id, taskCmd, "failed", "box failed to start")
			return
		}
		if time.Now().After(deadline) {
			b.setTask(id, taskCmd, "failed", "box not ready in time")
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	stream, err := b.hub.OpenExec(id, []string{"/bin/sh", "-c", taskCmd})
	if err != nil {
		b.setTask(id, taskCmd, "failed", err.Error())
		return
	}
	defer stream.Close()
	var out []byte
	var code int32
	for {
		typ, data, c, err := agentproto.ReadExecFrame(stream)
		if err != nil {
			break
		}
		switch typ {
		case agentproto.ExecStdout, agentproto.ExecStderr:
			out = append(out, data...)
		case agentproto.ExecExit:
			code = c
		}
		if typ == agentproto.ExecExit {
			break
		}
	}
	st := "done"
	if code != 0 {
		st = "failed"
	}
	b.setTask(id, taskCmd, st, strings.TrimSpace(string(out)))
}

func (b *EngineBackend) setTask(id, taskCmd, state, result string) {
	b.mu.Lock()
	b.tasks[id] = &task{task: taskCmd, state: state, result: result}
	b.mu.Unlock()
	b.notify()
}

func (b *EngineBackend) OnChange(fn func()) (cancel func()) {
	b.mu.Lock()
	id := b.nextSub
	b.nextSub++
	b.subs[id] = fn
	b.mu.Unlock()
	return func() { b.mu.Lock(); delete(b.subs, id); b.mu.Unlock() }
}

func (b *EngineBackend) notify() {
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

// watch pushes a change whenever the fleet's shape changes, so boxes spawned by
// other actors (the front door) surface too — server-side, so clients never poll.
func (b *EngineBackend) watch() {
	var last string
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		cur := ""
		for _, bx := range b.Fleet(context.Background()) {
			cur += bx.ID + bx.State + ";"
		}
		if cur != last {
			last = cur
			b.notify()
		}
	}
}
