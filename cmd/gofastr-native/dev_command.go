package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const devUsage = `Usage:
  gofastr-native dev --platform android-emu|ios-sim|go --pkg <bind package> [app flags]
                     [--poll 500ms] [--settle 400ms] [--test] [--no-checkpoint] [--dry-run]

dev builds, installs, and launches the app, then watches the Go packages it
imports. After a change it rebuilds and restarts the app. It is not hot
reload: navigation starts again at the root screen.

  --platform       android-emu or ios-sim (like run), or go: go vet and
                   go build on every change, no device
  --test           also run go test ./... in the module before each build;
                   a failure skips the install
  --no-checkpoint  do not background the app before reinstalling it
  --poll           how often to check files (50ms-10s, default 500ms)
  --settle         quiet time after a change before building (0-10s,
                   default 400ms; at most 3s of continuous changes)
  --dry-run        print one iteration's steps and exit

App and feature flags match build and run, except --screenshot and --wait.
dev always uses debug builds; release profile and signing flags do not apply.
--device pins one device for the whole session.

What persists across a restart: the app data directory, so saved data and
checkpointed drafts. Before reinstalling, dev sends the app to the
background so the renderer checkpoints open editors, then waits 1.5s. That
is best effort: dev cannot confirm the checkpoint finished, so edits newer
than the last completed checkpoint can be lost. A failed build leaves the
installed app alone. dev never uninstalls, clears data, or force-stops.

Changes to the gofastr-native CLI, its bundled renderers, or host templates
are compiled into the running dev and need a dev restart.
`

const (
	devCheckpointWait = 1500 * time.Millisecond
	devMaxSettle      = 3 * time.Second
	signingMismatch   = "INSTALL_FAILED_UPDATE_INCOMPATIBLE"
)

