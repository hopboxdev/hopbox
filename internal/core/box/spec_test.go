package box

import (
	"testing"
	"time"
)

func TestParseSpecGrammar(t *testing.T) {
	cases := []struct {
		in   string
		want Spec
	}{
		{"myproject", Spec{Name: "myproject"}},
		{"myproject:python", Spec{Name: "myproject", Image: "python"}},
		{"myproject~ovh:python:l4", Spec{Name: "myproject", Backend: "ovh", Image: "python", Flavor: "l4"}},
		{"myproject:go:cpu+5m", Spec{Name: "myproject", Image: "go", Flavor: "cpu", Grace: 5 * time.Minute}},
		{"myproject~runtime:go:cpu+1h", Spec{Name: "myproject", Backend: "runtime", Image: "go", Flavor: "cpu", Grace: time.Hour}},
		{"myproject+", Spec{Name: "myproject", ForceNew: true}},
		{"myproject~ovh+", Spec{Name: "myproject", Backend: "ovh", ForceNew: true}},
	}
	for _, c := range cases {
		got, err := ParseSpec(c.in)
		if err != nil {
			t.Errorf("ParseSpec(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSpec(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestParseSpecSpecials(t *testing.T) {
	cases := []struct {
		in        string
		special   string
		sessionID string
	}{
		{"cli", "cli", ""},
		{"sudo", "sudo", ""},
		{"_", "_", ""},
		{"session-abc123", "session", "abc123"},
	}
	for _, c := range cases {
		got, err := ParseSpec(c.in)
		if err != nil {
			t.Errorf("ParseSpec(%q) error: %v", c.in, err)
			continue
		}
		if got.Special != c.special || got.SessionID != c.sessionID {
			t.Errorf("ParseSpec(%q) special=%q session=%q, want %q/%q", c.in, got.Special, got.SessionID, c.special, c.sessionID)
		}
	}
}

func TestParseSpecRejectsEmptyAndBadDuration(t *testing.T) {
	if _, err := ParseSpec(""); err == nil {
		t.Error("empty username must error")
	}
	if _, err := ParseSpec("proj:go:cpu+nonsense"); err == nil {
		t.Error("bad duration must error")
	}
}
