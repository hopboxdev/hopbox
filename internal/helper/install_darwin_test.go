//go:build darwin

package helper

import (
	"strings"
	"testing"
)

func TestPlistContent(t *testing.T) {
	plist := buildPlist("/usr/local/bin/hop-helper")
	if !strings.Contains(plist, "dev.hopbox.helper") {
		t.Error("missing label")
	}
	if !strings.Contains(plist, "/usr/local/bin/hop-helper") {
		t.Error("missing binary path")
	}
	if !strings.Contains(plist, "RunAtLoad") {
		t.Error("missing RunAtLoad")
	}
	if !strings.Contains(plist, "KeepAlive") {
		t.Error("missing KeepAlive")
	}
}
