// Package dotenv parses .env files into key-value maps.
package dotenv

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseString parses dotenv-formatted text into a map.
func ParseString(s string) (map[string]string, error) {
	env := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := line[idx+1:]
		val = unquote(val)
		env[key] = val
	}
	return env, scanner.Err()
}

// unquote removes surrounding double or single quotes from a value.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ParseFile reads and parses a .env file.
func ParseFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseString(string(data))
}

// LoadDir loads .env and .env.local from dir, merging them (.env.local wins).
// Returns the merged map and the total number of unique variables loaded.
// Missing files are silently skipped.
func LoadDir(dir string) (map[string]string, int, error) {
	merged := make(map[string]string)
	for _, name := range []string{".env", ".env.local"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		env, err := ParseFile(path)
		if err != nil {
			return nil, 0, err
		}
		for k, v := range env {
			merged[k] = v
		}
	}
	return merged, len(merged), nil
}
