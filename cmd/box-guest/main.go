// Command box-guest is the in-box client for the box metadata API. It is shipped
// inside a box and talks to the control plane's metadata endpoint ($BOX_META),
// which identifies the box by its source IP — so there is no credential here.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const usage = `box-guest — in-box client for the box metadata API.

Usage:
  box-guest info                         Show this box's metadata (GET /v1/me).
  box-guest time                         Print the control plane's wall clock.
  box-guest keep-alive [DURATION]        Pin the box alive (no suspend) for DURATION (default 5m).
  box-guest auto-suspend on|off|status   Toggle / show auto-suspend.
  box-guest idle [DURATION]              Set this box's idle timeout (empty = back to default).
  box-guest status STATE [MESSAGE]       Report agent state (working|blocked|done) + a status line.
  box-guest mcp                          Run an MCP server (stdio) exposing the above as tools.

Durations are Go-style: 30s, 5m, 1h30m. Reads $BOX_META (default http://169.254.169.254).`

func base() string {
	if b := os.Getenv("BOX_META"); b != "" {
		return b
	}
	return "http://169.254.169.254"
}

func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "info":
		out, err := doGet(base() + "/v1/me")
		emit(out, err)
	case "time":
		out, err := doGet(base() + "/v1/me/time")
		emit(out, err)
	case "keep-alive":
		emit("", keepAlive(arg(args, 1)))
	case "auto-suspend":
		switch arg(args, 1) {
		case "on":
			emit("", autoSuspend(true))
		case "off":
			emit("", autoSuspend(false))
		case "status", "":
			out, err := doGet(base() + "/v1/me")
			emit(out, err)
		default:
			fmt.Fprintln(os.Stderr, "box-guest: auto-suspend on|off|status")
			os.Exit(2)
		}
	case "idle":
		emit("", setIdle(arg(args, 1)))
	case "status":
		emit("", setStatus(arg(args, 1), arg(args, 2)))
	case "mcp":
		runMCP(base())
	default:
		fmt.Println(usage)
		if cmd != "" && cmd != "help" && cmd != "-h" && cmd != "--help" {
			os.Exit(2)
		}
	}
}

func arg(args []string, i int) string {
	if len(args) > i {
		return args[i]
	}
	return ""
}

// emit prints a result (if any) or fails with the error.
func emit(out string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "box-guest:", err)
		os.Exit(1)
	}
	if out != "" {
		fmt.Print(out)
	}
}

// --- metadata operations (shared by the CLI and the MCP tools) ---

func keepAlive(dur string) error {
	body := "{}"
	if dur != "" {
		body = fmt.Sprintf(`{"duration":%q}`, dur)
	}
	return doPost(base()+"/v1/me/keep-alive", body)
}

func autoSuspend(on bool) error {
	return doPost(base()+"/v1/me/auto-suspend", fmt.Sprintf(`{"enabled":%t}`, on))
}

func setIdle(timeout string) error {
	return doPost(base()+"/v1/me/idle", fmt.Sprintf(`{"timeout":%q}`, timeout))
}

func setStatus(state, status string) error {
	return doPost(base()+"/v1/me/status", fmt.Sprintf(`{"state":%q,"status":%q}`, state, status))
}

func client() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

func doGet(url string) (string, error) {
	resp, err := client().Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s", resp.Status)
	}
	return string(b), nil
}

func doPost(url, body string) error {
	resp, err := client().Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s", resp.Status)
	}
	return nil
}
