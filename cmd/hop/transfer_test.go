package main

import "testing"

func TestParseTransferTarget(t *testing.T) {
	tests := []struct {
		input      string
		wantLocal  string
		wantRemote string
	}{
		{"./file.txt", "./file.txt", "~/"},
		{"./file.txt:/home/dev/projects/", "./file.txt", "/home/dev/projects/"},
		{"/tmp/data.tar.gz:/opt/", "/tmp/data.tar.gz", "/opt/"},
		{"file.txt:", "file.txt", "~/"},
	}
	for _, tt := range tests {
		local, remote := parseTransferTarget(tt.input)
		if local != tt.wantLocal {
			t.Errorf("parseTransferTarget(%q) local = %q, want %q", tt.input, local, tt.wantLocal)
		}
		if remote != tt.wantRemote {
			t.Errorf("parseTransferTarget(%q) remote = %q, want %q", tt.input, remote, tt.wantRemote)
		}
	}
}
