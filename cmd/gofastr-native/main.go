// Command gofastr-native builds a GoFastr app into a phone app and runs it
// on the iOS simulator or an Android emulator, from the command line: no
// Xcode project, no Gradle, no IDE. PROTOTYPE.
//
//	gofastr-native doctor [--platform ios-sim|android-emu|ios-device] [--devices]
//	gofastr-native init --out ../myapp
//	gofastr-native dev --platform android-emu|ios-sim|go --pkg <bind package>
//	gofastr-native build  --platform ios-sim|android-emu|ios-device --pkg ./examples/notes/notesmobile [--profile release ...]
//	gofastr-native run    --platform ios-sim|android-emu|ios-device --pkg ./examples/notes/notesmobile [--screenshot shot.png]
//
// Run it from the Go module that holds the bind package (the module must
// require golang.org/x/mobile; this one pins it with a tool directive).
// --dry-run prints every step without running any.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
)

const usage = `gofastr-native builds a GoFastr app into a phone app and runs it.

Usage:
  gofastr-native doctor [--platform ios-sim|android-emu|ios-device] [--devices]
  gofastr-native init   --out <new-directory> [--module <path>] [--package <name>]
                       [--id <id>] [--name <name>] [--native <checkout|version>] [--dry-run]
  gofastr-native dev    --platform android-emu|ios-sim|go --pkg <bind package> [app flags]
                       [--test] [--no-checkpoint] [--poll 500ms] [--settle 400ms] [--dry-run]
  gofastr-native extension check --pkg <Go package> [--dry-run]
  gofastr-native extension init --name <namespace/v1> --out <new-directory> [--kind file|component] [--dry-run]
  gofastr-native inspect --pkg <bind package> [--profile uikit|android|webview|minimal|all]
                        [--route /path] [--depth 4] [--max-screens 200] [--json] [--dry-run]
  gofastr-native build  --platform ios-sim|android-emu|ios-device --pkg <bind package> [flags]
  gofastr-native run    --platform ios-sim|android-emu|ios-device --pkg <bind package> [flags]

Flags (build, run):
  --platform    ios-sim (iOS simulator, needs a Mac with Xcode installed)
                android-emu, or android (Android emulator or attached device,
                needs the Android SDK, NDK and a JDK)
                ios-device (signed iPhone build and IPA, needs a Mac, your
                signing identity and provisioning profiles)
  --pkg         the bind package: webview uses Start, Stop, Handle, Open,
                Bridge; native uses Start, Stop, Root, Render, Dispatch
                (see examples/notes/notesnative)
  --id          bundle / application id (default dev.gofastr.<package>)
  --name        name on the home screen (default: the package name)
  --version     app version (default 0.1.0)
  --out         output directory (default dist/mobile/<platform>)
  --host-template  caller-owned Swift/Java host template (default: WebView host)
  --renderer    webview (default) or native (Go screen definitions; iOS/Android)
  --camera-usage  camera purpose text (default: user-initiated photo capture;
                empty disables native camera access)
  --microphone-usage  optional microphone purpose text; empty disables recording
  --url-scheme  optional application-specific deep-link scheme (native renderer)
  --receive-shares  accept shared text and files into the app's private inbox
  --secure-storage  enable Keychain/Keystore storage and authentication
  --biometric-usage  Face ID purpose text; required for secure storage on iOS
  --widgets     package a home-screen widget; requires --url-scheme
  --app-group   iOS app-group identifier for share and widget extensions
  --dry-run     print every step without running it
  --debug-trace enable redacted Go runtime traces in instrumented native apps
                (debug builds only; see docs/debugging.md)
  --profile     debug (default) or release; see docs/release.md
  --build-number  Android versionCode / iOS CFBundleVersion, 1-2100000000
                (default 1; required with --profile release)
  --abi         Android ABIs: arm64-v8a,x86_64 (default both)
  --package     Android artifacts: apk (default), aab, or apk,aab (release)
  --bundletool  your bundletool.jar for --package aab (or $GOFASTR_BUNDLETOOL)

Flags (Android release signing; passwords stay in environment variables):
  --keystore    your release (upload) keystore; never the debug keystore
  --key-alias   the key's alias in that keystore
  --keystore-pass-env  name of the variable holding the keystore password
  --key-pass-env       name of the variable holding the key password
                (default: --keystore-pass-env)

Flags (ios-device signing):
  --team-id     your 10-character Apple team ID
  --sign-identity  codesigning identity: SHA-1 (recommended) or exact name
  --keychain    keychain holding the identity (optional; never unlocked)
  --provisioning-profile  app profile: development (debug) or ad hoc (release)
  --share-provisioning-profile   profile for <id>.Share (with --receive-shares)
  --widget-provisioning-profile  profile for <id>.Widget (with --widgets)

Flags (run only):
  --device      simulator name or UDID / adb serial or AVD name /
                exact CoreDevice identifier or UDID (required for ios-device)
  --screenshot  write a screenshot here once the app is up
  --wait        how long to wait before the screenshot (default 8s)

Flags (doctor):
  --devices     list attached Android devices and boot state (starts the adb server)

Flags (doctor --platform ios-device only):
  --app         existing signed physical-device .app bundle (never modified)
  --pkg         consuming Go package; check compiled extension manifests/assets
  --device      optional exact CoreDevice identifier or UDID (otherwise iPhone)
  --require     comma-separated camera,microphone,notifications,extensions gates
  --url-scheme  expected application-specific reminder/deep-link scheme
  --dry-run     no commands, temporary reports, or writes; readiness NOT RUN
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "extension":
		return extensionCommand(ctx, rest, stdout, stderr)
	case "init":
		return initCommand(ctx, rest, stdout, stderr)
	case "dev":
		return devCommand(ctx, rest, stdout, stderr)
	case "inspect":
		return inspectCommand(ctx, rest, stdout, stderr)
	}
	fs := flag.NewFlagSet(sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	f := registerBuildFlags(fs)
	rel := addReleaseFlags(fs)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}
	r := &runner{ctx: ctx, out: stdout, dryRun: *f.dry}
	switch sub {
	case "doctor", "build", "run":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", sub, usage)
		return 2
	}
	if name := rel.anyGiven(); name != "" && sub == "doctor" {
		fmt.Fprintf(stderr, "--%s applies only to build and run\n", name)
		return 2
	}
	if code := f.validate(sub, stderr); code != 0 {
		return code
	}
	if sub == "doctor" {
		if *f.platform == "ios-device" {
			return doctorIOSDevice(r, deviceDoctorOptions{app: *f.existingApp, pkg: *f.pkg, device: *f.device, scheme: *f.urlScheme, require: *f.require}, stderr)
		}
		if *f.platform != "" && *f.platform != "ios-sim" && *f.platform != "android-emu" {
			fmt.Fprintln(stderr, "doctor --platform must be ios-sim, android-emu, or ios-device")
			return 2
		}
		return doctorWithOptions(r, doctorOptions{platform: *f.platform, devices: *f.devices})
	}
	a, code := f.resolveApp(r, stderr)
	if code != 0 {
		return code
	}
	release, dir, err := rel.resolve(releaseContext{sub: sub, platform: *f.platform, out: *f.out, device: *f.device, screenshot: *f.shot, dryRun: r.dryRun}, &a)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if _, err := debugTraceBindFlags(a.DebugTrace, release.release(), a.Renderer); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if a.DebugTrace {
		fmt.Fprintln(stdout, "Debug trace enabled for instrumented native bind packages.")
	}
	if *f.shot != "" {
		if *f.shot, err = filepath.Abs(*f.shot); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if !r.dryRun {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	fmt.Fprintf(stdout, "gofastr-native %s: %s (%s) from %s\n", sub, a.Name, a.ID, a.PkgPath)
	opts := runOpts{device: *f.device, screenshot: *f.shot, wait: *f.wait}

	var artifact string
	switch *f.platform {
	case "ios-sim":
		if artifact, err = buildIOS(r, a, dir); err == nil && sub == "run" {
			err = runIOS(r, a.forIOS(), artifact, opts)
		}
	case "ios-device":
		if artifact, err = buildIOSDevice(r, a, dir, release); err == nil && sub == "run" {
			err = runIOSDevice(r, a, artifact, opts)
		}
	case "android-emu":
		if artifact, err = buildAndroidProfile(r, a, dir, release); err == nil && sub == "run" {
			err = runAndroidProfile(r, a, artifact, opts, release)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "\ngofastr-native: %v\n", err)
		if errors.Is(err, context.Canceled) {
			return 130
		}
		return 1
	}
	if r.dryRun {
		fmt.Fprintln(stdout, "\n(dry run: nothing was executed)")
	} else {
		fmt.Fprintf(stdout, "\nBuilt %s\n", artifact)
	}
	return 0
}
