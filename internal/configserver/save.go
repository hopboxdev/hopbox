package configserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

type saveRequest struct {
	Content string `json:"content"`
}

type saveResponse struct {
	OK     bool     `json:"ok"`
	Errors []string `json:"errors,omitempty"`
}

// SaveHandler returns an http.HandlerFunc that validates and atomically writes
// devcontainer.json content to devcontainerPath.
func SaveHandler(devcontainerPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(saveResponse{OK: false, Errors: []string{"read body: " + err.Error()}})
			return
		}

		var req saveRequest
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(saveResponse{OK: false, Errors: []string{"invalid request: " + err.Error()}})
			return
		}

		// Validate content is valid JSON
		var parsed interface{}
		if err := json.Unmarshal([]byte(req.Content), &parsed); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(saveResponse{OK: false, Errors: []string{"invalid JSON: " + err.Error()}})
			return
		}

		// Atomic write: temp file in same dir, then rename
		dir := filepath.Dir(devcontainerPath)
		tmp, err := os.CreateTemp(dir, ".devcontainer-*.json.tmp")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(saveResponse{OK: false, Errors: []string{"create temp: " + err.Error()}})
			return
		}
		tmpName := tmp.Name()

		if _, err := fmt.Fprint(tmp, req.Content); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(saveResponse{OK: false, Errors: []string{"write: " + err.Error()}})
			return
		}
		tmp.Close()

		if err := os.Rename(tmpName, devcontainerPath); err != nil {
			os.Remove(tmpName)
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(saveResponse{OK: false, Errors: []string{"rename: " + err.Error()}})
			return
		}

		json.NewEncoder(w).Encode(saveResponse{OK: true})
	}
}