// devCommand is the hook for "gofastr-native dev".
func devCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, devUsage)
		return 0
	}
	fs := flag.NewFlagSet("dev", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, devUsage) }
	f := registerBuildFlags(fs)
	poll := fs.Duration("poll", 500*time.Millisecond, "file check interval")
	settle := fs.Duration("settle", 400*time.Millisecond, "quiet time before a build")
	test := fs.Bool("test", false, "run go test ./... before each build")
	noCheckpoint := fs.Bool("no-checkpoint", false, "do not background the app before reinstalling")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}
	if code := f.validate("dev", stderr); code != 0 {
		return code
	}
	if *poll < 50*time.Millisecond || *poll > 10*time.Second {
		fmt.Fprintln(stderr, "--poll must be between 50ms and 10s")
		return 2
	}
	if *settle < 0 || *settle > 10*time.Second {
		fmt.Fprintln(stderr, "--settle must be between 0 and 10s")
		return 2
	}
	r := &runner{ctx: ctx, out: stdout, dryRun: *f.dry}
	a, code := f.resolveApp(r, stderr)
	if code != 0 {
		return code
	}
	s := &devSession{ctx: ctx, out: stdout, a: a, platform: *f.platform, pkg: *f.pkg, device: *f.device,
		dry: *f.dry, test: *test, checkpoint: !*noCheckpoint}
	gomod, err := readOnlyOutput(r, "find the main module", "go", "env", "GOMOD")
	if gomod = strings.TrimSpace(gomod); err != nil || gomod == "" || gomod == os.DevNull || strings.EqualFold(gomod, "NUL") {
		fmt.Fprintln(stderr, "dev must run inside the Go module that holds --pkg")
		return 2
	}
	s.moduleRoot = filepath.Dir(gomod)
	if s.platform != "go" {
		if s.outDir, err = f.outputDir(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	s.banner()

	if err := s.pinTarget(r); err != nil {
		return s.exit(stderr, err)
	}
	if s.dry {
		if s.platform != "go" {
			s.launched = true // show the checkpoint a rebuild would run
		}
		if err := s.iterate(ctx); err != nil {
			return s.exit(stderr, err)
		}
		fmt.Fprintln(stdout, "\n(dry run: nothing was executed; dev would now watch for changes and repeat from the build step)")
		return 0
	}
	if s.platform != "go" {
		if err := os.MkdirAll(s.outDir, 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	queries := []watchQuery{{dir: "", pattern: s.pkg}}
	if s.test {
		queries = append(queries, watchQuery{dir: s.moduleRoot, pattern: "./...", test: true})
	}
	set, err := computeWatchSet(r, queries, []string{s.outDir})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "watching %d directories and %d files\n", len(set.dirs), len(set.files))
	lastWatchError := ""
	loop := &devLoop{
		poll: *poll, settle: *settle, maxSettle: devMaxSettle,
		now: time.Now, sleep: sleepContext, out: stdout,
		scan:  func() map[string]stamp { return set.fingerprint() },
		build: s.iterate,
		refresh: func(base map[string]stamp) map[string]stamp {
			next, err := computeWatchSet(r, queries, []string{s.outDir})
			if err != nil {
				if message := err.Error(); message != lastWatchError {
					fmt.Fprintf(stdout, "\nkeeping the previous watch set: %v\n", err)
					lastWatchError = message
				}
				return base
			}
			lastWatchError = ""
			if next.equal(set) {
				return base
			}
			base = rebase(base, set, next)
			set = next
			fmt.Fprintf(stdout, "now watching %d directories and %d files\n", len(set.dirs), len(set.files))
			return base
		},
	}
	return s.exit(stderr, loop.Run(ctx))
}

// devSession is the state one dev invocation pins: the app identity, the
// device, and whether anything has been launched yet.
type devSession struct {
	ctx        context.Context
	out        io.Writer
	a          app
	platform   string
	pkg        string
	device     string
	dry        bool
	test       bool
	checkpoint bool
	moduleRoot string
	outDir     string

	sdk       androidSDK
	serial    string // pinned adb serial
	udid      string // pinned simulator
	launched  bool
	iteration int
}

func (s *devSession) banner() {
	fmt.Fprintf(s.out, "gofastr-native dev: %s (%s) from %s on %s\n", s.a.Name, s.a.ID, s.a.PkgPath, s.platform)
	if s.platform == "go" {
		fmt.Fprintln(s.out, "Each change runs go vet and go build"+map[bool]string{true: ", then go test ./...", false: ""}[s.test]+". No device is used.")
		return
	}
	fmt.Fprintln(s.out, "Rebuild and restart, not hot reload: navigation resets to the root screen; saved data persists.")
	if s.checkpoint {
		fmt.Fprintln(s.out, "Before each reinstall the app is sent to the background so open editors checkpoint (best effort: edits newer than the last completed checkpoint can be lost).")
	} else {
		fmt.Fprintln(s.out, "--no-checkpoint: the app is reinstalled without backgrounding it first; unsaved edits since the last checkpoint are lost.")
	}
	fmt.Fprintln(s.out, "A failed build leaves the installed app alone. Changes to the gofastr-native CLI, renderers, or host templates need a dev restart.")
}

// pinTarget resolves the device once. Every iteration then names it
// exactly, so a second emulator or simulator is never picked or booted.
func (s *devSession) pinTarget(r *runner) error {
	switch s.platform {
	case "android-emu":
		sdk, err := findAndroidSDK(r.dryRun)
		if err != nil {
			return fmt.Errorf("%w (run: gofastr-native doctor --platform android-emu)", err)
		}
		serial, err := androidDevice(r, sdk, s.device)
		if err != nil {
			return err
		}
		s.sdk, s.serial = sdk, serial
		fmt.Fprintf(s.out, "\ndevice pinned for this session: %s\n", serial)
	case "ios-sim":
		listing, err := r.outputBytes("list the available simulators", []byte(`{"devices":{}}`), "xcrun", "simctl", "list", "devices", "available", "--json")
		if err != nil {
			return err
		}
		s.udid = "<simulator-udid>"
		if !r.dryRun {
			if s.udid, err = pickSimulator(listing, s.device); err != nil {
				return err
			}
		}
		fmt.Fprintf(s.out, "\nsimulator pinned for this session: %s\n", s.udid)
	}
	return nil
}

// iterate is one build-and-restart, or one round of Go checks.
func (s *devSession) iterate(ctx context.Context) error {
	s.iteration++
	fmt.Fprintf(s.out, "\n== iteration %d ==\n", s.iteration)
	// A fresh runner per iteration: steps renumber, and environment the
	// build appends (PATH, SDK variables) does not accumulate.
	r := &runner{ctx: ctx, out: s.out, dryRun: s.dry}
	if s.platform == "go" {
		return s.goChecks(r)
	}
	if s.test {
		if err := s.goTest(r); err != nil {
			return fmt.Errorf("tests failed; nothing was built or installed: %w", err)
		}
	}
	var artifact string
	var err error
	if s.platform == "android-emu" {
		artifact, err = buildAndroid(r, s.a, s.outDir)
	} else {
		artifact, err = buildIOS(r, s.a, s.outDir)
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("build failed; the installed app was left as it is: %w", err)
	}
	if s.launched && s.checkpoint {
		s.checkpointApp(r)
	}
	if s.platform == "android-emu" {
		return s.installAndroid(ctx, r, artifact)
	}
	return s.installIOS(ctx, r, artifact)
}

func (s *devSession) goChecks(r *runner) error {
	flags, err := debugTraceBindFlags(s.a.DebugTrace, false, s.a.Renderer)
	if err != nil {
		return err
	}
	if err := r.run("vet the bind package", "go", append(append([]string{"vet"}, flags...), s.pkg)...); err != nil {
		return fmt.Errorf("go vet failed: %w", err)
	}
	if err := r.run("build the bind package for this computer", "go", append(append([]string{"build"}, flags...), s.pkg)...); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	if s.test {
		if err := s.goTest(r); err != nil {
			return fmt.Errorf("tests failed: %w", err)
		}
	}
	fmt.Fprintln(s.out, "\nchecks passed")
	return nil
}

func (s *devSession) goTest(r *runner) error {
	tests := *r
	tests.dir = s.moduleRoot
	flags, err := debugTraceBindFlags(s.a.DebugTrace, false, s.a.Renderer)
	if err != nil {
		return err
	}
	err = tests.run("run the module's tests", "go", append(append([]string{"test"}, flags...), "./...")...)
	r.step = tests.step
	return err
}

// checkpointApp backgrounds the app so the renderer checkpoints open
// editors. Nothing confirms that the checkpoint finished, so the wait is
// a heuristic and a failure here never blocks the install.
func (s *devSession) checkpointApp(r *runner) {
	if s.dry {
		fmt.Fprintln(s.out, "\n(on rebuilds only; the first launch has nothing to checkpoint)")
	}
	var err error
	if s.platform == "android-emu" {
		err = r.run("background the app so open editors checkpoint (best effort)", s.sdk.adb, "-s", s.serial,
			"shell", "am", "start", "-W", "-a", "android.intent.action.MAIN", "-c", "android.intent.category.HOME")
	} else {
		err = r.run("background the app so open editors checkpoint (best effort)", "xcrun", "simctl", "launch", s.udid, "com.apple.Preferences")
	}
	if err != nil {
		fmt.Fprintf(s.out, "    (backgrounding failed: %v; installing without a checkpoint)\n", err)
		return
	}
	r.sleep(devCheckpointWait, "for the checkpoint (not confirmed; newer edits can be lost)")
}

func (s *devSession) installAndroid(ctx context.Context, r *runner, apk string) error {
	before, _ := s.androidPackage(ctx)
	marker := &markerWriter{w: r.out, needles: []string{signingMismatch, "INSTALL_FAILED", "INSTALL_PARSE_FAILED"}}
	r.out = marker
	err := runAndroid(r, s.a, apk, runOpts{device: s.serial})
	s.launched = true
	if err == nil {
		fmt.Fprintf(s.out, "\nrestarted %s on %s\n", s.a.ID, s.serial)
		return nil
	}
	if ctx.Err() != nil {
		s.reportInterruptedAndroid(before)
		return ctx.Err()
	}
	if marker.found[signingMismatch] {
		return fmt.Errorf("%w: %s on %s is signed with a different key. Only a manual uninstall fixes that, and it deletes the app's data, so dev does not do it. To keep the data, build with the key that signed the installed app; otherwise run: %s -s %s uninstall %s",
			errStopDev, s.a.ID, s.serial, s.sdk.adb, s.serial, s.a.ID)
	}
	if marker.found["INSTALL_FAILED"] || marker.found["INSTALL_PARSE_FAILED"] {
		return fmt.Errorf("the package manager rejected the install, so the previously installed build is still in place. dev does not retry; save a change to try a new build: %w", err)
	}
	return fmt.Errorf("install or launch did not complete and its outcome is unknown. dev does not retry it. Check with: %s -s %s shell dumpsys package %s (%w)", s.sdk.adb, s.serial, s.a.ID, err)
}

// androidPackageState is what dumpsys reports about the app's package.
type androidPackageState struct {
	present    bool
	lastUpdate string
}

// androidPackage reads the installed package's update time. Read-only.
func (s *devSession) androidPackage(ctx context.Context) (androidPackageState, error) {
	if s.dry {
		return androidPackageState{}, nil
	}
	query := runner{ctx: ctx}
	out, err := query.quietOutput(s.sdk.adb, "-s", s.serial, "shell", "dumpsys", "package", s.a.ID)
	if err != nil {
		return androidPackageState{}, err
	}
	return parseAndroidPackage(out, s.a.ID), nil
}

func parseAndroidPackage(dumpsys, id string) androidPackageState {
	state := androidPackageState{}
	scanner := bufio.NewScanner(strings.NewReader(dumpsys))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Package ["+id+"]") {
			state.present = true
		}
		if value, ok := strings.CutPrefix(line, "lastUpdateTime="); ok && state.present && state.lastUpdate == "" {
			state.lastUpdate = value
		}
	}
	return state
}

