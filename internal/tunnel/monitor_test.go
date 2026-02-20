package tunnel_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hopboxdev/hopbox/internal/tunnel"
)

func TestConnMonitor_DetectsDisconnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var mu sync.Mutex
	var events []tunnel.ConnEvent

	m := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: srv.URL,
		Client:    srv.Client(),
		Interval:  50 * time.Millisecond,
		Timeout:   25 * time.Millisecond,
		OnStateChange: func(evt tunnel.ConnEvent) {
			mu.Lock()
			events = append(events, evt)
			mu.Unlock()
		},
	})

	ctx := t.Context()
	go m.Run(ctx)

	// Let a few checks pass.
	time.Sleep(200 * time.Millisecond)

	// Close server to simulate disconnection.
	srv.Close()

	// Wait for disconnect event (2 failures × 50ms interval + buffer).
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(events) == 0 {
		t.Fatal("expected disconnect event, got none")
	}
	if events[0].State != tunnel.ConnStateDisconnected {
		t.Fatalf("expected ConnStateDisconnected, got %v", events[0].State)
	}
}

func TestConnMonitor_DetectsReconnect(t *testing.T) {
	var serving atomic.Bool
	serving.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serving.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			http.Error(w, "down", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	var mu sync.Mutex
	var events []tunnel.ConnEvent
	disconnectCh := make(chan struct{}, 1)
	reconnectCh := make(chan struct{}, 1)

	m := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: srv.URL,
		Client:    srv.Client(),
		Interval:  50 * time.Millisecond,
		Timeout:   500 * time.Millisecond, // generous: race detector can slow loopback HTTP
		OnStateChange: func(evt tunnel.ConnEvent) {
			mu.Lock()
			events = append(events, evt)
			mu.Unlock()
			switch evt.State {
			case tunnel.ConnStateDisconnected:
				select {
				case disconnectCh <- struct{}{}:
				default:
				}
			case tunnel.ConnStateConnected:
				select {
				case reconnectCh <- struct{}{}:
				default:
				}
			}
		},
	})

	ctx := t.Context()
	go m.Run(ctx)

	// Let initial checks pass.
	time.Sleep(200 * time.Millisecond)

	// Simulate outage; wait for the disconnect event rather than a fixed sleep.
	serving.Store(false)
	select {
	case <-disconnectCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for disconnect event")
	}

	// Restore; wait for the reconnect event rather than a fixed sleep.
	serving.Store(true)
	select {
	case <-reconnectCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for reconnect event")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(events) < 2 {
		t.Fatalf("expected disconnect + reconnect events, got %d", len(events))
	}
	if events[0].State != tunnel.ConnStateDisconnected {
		t.Fatalf("first event should be ConnStateDisconnected, got %v", events[0].State)
	}
	if events[1].State != tunnel.ConnStateConnected {
		t.Fatalf("second event should be ConnStateConnected, got %v", events[1].State)
	}
	if events[1].Duration <= 0 {
		t.Fatalf("reconnect event should have Duration > 0, got %v", events[1].Duration)
	}
}

func TestConnMonitor_NoFlapOnSingleFailure(t *testing.T) {
	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callCount.Add(1) == 1 {
			http.Error(w, "one bad check", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var mu sync.Mutex
	var events []tunnel.ConnEvent

	m := tunnel.NewConnMonitor(tunnel.MonitorConfig{
		HealthURL: srv.URL,
		Client:    srv.Client(),
		Interval:  50 * time.Millisecond,
		Timeout:   500 * time.Millisecond, // generous: ensures server handler runs before timeout
		OnStateChange: func(evt tunnel.ConnEvent) {
			mu.Lock()
			events = append(events, evt)
			mu.Unlock()
		},
	})

	ctx := t.Context()
	go m.Run(ctx)

	// Run long enough for the failure and several successes to be processed.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(events) != 0 {
		t.Fatalf("expected no state change events on single failure, got %d", len(events))
	}
}
