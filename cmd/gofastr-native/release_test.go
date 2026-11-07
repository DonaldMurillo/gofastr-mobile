package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nativePkg = "github.com/DonaldMurillo/gofastr-native/examples/notes/notesnative"

// runCLI runs the command and returns exit code, stdout and stderr.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// planText strips the shell quoting and Windows suffixes from a plan.
func planText(s string) string {
	s = strings.ReplaceAll(s, "'", "")
	return strings.ReplaceAll(strings.ReplaceAll(s, ".exe", ""), ".bat", "")
}

func TestReleaseValidationMatrix(t *testing.T) {
	keystore := filepath.Join(t.TempDir(), "upload.jks")
	if err := os.WriteFile(keystore, []byte("not read in a dry run"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFASTR_TEST_STORE_PASS", "unused-password")
	android := []string{"build", "--platform", "android", "--pkg", nativePkg, "--renderer", "native", "--dry-run"}
	signing := []string{"--profile", "release", "--build-number", "3", "--keystore", keystore, "--key-alias", "upload", "--keystore-pass-env", "GOFASTR_TEST_STORE_PASS"}
	ios := []string{"build", "--platform", "ios-device", "--pkg", nativePkg, "--renderer", "native", "--dry-run"}
	iosSigning := []string{"--team-id", "ABCDE12345", "--sign-identity", strings.Repeat("a", 40), "--provisioning-profile", filepath.Join(t.TempDir(), "app.mobileprovision")}
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	without := func(args []string, flag string) []string {
		var out []string
		for i := 0; i < len(args); i++ {
			if args[i] == flag {
				i++
				continue
			}
			out = append(out, args[i])
		}
		return out
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown profile", join(android, []string{"--profile", "beta"}), "--profile must be debug or release"},
		{"simulator release", []string{"build", "--platform", "ios-sim", "--pkg", nativePkg, "--profile", "release", "--build-number", "2", "--dry-run"}, "simulator build is never a release artifact"},
		{"release needs build number", join(android, without(signing, "--build-number")), "requires --build-number"},
		{"build number zero", join(android, []string{"--build-number", "0"}), "--build-number must be an integer"},
		{"build number too large", join(android, []string{"--build-number", "2100000001"}), "--build-number must be an integer"},
		{"build number padded", join(android, []string{"--build-number", "007"}), "--build-number must be an integer"},
		{"32-bit abi", join(android, []string{"--abi", "armeabi-v7a"}), "not supported: 32-bit Go"},
		{"x86 abi", join(android, []string{"--abi", "x86"}), "not supported"},
		{"unknown abi", join(android, []string{"--abi", "mips"}), "want arm64-v8a, x86_64"},
		{"duplicate abi", join(android, []string{"--abi", "x86_64,x86_64"}), "twice"},
		{"bad package", join(android, []string{"--package", "zip"}), "--package must be apk, aab"},
		{"aab in debug", join(android, []string{"--package", "aab"}), "--package aab requires --profile release"},
		{"signing flag in debug", join(android, []string{"--keystore", keystore}), "requires --profile release"},
		{"missing keystore", join(android, without(signing, "--keystore")), "requires --keystore"},
		{"missing alias", join(android, without(signing, "--key-alias")), "requires --key-alias"},
		{"missing pass env", join(android, without(signing, "--keystore-pass-env")), "requires --keystore-pass-env"},
		{"password instead of env name", join(android, without(signing, "--keystore-pass-env"), []string{"--keystore-pass-env", "hunter2!"}), "want an environment variable name"},
		{"debug alias", join(android, without(signing, "--key-alias"), []string{"--key-alias", "androiddebugkey"}), "not androiddebugkey"},
		{"keystore inside out", join(android, signing, []string{"--out", filepath.Dir(keystore)}), "must not live inside --out"},
		{"aab without bundletool", join(android, signing, []string{"--package", "aab"}), "needs bundletool"},
		{"bundletool without aab", join(android, signing, []string{"--bundletool", "bt.jar"}), "--bundletool applies only with --package aab"},
		{"android flag on ios", join(ios, iosSigning, []string{"--abi", "x86_64"}), "applies only to --platform android-emu"},
		{"ios flag on android", join(android, []string{"--team-id", "ABCDE12345"}), "applies only to --platform ios-device"},
		{"ios flag on simulator", []string{"build", "--platform", "ios-sim", "--pkg", nativePkg, "--team-id", "ABCDE12345", "--dry-run"}, "applies only to --platform ios-device"},
		{"device needs team", join(ios, without(iosSigning, "--team-id")), "requires --team-id"},
		{"device bad team", join(ios, without(iosSigning, "--team-id"), []string{"--team-id", "abc"}), "requires --team-id"},
		{"device needs identity", join(ios, without(iosSigning, "--sign-identity")), "requires --sign-identity"},
		{"device needs profile", join(ios, without(iosSigning, "--provisioning-profile")), "requires --provisioning-profile"},
		{"device share needs group", join(ios, iosSigning, []string{"--receive-shares"}), "require --app-group"},
		{"device share needs profile", join(ios, iosSigning, []string{"--receive-shares", "--app-group", "group.dev.gofastr.x"}), "requires --share-provisioning-profile"},
		{"device widget needs profile", join(ios, iosSigning, []string{"--widgets", "--url-scheme", "notes-x", "--app-group", "group.dev.gofastr.x"}), "requires --widget-provisioning-profile"},
		{"share profile without shares", join(ios, iosSigning, []string{"--share-provisioning-profile", "x.mobileprovision"}), "requires --receive-shares"},
		{"device secure storage needs purpose", join(ios, iosSigning, []string{"--secure-storage"}), "requires --biometric-usage"},
		{"device run needs device", append([]string{"run"}, join(ios, iosSigning)[1:]...), "requires --device"},
		{"device screenshot", append(append([]string{"run"}, join(ios, iosSigning)[1:]...), "--device", "X", "--screenshot", "s.png"), "--screenshot is not supported"},
		{"release flags on doctor", []string{"doctor", "--platform", "android-emu", "--profile", "release"}, "applies only to build and run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tc.args...)
			if code != 2 || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d, stderr %q, want exit 2 with %q\n%s", code, stderr, tc.want, stdout)
			}
		})
	}
}

