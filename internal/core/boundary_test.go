package core_test

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

// Packages that must never appear in internal/core's import graph.
var banned = []string{
	"github.com/docker/docker",
	"modernc.org/sqlite",
	"github.com/hashicorp/yamux",
	"github.com/creack/pty",
}

func TestCoreHasNoProviderSDKImports(t *testing.T) {
	corePkgs := []string{
		"workspace", "ports", "store", "store/sqlite", "reconciler",
	}
	for _, rel := range corePkgs {
		dir := filepath.Join(".", rel)
		pkg, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("import %s: %v", rel, err)
		}
		// include test imports too, EXCEPT we allow the store/sqlite package itself
		// to import the sqlite driver (it is the driver's home; it imports no docker/yamux/pty).
		imports := append([]string{}, pkg.Imports...)
		for _, imp := range imports {
			for _, b := range banned {
				if strings.HasPrefix(imp, b) {
					if rel == "store/sqlite" && b == "modernc.org/sqlite" {
						continue // the driver lives here by design
					}
					t.Errorf("internal/core/%s imports banned provider SDK %q", rel, imp)
				}
			}
		}
	}
}
