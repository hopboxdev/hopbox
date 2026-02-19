package setup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"golang.org/x/crypto/ssh"
)

const systemdUnit = `[Unit]
Description=Hopbox Agent
After=network.target

[Service]
ExecStart=/usr/local/bin/hop-agent serve
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

// installAgent uploads the hop-agent binary and installs the systemd unit.
func installAgent(_ context.Context, client *ssh.Client, opts Options, out io.Writer) error {
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "  "+format+"\n", args...)
	}

	// Find the agent binary
	agentPath := opts.AgentBinaryPath
	if agentPath == "" {
		var err error
		agentPath, err = exec.LookPath("hop-agent")
		if err != nil {
			return fmt.Errorf("hop-agent binary not found: %w", err)
		}
	}

	data, err := os.ReadFile(agentPath)
	if err != nil {
		return fmt.Errorf("read agent binary: %w", err)
	}

	logf("Uploading hop-agent (%d bytes)...", len(data))
	if err := scpFile(client, "/usr/local/bin/hop-agent", data, 0755); err != nil {
		return fmt.Errorf("upload hop-agent: %w", err)
	}

	logf("Installing systemd unit...")
	if err := scpFile(client, "/etc/systemd/system/hop-agent.service", []byte(systemdUnit), 0644); err != nil {
		return fmt.Errorf("upload systemd unit: %w", err)
	}

	if _, err := runRemote(client, "systemctl daemon-reload"); err != nil {
		logf("Warning: systemctl daemon-reload failed: %v", err)
	}

	return nil
}

// scpFile uploads a file to the remote host via SSH.
func scpFile(client *ssh.Client, remotePath string, data []byte, mode os.FileMode) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	// Use cat redirect for simplicity (avoids scp binary dependency).
	sess.Stdin = newByteReader(data)
	cmd := fmt.Sprintf("cat > %q && chmod %o %q", remotePath, mode, remotePath)
	if out, err := sess.CombinedOutput(cmd); err != nil {
		return fmt.Errorf("upload %q: %w (output: %s)", remotePath, err, out)
	}
	return nil
}

type byteReader struct {
	data []byte
	pos  int
}

func newByteReader(data []byte) io.Reader {
	return &byteReader{data: data}
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}
