package configserver

import (
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHeartbeatManager_ShutdownOnDisconnect(t *testing.T) {
	var shutdownCalled atomic.Bool

	hb := NewHeartbeatManager(func() { shutdownCalled.Store(true) }, 100*time.Millisecond)

	srv := httptest.NewServer(hb.Handler())
	defer srv.Close()

	url := "ws" + srv.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	conn.Close()

	time.Sleep(300 * time.Millisecond)

	if !shutdownCalled.Load() {
		t.Error("shutdown not called after disconnect + grace period")
	}
}

func TestHeartbeatManager_NoShutdownWhileConnected(t *testing.T) {
	var shutdownCalled atomic.Bool

	hb := NewHeartbeatManager(func() { shutdownCalled.Store(true) }, 50*time.Millisecond)

	srv := httptest.NewServer(hb.Handler())
	defer srv.Close()

	url := "ws" + srv.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(200 * time.Millisecond)

	if shutdownCalled.Load() {
		t.Error("shutdown called while client still connected")
	}
}

func TestHeartbeatManager_GraceCancelOnReconnect(t *testing.T) {
	var shutdownCalled atomic.Bool

	hb := NewHeartbeatManager(func() { shutdownCalled.Store(true) }, 200*time.Millisecond)

	srv := httptest.NewServer(hb.Handler())
	defer srv.Close()

	url := "ws" + srv.URL[len("http"):]

	conn1, _, _ := websocket.DefaultDialer.Dial(url, nil)
	conn1.Close()
	time.Sleep(50 * time.Millisecond)

	conn2, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer conn2.Close()

	time.Sleep(400 * time.Millisecond)

	if shutdownCalled.Load() {
		t.Error("shutdown called despite reconnect before grace expired")
	}
}
