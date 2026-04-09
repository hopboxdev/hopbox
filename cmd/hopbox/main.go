package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/hopboxdev/hopbox/internal/control"
)

const socketPath = "/var/run/hopbox.sock"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "status":
		jsonOutput := len(os.Args) > 2 && os.Args[2] == "--json"
		doStatus(jsonOutput)
	case "destroy":
		doDestroy()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: hopbox <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  status [--json]  Show box info")
	fmt.Fprintln(os.Stderr, "  destroy          Destroy this box")
}

func sendRequest(req control.Request) (control.Response, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return control.Response{}, fmt.Errorf("connect to hopboxd: %w (is hopboxd running?)", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return control.Response{}, fmt.Errorf("send request: %w", err)
	}

	var resp control.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return control.Response{}, fmt.Errorf("read response: %w", err)
	}
	return resp, nil
}

func doStatus(jsonOutput bool) {
	resp, err := sendRequest(control.Request{Command: "status"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	if jsonOutput {
		data, _ := json.Marshal(resp.Data)
		fmt.Println(string(data))
		return
	}

	fmt.Printf("Box:         %s\n", resp.Data["box"])
	fmt.Printf("User:        %s\n", resp.Data["user"])
	fmt.Printf("OS:          %s\n", resp.Data["os"])
	fmt.Printf("Shell:       %s\n", resp.Data["shell"])
	fmt.Printf("Multiplexer: %s\n", resp.Data["multiplexer"])
	fmt.Printf("Uptime:      %s\n", resp.Data["uptime"])
}

func doDestroy() {
	// First get status to know the box name
	statusResp, err := sendRequest(control.Request{Command: "status"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !statusResp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", statusResp.Error)
		os.Exit(1)
	}

	boxName := statusResp.Data["box"]

	fmt.Printf("Are you sure you want to destroy box %q? This will:\n", boxName)
	fmt.Println("  - Stop and remove this container")
	fmt.Println("  - Delete the home directory for this box")
	fmt.Println()
	fmt.Printf("Type the box name to confirm: ")

	var confirm string
	fmt.Scanln(&confirm)

	if confirm != boxName {
		fmt.Println("Aborted.")
		os.Exit(1)
	}

	resp, err := sendRequest(control.Request{Command: "destroy", Confirm: confirm})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}

	fmt.Println("Destroying... done.")
}
