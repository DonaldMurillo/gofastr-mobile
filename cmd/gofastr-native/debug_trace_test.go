package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDebugTraceBindFlags(t *testing.T) {
	if flags, err := debugTraceBindFlags(false, true, "webview"); err != nil || flags != nil {
		t.Fatalf("disabled: %v %v", flags, err)
	}
	if _, err := debugTraceBindFlags(true, true, "native"); !errors.Is(err, errDebugTraceRelease) {
		t.Fatalf("release accepted: %v", err)
	}
	if _, err := debugTraceBindFlags(true, false, "webview"); !errors.Is(err, errDebugTraceRenderer) {
		t.Fatalf("webview accepted: %v", err)
	}
	flags, err := debugTraceBindFlags(true, false, "native")
	if err != nil || !slices.Equal(flags, []string{"-tags=gofastr_debugtrace"}) {
		t.Fatalf("native debug: %v %v", flags, err)
	}
}

func TestReleaseBuildTagExcludesDiagnosticExports(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags="+debugTraceTag+","+inspectTag)
	cmd := exec.Command("go", "list", "-f", `{{join .GoFiles "\n"}}`, "-tags="+releaseTag, "../../examples/gallery/gallerynative")
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, data)
	}
	files := string(data)
	if !strings.Contains(files, "debugtrace_off.go") || strings.Contains(files, "debugtrace_on.go") || strings.Contains(files, "inspect_on.go") {
		t.Fatalf("release selected development-only files: %s", files)
	}
}

func TestDebugTraceCLIBindPlans(t *testing.T) {
	for _, platform := range []string{"android", "ios-sim"} {
		t.Run(platform, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(context.Background(), []string{"build", "--platform", platform, "--pkg", starterPkg, "--renderer", "native", "--debug-trace", "--dry-run"}, &out, &errOut)
			if code != 0 || !strings.Contains(out.String(), "-tags="+debugTraceTag) {
				t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
			}
		})
	}
}

func TestDebugTraceCLIRefusesReleaseBeforeWriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output")
	key := filepath.Join(t.TempDir(), "upload.jks")
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"build", "--platform", "android", "--pkg", starterPkg, "--renderer", "native", "--debug-trace", "--dry-run", "--profile", "release", "--build-number", "2", "--keystore", key, "--key-alias", "upload", "--keystore-pass-env", "GOFASTR_TEST_TRACE_PASSWORD", "--out", dir}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "--profile debug") {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("rejected build created output: %v", err)
	}
}

func TestDebugTraceDevGoChecksUseTag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"dev", "--platform", "go", "--pkg", starterPkg, "--renderer", "native", "--debug-trace", "--test", "--dry-run"}, &out, &errOut)
	if code != 0 || strings.Count(out.String(), "-tags="+debugTraceTag) != 3 {
		t.Fatalf("exit %d: %s\n%s", code, errOut.String(), out.String())
	}
}