// reportInterruptedAndroid runs after Ctrl+C during install or launch. The
// outcome is unknown until this read-only check, and the check is only a
// snapshot: adb was stopped, but the device may still finish an install it
// had already received.
func (s *devSession) reportInterruptedAndroid(before androidPackageState) {
	fmt.Fprintf(s.out, "\nInterrupted during install or launch on %s. Outcome UNKNOWN until checked; the device may still finish an install it already received.\n", s.serial)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	after, err := s.androidPackage(ctx)
	switch {
	case err != nil:
		fmt.Fprintf(s.out, "Read-only check failed (%v). Check with: %s -s %s shell dumpsys package %s\n", err, s.sdk.adb, s.serial, s.a.ID)
	case !after.present:
		fmt.Fprintf(s.out, "Read-only check: %s is not installed on %s right now.\n", s.a.ID, s.serial)
	case before.present && after.lastUpdate == before.lastUpdate:
		fmt.Fprintf(s.out, "Read-only check: the package manager still reports the earlier install (lastUpdateTime=%s).\n", after.lastUpdate)
	default:
		fmt.Fprintf(s.out, "Read-only check: the package manager reports an install at lastUpdateTime=%s, so the new build was installed. It may not have been launched.\n", after.lastUpdate)
	}
	fmt.Fprintln(s.out, "dev retried nothing and did not uninstall, clear data, or force-stop.")
}

