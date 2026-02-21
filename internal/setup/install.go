package setup

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

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
// It checks $HOP_AGENT_BINARY for a local override first; otherwise downloads
// the release binary matching the VPS architecture and verifies its SHA256
// checksum against the published checksums file.
func installAgent(ctx context.Context, client *ssh.Client, out io.Writer, targetVersion string, onStep func(string)) error {
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if onStep != nil {
			onStep(msg)
		} else {
			_, _ = fmt.Fprintln(out, "  "+msg)
		}
	}

	// Suppress progress bar when running inside a TUI step — bubbletea
	// owns the terminal and \r-based updates would garble the output.
	var progressOut io.Writer
	if onStep == nil {
		progressOut = out
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
		v := targetVersion
		if v == "" || v == "dev" {
			return fmt.Errorf(
				"no release found for version %q; set HOP_AGENT_BINARY to a local hop-agent binary",
				v,
			)
		}

		// Detect VPS architecture via SSH.
		archOut, err := runRemote(client, "uname -m")
		if err != nil {
			return fmt.Errorf("detect VPS architecture: %w", err)
		}
		goarch := archToGoarch(strings.TrimSpace(archOut))

		binName := fmt.Sprintf("hop-agent_%s_linux_%s", v, goarch)
		binURL := fmt.Sprintf(
			"https://github.com/hopboxdev/hopbox/releases/download/v%s/%s",
			v, binName,
		)
		logf("Downloading hop-agent %s (%s)...", v, goarch)

		data, err = FetchURL(ctx, binURL)
		if err != nil {
			return fmt.Errorf("download hop-agent: %w", err)
		}

		// Verify the downloaded binary against the published SHA256 checksums.
		logf("Verifying checksum...")
		csURL := fmt.Sprintf(
			"https://github.com/hopboxdev/hopbox/releases/download/v%s/checksums.txt",
			v,
		)
		expected, err := LookupChecksum(ctx, csURL, binName)
		if err != nil {
			return fmt.Errorf("checksum lookup: %w", err)
		}
		actual := fmt.Sprintf("%x", sha256.Sum256(data))
		if actual != expected {
			return fmt.Errorf("checksum mismatch for %s: got %s, want %s", binName, actual, expected)
		}
	}

	logf("Uploading hop-agent (%d bytes)...", len(data))
	if err := scpFile(client, "/usr/local/bin/hop-agent", data, 0755, progressOut); err != nil {
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

// FetchURL fetches url using ctx and returns the response body.
func FetchURL(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// LookupChecksum downloads the checksums file at url and returns the expected
// SHA256 hex digest for filename. The file format is "<hash>  <filename>" per
// goreleaser defaults; both one-space and two-space separators are accepted.
func LookupChecksum(ctx context.Context, url, filename string) (string, error) {
	data, err := FetchURL(ctx, url)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == filename {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum found for %q in checksums file", filename)
}

// scpFile uploads a file to the remote host via SSH.
// If out is non-nil and the file is large, progress is reported every 10%.
func scpFile(client *ssh.Client, remotePath string, data []byte, mode os.FileMode, out io.Writer) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()

	// Write to a temp file then mv atomically into place.
	// Direct overwrite fails with "text file busy" if the binary is running.
	tmpPath := remotePath + ".new"
	sess.Stdin = &progressReader{data: data, total: len(data), out: out}
	cmd := fmt.Sprintf(
		"sudo tee %q > /dev/null && sudo chmod %o %q && sudo mv -f %q %q",
		tmpPath, mode, tmpPath, tmpPath, remotePath,
	)
	if cmdOut, err := sess.CombinedOutput(cmd); err != nil {
		return fmt.Errorf("upload %q: %w (output: %s)", remotePath, err, cmdOut)
	}
	return nil
}

// progressReader wraps a byte slice and reports upload progress to out as
// an inline progress bar that updates in place via carriage return.
type progressReader struct {
	data    []byte
	pos     int
	total   int
	out     io.Writer
	lastPct int
}

const barWidth = 30

func (r *progressReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	if r.total > 0 && r.out != nil {
		pct := r.pos * 100 / r.total
		if pct != r.lastPct {
			r.lastPct = pct
			filled := barWidth * pct / 100
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			_, _ = fmt.Fprintf(r.out, "\r    [%s] %3d%%", bar, pct)
			if pct == 100 {
				_, _ = fmt.Fprintln(r.out)
			}
		}
	}
	return n, nil
}
