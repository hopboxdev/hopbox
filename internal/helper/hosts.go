package helper

import (
	"fmt"
	"os"
	"strings"
)

const (
	markerStart = "# --- hopbox managed start ---"
	markerEnd   = "# --- hopbox managed end ---"
)

// AddHostEntry adds an IP->hostname mapping to the managed section of the
// hosts file. Creates the managed section if it doesn't exist. Idempotent.
func AddHostEntry(path, ip, hostname string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)
	entry := ip + " " + hostname

	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)

	if startIdx == -1 || endIdx == -1 {
		// No managed section — append one.
		content = strings.TrimRight(content, "\n") + "\n" +
			markerStart + "\n" + entry + "\n" + markerEnd + "\n"
	} else {
		// Extract existing managed entries.
		sectionStart := startIdx + len(markerStart) + 1
		section := content[sectionStart:endIdx]
		lines := strings.Split(strings.TrimRight(section, "\n"), "\n")

		// Check if entry already exists.
		for _, line := range lines {
			if strings.TrimSpace(line) == entry {
				return nil // already present
			}
		}

		// Remove any existing entry for this hostname (update case).
		var kept []string
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == hostname {
				continue
			}
			if strings.TrimSpace(line) != "" {
				kept = append(kept, line)
			}
		}
		kept = append(kept, entry)

		newSection := strings.Join(kept, "\n")
		content = content[:startIdx] + markerStart + "\n" + newSection + "\n" + markerEnd + "\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
}

// RemoveHostEntry removes a hostname from the managed section. Removes the
// entire managed section if it becomes empty.
func RemoveHostEntry(path, hostname string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)

	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)
	if startIdx == -1 || endIdx == -1 {
		return nil // no managed section
	}

	sectionStart := startIdx + len(markerStart) + 1
	section := content[sectionStart:endIdx]
	lines := strings.Split(strings.TrimRight(section, "\n"), "\n")

	var kept []string
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == hostname {
			continue
		}
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}

	if len(kept) == 0 {
		// Remove entire managed section.
		content = content[:startIdx]
	} else {
		newSection := strings.Join(kept, "\n")
		content = content[:startIdx] + markerStart + "\n" + newSection + "\n" + markerEnd + "\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
}
