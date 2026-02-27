//go:build linux

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	serviceName       = "hopbox-helper"
	unitPath          = "/etc/systemd/system/hopbox-helper.service"
	helperInstallPath = "/usr/local/bin/hop-helper"
)

func buildSystemdUnit(binaryPath string) string {
	return fmt.Sprintf(`[Unit]
Description=Hopbox privileged helper daemon
After=network.target

[Service]
Type=simple
ExecStart=%s
Restart=always

[Install]
WantedBy=multi-user.target
`, binaryPath)
}

// Install copies the helper binary and installs the systemd service.
// Must be run with sudo.
func Install(helperBinary string) error {
	data, err := os.ReadFile(helperBinary)
	if err != nil {
		return fmt.Errorf("read helper binary: %w", err)
	}
	if err := os.WriteFile(helperInstallPath, data, 0755); err != nil {
		return fmt.Errorf("write helper binary: %w", err)
	}

	unit := buildSystemdUnit(helperInstallPath)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "enable", "--now", serviceName).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable --now: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsInstalled checks if the helper systemd service is installed.
func IsInstalled() bool {
	_, err := os.Stat(unitPath)
	return err == nil
}
