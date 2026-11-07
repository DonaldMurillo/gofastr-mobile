package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDXCommandsAreReachable(t *testing.T) {
	for _, command := range []string{"init", "dev", "inspect"} {
		t.Run(command, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(context.Background(), []string{command, "--help"}, &out, &errOut); code != 0 {
				t.Fatalf("exit %d: %s", code, errOut.String())
			}
			if !strings.Contains(out.String()+errOut.String(), "gofastr-native "+command) {
				t.Fatalf("missing command help: %s%s", out.String(), errOut.String())
			}
		})
	}
}

func TestInitCLIDryRunDoesNotWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hello")
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"init", "--out", dir, "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("dry run created output or could not check it: %v", err)
	}
	if !strings.Contains(out.String(), "go.mod") {
		t.Fatalf("missing scaffold plan: %s", out.String())
	}
}

func TestDevGoCLIDryRun(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"dev", "--platform", "go", "--pkg", starterPkg, "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	for _, want := range []string{"go vet", "go build", "No device is used."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
}

func TestDoctorDevicesFlagIsReachable(t *testing.T) {
	var errOut bytes.Buffer
	code := run(context.Background(), []string{"doctor", "--platform", "invalid", "--devices"}, &bytes.Buffer{}, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "doctor --platform must be") {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
}
