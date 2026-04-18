package containers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// DefaultBaseImage is the image referenced by the default new-box devcontainer.json.
// Bumped deliberately when the hopbox base image is re-tagged.
const DefaultBaseImage = "ghcr.io/hopboxdev/devcontainer-base:dev"

// BuilderImage is the devcontainers-cli-bundled builder image.
const BuilderImage = "ghcr.io/hopboxdev/builder:dev"

// DefaultDevcontainer returns the JSON bytes of a minimal default box configuration.
func DefaultDevcontainer() []byte {
	return []byte(fmt.Sprintf(`{
  "name": "default",
  "image": %q,
  "remoteUser": "dev",
  "features": {
    "ghcr.io/devcontainers/features/common-utils:2": {
      "username": "dev",
      "uid": "1000",
      "installZsh": false,
      "configureZshAsDefaultShell": false
    }
  }
}
`, DefaultBaseImage))
}

// CanonicalHash returns a 12-hex-char hash derived from the canonical
// (sorted-keys, compact-form) JSON representation of raw. Invalid JSON input
// returns an error.
func CanonicalHash(raw []byte) (string, error) {
	var any interface{}
	if err := json.Unmarshal(raw, &any); err != nil {
		return "", fmt.Errorf("parse json: %w", err)
	}
	canonical, err := canonicalize(any)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:12], nil
}

// ReadDevcontainer reads a devcontainer.json file at path. Returns fs.ErrNotExist
// wrapped if the file does not exist.
func ReadDevcontainer(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// DevcontainerHash reads and returns the canonical hash of the devcontainer.json
// at path. Convenience wrapper over ReadDevcontainer + CanonicalHash.
func DevcontainerHash(path string) (string, error) {
	raw, err := ReadDevcontainer(path)
	if err != nil {
		return "", err
	}
	return CanonicalHash(raw)
}

// canonicalize walks an arbitrary JSON structure and emits JSON bytes with
// map keys sorted at every level. Numbers, strings, bools, and nils pass
// through as standard encoding/json output.
func canonicalize(v interface{}) ([]byte, error) {
	switch vv := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(vv))
		for k := range vv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kb, _ := json.Marshal(k)
			buf = append(buf, kb...)
			buf = append(buf, ':')
			inner, err := canonicalize(vv[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, inner...)
		}
		buf = append(buf, '}')
		return buf, nil
	case []interface{}:
		buf := []byte{'['}
		for i, e := range vv {
			if i > 0 {
				buf = append(buf, ',')
			}
			inner, err := canonicalize(e)
			if err != nil {
				return nil, err
			}
			buf = append(buf, inner...)
		}
		buf = append(buf, ']')
		return buf, nil
	default:
		return json.Marshal(vv)
	}
}
