package containers

import "testing"

func TestShouldRecreate(t *testing.T) {
	tests := []struct {
		name           string
		containerLabel string
		wantHash       string
		want           bool
	}{
		{"matching", "abc123", "abc123", false},
		{"different hash", "abc123", "def456", true},
		{"no label", "", "def456", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRecreate(tt.containerLabel, tt.wantHash)
			if got != tt.want {
				t.Errorf("ShouldRecreate(%q, %q) = %v, want %v", tt.containerLabel, tt.wantHash, got, tt.want)
			}
		})
	}
}

func TestContainerName(t *testing.T) {
	tests := []struct {
		username string
		boxname  string
		want     string
	}{
		{"gandalf", "default", "hopbox-gandalf-default"},
		{"user", "myproject", "hopbox-user-myproject"},
	}
	for _, tt := range tests {
		got := ContainerName(tt.username, tt.boxname)
		if got != tt.want {
			t.Errorf("ContainerName(%q, %q) = %q, want %q", tt.username, tt.boxname, got, tt.want)
		}
	}
}