func (s *devSession) installIOS(ctx context.Context, r *runner, appDir string) error {
	err := runIOS(r, s.a.forIOS(), appDir, runOpts{device: s.udid})
	s.launched = true
	if err == nil {
		fmt.Fprintf(s.out, "\nrestarted %s on %s\n", s.a.ID, s.udid)
		return nil
	}
	if ctx.Err() != nil {
		fmt.Fprintf(s.out, "\nInterrupted during install or launch on simulator %s. Outcome UNKNOWN until checked.\n", s.udid)
		check, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		query := runner{ctx: check}
		if path, err := query.quietOutput("xcrun", "simctl", "get_app_container", s.udid, s.a.ID, "app"); err == nil {
			fmt.Fprintf(s.out, "Read-only check: an app with this id is installed at %s; which build it is is not known.\n", strings.TrimSpace(path))
		} else {
			fmt.Fprintf(s.out, "Read-only check: no installed app with id %s was found (%v).\n", s.a.ID, err)
		}
		fmt.Fprintln(s.out, "dev retried nothing and did not uninstall or erase the simulator.")
		return ctx.Err()
	}
	return fmt.Errorf("install or launch failed; dev does not retry it. Save a change to try a new build: %w", err)
}

func (s *devSession) exit(stderr io.Writer, err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(stderr, "\ngofastr-native dev: stopped")
		return 130
	default:
		fmt.Fprintf(stderr, "\ngofastr-native dev: %v\n", err)
		return 1
	}
}

// markerWriter passes output through and records which needles appeared,
// including ones split across writes.
type markerWriter struct {
	w       io.Writer
	needles []string
	found   map[string]bool
	tail    []byte
}

func (m *markerWriter) Write(p []byte) (int, error) {
	if m.found == nil {
		m.found = map[string]bool{}
	}
	window := append(append([]byte(nil), m.tail...), p...)
	longest := 0
	for _, needle := range m.needles {
		if bytes.Contains(window, []byte(needle)) {
			m.found[needle] = true
		}
		longest = max(longest, len(needle))
	}
	if keep := longest - 1; len(window) > keep {
		m.tail = append(m.tail[:0], window[len(window)-keep:]...)
	} else {
		m.tail = append(m.tail[:0], window...)
	}
	return m.w.Write(p)
}
