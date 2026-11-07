package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// buildFlags is the one parser for the flags doctor, build, run, and dev
// share. Build, run, and dev use the same app and feature validation.
type buildFlags struct {
	fs *flag.FlagSet

	platform, pkg, id, name, version, out, hostTemplate, renderer   *string
	cameraUsage, microphoneUsage, urlScheme, biometricUsage         *string
	appGroup, device, shot, existingApp, require                    *string
	receiveShares, secureStorage, widgets, dry, devices, debugTrace *bool
	wait                                                            *time.Duration
}

func registerBuildFlags(fs *flag.FlagSet) *buildFlags {
	return &buildFlags{
		fs:              fs,
		platform:        fs.String("platform", "", "ios-sim, android-emu (or android), ios-device; dev also accepts go"),
		pkg:             fs.String("pkg", "", "bind package"),
		id:              fs.String("id", "", "bundle / application id"),
		name:            fs.String("name", "", "home-screen name"),
		version:         fs.String("version", "", "app version"),
		out:             fs.String("out", "", "output directory"),
		hostTemplate:    fs.String("host-template", "", "caller-owned native host template"),
		renderer:        fs.String("renderer", "webview", "webview or native (iOS/Android)"),
		cameraUsage:     fs.String("camera-usage", defaultCameraUsage, "iOS camera purpose string; empty disables native camera"),
		microphoneUsage: fs.String("microphone-usage", "", "iOS microphone purpose string; empty disables recording"),
		urlScheme:       fs.String("url-scheme", "", "application-specific deep-link scheme (native renderer)"),
		receiveShares:   fs.Bool("receive-shares", false, "enable the private incoming-share inbox"),
		secureStorage:   fs.Bool("secure-storage", false, "enable secure storage and authentication"),
		biometricUsage:  fs.String("biometric-usage", "", "Face ID purpose text"),
		widgets:         fs.Bool("widgets", false, "package a home-screen widget"),
		appGroup:        fs.String("app-group", "", "iOS app-group identifier"),
		dry:             fs.Bool("dry-run", false, "print the steps only"),
		debugTrace:      fs.Bool("debug-trace", false, "enable redacted tracing in instrumented debug native apps"),
		device:          fs.String("device", "", "simulator / device / AVD"),
		shot:            fs.String("screenshot", "", "screenshot path"),
		existingApp:     fs.String("app", "", "existing signed iOS device app (doctor only)"),
		require:         fs.String("require", "", "required iOS device features (doctor only)"),
		devices:         fs.Bool("devices", false, "list attached devices (doctor only; starts the adb server)"),
		wait:            fs.Duration("wait", 8*time.Second, "wait before the screenshot"),
	}
}

func (f *buildFlags) given(name string) bool {
	set := false
	f.fs.Visit(func(fl *flag.Flag) { set = set || fl.Name == name })
	return set
}

// validate runs the checks that need no go tool or file system beyond
// --host-template. It returns 0 when the flags are usable, else the exit
// code after printing the reason. sub is doctor, build, run, or dev. It
// rewrites the android alias to android-emu, as main.go does.
func (f *buildFlags) validate(sub string, stderr io.Writer) int {
	*f.platform = normalizePlatform(*f.platform)
	if (*f.existingApp != "" || *f.require != "") && (sub != "doctor" || *f.platform != "ios-device") {
		fmt.Fprintln(stderr, "--app and --require are only supported by doctor --platform ios-device")
		return 2
	}
	if *f.devices && sub != "doctor" {
		fmt.Fprintln(stderr, "--devices is only supported by doctor")
		return 2
	}
	if sub == "doctor" {
		if *f.debugTrace {
			fmt.Fprintln(stderr, "--debug-trace applies only to build, run, and dev")
			return 2
		}
		return 0
	}
	switch {
	case sub == "dev":
		if *f.platform != "ios-sim" && *f.platform != "android-emu" && *f.platform != "go" {
			fmt.Fprintln(stderr, "dev --platform must be android-emu, ios-sim, or go")
			return 2
		}
		for _, name := range []string{"screenshot", "wait"} {
			if f.given(name) {
				fmt.Fprintf(stderr, "dev does not take --%s; use run for a one-off screenshot\n", name)
				return 2
			}
		}
	case *f.platform != "ios-sim" && *f.platform != "android-emu" && *f.platform != "ios-device":
		fmt.Fprintln(stderr, "--platform must be ios-sim, android-emu (or android), or ios-device")
		return 2
	}
	if *f.pkg == "" {
		fmt.Fprintln(stderr, "--pkg is required (for this module: --pkg ./examples/notes/notesmobile)")
		return 2
	}
	if *f.renderer != "webview" && *f.renderer != "native" {
		fmt.Fprintln(stderr, "--renderer must be webview or native")
		return 2
	}
	if *f.renderer == "native" && *f.hostTemplate != "" {
		fmt.Fprintln(stderr, "--renderer native cannot use --host-template")
		return 2
	}
	if _, err := debugTraceBindFlags(*f.debugTrace, false, *f.renderer); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := validateURLScheme(*f.urlScheme); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *f.urlScheme != "" && *f.renderer != "native" {
		fmt.Fprintln(stderr, "--url-scheme requires --renderer native")
		return 2
	}
	if *f.wait < 0 {
		fmt.Fprintln(stderr, "--wait must not be negative")
		return 2
	}
	if err := validateCameraUsage(*f.cameraUsage); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := validateMicrophoneUsage(*f.microphoneUsage); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if strings.TrimSpace(*f.microphoneUsage) != "" && *f.platform == "android-emu" && *f.renderer != "native" {
		fmt.Fprintln(stderr, "--microphone-usage on Android requires --renderer native")
		return 2
	}
	if *f.hostTemplate != "" {
		resolved, err := filepath.Abs(*f.hostTemplate)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			fmt.Fprintln(stderr, "--host-template must be a readable regular file under 1 MB")
			return 2
		}
		*f.hostTemplate = resolved
	}
	return 0
}

// resolveApp asks the go tool for the bind package (read-only, also in a
// dry run) and builds the validated app identity. It returns the exit code
// main.go uses for each failure: 1 for go list, 2 for bad identity flags.
func (f *buildFlags) resolveApp(r *runner, stderr io.Writer) (app, int) {
	pkgPath, pkgName, err := resolvePkg(r, *f.pkg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return app{}, 1
	}
	a, err := newApp(pkgPath, pkgName, *f.id, *f.name, *f.version)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return app{}, 2
	}
	a.HostTemplate = *f.hostTemplate
	a.Renderer = *f.renderer
	a.DebugTrace = *f.debugTrace
	a.CameraUsage = strings.TrimSpace(*f.cameraUsage)
	a.MicrophoneUsage = strings.TrimSpace(*f.microphoneUsage)
	a.URLScheme = *f.urlScheme
	a.ReceiveShares, a.SecureStorage, a.Widgets = *f.receiveShares, *f.secureStorage, *f.widgets
	a.BiometricUsage, a.AppGroup = strings.TrimSpace(*f.biometricUsage), *f.appGroup
	if err := validateDeviceFeatures(a, *f.platform); err != nil {
		fmt.Fprintln(stderr, err)
		return app{}, 2
	}
	return a, 0
}

// outputDir is --out made absolute, defaulting to dist/mobile/<platform>.
// dev uses it; build and run take the directory from releaseFlags.resolve.
func (f *buildFlags) outputDir() (string, error) {
	dir := *f.out
	if dir == "" {
		dir = filepath.Join("dist", "mobile", *f.platform)
	}
	return filepath.Abs(dir)
}
