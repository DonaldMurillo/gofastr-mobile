package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	hostios "github.com/DonaldMurillo/gofastr-native/mobilehost/ios"
	renderios "github.com/DonaldMurillo/gofastr-native/render/ios"
)

// The iOS simulator build never touches an Xcode project. Like
// `gofastr desktop build`, it assembles the .app folder itself:
//
//	gomobile bind   -> <Prefix>.xcframework (the Go app as a static framework)
//	swiftc          -> the host executable (App.swift, linked to the framework)
//	Info.plist      -> written from a template
//	codesign -s -   -> ad-hoc signature (all the simulator asks for)
//	simctl          -> boot, install, launch, screenshot
//
// Xcode must be installed (the iOS SDK and swiftc ship only inside it), but
// nothing here opens it. --platform ios-device builds the same bundle
// against the iphoneos SDK and signs it with the owner's identity and
// provisioning profiles (ios_release.go).

// iosTarget is the SDK a build compiles against: the simulator, or a
// physical iPhone.
type iosTarget struct {
	sdk      string // xcrun --sdk name: iphonesimulator or iphoneos
	gomobile string // gomobile -target: iossimulator or ios
	triple   string // swiftc -target
	device   bool
}

func simulatorTarget(minOS string) iosTarget {
	return iosTarget{sdk: "iphonesimulator", gomobile: "iossimulator", triple: simArch() + "-apple-ios" + minOS + "-simulator"}
}

func deviceTarget(minOS string) iosTarget {
	return iosTarget{sdk: "iphoneos", gomobile: "ios", triple: "arm64-apple-ios" + minOS, device: true}
}

// iosSigner signs one bundle of a physical-device build. The simulator
// build has none and signs ad hoc.
type iosSigner interface {
	sign(r *runner, label, bundleID, bundle string) error
}

func buildIOS(r *runner, a app, out string) (string, error) {
	return buildIOSFor(r, a, out, simulatorTarget(a.MinOS), nil, false)
}

// buildIOSFor assembles and signs the .app for target. Extensions are
// signed before the app (inside-out).
func buildIOSFor(r *runner, a app, out string, t iosTarget, signer iosSigner, release bool) (string, error) {
	traceFlags, err := debugTraceBindFlags(a.DebugTrace, release, a.Renderer)
	if err != nil {
		return "", err
	}
	a = a.forIOS()
	if a.HostTemplate == "" {
		definitions, err := discoverExtensions(r, a.PkgPath)
		if err != nil {
			return "", err
		}
		a.Extensions = definitions
	}
	work := filepath.Join(out, "work")
	bin := filepath.Join(work, "bin")
	if err := bootstrapGomobile(r, bin); err != nil {
		return "", err
	}
	sdkDesc, bindDesc, doctorPlatform := "find the iOS simulator SDK", "bind the Go app as an iOS simulator framework", "ios-sim"
	if t.device {
		sdkDesc, bindDesc, doctorPlatform = "find the iOS device SDK", "bind the Go app as an iOS device framework", "ios-device"
	}
	sdk, err := r.output(sdkDesc, "<"+t.sdk+"-sdk>", "xcrun", "--sdk", t.sdk, "--show-sdk-path")
	if err != nil {
		return "", fmt.Errorf("%w\n    (is Xcode installed and selected? run: gofastr-native doctor --platform %s)", err, doctorPlatform)
	}
	xcf := filepath.Join(work, a.Prefix+".xcframework")
	bindArgs := append([]string{"bind", "-target=" + t.gomobile, "-iosversion=" + a.MinOS}, goLDFlags(release, false)...)
	bindArgs = append(bindArgs, traceFlags...)
	if release {
		bindArgs = append(bindArgs, "-tags="+releaseTag)
	}
	if err := r.run(bindDesc, filepath.Join(bin, "gomobile"), append(bindArgs, "-o", xcf, a.PkgPath)...); err != nil {
		return "", err
	}
	slice := filepath.Join(xcf, "<simulator-slice>")
	if t.device {
		slice = filepath.Join(xcf, "<ios-arm64-slice>")
	}
	if !r.dryRun {
		if t.device {
			slice, err = deviceSlice(xcf, a.Prefix)
		} else {
			slice, err = simulatorSlice(xcf, a.Prefix)
		}
		if err != nil {
			return "", err
		}
	}
	plist := filepath.Join(work, "ios", "Info.plist")
	sources, err := prepareIOSHost(r, a, filepath.Join(work, "ios"))
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(out, a.Exe+".app")
	if err := r.do("create "+appDir, func() error {
		if err := os.RemoveAll(appDir); err != nil {
			return err
		}
		return os.MkdirAll(appDir, 0o700)
	}); err != nil {
		return "", err
	}
	args := []string{"--sdk", t.sdk, "swiftc",
		"-parse-as-library", "-O",
		// swiftc names the module after -o, and the executable shares the
		// Go framework's name (Notesmobile): App.swift would then be part of
		// module Notesmobile and its `import Notesmobile` is ignored.
		"-module-name", "GofastrHost",
		"-target", t.triple, "-sdk", sdk,
		"-F", slice, "-framework", a.Prefix,
		// The Go runtime inside the framework resolves DNS and verifies
		// certificates through these; a static framework does not carry
		// its link flags, so the final link names them.
		"-framework", "Foundation", "-framework", "Security", "-framework", "CoreFoundation", "-lresolv",
		"-o", filepath.Join(appDir, a.Exe)}
	args = append(args, sources...)
	if err := r.run("compile the Swift host and link the Go framework", "xcrun", args...); err != nil {
		return "", err
	}
	if err := r.do("copy Info.plist into the bundle", func() error {
		b, err := os.ReadFile(plist)
		if err != nil {
			return err
		}
		return writeOwnerOnly(filepath.Join(appDir, "Info.plist"), b)
	}); err != nil {
		return "", err
	}
	if err := buildIOSExtensions(r, a, work, appDir, sdk, t, signer); err != nil {
		return "", err
	}
	if signer != nil {
		return appDir, signer.sign(r, "app", a.ID, appDir)
	}
	signArgs := []string{"--force", "--sign", "-", "--timestamp=none"}
	if a.AppGroup != "" || a.SecureStorage {
		entitlements := filepath.Join(work, "ios", "Host.entitlements")
		if err := r.do("generate simulator app-group and private Keychain entitlements", func() error {
			return writeRendered("templates/ios/DeviceFeatures.entitlements.tmpl", entitlements, a)
		}); err != nil {
			return "", err
		}
		signArgs = append(signArgs, "--entitlements", entitlements)
	}
	if err := r.run("sign the bundle ad hoc", "codesign", append(signArgs, appDir)...); err != nil {
		return "", err
	}
	return appDir, nil
}

