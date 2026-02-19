package setup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/hopboxdev/hopbox/internal/version"
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
// It checks $HOP_AGENT_BINARY for a local override first; otherwise downloads
// the release binary matching the VPS architecture.
func installAgent(_ context.Context, client *ssh.Client, out io.Writer) error {
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(out, "  "+format+"\n", args...)
	}

	var data []byte

	// Dev override: use a local binary if HOP_AGENT_BINARY is set.
	if localPath := os.Getenv("HOP_AGENT_BINARY"); localPath != "" {
		logf("Using local agent binary: %s", localPath)
		var err error
		data, err = os.ReadFile(localPath)
		if err != nil {
			return fmt.Errorf("read agent binary: %w", err)
		}
	} else {
		v := version.Version
		if v == "dev" {
			return fmt.Errorf(
				"no release found for version dev; set HOP_AGENT_BINARY to a local hop-agent binary",
			)
		}

		// Detect VPS architecture via SSH.
		archOut, err := runRemote(client, "uname -m")
		if err != nil {
			return fmt.Errorf("detect VPS architecture: %w", err)
		}
		goarch := archToGoarch(strings.TrimSpace(archOut))

		url := fmt.Sprintf(
			"https://github.com/hopboxdev/hopbox/releases/download/v%s/hop-agent_%s_linux_%s",
			v, v, goarch,
		)
		logf("Downloading hop-agent %s (%s)...", v, goarch)

		resp, err := http.Get(url) //nolint:noctx
		if err != nil {
			return fmt.Errorf("download hop-agent: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download hop-agent: HTTP %d from %s", resp.StatusCode, url)
		}
		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read download: %w", err)
		}
	}

	logf("Uploading hop-agent (%d bytes)...", len(data))
	if err := scpFile(client, "/usr/local/bin/hop-agent", data, 0755, out); err != nil {
		return fmt.Errorf("upload hop-agent: %w", err)
	}

	logf("Installing systemd unit...")
	if err := scpFile(client, "/etc/systemd/system/hop-agent.service", []byte(systemdUnit), 0644, nil); err != nil {
		return fmt.Errorf("upload systemd unit: %w", err)
	}

	if _, err := runRemote(client, "sudo systemctl daemon-reload"); err != nil {
		logf("Warning: systemctl daemon-reload failed: %v", err)
	}

	return nil
}

// archToGoarch maps uname -m output to a Go architecture string.
func archToGoarch(uname string) string {
	switch uname {
	case "aarch64", "arm64":
		return "arm64"
	default:
		return "amd64"
	}
}

// scpFile uploads a file to the remote host via SSH.
// If out is non-nil and the file is large, progress is reported every 10%.
func scpFile(client *ssh.Client, remotePath string, data []byte, mode os.FileMode, out io.Writer) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	// Use sudo tee so non-root SSH users with sudo access can write to
	// system paths like /usr/local/bin and /etc/systemd.
	sess.Stdin = &progressReader{data: data, total: len(data), out: out}
	cmd := fmt.Sprintf("sudo tee %q > /dev/null && sudo chmod %o %q", remotePath, mode, remotePath)
	if cmdOut, err := sess.CombinedOutput(cmd); err != nil {
		return fmt.Errorf("upload %q: %w (output: %s)", remotePath, err, cmdOut)
	}
	return nil
}

// progressReader wraps a byte slice and reports upload progress to out every 10%.
type progressReader struct {
	data    []byte
	pos     int
	total   int
	out     io.Writer
	lastPct int
}

func (r *progressReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	if r.total > 0 && r.out != nil {
		pct := r.pos * 100 / r.total
		if pct/10 > r.lastPct/10 {
			r.lastPct = pct
			_, _ = fmt.Fprintf(r.out, "    %d%%\n", pct)
		}
	}
	return n, nil
}
