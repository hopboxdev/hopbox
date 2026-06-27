// Command box-guest is the in-box client for the box metadata API. It is shipped
// inside a box and talks to the control plane's metadata endpoint ($BOX_META),
// which identifies the box by its source IP — so there is no credential here.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const usage = `box-guest — in-box client for the box metadata API.

Usage:
  box-guest info     Show this box's metadata (GET /v1/me).
  box-guest time     Print the control plane's wall clock (GET /v1/me/time).

Reads $BOX_META (default http://169.254.169.254).`

func main() {
	base := os.Getenv("BOX_META")
	if base == "" {
		base = "http://169.254.169.254"
	}
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "info":
		get(base + "/v1/me")
	case "time":
		get(base + "/v1/me/time")
	default:
		fmt.Println(usage)
		if cmd != "" && cmd != "help" && cmd != "-h" && cmd != "--help" {
			os.Exit(2)
		}
	}
}

func get(url string) {
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "box-guest:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "box-guest: %s\n", resp.Status)
		os.Exit(1)
	}
	_, _ = io.Copy(os.Stdout, resp.Body)
}
