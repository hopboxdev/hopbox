package picker

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RunPicker shows a box selection prompt and returns the chosen box name.
// Uses simple text-based I/O that works reliably over SSH without bubbletea.
func RunPicker(boxes []string, in io.Reader, out io.Writer) (string, error) {
	if len(boxes) == 0 {
		return "", fmt.Errorf("no boxes found")
	}

	if len(boxes) == 1 {
		return boxes[0], nil
	}

	fmt.Fprintf(out, "\r\nSelect a box:\r\n\r\n")
	for i, box := range boxes {
		fmt.Fprintf(out, "  %d) %s\r\n", i+1, box)
	}
	fmt.Fprintf(out, "\r\nEnter number (1-%d): ", len(boxes))

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", fmt.Errorf("picker cancelled")
	}

	input := strings.TrimSpace(scanner.Text())
	num, err := strconv.Atoi(input)
	if err != nil || num < 1 || num > len(boxes) {
		return "", fmt.Errorf("invalid selection: %s", input)
	}

	return boxes[num-1], nil
}
