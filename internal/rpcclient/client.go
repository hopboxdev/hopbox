package rpcclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

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

// Call makes an RPC call using the host's .hop hostname.
func Call(hostName, method string, params any) (json.RawMessage, error) {
	if hostName == "" {
		return nil, fmt.Errorf("--host <name> required")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://%s.hop:%d/rpc", hostName, tunnel.AgentAPIPort)
	return doRPC(client, url, method, params)
}

// CallWithClient makes an RPC call using the provided HTTP client and agent IP.
// Used by hop to for temporary netstack tunnels where the agent is reached via
// DialContext rather than the OS network stack.
func CallWithClient(client *http.Client, agentIP, method string, params any) (json.RawMessage, error) {
	url := fmt.Sprintf("http://%s:%d/rpc", agentIP, tunnel.AgentAPIPort)
	return doRPC(client, url, method, params)
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

// CopyTo sends an RPC request and copies the plain-text response body to dst.
// Used for streaming endpoints (e.g. logs.stream) that write text/plain instead
// of wrapping output in a JSON envelope.
func CopyTo(hostName, method string, params any, dst io.Writer) error {
	if hostName == "" {
		return fmt.Errorf("--host <name> required")
	}
	reqBody, _ := json.Marshal(map[string]any{"method": method, "params": params})
	url := fmt.Sprintf("http://%s.hop:%d/rpc", hostName, tunnel.AgentAPIPort)

	client := &http.Client{}
	resp, err := client.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("RPC call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var rpcResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &rpcResp) == nil && rpcResp.Error != "" {
			return fmt.Errorf("RPC error: %s", rpcResp.Error)
		}
		return fmt.Errorf("RPC error: HTTP %d", resp.StatusCode)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}
