package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var updateStarter = flag.Bool("update-starter", false, "rewrite examples/starter from the app scaffold")

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func runInit(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := initCommand(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestInitRejectsBadInputsAndCreatesNothing(t *testing.T) {
	parent := t.TempDir()
	existing := filepath.Join(parent, "taken")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	native := "--native=" + repoRoot(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{native}, "--out"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--module", "not a path"}, "--module"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--module", nativeModulePath + "/x"}, "--module"},
		{[]string{native, "--out", filepath.Join(parent, "main")}, "--package"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--package", "class"}, "--package"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--package", "func"}, "--package"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--package", "Hello"}, "--package"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--package", "screens"}, "--package"},
		{[]string{native, "--out", filepath.Join(parent, "123")}, "--package"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--id", "dev.gofastr.class"}, "--id"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--id", "nodots"}, "--id"},
		{[]string{native, "--out", filepath.Join(parent, "a"), "--name", "bad/name"}, "--name"},
		{[]string{native, "--out", existing}, "already exists"},
		{[]string{native, "--out", filepath.Join(parent, "missing", "app")}, "parent"},
		{[]string{"--native", filepath.Join(parent, "nope"), "--out", filepath.Join(parent, "a")}, "--native"},
		{[]string{"--native", parent, "--out", filepath.Join(parent, "a")}, "--native"},
	} {
		code, _, stderr := runInit(t, tc.args...)
		if code == 0 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: code %d, stderr %q, want it to name %q", tc.args, code, stderr, tc.want)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 {
		t.Fatalf("rejected inits created entries: %v %v", entries, err)
	}
}

func TestInitDryRunListsFilesAndWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hello")
	code, stdout, stderr := runInit(t, "--out", out, "--native", repoRoot(t), "--dry-run")
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr)
	}
	for _, want := range []string{".gitignore", "README.md", "go.mod", filepath.Join("hello", "hello.go"), filepath.Join("screens", "screens_test.go"), "go mod tidy", "NOT RUN", "example.com/hello"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("dry run output lacks %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Lstat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run created %s", out)
	}
}

func TestInitGoModPinsToolsAndReplace(t *testing.T) {
	s := appScaffold{Module: "example.com/hello", ImportBase: "example.com/hello", Package: "hello", ID: "dev.gofastr.hello", Name: "Hello",
		Native: nativeSource{dir: `C:\src\gofastr-native`}, GoVersion: "1.27.3"}
	files, err := appScaffoldFiles(s)
	if err != nil {
		t.Fatal(err)
	}
	mod := string(files["go.mod"])
	for _, want := range []string{"module example.com/hello", "go 1.27.3", nativeModulePath + " " + placeholderVersion, "=> C:/src/gofastr-native", nativeModulePath + "/cmd/gofastr-native", "golang.org/x/mobile/cmd/gomobile", "golang.org/x/mobile/cmd/gobind"} {
		if !strings.Contains(mod, want) {
			t.Errorf("go.mod lacks %q:\n%s", want, mod)
		}
	}
	s.Native, s.CustomID, s.ID = nativeSource{version: "v0.2.0"}, true, "com.example.hello"
	if files, err = appScaffoldFiles(s); err != nil {
		t.Fatal(err)
	}
	if mod := string(files["go.mod"]); strings.Contains(mod, "replace") || !strings.Contains(mod, nativeModulePath+" v0.2.0") {
		t.Errorf("version mode go.mod:\n%s", mod)
	}
	if readme := string(files["README.md"]); !strings.Contains(readme, "--pkg ./hello --id com.example.hello") {
		t.Errorf("README commands drop the custom id:\n%s", readme)
	}
}

