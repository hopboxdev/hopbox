package users

import "testing"

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"gandalf", true},
		{"my-user", true},
		{"user123", true},
		{"a", true},
		{"", false},
		{"has spaces", false},
		{"has@symbol", false},
		{"UPPER", false},
		{"-leading", false},
		{"trailing-", false},
		{"has--double", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateUsername(tt.input)
			if tt.valid && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected invalid, got nil")
			}
		})
	}
}
