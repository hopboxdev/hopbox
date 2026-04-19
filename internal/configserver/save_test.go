package configserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveHandler_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devcontainer.json")

	handler := SaveHandler(path)
	body, _ := json.Marshal(saveRequest{Content: `{"name":"test"}`})
	req := httptest.NewRequest(http.MethodPost, "/save", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp saveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK {
		t.Errorf("expected OK=true, got errors: %v", resp.Errors)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != `{"name":"test"}` {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestSaveHandler_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devcontainer.json")

	handler := SaveHandler(path)
	body, _ := json.Marshal(saveRequest{Content: `{not json}`})
	req := httptest.NewRequest(http.MethodPost, "/save", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var resp saveResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.OK {
		t.Error("expected OK=false for invalid JSON")
	}
	if len(resp.Errors) == 0 {
		t.Error("expected error messages")
	}

	if _, err := os.Stat(path); err == nil {
		t.Error("file should not exist after invalid JSON save")
	}
}

func TestSaveHandler_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devcontainer.json")

	os.WriteFile(path, []byte(`{"name":"old"}`), 0o644)

	handler := SaveHandler(path)
	body, _ := json.Marshal(saveRequest{Content: `{"name":"new"}`})
	req := httptest.NewRequest(http.MethodPost, "/save", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler(w, req)

	data, _ := os.ReadFile(path)
	if string(data) != `{"name":"new"}` {
		t.Errorf("expected new content, got: %s", data)
	}
}
