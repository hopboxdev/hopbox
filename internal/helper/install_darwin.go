//go:build darwin

package helper

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	launchDaemonLabel = "dev.hopbox.helper"
	plistPath         = "/Library/LaunchDaemons/dev.hopbox.helper.plist"
	helperInstallPath = "/Library/PrivilegedHelperTools/dev.hopbox.helper"
)

func buildPlist(binaryPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`, launchDaemonLabel, binaryPath)
}

// Install copies the helper binary and installs the LaunchDaemon.
// Must be run with sudo.
func Install(helperBinary string) error {
	// Copy binary.
	if err := os.MkdirAll("/Library/PrivilegedHelperTools", 0755); err != nil {
		return fmt.Errorf("create helper dir: %w", err)
	}
	data, err := os.ReadFile(helperBinary)
	if err != nil {
		return fmt.Errorf("read helper binary: %w", err)
	}
	if err := os.WriteFile(helperInstallPath, data, 0755); err != nil {
		return fmt.Errorf("write helper binary: %w", err)
	}

	// Write plist.
	plist := buildPlist(helperInstallPath)
	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Load the daemon.
	_ = exec.Command("launchctl", "unload", plistPath).Run() // ignore error — may not be loaded
	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// IsInstalled checks if the helper LaunchDaemon is installed.
func IsInstalled() bool {
	_, err := os.Stat(plistPath)
	return err == nil
}
