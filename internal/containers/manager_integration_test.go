//go:build integration

package containers

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func newDockerClientForTest(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	return cli
}

func startTestContainer(t *testing.T, cli *client.Client) string {
	t.Helper()
	ctx := context.Background()

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: "ubuntu:24.04",
			Cmd:   []string{"sleep", "infinity"},
			Tty:   false,
		},
		nil, nil, nil, "")
	if err != nil {
		t.Fatalf("container create: %v", err)
	}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		t.Fatalf("container start: %v", err)
	}
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	})
	return resp.ID
}

func TestExec_TerminatesOnContextCancel(t *testing.T) {
	cli := newDockerClientForTest(t)
	containerID := startTestContainer(t, cli)
	m := &Manager{cli: cli}

	ctx, cancel := context.WithCancel(context.Background())

	// Pipe for stdin so the exec stays attached until we close it.
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	resizeCh := make(chan [2]uint, 1)
	resizeCh <- [2]uint{80, 24}

	execDone := make(chan error, 1)
	go func() {
		execDone <- m.Exec(ctx, containerID, []string{"sleep", "600"}, nil, pr, io.Discard, resizeCh)
	}()

	// Give the exec a moment to create and attach.
	time.Sleep(500 * time.Millisecond)

	cancel()

	select {
	case err := <-execDone:
		if err != nil {
			t.Logf("Exec returned with (expected) err: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Exec did not return within 5s after ctx cancel")
	}

	// Within another 3 seconds, inspect the container's exec IDs and
	// confirm none are still Running.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		info, err := cli.ContainerInspect(context.Background(), containerID)
		if err != nil {
			t.Fatalf("container inspect: %v", err)
		}
		stillRunning := false
		for _, execID := range info.ExecIDs {
			inspect, err := cli.ContainerExecInspect(context.Background(), execID)
			if err != nil {
				continue
			}
			if inspect.Running {
				stillRunning = true
				break
			}
		}
		if !stillRunning {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("at least one exec still Running 3s after cancel")
}

func TestExecNoTTY_TerminatesOnContextCancel(t *testing.T) {
	cli := newDockerClientForTest(t)
	containerID := startTestContainer(t, cli)
	m := &Manager{cli: cli}

	ctx, cancel := context.WithCancel(context.Background())

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	var stdout, stderr bytes.Buffer
	execDone := make(chan error, 1)
	go func() {
		_, err := m.ExecNoTTY(ctx, containerID, []string{"sleep", "600"}, nil, pr, &stdout, &stderr)
		execDone <- err
	}()

	time.Sleep(500 * time.Millisecond)
	cancel()

	select {
	case err := <-execDone:
		if err != nil {
			t.Logf("ExecNoTTY returned with (expected) err: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("ExecNoTTY did not return within 5s after ctx cancel")
	}

	// Unused to keep compiler happy if buffers were unreferenced.
	_ = strings.TrimSpace(stdout.String())
}
