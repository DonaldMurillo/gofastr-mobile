package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorModuleProbeDoesNotUpdateManifest(t *testing.T) {
	requireGo(t)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "helper"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":           "module example.com/doctor-readonly\n\ngo 1.22\n\nreplace example.com/helper => ./helper\n",
		"app.go":           "package app\nimport _ \"example.com/helper\"\n",
		"helper/go.mod":    "module example.com/helper\n\ngo 1.22\n",
		"helper/helper.go": "package helper\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := &runner{ctx: context.Background(), dir: dir}
	_, _ = r.query("go", "list", "./...")
	got, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || string(got) != files["go.mod"] {
		t.Fatalf("doctor modified go.mod: %v\n%s", err, got)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.sum")); !os.IsNotExist(err) {
		t.Fatalf("doctor wrote go.sum: %v", err)
	}
}
