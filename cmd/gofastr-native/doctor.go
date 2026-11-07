package main

import (
	"encoding/json"
	"fmt"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

type checkLevel int

const (
	levelCheck checkLevel = iota // [ok  ] or [MISS]; MISS fails doctor
	levelWarn                    // [warn]; never changes the exit code
	levelInfo                    // [info]; never changes the exit code
)

type check struct {
	name  string
	ok    bool
	got   string
	fix   string
	level checkLevel
}

type doctorOptions struct {
	platform string
	devices  bool // run adb devices, which starts the adb server
	goos     string
}

// doctor checks the toolchain for one platform (or both) and prints what
// to install for anything missing. It only reads.
func doctor(r *runner, platform string) int {
	return doctorWithOptions(r, doctorOptions{platform: platform})
}

func doctorWithOptions(r *runner, o doctorOptions) int {
	if o.goos == "" {
		o.goos = runtime.GOOS
	}
	var checks []check
	add := func(c check) { checks = append(checks, c) }
	info := func(name, got string) { add(check{name: name, got: got, level: levelInfo}) }
	warn := func(name, got, fix string) { add(check{name: name, got: got, fix: fix, level: levelWarn}) }

	inModule := doctorGo(r, add, info, warn)

	if o.platform == "" || o.platform == "ios-sim" {
		onMac := o.goos == "darwin"
		add(check{name: "macOS (the iOS SDK only exists there)", ok: onMac, got: o.goos, fix: "iOS builds need a Mac"})
		if onMac {
			dev, err := r.query("xcode-select", "-p")
			dev = strings.TrimSpace(dev)
			full := err == nil && strings.Contains(dev, ".app/")
			add(check{name: "Xcode selected (not just the Command Line Tools)", ok: full, got: dev,
				fix: "install Xcode from the App Store, then: sudo xcode-select -s /Applications/Xcode.app && xcodebuild -runFirstLaunch"})
			if v, err := r.query("xcodebuild", "-version"); err == nil {
				info("Xcode version", strings.Join(strings.Fields(v), " "))
			}
			sdk, err := r.query("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path")
			add(check{name: "iOS simulator SDK", ok: err == nil, got: strings.TrimSpace(sdk), fix: "open Xcode once and accept the license, or run xcodebuild -runFirstLaunch"})
			_, err = r.query("xcrun", "--sdk", "iphonesimulator", "--find", "swiftc")
			add(check{name: "swiftc", ok: err == nil, fix: "comes with Xcode"})
			listing, err := r.query("xcrun", "simctl", "list", "devices", "available", "--json")
			udid := ""
			if err == nil {
				udid, err = pickSimulator([]byte(listing), "")
			}
			add(check{name: "an iPhone simulator", ok: err == nil, got: udid, fix: "Xcode > Settings > Components: install an iOS simulator runtime"})
			_, err = exec.LookPath("codesign")
			add(check{name: "codesign", ok: err == nil, fix: "comes with Xcode"})
		}
	}

	if o.platform == "" || o.platform == "android-emu" {
		doctorAndroid(r, o, add, info, warn)
	}

	failed := 0
	for _, c := range checks {
		mark := "ok  "
		switch {
		case c.level == levelInfo:
			mark = "info"
		case c.level == levelWarn:
			mark = "warn"
		case !c.ok:
			mark = "MISS"
			failed++
		}
		line := fmt.Sprintf("[%s] %s", mark, c.name)
		if c.got != "" {
			line += ": " + c.got
		}
		fmt.Fprintln(r.out, line)
		if (c.level == levelWarn || c.level == levelCheck && !c.ok) && c.fix != "" {
			fmt.Fprintf(r.out, "       fix: %s\n", c.fix)
		}
	}
	if failed > 0 {
		fmt.Fprintf(r.out, "\n%d missing.\n", failed)
		return 1
	}
	fmt.Fprintln(r.out, "\nEverything is in place.")
	if !inModule {
		fmt.Fprintln(r.out, "next: gofastr-native init --out ../myapp")
	}
	return 0
}

// doctorGo reports the Go toolchain and module. It returns whether the
// current directory is inside a Go module.
func doctorGo(r *runner, add func(check), info func(string, string), warn func(string, string, string)) bool {
	goVer, err := r.query("go", "version")
	add(check{name: "Go toolchain", ok: err == nil, got: strings.TrimSpace(goVer), fix: "install Go: https://go.dev/dl/"})
	if err != nil {
		return false
	}
	envOut, err := r.query("go", "env", "GOVERSION", "GOTOOLCHAIN", "GOWORK", "GOMOD")
	lines := strings.Split(strings.ReplaceAll(envOut, "\r\n", "\n"), "\n")
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	goVersion, toolchain, work, gomod := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), strings.TrimSpace(lines[2]), strings.TrimSpace(lines[3])
	if err != nil {
		warn("go env", err.Error(), "")
		return false
	}
	info("Go environment", fmt.Sprintf("GOVERSION=%s GOTOOLCHAIN=%s GOWORK=%s GOMOD=%s", goVersion, toolchain, firstNonEmpty(work, "(none)"), firstNonEmpty(gomod, "(none)")))
	if gomod == "" || gomod == os.DevNull || strings.EqualFold(gomod, "NUL") {
		warn("Go module", "not inside a Go module", "run gofastr-native init, or cd into your app")
		return false
	}
	if content, err := os.ReadFile(gomod); err == nil {
		if parsed, err := modfile.ParseLax(gomod, content, nil); err == nil && parsed.Go != nil {
			want := "go" + parsed.Go.Version
			if version.IsValid(want) && version.IsValid(goVersion) && version.Compare(want, goVersion) > 0 {
				if toolchain == "local" {
					add(check{name: "Go version for this module", got: goVersion + " is older than go.mod's " + want,
						fix: "install " + want + " or later, or unset GOTOOLCHAIN=local so the go command can switch toolchains"})
				} else {
					info("Go version for this module", "go.mod asks for "+want+"; the go command downloads and switches to it (GOTOOLCHAIN="+toolchain+")")
				}
			}
		}
	}
	_, err = r.query("go", "list", "golang.org/x/mobile/cmd/gomobile", "golang.org/x/mobile/cmd/gobind")
	add(check{name: "gomobile and gobind resolve in this module", ok: err == nil,
		fix: "go get -tool golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind"})
	if out, err := r.query("go", "list", "-m", "-json", nativeModulePath); err == nil {
		var m goListModule
		if json.Unmarshal([]byte(out), &m) == nil {
			switch {
			case m.Main:
				info("gofastr-native", "this checkout is the main module ("+m.Dir+")")
			case m.Replace != nil && m.Replace.Version == "":
				info("gofastr-native", "local replace "+firstNonEmpty(m.Replace.Dir, m.Replace.Path))
			case m.Replace != nil:
				info("gofastr-native", m.Version+" replaced by "+m.Replace.Path+" "+m.Replace.Version)
			default:
				info("gofastr-native", m.Version)
			}
		}
	} else {
		info("gofastr-native", "not required by this module")
	}
	return true
}

