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

Durations are Go-style: 30s, 5m, 1h30m. Reads $BOX_META (default http://169.254.169.254).`

func main() {
	base := os.Getenv("BOX_META")
	if base == "" {
		base = "http://169.254.169.254"
	}
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "info":
		get(base + "/v1/me")
	case "time":
		get(base + "/v1/me/time")
	case "keep-alive":
		body := "{}"
		if len(args) > 1 {
			body = fmt.Sprintf(`{"duration":%q}`, args[1])
		}
		post(base+"/v1/me/keep-alive", body)
	case "auto-suspend":
		switch arg(args, 1) {
		case "on":
			post(base+"/v1/me/auto-suspend", `{"enabled":true}`)
		case "off":
			post(base+"/v1/me/auto-suspend", `{"enabled":false}`)
		case "status", "":
			get(base + "/v1/me") // auto_suspend is in the metadata
		default:
			fmt.Fprintln(os.Stderr, "box-guest: auto-suspend on|off|status")
			os.Exit(2)
		}
	case "idle":
		post(base+"/v1/me/idle", fmt.Sprintf(`{"timeout":%q}`, arg(args, 1)))
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

func get(url string) {
	resp, err := client().Get(url)
	finish(resp, err, true)
}

func post(url, body string) {
	resp, err := client().Post(url, "application/json", strings.NewReader(body))
	finish(resp, err, false)
}

func client() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

func finish(resp *http.Response, err error, echo bool) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "box-guest:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "box-guest: %s\n", resp.Status)
		os.Exit(1)
	}
	if echo {
		_, _ = io.Copy(os.Stdout, resp.Body)
	}
}
