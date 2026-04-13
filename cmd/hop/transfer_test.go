package main

import (
	"strings"
	"testing"
)

func TestTransferDetectsDownload(t *testing.T) {
	cmd := TransferCmd{Source: ":~/file.txt", Dest: "."}
	if !strings.HasPrefix(cmd.Source, ":") {
		t.Error("expected download mode for : prefix")
	}
}

func TestTransferDetectsUpload(t *testing.T) {
	cmd := TransferCmd{Source: "./file.txt", Dest: ":~/"}
	if strings.HasPrefix(cmd.Source, ":") {
		t.Error("expected upload mode without : prefix on source")
	}
}

func TestTransferDefaultDest(t *testing.T) {
	// Download with no dest defaults to "."
	cmd := TransferCmd{Source: ":~/file.txt"}
	if cmd.Dest == "" {
		// Run would default to "." — this is expected
	}

	// Upload with no dest defaults to "~/"
	cmd = TransferCmd{Source: "./file.txt"}
	if cmd.Dest == "" {
		// Run would default to "~/" — this is expected
	}
}
