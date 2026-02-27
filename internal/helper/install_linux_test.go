//go:build linux

package helper

import (
	"strings"
	"testing"
)

func TestSystemdUnitContent(t *testing.T) {
	unit := buildSystemdUnit("/usr/local/bin/hop-helper")
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/hop-helper") {
		t.Error("missing ExecStart with binary path")
	}
	if !strings.Contains(unit, "Restart=always") {
		t.Error("missing Restart=always")
	}
	if !strings.Contains(unit, "[Install]") {
		t.Error("missing [Install] section")
	}
	if !strings.Contains(unit, "WantedBy=multi-user.target") {
		t.Error("missing WantedBy")
	}
}