func prepareIOSHost(r *runner, a app, directory string) ([]string, error) {
	src := filepath.Join(directory, "App.swift")
	if err := r.do("generate the Swift host and Info.plist", func() error {
		if err := writeRendered("templates/ios/App.swift.tmpl", src, a); err != nil {
			return err
		}
		return writeRendered("templates/ios/Info.plist.tmpl", filepath.Join(directory, "Info.plist"), a)
	}); err != nil {
		return nil, err
	}
	sources := []string{src}
	if a.HostTemplate != "" {
		return sources, nil
	}
	rendererSources, err := copyIOSLibrary(r, renderios.Sources, filepath.Join(directory, "renderer"), "UIKit renderer")
	if err != nil {
		return nil, err
	}
	sources = append(sources, rendererSources...)
	extensionSources, err := copyIOSExtensions(r, a.Extensions, filepath.Join(directory, "renderer"))
	if err != nil {
		return nil, err
	}
	sources = append(sources, extensionSources...)
	if a.Renderer != "native" {
		hostSources, err := copyIOSLibrary(r, hostios.Sources, filepath.Join(directory, "host"), "WebView service adapter")
		if err != nil {
			return nil, err
		}
		sources = append(sources, hostSources...)
	}
	return sources, nil
}

func copyIOSLibrary(r *runner, library fs.ReadFileFS, directory, label string) ([]string, error) {
	entries, err := fs.ReadDir(library, ".")
	if err != nil {
		return nil, err
	}
	var sources []string
	for _, entry := range entries {
		destination := filepath.Join(directory, entry.Name())
		if err := r.do("copy bundled "+label+" "+entry.Name(), func() error {
			content, err := library.ReadFile(entry.Name())
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				return err
			}
			return writeOwnerOnly(destination, content)
		}); err != nil {
			return nil, err
		}
		sources = append(sources, destination)
	}
	return sources, nil
}

