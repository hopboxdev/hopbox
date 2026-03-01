package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDaemonErrorIncludesLogPath(t *testing.T) {
	logPath := "/Users/test/.config/hopbox/run/mybox.log"
	err := fmt.Errorf("daemon failed to start (logs: %s): %w", logPath, errors.New("connection refused"))
	if !strings.Contains(err.Error(), "logs:") {
		t.Fatal("error should contain log path hint")
	}
	if !strings.Contains(err.Error(), logPath) {
		t.Fatal("error should contain the actual log path")
	}
}
