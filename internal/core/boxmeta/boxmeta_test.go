package boxmeta

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/core/box"
)

func TestMeIdentifiesBoxBySourceIP(t *testing.T) {
	b := box.New("default", "alice", "proj", "alpine")
	b.IP = "10.0.0.7"
	b.Phase = box.PhaseRunning
	b.MemMB, b.CPUMillis = 2048, 2000

	srv := New(func(_ context.Context, ip string) (*box.Box, error) {
		if ip == "10.0.0.7" {
			return b, nil
		}
		return nil, box.ErrNotFound
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// A request whose source IP matches the box -> its metadata.
	req, _ := http.NewRequest("GET", ts.URL+"/v1/me", nil)
	req.RemoteAddr = "10.0.0.7:54321"
	// httptest server sets RemoteAddr from the connection, so call the handler directly.
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var m meta
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "proj" || m.Owner != "alice" || m.IP != "10.0.0.7" || m.Phase != "Running" {
		t.Fatalf("metadata wrong: %+v", m)
	}
	if m.MemMB != 2048 || m.CPUMillis != 2000 {
		t.Fatalf("caps wrong: %+v", m)
	}

	// An unknown source IP -> 404 (no box leaks).
	req2, _ := http.NewRequest("GET", ts.URL+"/v1/me", nil)
	req2.RemoteAddr = "10.0.0.99:1234"
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != 404 {
		t.Fatalf("unknown source must 404, got %d", rec2.Code)
	}
}

// mutateServer wires a Server whose mutate applies fn to b (resolved by IP).
func mutateServer(b *box.Box) *Server {
	return New(
		func(_ context.Context, _ string) (*box.Box, error) { return b, nil },
		func(_ context.Context, ip string, fn func(*box.Box)) error {
			if ip != b.IP {
				return box.ErrNotFound
			}
			fn(b)
			return nil
		},
	)
}

func postTo(t *testing.T, srv *Server, path, body, srcIP string) {
	t.Helper()
	req, _ := http.NewRequest("POST", path, strings.NewReader(body))
	req.RemoteAddr = srcIP + ":40000"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
}

func TestHeartbeatRecordsLoad(t *testing.T) {
	b := box.New("default", "alice", "proj", "alpine")
	b.IP = "10.0.0.5"
	postTo(t, mutateServer(b), "/v1/me/heartbeat", `{"load":1.5}`, "10.0.0.5")
	if b.Load != 1.5 {
		t.Fatalf("load not recorded: %v", b.Load)
	}
}

func TestOwnerCommands(t *testing.T) {
	b := box.New("default", "alice", "proj", "alpine")
	b.IP = "10.0.0.6"
	b.AutoSuspend = true
	srv := mutateServer(b)

	postTo(t, srv, "/v1/me/keep-alive", `{"duration":"30m"}`, "10.0.0.6")
	if d := time.Until(b.KeepAliveUntil); d < 29*time.Minute || d > 31*time.Minute {
		t.Fatalf("keep-alive not applied: until=%v", b.KeepAliveUntil)
	}

	postTo(t, srv, "/v1/me/auto-suspend", `{"enabled":false}`, "10.0.0.6")
	if b.AutoSuspend {
		t.Fatal("auto-suspend off not applied")
	}

	postTo(t, srv, "/v1/me/idle", `{"timeout":"45m"}`, "10.0.0.6")
	if b.IdleTimeoutOverride != 45*time.Minute {
		t.Fatalf("idle override not applied: %v", b.IdleTimeoutOverride)
	}
}

func TestTime(t *testing.T) {
	fixed := time.Unix(1782554568, 351837502).UTC()
	srv := New(nil, nil)
	srv.now = func() time.Time { return fixed }
	req, _ := http.NewRequest("GET", "/v1/me/time", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["server_time_ns"] != "1782554568.351837502" {
		t.Fatalf("server_time_ns = %v", out["server_time_ns"])
	}
}
