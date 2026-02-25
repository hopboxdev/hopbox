package dotenv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hopboxdev/hopbox/internal/dotenv"
)

func TestParseBasicKeyValue(t *testing.T) {
	env, err := dotenv.ParseString("FOO=bar\nBAZ=qux")
	if err != nil {
		t.Fatal(err)
	}
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
	if env["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", env["BAZ"], "qux")
	}
}

func TestParseCommentsAndBlanks(t *testing.T) {
	input := "# this is a comment\nFOO=bar\n\n# another\nBAZ=qux\n"
	env, err := dotenv.ParseString(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 {
		t.Errorf("len = %d, want 2", len(env))
	}
}

func TestParseDoubleQuoted(t *testing.T) {
	env, err := dotenv.ParseString(`KEY="hello world"`)
	if err != nil {
		t.Fatal(err)
	}
	if env["KEY"] != "hello world" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "hello world")
	}
}

func TestParseSingleQuoted(t *testing.T) {
	env, err := dotenv.ParseString(`KEY='literal $value'`)
	if err != nil {
		t.Fatal(err)
	}
	if env["KEY"] != "literal $value" {
		t.Errorf("KEY = %q, want %q", env["KEY"], "literal $value")
	}
}

func TestParseExportPrefix(t *testing.T) {
	env, err := dotenv.ParseString("export FOO=bar")
	if err != nil {
		t.Fatal(err)
	}
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
}

func TestParseEmptyValue(t *testing.T) {
	env, err := dotenv.ParseString("KEY=")
	if err != nil {
		t.Fatal(err)
	}
	if env["KEY"] != "" {
		t.Errorf("KEY = %q, want empty", env["KEY"])
	}
}

func TestParseValueWithEquals(t *testing.T) {
	env, err := dotenv.ParseString("URL=postgres://localhost/db?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if env["URL"] != "postgres://localhost/db?sslmode=disable" {
		t.Errorf("URL = %q, want full URL", env["URL"])
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\nB=2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	env, err := dotenv.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if env["A"] != "1" || env["B"] != "2" {
		t.Errorf("env = %v", env)
	}
}

func TestParseFileNotFound(t *testing.T) {
	_, err := dotenv.ParseFile("/nonexistent/.env")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\nB=2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("B=override\nC=3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	env, n, err := dotenv.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if env["A"] != "1" {
		t.Errorf("A = %q, want %q", env["A"], "1")
	}
	if env["B"] != "override" {
		t.Errorf("B = %q, want %q (should be overridden by .env.local)", env["B"], "override")
	}
	if env["C"] != "3" {
		t.Errorf("C = %q, want %q", env["C"], "3")
	}
	if n != 3 {
		t.Errorf("n = %d, want 3 (total unique vars loaded)", n)
	}
}

func TestLoadDirNoFiles(t *testing.T) {
	dir := t.TempDir()
	env, n, err := dotenv.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 0 || n != 0 {
		t.Errorf("expected empty env, got %v (n=%d)", env, n)
	}
}
