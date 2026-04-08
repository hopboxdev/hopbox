package containers

import "testing"

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
