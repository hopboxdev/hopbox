package daemon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequestMarshal(t *testing.T) {
	req := Request{Method: "shutdown"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Method != "shutdown" {
		t.Errorf("Method = %q, want %q", decoded.Method, "shutdown")
	}
}

func TestResponseMarshal(t *testing.T) {
	resp := Response{
		OK: true,
		State: &DaemonStatus{
			PID:         1234,
			Connected:   true,
			LastHealthy: time.Now().Truncate(time.Second),
			Interface:   "utun5",
			StartedAt:   time.Now().Truncate(time.Second),
			Bridges:     []string{"clipboard", "cdp"},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK {
		t.Error("OK = false, want true")
	}
	if decoded.State == nil {
		t.Fatal("State is nil")
	}
	if decoded.State.PID != 1234 {
		t.Errorf("PID = %d, want 1234", decoded.State.PID)
	}
	if len(decoded.State.Bridges) != 2 {
		t.Errorf("Bridges len = %d, want 2", len(decoded.State.Bridges))
	}
}

func TestErrorResponse(t *testing.T) {
	resp := Response{OK: false, Error: "not running"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OK {
		t.Error("OK = true, want false")
	}
	if decoded.Error != "not running" {
		t.Errorf("Error = %q, want %q", decoded.Error, "not running")
	}
}
