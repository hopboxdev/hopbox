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
	"github.com/mesadev/mesa/gen/mesa/provider",
	"k8s.io/api",
	"k8s.io/apimachinery",
	"k8s.io/client-go",
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
		// Check production AND test imports (in-package _test.go via TestImports,
		// external _test package via XTestImports). The only allowed banned import
		// is the sqlite driver inside store/sqlite (it is the driver's home).
		var imports []string
		imports = append(imports, pkg.Imports...)
		imports = append(imports, pkg.TestImports...)
		imports = append(imports, pkg.XTestImports...)
		for _, imp := range imports {
			for _, b := range banned {
				// match the exact module path or a subpackage of it, so a
				// hypothetical "modernc.org/sqlitexyz" can't false-positive.
				if imp == b || strings.HasPrefix(imp, b+"/") {
					if rel == "store/sqlite" && b == "modernc.org/sqlite" {
						continue // the driver lives here by design
					}
					t.Errorf("internal/core/%s imports banned provider SDK %q (in prod or test)", rel, imp)
				}
			}
		}
	}
}
