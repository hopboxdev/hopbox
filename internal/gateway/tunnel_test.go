package gateway

import "testing"

func TestRewriteDestination(t *testing.T) {
	tests := []struct {
		host        string
		containerIP string
		want        string
	}{
		{"localhost", "172.17.0.5", "172.17.0.5"},
		{"127.0.0.1", "172.17.0.5", "172.17.0.5"},
		{"::1", "172.17.0.5", "172.17.0.5"},
		{"0.0.0.0", "172.17.0.5", "172.17.0.5"},
		{"example.com", "172.17.0.5", "example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := RewriteDestination(tt.host, tt.containerIP)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
