package gateway

import "testing"

func TestParseUsername(t *testing.T) {
	tests := []struct {
		input   string
		user    string
		boxname string
	}{
		{"hop", "hop", "default"},
		{"hop+myproject", "hop", "myproject"},
		{"gandalf+dev", "gandalf", "dev"},
		{"user+my-box", "user", "my-box"},
		{"hop+", "hop", "default"},
		{"+box", "", "box"},
		{"simple", "simple", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			user, boxname := ParseUsername(tt.input)
			if user != tt.user {
				t.Errorf("user: got %q, want %q", user, tt.user)
			}
			if boxname != tt.boxname {
				t.Errorf("boxname: got %q, want %q", boxname, tt.boxname)
			}
		})
	}
}
