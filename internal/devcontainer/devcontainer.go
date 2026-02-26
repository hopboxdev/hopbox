package devcontainer

import (
	"regexp"
	"strings"
)

// StripJSONC removes // comments, /* */ comments, and trailing commas from JSONC.
// Preserves strings containing comment-like sequences.
func StripJSONC(data []byte) ([]byte, error) {
	s := string(data)
	var result strings.Builder
	i := 0
	for i < len(s) {
		// String literal — copy verbatim.
		if s[i] == '"' {
			result.WriteByte(s[i])
			i++
			for i < len(s) {
				result.WriteByte(s[i])
				if s[i] == '\\' && i+1 < len(s) {
					i++
					result.WriteByte(s[i])
				} else if s[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}
		// Line comment.
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && (s[i] != '*' || s[i+1] != '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		result.WriteByte(s[i])
		i++
	}

	// Remove trailing commas before } or ].
	re := regexp.MustCompile(`,\s*([}\]])`)
	cleaned := re.ReplaceAllString(result.String(), "$1")
	return []byte(cleaned), nil
}
