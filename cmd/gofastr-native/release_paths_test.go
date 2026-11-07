package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseInputContainment(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"key.jks", ".keys/upload.jks", "..keys/upload.jks"} {
		if !inside(dir, filepath.Join(dir, filepath.FromSlash(name))) {
			t.Errorf("input inside output was allowed: %s", name)
		}
	}
	if inside(dir, filepath.Join(filepath.Dir(dir), "outside.jks")) {
		t.Fatal("input outside output was classified as inside")
	}
}

func TestReleaseInputContainmentResolvesLinks(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "output")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(out, "upload.jks")
	if err := os.WriteFile(key, []byte("test fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.jks")
	if err := os.Symlink(key, link); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	if !inside(out, link) {
		t.Fatal("linked signing input inside output was allowed")
	}
}