// A release dry run plans signing through environment variables only and
// never prints or writes the password.
func TestAndroidReleaseDryRunKeepsSecretsOut(t *testing.T) {
	const sentinel = "s3cr3t-sentinel-6d1f0a"
	t.Setenv("GOFASTR_TEST_PASS", sentinel)
	t.Setenv("GOFASTR_TEST_KEY_PASS", sentinel+"-key")
	out := t.TempDir()
	keystore := filepath.Join(t.TempDir(), "upload.jks")
	code, stdout, stderr := runCLI(t, "run", "--platform", "android", "--pkg", nativePkg, "--renderer", "native",
		"--profile", "release", "--build-number", "42", "--abi", "x86_64", "--out", out,
		"--keystore", keystore, "--key-alias", "upload", "--keystore-pass-env", "GOFASTR_TEST_PASS", "--key-pass-env", "GOFASTR_TEST_KEY_PASS", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, sentinel) {
		t.Fatal("the password reached the output")
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("a dry run wrote %d entries into --out", len(entries))
	}
	plan := planText(stdout)
	if strings.Contains(strings.ReplaceAll(plan, "pass:env", ""), "pass:") {
		t.Fatalf("release plan passes a password inline:\n%s", plan)
	}
	for _, want := range []string{
		"-storepass:env GOFASTR_TEST_PASS",
		"--ks-pass env:GOFASTR_TEST_PASS --key-pass env:GOFASTR_TEST_KEY_PASS",
		"NOT CHECKED: signing secret presence",
		"bind -target=android/amd64 -ldflags=-s -w -extldflags=-Wl,-z,max-page-size=16384 -trimpath",
		"-tags=gofastr_release",
		"--version-code 42",
		"zipalign -c -P 16 -v 4",
		"apksigner verify --print-certs -Werr",
		"aapt2 dump badging",
		reportName,
		"install -r",
	} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan lacks %q", want)
		}
	}
	if strings.Contains(plan, "android/arm64") || strings.Contains(plan, "debug.keystore") || strings.Contains(plan, "androiddebugkey") {
		t.Errorf("release plan uses the debug key or an unselected ABI:\n%s", plan)
	}
	// The plan checks the key before the slow bind.
	if strings.Index(plan, "-storepass:env") > strings.Index(plan, "gomobile bind") {
		t.Error("the key preflight runs after gomobile bind")
	}
}

