package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"sync/atomic"
)

const defaultNotificationPort = 2226

// notificationPayload is the JSON structure sent by the server-side
// notify-send shim.
type notificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// NotificationBridge listens for notification payloads from the server and
// displays them as local desktop notifications. The server-side `notify-send`
// shim sends a JSON object with "title" and "body" fields.
type NotificationBridge struct {
	listenAddr string
	listener   net.Listener
	running    atomic.Bool
	notifier   func(title, body string) error // injectable for tests
}

// NewNotificationBridge creates a notification bridge on the default port.
func NewNotificationBridge(listenIP string) *NotificationBridge {
	return NewNotificationBridgeOnPort(listenIP, defaultNotificationPort)
}

// NewNotificationBridgeOnPort creates a notification bridge on a specific port.
// If port is 0, an ephemeral port is used.
func NewNotificationBridgeOnPort(listenIP string, port int) *NotificationBridge {
	return &NotificationBridge{
		listenAddr: fmt.Sprintf("%s:%d", listenIP, port),
		notifier:   showNotification,
	}
}

// SetNotifier overrides the notification function. Used in tests.
func (b *NotificationBridge) SetNotifier(fn func(title, body string) error) {
	b.notifier = fn
}

// Start begins listening for notification events.
func (b *NotificationBridge) Start(ctx context.Context) error {
	return b.start(ctx, nil)
}

// StartWithNotify starts the bridge and sends the bound port to ch once listening.
func (b *NotificationBridge) StartWithNotify(ctx context.Context, ch chan<- int) error {
	return b.start(ctx, ch)
}

func (b *NotificationBridge) start(ctx context.Context, portNotify chan<- int) error {
	ln, err := net.Listen("tcp", b.listenAddr)
	if err != nil {
		return fmt.Errorf("notification bridge listen: %w", err)
	}
	b.listener = ln
	b.running.Store(true)

	if portNotify != nil {
		portNotify <- ln.Addr().(*net.TCPAddr).Port
	}

	go b.acceptLoop(ctx)

	<-ctx.Done()
	b.Stop()
	return nil
}

func (b *NotificationBridge) acceptLoop(_ context.Context) {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		go b.handleConn(conn)
	}
}

func (b *NotificationBridge) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	data, err := io.ReadAll(io.LimitReader(conn, 64<<10)) // 64 KB limit
	if err != nil || len(data) == 0 {
		return
	}

	var payload notificationPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	if payload.Title == "" && payload.Body == "" {
		return
	}
	_ = b.notifier(payload.Title, payload.Body)
}

// Stop tears down the bridge listener.
func (b *NotificationBridge) Stop() {
	b.running.Store(false)
	if b.listener != nil {
		_ = b.listener.Close()
	}
}

// Status returns the bridge status.
func (b *NotificationBridge) Status() string {
	if b.running.Load() {
		return fmt.Sprintf("notification bridge running (%s)", b.listenAddr)
	}
	return "notification bridge stopped"
}

// showNotification displays a desktop notification using platform-native tools.
func showNotification(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, body, title)
		return exec.Command("osascript", "-e", script).Start()
	case "linux":
		return exec.Command("notify-send", title, body).Start()
	default:
		return fmt.Errorf("notifications not supported on %s", runtime.GOOS)
	}
}
