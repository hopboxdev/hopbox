package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/alecthomas/kong"
	"github.com/hopboxdev/hopbox/internal/control"
)

const socketPath = "/var/run/hopbox/control.sock"

type CLI struct {
	Status  StatusCmd  `cmd:"" help:"Show box info."`
	Expose  ExposeCmd  `cmd:"" help:"Print SSH tunnel instructions for a port."`
	Destroy DestroyCmd `cmd:"" help:"Destroy this box."`
}

type ExposeCmd struct {
	Port int `arg:"" help:"Port to expose."`
}

type StatusCmd struct {
	JSON bool `help:"Output as JSON." default:"false"`
}

type DestroyCmd struct{}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli, kong.Name("hopbox"), kong.Description("Hopbox dev environment CLI"))
	switch ctx.Command() {
	case "status":
		doStatus(cli.Status.JSON)
	case "expose <port>":
		doExpose(cli.Expose.Port)
	case "destroy":
		doDestroy()
	}
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

func doExpose(port int) {
	resp, err := sendRequest(control.Request{Command: "status"})
	if err != nil {
		// Fallback if socket not available
		fmt.Printf("To access port %d from your machine, run:\n\n", port)
		fmt.Printf("  ssh -p 2222 -L %d:localhost:%d -N hop@<server>\n\n", port, port)
		fmt.Printf("Then open http://localhost:%d\n", port)
		return
	}
	if !resp.OK {
		fmt.Printf("To access port %d from your machine, run:\n\n", port)
		fmt.Printf("  ssh -p 2222 -L %d:localhost:%d -N hop@<server>\n\n", port, port)
		fmt.Printf("Then open http://localhost:%d\n", port)
		return
	}

	hostname := resp.Data["hostname"]
	if hostname == "" {
		hostname = "<server>"
	}
	sshPort := resp.Data["ssh_port"]
	if sshPort == "" {
		sshPort = "2222"
	}
	user := resp.Data["user"]
	if user == "" {
		user = "hop"
	}

	fmt.Printf("To access port %d from your machine, run:\n\n", port)
	fmt.Printf("  ssh -p %s -L %d:localhost:%d -N %s@%s\n\n", sshPort, port, port, user, hostname)
	fmt.Printf("Then open http://localhost:%d\n", port)
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