func runIOS(r *runner, a app, appDir string, opts runOpts) error {
	listing, err := r.outputBytes("list the available simulators", []byte(`{"devices":{}}`), "xcrun", "simctl", "list", "devices", "available", "--json")
	if err != nil {
		return err
	}
	udid := "<simulator-udid>"
	if !r.dryRun {
		if udid, err = pickSimulator(listing, opts.device); err != nil {
			return err
		}
	}
	r.tolerate("boot the simulator (an already-booted one is fine)", "xcrun", "simctl", "boot", udid)
	r.tolerate("show the Simulator window", "open", "-a", "Simulator")
	if err := r.run("wait until the simulator has booted", "xcrun", "simctl", "bootstatus", udid, "-b"); err != nil {
		return err
	}
	if err := r.run("install the app", "xcrun", "simctl", "install", udid, appDir); err != nil {
		return err
	}
	if err := r.run("launch the app", "xcrun", "simctl", "launch", "--terminate-running-process", udid, a.ID); err != nil {
		return err
	}
	if opts.screenshot != "" {
		r.sleep(opts.wait, "for the first page")
		if err := r.run("take a screenshot", "xcrun", "simctl", "io", udid, "screenshot", opts.screenshot); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.out, "\nRunning on simulator %s.\n  Logs:      xcrun simctl spawn %s log stream --predicate 'process == \"%s\"'\n", udid, udid, a.Exe)
	if a.HostTemplate == "" && a.Renderer != "native" {
		fmt.Fprintf(r.out, "  Inspector: Safari > Develop > Simulator > %s\n", a.Name)
	}
	return nil
}

// simArch is the simulator architecture of this Mac.
func simArch() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return "arm64"
}

// simulatorSlice finds the framework directory gomobile wrote for the
// simulator inside the xcframework (its name varies with the arches).
func simulatorSlice(xcf, prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(xcf, "*simulator*", prefix+".framework"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no simulator slice in %s (gomobile bind -target=iossimulator should have written one)", xcf)
	}
	return filepath.Dir(matches[0]), nil
}

// deviceSlice finds the arm64 iphoneos framework inside the xcframework.
func deviceSlice(xcf, prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(xcf, "ios-*", prefix+".framework"))
	if err != nil {
		return "", err
	}
	var device []string
	for _, m := range matches {
		slice := filepath.Base(filepath.Dir(m))
		if !strings.Contains(slice, "simulator") && !strings.Contains(slice, "maccatalyst") && strings.Contains(slice, "arm64") {
			device = append(device, m)
		}
	}
	if len(device) != 1 {
		return "", fmt.Errorf("want exactly one ios-arm64 device slice in %s, found %d (gomobile bind -target=ios should have written one)", xcf, len(device))
	}
	return filepath.Dir(device[0]), nil
}

type simDevice struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	State       string `json:"state"`
	IsAvailable bool   `json:"isAvailable"`
	runtime     []int
}

// pickSimulator chooses the simulator: the one --device names (UDID or
// exact name), else a booted iPhone, else the iPhone on the newest iOS
// runtime.
func pickSimulator(listing []byte, want string) (string, error) {
	var parsed struct {
		Devices map[string][]simDevice `json:"devices"`
	}
	if err := json.Unmarshal(listing, &parsed); err != nil {
		return "", fmt.Errorf("read simctl's device list: %w", err)
	}
	var all []simDevice
	for rt, devs := range parsed.Devices {
		ver, ok := iosRuntimeVersion(rt)
		if !ok {
			continue // watchOS, tvOS, visionOS
		}
		for _, d := range devs {
			if d.IsAvailable || d.State != "" {
				d.runtime = ver
				all = append(all, d)
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if c := compareVersions(all[i].runtime, all[j].runtime); c != 0 {
			return c > 0
		}
		return all[i].Name < all[j].Name
	})
	if want != "" {
		for _, d := range all {
			if d.UDID == want || d.Name == want {
				return d.UDID, nil
			}
		}
		return "", fmt.Errorf("no available iOS simulator named %q (list them: xcrun simctl list devices available)", want)
	}
	for _, d := range all {
		if d.State == "Booted" && strings.HasPrefix(d.Name, "iPhone") {
			return d.UDID, nil
		}
	}
	for _, d := range all {
		if strings.HasPrefix(d.Name, "iPhone") {
			return d.UDID, nil
		}
	}
	return "", fmt.Errorf("no iPhone simulator is installed: Xcode > Settings > Components adds an iOS runtime")
}

// iosRuntimeVersion reads "com.apple.CoreSimulator.SimRuntime.iOS-18-2".
func iosRuntimeVersion(key string) ([]int, bool) {
	_, rest, ok := strings.Cut(key, ".SimRuntime.iOS-")
	if !ok {
		return nil, false
	}
	var ver []int
	for _, p := range strings.Split(rest, "-") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		ver = append(ver, n)
	}
	return ver, true
}

func compareVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

type runOpts struct {
	device     string
	screenshot string
	wait       time.Duration
}
