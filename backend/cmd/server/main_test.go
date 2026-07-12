package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectBackendRootFromFindsGoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := detectBackendRootFrom(nested); got != root {
		t.Fatalf("root = %q, want %q", got, root)
	}
}

func TestDetectBackendRootFromSupportsStandaloneBinary(t *testing.T) {
	start := t.TempDir()
	if got := detectBackendRootFrom(start); got != start {
		t.Fatalf("root = %q, want standalone cwd %q", got, start)
	}
}
