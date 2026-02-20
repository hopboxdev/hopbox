package version

import "testing"

func TestDetectPackageManager(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"homebrew cellar", "/opt/homebrew/Cellar/hopbox/0.1.0/bin/hop", "brew"},
		{"linuxbrew", "/home/user/.linuxbrew/bin/hop", "brew"},
		{"nix store", "/nix/store/abc123-hopbox/bin/hop", "nix"},
		{"standalone", "/usr/local/bin/hop", ""},
		{"go install", "/Users/user/go/bin/hop", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPackageManager(tt.path)
			if got != tt.want {
				t.Errorf("DetectPackageManager(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestDetectPackageManager_BuildTimeOverride(t *testing.T) {
	old := PackageManager
	PackageManager = "brew"
	defer func() { PackageManager = old }()

	got := DetectPackageManager("/usr/local/bin/hop")
	if got != "brew" {
		t.Errorf("expected build-time override 'brew', got %q", got)
	}
}