func TestAndroidBundleDryRunPlan(t *testing.T) {
	t.Setenv("GOFASTR_TEST_PASS", "unused-password")
	t.Setenv(bundletoolEnv, filepath.Join(t.TempDir(), "bundletool.jar"))
	out := t.TempDir()
	code, stdout, stderr := runCLI(t, "build", "--platform", "android", "--pkg", nativePkg, "--renderer", "native",
		"--profile", "release", "--build-number", "5", "--package", "apk,aab", "--out", out,
		"--keystore", filepath.Join(t.TempDir(), "k.jks"), "--key-alias", "upload", "--keystore-pass-env", "GOFASTR_TEST_PASS", "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	plan := planText(stdout)
	for _, want := range []string{"link --proto-format", "assemble the base bundle module", "BundleConfig.json", "build-bundle --modules=",
		"jarsigner -keystore", "-storepass:env GOFASTR_TEST_PASS -keypass:env GOFASTR_TEST_PASS", "jarsigner -verify", "validate --bundle=", "dump config --bundle=", "Notesnative.aab", "Notesnative.apk"} {
		if !strings.Contains(plan, want) {
			t.Errorf("bundle plan lacks %q", want)
		}
	}
	if strings.Contains(strings.ReplaceAll(plan, "pass:env", ""), "pass:") || strings.Contains(plan, "-sigalg") {
		t.Errorf("bundle plan has an inline password or a fixed signature algorithm:\n%s", plan)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("a dry run wrote %d entries into --out", len(entries))
	}
}

// The debug plan is the one that shipped before profiles: same target,
// same flags, debug key.
func TestDebugProfileKeepsTodaysAndroidPlan(t *testing.T) {
	code, stdout, stderr := runCLI(t, "build", "--platform", "android-emu", "--pkg", notesPkg, "--out", t.TempDir(), "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	plan := planText(stdout)
	for _, want := range []string{"bind -target=android/arm64,android/amd64 -ldflags=-extldflags=-Wl,-z,max-page-size=16384 -androidapi 24",
		"zipalign -p -f 4", "--ks-pass pass:android", "androiddebugkey", "--version-code 1 "} {
		if !strings.Contains(plan, want) {
			t.Errorf("debug plan lacks %q", want)
		}
	}
	for _, absent := range []string{"-trimpath", "-s -w", reportName, "verify --print-certs"} {
		if strings.Contains(plan, absent) {
			t.Errorf("debug plan gained %q", absent)
		}
	}
}

func TestDefaultReleaseOutputIsSeparate(t *testing.T) {
	a := notesApp(t)
	f := addReleaseFlagsForTest(t, "--profile", "release", "--build-number", "9", "--keystore", "k.jks", "--key-alias", "upload", "--keystore-pass-env", "X_PASS")
	_, dir, err := f.resolve(releaseContext{sub: "build", platform: "android-emu", dryRun: true}, &a)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "android-emu-release" {
		t.Fatalf("release out dir = %s", dir)
	}
	if a.VersionCode != 9 || a.Build != "9" || a.Debuggable || a.Profile != profileRelease {
		t.Fatalf("profile not applied: %+v", a)
	}
	a = notesApp(t)
	f = addReleaseFlagsForTest(t)
	_, dir, err = f.resolve(releaseContext{sub: "build", platform: "android-emu", dryRun: true}, &a)
	if err != nil || filepath.Base(dir) != "android-emu" || !a.Debuggable || a.VersionCode != 1 {
		t.Fatalf("debug defaults changed: %s %+v %v", dir, a, err)
	}
}

func addReleaseFlagsForTest(t *testing.T, args ...string) *releaseFlags {
	t.Helper()
	fs := newTestFlagSet()
	f := addReleaseFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestReleaseRendersOmitDebugHooks(t *testing.T) {
	debug := notesApp(t)
	release := debug
	release.Debuggable = false
	for _, tc := range []struct {
		tmpl, hook string
		a          func(app) app
	}{
		{"templates/android/MainActivity.java.tmpl", "setWebContentsDebuggingEnabled", func(a app) app { return a.forAndroid(36) }},
		{"templates/ios/App.swift.tmpl", "isInspectable", func(a app) app { return a.forIOS() }},
	} {
		d, err := render(tc.tmpl, tc.a(debug))
		if err != nil {
			t.Fatal(err)
		}
		r, err := render(tc.tmpl, tc.a(release))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(d, []byte(tc.hook)) || bytes.Contains(r, []byte(tc.hook)) {
			t.Errorf("%s: debug has %s = %t, release has it = %t", tc.tmpl, tc.hook, bytes.Contains(d, []byte(tc.hook)), bytes.Contains(r, []byte(tc.hook)))
		}
	}
	// The simulator eval hook stays compiled out of device builds by Swift.
	r, _ := render("templates/ios/App.swift.tmpl", release.forIOS())
	if !bytes.Contains(r, []byte("#if targetEnvironment(simulator)")) {
		t.Error("the simulator-only eval guard disappeared")
	}
}

func TestDeviceInfoPlistTargetsIPhoneOS(t *testing.T) {
	a := notesApp(t)
	a.applyProfile(debugOptions(), "ios-device")
	for _, tmpl := range []string{"templates/ios/Info.plist.tmpl", "templates/ios/ShareExtension.plist.tmpl", "templates/ios/WidgetExtension.plist.tmpl"} {
		b, err := render(tmpl, a.forIOS())
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, tmpl, b)
		if !bytes.Contains(b, []byte("<string>iPhoneOS</string>")) || bytes.Contains(b, []byte("iPhoneSimulator")) {
			t.Errorf("%s does not target iPhoneOS:\n%s", tmpl, b)
		}
	}
	b, _ := render("templates/ios/Info.plist.tmpl", a.forIOS())
	for _, want := range []string{"<string>iphoneos</string>", "UIRequiredDeviceCapabilities", "<string>arm64</string>"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("device Info.plist lacks %s", want)
		}
	}
	sim, _ := render("templates/ios/Info.plist.tmpl", notesApp(t).forIOS())
	if bytes.Contains(sim, []byte("UIRequiredDeviceCapabilities")) || !bytes.Contains(sim, []byte("<string>iphonesimulator</string>")) {
		t.Error("simulator Info.plist changed")
	}
}

func TestGomobileFlagsPerProfile(t *testing.T) {
	if got := gomobileAndroidTarget([]string{"arm64-v8a", "x86_64"}); got != "-target=android/arm64,android/amd64" {
		t.Fatal(got)
	}
	if got := strings.Join(goLDFlags(false, true), " "); got != "-ldflags=-extldflags=-Wl,-z,max-page-size=16384" {
		t.Fatal(got)
	}
	if got := strings.Join(goLDFlags(true, false), " "); got != "-ldflags=-s -w -trimpath" {
		t.Fatal(got)
	}
	if got := goLDFlags(false, false); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestInsideAndDebugKeystore(t *testing.T) {
	dir := t.TempDir()
	if !inside(dir, filepath.Join(dir, "a", "k.jks")) || !inside(dir, dir) || inside(dir, filepath.Join(filepath.Dir(dir), "other.jks")) {
		t.Fatal("inside")
	}
	if home, err := os.UserHomeDir(); err == nil && !isDebugKeystore(filepath.Join(home, ".android", "debug.keystore")) {
		t.Fatal("the SDK debug keystore path was not recognised")
	}
}

func newTestFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
