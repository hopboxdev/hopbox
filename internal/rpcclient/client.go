package rpcclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hopboxdev/hopbox/internal/hostconfig"
	"github.com/hopboxdev/hopbox/internal/tunnel"
)

// doRPC sends a JSON-RPC request to the given URL using the provided client.
// Returns the raw JSON result or an error.
func doRPC(client *http.Client, url, method string, params any) (json.RawMessage, error) {
	reqBody, _ := json.Marshal(map[string]any{
		"method": method,
		"params": params,
	})

	resp, err := client.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("RPC call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("parse RPC response: %w", err)
	}
	if rpcResp.Error != "" {
		return nil, fmt.Errorf("RPC error: %s", rpcResp.Error)
	}
	return rpcResp.Result, nil
}

// CallVia makes an RPC call using the provided client, looking up the agent URL
// from the named host's config. Use this when you already have a tunnel-aware
// client (e.g. one with tun.DialContext set as the transport).
func CallVia(client *http.Client, hostName, method string, params any) (json.RawMessage, error) {
	if hostName == "" {
		return nil, fmt.Errorf("--host <name> required")
	}
	cfg, err := hostconfig.Load(hostName)
	if err != nil {
		return nil, fmt.Errorf("load host config: %w", err)
	}
	url := fmt.Sprintf("http://%s:%d/rpc", cfg.AgentIP, tunnel.AgentAPIPort)
	return doRPC(client, url, method, params)
}

// Call makes an RPC call using a plain HTTP client. It checks the tunnel state
// file for a local proxy address first; if found it dials that (works on all
// platforms). Falls back to dialing the WireGuard IP directly (works on Linux
// with kernel WireGuard active).
func Call(hostName, method string, params any) (json.RawMessage, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	if state, _ := tunnel.LoadState(hostName); state != nil && state.AgentAPIAddr != "" {
		url := "http://" + state.AgentAPIAddr + "/rpc"
		return doRPC(client, url, method, params)
	}
	return CallVia(client, hostName, method, params)
}

// CallAndPrint calls Call and prints the JSON result to stdout.
func CallAndPrint(hostName, method string, params any) error {
	result, err := Call(hostName, method, params)
	if err != nil {
		return err
	}
	fmt.Println(string(result))
	return nil
}