var javacVersionRe = regexp.MustCompile(`javac\s+([0-9]+)(?:\.([0-9]+))?`)

// javacMajor reads "javac 17.0.9" (17) and "javac 1.8.0_292" (8).
func javacMajor(out string) int {
	m := javacVersionRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	major, _ := strconv.Atoi(m[1])
	if major == 1 && m[2] != "" {
		major, _ = strconv.Atoi(m[2])
	}
	return major
}

func doctorAndroid(r *runner, o doctorOptions, add func(check), info func(string, string), warn func(string, string, string)) {
	fix := androidFixFor(o.goos)
	home, sdkRoot := r.envValue("ANDROID_HOME"), r.envValue("ANDROID_SDK_ROOT")
	source := "auto-detected"
	switch {
	case home != "":
		source = "ANDROID_HOME"
	case sdkRoot != "":
		source = "ANDROID_SDK_ROOT"
	}
	if home != "" && sdkRoot != "" && filepath.Clean(home) != filepath.Clean(sdkRoot) {
		warn("ANDROID_HOME and ANDROID_SDK_ROOT differ", "ANDROID_HOME="+home+" is used; ANDROID_SDK_ROOT="+sdkRoot+" is ignored",
			"point both at the same SDK, or unset ANDROID_SDK_ROOT")
	}
	sdk, err := findAndroidSDK(false)
	missing := ""
	if err != nil {
		missing = err.Error()
	}
	add(check{name: "Android SDK, build-tools, platform, NDK, JDK", ok: err == nil, got: firstNonEmpty(missing, sdk.root), fix: fix})
	if sdk.root != "" && sdk.root != "$ANDROID_HOME" {
		info("Android SDK root", sdk.root+" (from "+source+")")
	}
	if ndkHome := r.envValue("ANDROID_NDK_HOME"); ndkHome != "" {
		ok := isDir(ndkHome)
		add(check{name: "ANDROID_NDK_HOME points at an existing directory", ok: ok, got: ndkHome,
			fix: "unset ANDROID_NDK_HOME to use the newest NDK under the SDK, or point it at an installed NDK"})
	} else if isDir(sdk.ndk) {
		info("NDK", filepath.Base(sdk.ndk)+" ("+sdk.ndk+")")
	}
	if err != nil {
		return
	}
	for _, t := range []string{"d8", "aapt2", "zipalign", "apksigner"} {
		add(check{name: "build-tools " + t, ok: fileExists(sdk.tool(t)), got: sdk.tool(t), fix: fix})
	}
	add(check{name: "adb", ok: fileExists(sdk.adb), got: sdk.adb, fix: "sdkmanager \"platform-tools\""})
	hasEmu := fileExists(sdk.emulator)
	add(check{name: "emulator", ok: hasEmu, got: sdk.emulator, fix: "sdkmanager \"emulator\""})
	if hasEmu {
		avds, err := r.query(sdk.emulator, "-list-avds")
		names := strings.Fields(avds)
		add(check{name: "an Android virtual device (AVD)", ok: err == nil && len(names) > 0, got: strings.Join(names, ", "),
			fix: "Android Studio > Device Manager > Create device, or: avdmanager create avd -n gofastr -k \"system-images;android-35;google_apis;" + avdABI() + "\""})
		accel, err := r.query(sdk.emulator, "-accel-check")
		summary := accelSummary(accel)
		if err == nil {
			info("emulator acceleration", summary)
		} else {
			warn("emulator acceleration", firstNonEmpty(summary, err.Error()),
				"enable hardware virtualization; on Windows turn on Windows Hypervisor Platform. The emulator runs without it, very slowly")
		}
	}
	jv, err := r.query(sdk.javac, "-version")
	jv = strings.TrimSpace(jv)
	major := javacMajor(jv)
	from := "PATH"
	if jh := r.envValue("JAVA_HOME"); jh != "" && pathWithin(jh, sdk.javac) {
		from = "JAVA_HOME"
	}
	jdkFix := "install JDK 17+ and point JAVA_HOME at it"
	add(check{name: "javac 17 or newer", ok: err == nil && major >= 17, got: firstNonEmpty(jv, "version unknown") + " (" + sdk.javac + ", from " + from + ")", fix: jdkFix})
	doctorAndroidRelease(r, sdk, add, info, warn)
	if o.devices {
		doctorDevices(r, sdk, info, warn)
	} else {
		info("devices", "run doctor --devices to list attached devices (starts the adb server)")
	}
}

