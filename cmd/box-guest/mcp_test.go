package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func dispatchJSON(t *testing.T, ts []tool, byName map[string]tool, method, id, params string) string {
	t.Helper()
	req := rpcRequest{JSONRPC: "2.0", Method: method, ID: json.RawMessage(id)}
	if params != "" {
		req.Params = json.RawMessage(params)
	}
	b, _ := json.Marshal(dispatch(req, ts, byName))
	return string(b)
}

func TestMCPServer(t *testing.T) {
	keepAliveHit := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"box","idle":false}`))
	})
	mux.HandleFunc("POST /v1/me/keep-alive", func(w http.ResponseWriter, _ *http.Request) {
		keepAliveHit = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("BOX_META", srv.URL)

	ts := tools(base())
	byName := map[string]tool{}
	for _, tl := range ts {
		byName[tl.Name] = tl
	}

	// initialize -> advertises the protocol version + tools capability.
	if got := dispatchJSON(t, ts, byName, "initialize", "1", ""); !strings.Contains(got, mcpProtocol) || !strings.Contains(got, "box-guest") {
		t.Fatalf("initialize: %s", got)
	}

	// tools/list -> the four lifecycle tools.
	list := dispatchJSON(t, ts, byName, "tools/list", "2", "")
	for _, name := range []string{"box_info", "box_keep_alive", "box_auto_suspend", "box_set_idle"} {
		if !strings.Contains(list, name) {
			t.Fatalf("tools/list missing %s: %s", name, list)
		}
	}

	// tools/call box_info -> returns the metadata as text content.
	info := dispatchJSON(t, ts, byName, "tools/call", "3", `{"name":"box_info","arguments":{}}`)
	if !strings.Contains(info, `\"name\":\"box\"`) || strings.Contains(info, `"isError":true`) {
		t.Fatalf("box_info: %s", info)
	}

	// tools/call box_keep_alive -> hits the endpoint.
	ka := dispatchJSON(t, ts, byName, "tools/call", "4", `{"name":"box_keep_alive","arguments":{"duration":"10m"}}`)
	if !keepAliveHit || strings.Contains(ka, `"isError":true`) {
		t.Fatalf("box_keep_alive hit=%v resp=%s", keepAliveHit, ka)
	}

	// unknown method -> JSON-RPC error.
	if got := dispatchJSON(t, ts, byName, "bogus/method", "5", ""); !strings.Contains(got, "-32601") {
		t.Fatalf("expected method-not-found: %s", got)
	}
}
