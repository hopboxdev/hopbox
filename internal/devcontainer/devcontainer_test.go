package devcontainer_test

import (
	"encoding/json"
	"testing"

	"github.com/hopboxdev/hopbox/internal/devcontainer"
)

func TestStripJSONC_Comments(t *testing.T) {
	input := `{
		// this is a comment
		"name": "test", // inline
		/* block
		   comment */
		"image": "ubuntu"
	}`
	got, err := devcontainer.StripJSONC([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	// Should parse as valid JSON.
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("not valid JSON after strip: %v", err)
	}
	if m["name"] != "test" || m["image"] != "ubuntu" {
		t.Errorf("got %v", m)
	}
}

func TestStripJSONC_TrailingCommas(t *testing.T) {
	input := `{"items": ["a", "b",], "key": "val",}`
	got, err := devcontainer.StripJSONC([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, got)
	}
}

func TestStripJSONC_StringsPreserved(t *testing.T) {
	input := `{"url": "https://example.com/path // not a comment"}`
	got, err := devcontainer.StripJSONC([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	if m["url"] != "https://example.com/path // not a comment" {
		t.Errorf("string mangled: %v", m["url"])
	}
}