// doctorAndroidRelease reports the tools only release builds use. Debug
// builds and dev need none of them, so a missing tool is a warning, except
// a GOFASTR_BUNDLETOOL that names no file.
func doctorAndroidRelease(r *runner, sdk androidSDK, add func(check), info func(string, string), warn func(string, string, string)) {
	if jarsigner := jdkTool("jarsigner"); jarsigner != "" {
		info("jarsigner (release App Bundles)", jarsigner)
	} else {
		warn("jarsigner (release App Bundles)", "not found under JAVA_HOME or on PATH", "it comes with the JDK; only --package aab needs it")
	}
	if major := buildToolsMajor(sdk); major > 0 && major < 35 {
		warn("build-tools for release builds", filepath.Base(sdk.buildTools),
			"install build-tools 35 or newer; with older ones, release builds report the 16 KiB page alignment check (zipalign -P 16) as NOT RUN")
	}
	if path := r.envValue(bundletoolEnv); path != "" {
		add(check{name: bundletoolEnv + " names a file", ok: fileExists(path), got: path, fix: "point it at your bundletool.jar, or unset it"})
	} else {
		info("bundletool", "not configured; only --package aab needs it (--bundletool or "+bundletoolEnv+"; the CLI never downloads it)")
	}
}

func accelSummary(out string) string {
	var kept []string
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "accel:" || line == "accel" {
			continue
		}
		if _, err := strconv.Atoi(line); err == nil {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, " ")
}

func doctorDevices(r *runner, sdk androidSDK, info func(string, string), warn func(string, string, string)) {
	listing, err := r.query(sdk.adb, "devices")
	if err != nil {
		warn("adb devices", err.Error(), "")
		return
	}
	serials := parseADBDevices(listing)
	if len(serials) == 0 {
		info("attached devices", "none (dev and run boot an AVD when none is attached)")
		return
	}
	for _, serial := range serials {
		state := "attached"
		if strings.HasPrefix(serial, "emulator-") {
			state = "booting"
			if done, err := r.query(sdk.adb, "-s", serial, "shell", "getprop", "sys.boot_completed"); err == nil && strings.TrimSpace(done) == "1" {
				state = "booted"
			}
		}
		info("device "+serial, state)
	}
}

// androidFixFor is the SDK install advice, with environment syntax for
// the host's shell. It is text only; doctor never changes settings.
func androidFixFor(goos string) string {
	install := "Android Studio > Settings > Languages & Frameworks > Android SDK: install an SDK Platform, and under SDK Tools: Build-Tools, NDK (Side by side), Command-line Tools, Emulator, Platform-Tools. Or with sdkmanager: sdkmanager \"platforms;android-35\" \"build-tools;35.0.0\" \"ndk;27.2.12479018\" \"platform-tools\" \"emulator\" \"system-images;android-35;google_apis;" + avdABI() + "\" (versions are examples). "
	switch goos {
	case "windows":
		return install + "Then, in PowerShell, for this session: $env:ANDROID_HOME = 'C:\\path\\to\\android-sdk' ; to keep it for your user: [Environment]::SetEnvironmentVariable('ANDROID_HOME', 'C:\\path\\to\\android-sdk', 'User') (Android Studio's default is %LOCALAPPDATA%\\Android\\Sdk, which is found without ANDROID_HOME)"
	case "darwin":
		return install + "Then: export ANDROID_HOME=$HOME/Library/Android/sdk (add it to ~/.zshrc to keep it)"
	default:
		return install + "Then: export ANDROID_HOME=$HOME/Android/Sdk (add it to your shell profile to keep it)"
	}
}

func avdABI() string {
	if runtime.GOARCH == "arm64" {
		return "arm64-v8a"
	}
	return "x86_64"
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