func TestInitRollsBackOnWriteFailure(t *testing.T) {
	s := appScaffold{Module: "example.com/hello", ImportBase: "example.com/hello", Package: "hello", ID: "dev.gofastr.hello", Name: "Hello",
		Native: nativeSource{version: "v0.1.0"}, GoVersion: "1.27"}
	files, err := appScaffoldFiles(s)
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "hello")
	writes := 0
	err = writeScaffold(dir, files, func(path string, content []byte) error {
		if writes++; writes == 5 {
			return errors.New("disk full")
		}
		return writeNewScaffoldFile(path, content)
	})
	if err == nil || writes != 5 {
		t.Fatalf("writes %d, err %v", writes, err)
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 0 {
		t.Fatalf("rollback left %v", entries)
	}
	if err := writeScaffold(dir, map[string][]byte{"../escape.go": nil}, writeNewScaffoldFile); err == nil {
		t.Fatal("a path outside the directory was accepted")
	}
}

// examples/starter is the scaffold rendered for this repository. Run
// go test -run TestStarterExampleMatchesScaffold -update-starter to refresh it.
func TestStarterExampleMatchesScaffold(t *testing.T) {
	files, err := appScaffoldSources(starterScaffold())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repoRoot(t), "examples", "starter")
	for name, want := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if *updateStarter {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update-starter)", err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("%s differs from the scaffold; run go test -run TestStarterExampleMatchesScaffold -update-starter", path)
		}
	}
}

// The generated module compiles, its tests pass, and the pinned CLI plans
// a build of it. Needs the module cache to hold gofastr-native's
// dependencies; it never downloads (GOPROXY=off).
func TestInitScaffoldBuildsAndTests(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: builds a new module")
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	out := filepath.Join(t.TempDir(), "gfnhello")
	code, stdout, stderr := runInit(t, "--out", out, "--native", repoRoot(t), "--name", "GFN Hello")
	goCmd := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("go", args...)
		cmd.Dir = out
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, b)
		}
		return string(b)
	}
	if code != 0 {
		if !strings.Contains(stdout+stderr, "module lookup disabled") {
			t.Fatalf("init: %d\n%s\n%s", code, stdout, stderr)
		}
		// Offline, tidy cannot fetch test-only modules of dependencies; record
		// what the cache holds, as init's failure message suggests.
		t.Logf("offline tidy failed as expected; continuing with go mod tidy -e")
		cmd := exec.Command("go", "mod", "tidy", "-e")
		cmd.Dir = out
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go mod tidy -e: %v\n%s", err, b)
		}
	}
	goCmd("vet", "./...")
	goCmd("test", "./...")
	plan := goCmd("tool", "gofastr-native", "build", "--platform", "android-emu", "--renderer", "native", "--pkg", "./gfnhello", "--name", "GFN Hello", "--dry-run")
	if !strings.Contains(plan, "gomobile") || !strings.Contains(plan, "dev.gofastr.gfnhello") {
		t.Fatalf("dry-run plan:\n%s", plan)
	}

	// The watch set of the new module: its packages and go.mod, the local
	// replace checkout, nothing from the module cache or dist.
	r := &runner{ctx: context.Background(), out: &bytes.Buffer{}, dir: out}
	set, err := computeWatchSet(r, []watchQuery{{dir: out, pattern: "./gfnhello"}}, []string{filepath.Join(out, "dist")})
	if err != nil {
		t.Fatal(err)
	}
	evaluated, _ := filepath.EvalSymlinks(out)
	has := func(list []string, want string) bool {
		for _, item := range list {
			if strings.EqualFold(item, want) {
				return true
			}
		}
		return false
	}
	for _, want := range []string{filepath.Join(evaluated, "screens"), filepath.Join(evaluated, "gfnhello"), filepath.Join(repoRoot(t), "nativeui")} {
		if !has(set.dirs, want) {
			t.Errorf("watch set lacks %s: %v", want, set.dirs)
		}
	}
	if !has(set.files, filepath.Join(evaluated, "go.mod")) {
		t.Errorf("watch set lacks go.mod: %v", set.files)
	}
	cache, _ := exec.Command("go", "env", "GOMODCACHE").Output()
	for _, dir := range set.dirs {
		if pathWithin(strings.TrimSpace(string(cache)), dir) || strings.Contains(dir, string(filepath.Separator)+"dist") {
			t.Errorf("watch set includes %s", dir)
		}
	}
}
