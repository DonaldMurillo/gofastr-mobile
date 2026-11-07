package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTree is a watched file system on a fake clock. Each sleep advances
// the clock and then runs tick, which edits files the way an editor would.
type fakeTree struct {
	t       *testing.T
	clock   time.Time
	files   map[string]stamp
	tick    func(now time.Duration)
	start   time.Time
	cancel  context.CancelFunc
	limit   time.Duration
	builds  []time.Duration
	edits   int
	onBuild func(n int) error
}

func newFakeTree(t *testing.T) (*fakeTree, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	start := time.Unix(1_000_000, 0)
	return &fakeTree{t: t, clock: start, start: start, files: map[string]stamp{"a.go": {size: 1}}, cancel: cancel, limit: 10 * time.Second}, ctx
}

func (f *fakeTree) touch(path string) {
	f.edits++
	s := f.files[path]
	s.size++
	s.mod = f.clock.UnixNano()
	f.files[path] = s
}

func (f *fakeTree) loop(out *bytes.Buffer, poll, settle time.Duration) *devLoop {
	return &devLoop{
		poll: poll, settle: settle, maxSettle: devMaxSettle, out: out,
		now: func() time.Time { return f.clock },
		sleep: func(ctx context.Context, d time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			f.clock = f.clock.Add(d)
			if f.clock.Sub(f.start) > f.limit {
				f.cancel()
				return context.Canceled
			}
			if f.tick != nil {
				f.tick(f.clock.Sub(f.start))
			}
			return nil
		},
		scan: func() map[string]stamp {
			copied := make(map[string]stamp, len(f.files))
			for k, v := range f.files {
				copied[k] = v
			}
			return copied
		},
		build: func(ctx context.Context) error {
			f.builds = append(f.builds, f.clock.Sub(f.start))
			if f.onBuild != nil {
				return f.onBuild(len(f.builds))
			}
			return nil
		},
	}
}

func TestDevLoopCoalescesABurstIntoOneBuild(t *testing.T) {
	f, ctx := newFakeTree(t)
	f.tick = func(now time.Duration) {
		if f.edits < 50 {
			f.touch("a.go")
		}
	}
	var out bytes.Buffer
	err := f.loop(&out, 10*time.Millisecond, 100*time.Millisecond).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
	if f.edits != 50 || len(f.builds) != 2 {
		t.Fatalf("%d edits caused %d builds (%v), want the initial build and one more", f.edits, len(f.builds), f.builds)
	}
	if !strings.Contains(out.String(), "changed: a.go") {
		t.Errorf("output does not name the change:\n%s", out.String())
	}
}

func TestDevLoopEditsDuringABuildCauseExactlyOneFollowUp(t *testing.T) {
	f, ctx := newFakeTree(t)
	f.tick = func(now time.Duration) {
		if f.edits == 0 {
			f.touch("a.go")
		}
	}
	f.onBuild = func(n int) error {
		if n == 2 {
			// Three saves, to two files, while the build runs.
			f.touch("a.go")
			f.touch("b.go")
			f.touch("a.go")
		}
		return nil
	}
	err := f.loop(&bytes.Buffer{}, 10*time.Millisecond, 50*time.Millisecond).Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(f.builds) != 3 {
		t.Fatalf("builds at %v, want 3: initial, the first edit, one follow-up", f.builds)
	}
}

func TestDevLoopKeepsWatchingAfterAFailedBuild(t *testing.T) {
	f, ctx := newFakeTree(t)
	f.tick = func(now time.Duration) {
		if now == time.Second || now == 3*time.Second {
			f.touch("a.go")
		}
	}
	f.onBuild = func(n int) error {
		if n == 2 {
			return errors.New("build failed; the installed app was left as it is")
		}
		return nil
	}
	var out bytes.Buffer
	if err := f.loop(&out, 10*time.Millisecond, 50*time.Millisecond).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(f.builds) != 3 {
		t.Fatalf("builds at %v, want 3", f.builds)
	}
	if !strings.Contains(out.String(), "build failed") || strings.Count(out.String(), "watching for changes") != 3 {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestDevLoopBuildsWithinTheSettleCapUnderConstantChanges(t *testing.T) {
	f, ctx := newFakeTree(t)
	f.limit = 5 * time.Second
	f.tick = func(now time.Duration) {
		if now%(100*time.Millisecond) == 0 {
			f.touch("a.go") // a file rewritten every 100ms, forever
		}
	}
	if err := f.loop(&bytes.Buffer{}, 50*time.Millisecond, 400*time.Millisecond).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(f.builds) < 2 {
		t.Fatalf("constant changes starved the build: %v", f.builds)
	}
	// The first change lands at 100ms; the cap is 3s plus one poll.
	if got := f.builds[1]; got > 100*time.Millisecond+devMaxSettle+50*time.Millisecond {
		t.Fatalf("second build at %v, after the 3s settle cap", got)
	}
}

func TestDevLoopStopsOnErrStopDevAndCancellation(t *testing.T) {
	f, ctx := newFakeTree(t)
	f.onBuild = func(int) error { return errStopDev }
	if err := f.loop(&bytes.Buffer{}, 10*time.Millisecond, 0).Run(ctx); !errors.Is(err, errStopDev) {
		t.Fatalf("Run returned %v, want errStopDev", err)
	}

	f, ctx = newFakeTree(t)
	f.onBuild = func(int) error { f.cancel(); return errors.New("interrupted") }
	if err := f.loop(&bytes.Buffer{}, 10*time.Millisecond, 0).Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context.Canceled", err)
	}
	if len(f.builds) != 1 {
		t.Fatalf("builds after cancel: %v", f.builds)
	}
}

func TestRebaseKeepsEditsMadeDuringTheBuild(t *testing.T) {
	dir, moduleDir := t.TempDir(), t.TempDir()
	write := func(name, content string) {
		path := filepath.Join(dir, name)
		if name == "go.mod" {
			path = filepath.Join(moduleDir, name) // only the refreshed set watches it
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a")
	old := watchSet{dirs: []string{dir}}
	base := old.fingerprint()
	write("a.go", "package a // edited during the build")
	write("b.go", "package a")
	next := watchSet{dirs: []string{dir}, files: []string{filepath.Join(moduleDir, "go.mod")}}
	write("go.mod", "module a")

	adjusted := rebase(base, old, next)
	if sameFingerprint(adjusted, next.fingerprint()) {
		t.Fatal("edits made during the build were absorbed by the refresh")
	}
	changed := strings.Join(changedPaths(adjusted, next.fingerprint(), 10), " ")
	if !strings.Contains(changed, "a.go") || !strings.Contains(changed, "b.go") || strings.Contains(changed, "go.mod") {
		t.Fatalf("changed after rebase: %s", changed)
	}
}

func TestMarkerWriterFindsNeedlesSplitAcrossWrites(t *testing.T) {
	var sink bytes.Buffer
	m := &markerWriter{w: &sink, needles: []string{signingMismatch, "INSTALL_FAILED"}}
	for _, chunk := range []string{"Failure [INSTALL_FAI", "LED_UPDATE_INCOMP", "ATIBLE: signatures do not match]\n"} {
		if _, err := m.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if !m.found[signingMismatch] || !m.found["INSTALL_FAILED"] {
		t.Fatalf("found %v", m.found)
	}
	if !strings.Contains(sink.String(), "signatures do not match") {
		t.Fatal("output was not passed through")
	}
}

func TestParseAndroidPackage(t *testing.T) {
	dumpsys := "Packages:\n  Package [dev.gofastr.other] (1):\n    lastUpdateTime=2026-01-01 00:00:00\n  Package [dev.gofastr.hello] (2):\n    versionCode=1\n    lastUpdateTime=2026-11-06 10:00:00\n"
	if got := parseAndroidPackage(dumpsys, "dev.gofastr.hello"); !got.present || got.lastUpdate != "2026-11-06 10:00:00" {
		t.Fatalf("parsed %+v", got)
	}
	if got := parseAndroidPackage("Unable to find package: dev.gofastr.hello\n", "dev.gofastr.hello"); got.present {
		t.Fatalf("parsed %+v for a missing package", got)
	}
}

func runDev(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := devCommand(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

const starterPkg = "../../examples/starter/starter"

func TestDevDryRunPlans(t *testing.T) {
	forbidden := []string{"uninstall", "pm clear", "force-stop", "simctl erase", "simctl uninstall"}
	for _, tc := range []struct {
		name    string
		args    []string
		want    []string
		without []string
	}{
		{"android", []string{"--platform", "android-emu", "--renderer", "native"},
			[]string{"device pinned for this session: emulator-5554", "android.intent.category.HOME", "install -r", "am start -W -n dev.gofastr.starter/.MainActivity", "best effort"}, nil},
		{"android without checkpoint", []string{"--platform", "android-emu", "--renderer", "native", "--no-checkpoint"},
			[]string{"install -r"}, []string{"android.intent.category.HOME"}},
		{"ios", []string{"--platform", "ios-sim", "--renderer", "native"},
			[]string{"simulator pinned for this session", "com.apple.Preferences", "simctl install", "launch --terminate-running-process"}, nil},
		{"go", []string{"--platform", "go", "--test"},
			[]string{"go vet", "go build", "go test ./..."}, []string{"install", "gomobile"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "dist")
			args := append(append([]string{}, tc.args...), "--pkg", starterPkg, "--out", out, "--dry-run")
			code, stdout, stderr := runDev(t, args...)
			if code != 0 {
				t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("plan lacks %q", want)
				}
			}
			for _, bad := range append(append([]string{}, forbidden...), tc.without...) {
				if strings.Contains(stdout, bad) {
					t.Errorf("plan contains %q", bad)
				}
			}
			if _, err := os.Lstat(out); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("dry run created %s", out)
			}
			if t.Failed() {
				t.Log(stdout)
			}
		})
	}
}

func TestDevRejectsBadFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--platform", "ios-device", "--pkg", starterPkg}, "dev --platform"},
		{[]string{"--platform", "android-emu", "--pkg", starterPkg, "--screenshot", "x.png"}, "--screenshot"},
		{[]string{"--platform", "android-emu", "--pkg", starterPkg, "--wait", "1s"}, "--wait"},
		{[]string{"--platform", "android-emu", "--pkg", starterPkg, "--devices"}, "--devices"},
		{[]string{"--platform", "android-emu", "--pkg", starterPkg, "--poll", "1ms"}, "--poll"},
		{[]string{"--platform", "android-emu", "--pkg", starterPkg, "--settle", "1m"}, "--settle"},
		{[]string{"--platform", "android-emu"}, "--pkg"},
		{[]string{"--platform", "android-emu", "--pkg", starterPkg, "extra"}, "unexpected"},
	} {
		code, _, stderr := runDev(t, tc.args...)
		if code != 2 || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: code %d, stderr %q", tc.args, code, stderr)
		}
	}
}
