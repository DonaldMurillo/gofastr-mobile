package main

import (
	"bytes"
	"debug/macho"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type deviceDoctorOptions struct{ app, pkg, device, scheme, require string }

type deviceRecord struct {
	Identifier string `json:"identifier"`
	Connection struct {
		Pairing string `json:"pairingState"`
		Tunnel  string `json:"tunnelState"`
	} `json:"connectionProperties"`
	Properties struct {
		Name          string `json:"name"`
		DeveloperMode string `json:"developerModeStatus"`
		DDI           bool   `json:"ddiServicesAvailable"`
	} `json:"deviceProperties"`
	Hardware struct {
		UDID     string `json:"udid"`
		Platform string `json:"platform"`
		Reality  string `json:"reality"`
		Type     string `json:"deviceType"`
	} `json:"hardwareProperties"`
}

func parseDeviceRecords(content []byte) ([]deviceRecord, error) {
	if len(content) > 8<<20 {
		return nil, errors.New("devicectl report exceeds 8 MiB")
	}
	var report struct {
		Info struct {
			Outcome string `json:"outcome"`
		} `json:"info"`
		Result *struct {
			Devices []deviceRecord `json:"devices"`
		} `json:"result"`
	}
	if err := json.Unmarshal(content, &report); err != nil {
		return nil, err
	}
	if report.Info.Outcome != "success" || report.Result == nil {
		return nil, errors.New("devicectl did not return a successful device report")
	}
	devices := report.Result.Devices
	sort.Slice(devices, func(first, second int) bool { return devices[first].Identifier < devices[second].Identifier })
	return devices, nil
}

func usableDevice(device deviceRecord, selection string) bool {
	selected := device.Hardware.Type == "iPhone"
	if selection != "" {
		selected = selection == device.Identifier || selection == device.Hardware.UDID
	}
	return selected && device.Hardware.Platform == "iOS" && device.Hardware.Reality == "physical" &&
		device.Hardware.UDID != "" && device.Connection.Pairing == "paired" && device.Connection.Tunnel == "connected" &&
		device.Properties.DeveloperMode == "enabled" && device.Properties.DDI
}

func doctorIOSDevice(r *runner, options deviceDoctorOptions, stderr io.Writer) int {
	required := map[string]bool{}
	if options.require != "" {
		for _, name := range strings.Split(options.require, ",") {
			switch name {
			case "camera", "microphone", "notifications", "extensions":
				required[name] = true
			default:
				fmt.Fprintf(stderr, "unknown --require feature %q; use camera,microphone,notifications,extensions\n", name)
				return 2
			}
		}
	}
	if err := validateURLScheme(options.scheme); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if required["notifications"] && options.scheme == "" {
		fmt.Fprintln(stderr, "--require notifications needs --url-scheme <expected app scheme>")
		return 2
	}
	if required["extensions"] && options.pkg == "" {
		fmt.Fprintln(stderr, "--require extensions needs --pkg <consuming Go package>")
		return 2
	}
	if strings.HasPrefix(options.pkg, "-") {
		fmt.Fprintln(stderr, "--pkg must name a Go package, not a go list flag")
		return 2
	}
	if options.app != "" {
		absolute, err := filepath.Abs(options.app)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		options.app = absolute
	}
	if r.dryRun {
		fmt.Fprintln(r.out, "Plan: read selected Xcode, iphoneos SDK/swiftc, and devicectl JSON (temporary report outside the repository, removed afterward).")
		fmt.Fprintln(r.out, "Plan: inspect existing .app Info.plist, arm64 executable, codesign verification/team, and embedded provisioning expiry/device allowlist; optionally inspect compiled extensions.")
		fmt.Fprintln(r.out, "NOT RUN: readiness and physical acceptance (dry run; no commands, writes, installation, signing or device changes)")
		return 0
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintln(r.out, "FAIL: ios-device readiness requires macOS with Xcode; physical acceptance NOT RUN")
		return 1
	}
	execute := func(name string, args ...string) ([]byte, error) {
		r.header("read-only readiness: " + name)
		fmt.Fprintf(r.out, "    $ %s\n", shellJoin(name, args))
		content, err := r.command(name, args...).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(content)))
		}
		return content, nil
	}
	var checks []check
	for _, tool := range []struct {
		name  string
		args  []string
		label string
	}{
		{"xcode-select", []string{"-p"}, "selected full Xcode"},
		{"xcodebuild", []string{"-version"}, "Xcode version"},
		{"xcrun", []string{"--sdk", "iphoneos", "--show-sdk-path"}, "physical iOS SDK"},
		{"xcrun", []string{"--sdk", "iphoneos", "--find", "swiftc"}, "physical iOS Swift compiler"},
	} {
		content, err := execute(tool.name, tool.args...)
		ok := err == nil
		if tool.name == "xcode-select" {
			ok = ok && strings.Contains(string(content), ".app/")
		}
		checks = append(checks, check{name: tool.label, ok: ok, got: strings.TrimSpace(string(content)), fix: "select an installed full Xcode and its iOS SDK manually; doctor never changes toolchains"})
	}
	devices, err := readDeviceReport(execute)
	if err != nil {
		checks = append(checks, check{name: "CoreDevice JSON report", got: err.Error(), fix: "run xcrun devicectl list devices; inspect Xcode/device availability manually"})
	}
	var usable []deviceRecord
	for _, device := range devices {
		if usableDevice(device, options.device) {
			usable = append(usable, device)
		}
		fmt.Fprintf(r.out, "device %q type=%s pairing=%s tunnel=%s developerMode=%s ddi=%t\n", device.Properties.Name, device.Hardware.Type, device.Connection.Pairing, device.Connection.Tunnel, device.Properties.DeveloperMode, device.Properties.DDI)
	}
	checks = append(checks, check{name: "connected usable physical iPhone (or exact --device)", ok: len(usable) > 0,
		got: fmt.Sprintf("%d usable of %d known devices; cached/unavailable entries do not qualify", len(usable), len(devices)),
		fix: "owner must connect/unlock/trust an authorized test device and enable Developer Mode if desired; doctor does not pair, unlock, mount, or change it"})
	checks = append(checks, inspectDeviceApp(options, required, usable, execute, time.Now())...)
	if options.pkg != "" {
		definitions, err := discoverExtensions(r, options.pkg)
		checks = append(checks, check{name: "compiled extension manifests/assets/collisions", ok: err == nil && (!required["extensions"] || len(definitions) > 0),
			got: fmt.Sprintf("%d compiled definitions; %v", len(definitions), err), fix: "run extension check --pkg with the consuming package; import and explicitly select expected definitions"})
		if err == nil {
			printExtensionReport(r.out, definitions)
		}
	} else {
		fmt.Fprintln(r.out, "NOT CHECKED: compiled extension graph (supply --pkg; use --require extensions to make it mandatory)")
	}
	failed := 0
	for _, item := range checks {
		mark := "PASS"
		if !item.ok {
			mark = "FAIL"
			failed++
		}
		fmt.Fprintf(r.out, "%s: %s — %s\n", mark, item.name, item.got)
		if !item.ok {
			fmt.Fprintf(r.out, "  fix: %s\n", item.fix)
		}
	}
	fmt.Fprintln(r.out, "NOT RUN: physical camera/microphone, permission UX, reminder tap, share provider, VoiceOver, Dynamic Type, keyboard/IME, restart/drafts. See docs/ios-device-acceptance.md.")
	fmt.Fprintln(r.out, "Readiness is not hardware acceptance or App Store/distribution certification. No app/device/signing/permission/user-data changes made.")
	if failed != 0 {
		return 1
	}
	return 0
}

func readDeviceReport(execute func(string, ...string) ([]byte, error)) ([]deviceRecord, error) {
	directory, err := os.MkdirTemp("", "gofastr-device-doctor-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "devices.json")
	if _, err := execute("xcrun", "devicectl", "list", "devices", "--quiet", "--timeout", "15", "--json-output", path); err != nil {
		return nil, err
	}
	content, err := extensionAsset(directory, "devices.json", 8<<20)
	if err != nil {
		return nil, err
	}
	return parseDeviceRecords(content)
}

type deviceAppInfo struct {
	Identifier string   `json:"CFBundleIdentifier"`
	Executable string   `json:"CFBundleExecutable"`
	Platforms  []string `json:"CFBundleSupportedPlatforms"`
	Camera     string   `json:"NSCameraUsageDescription"`
	Microphone string   `json:"NSMicrophoneUsageDescription"`
	URLTypes   []struct {
		Schemes []string `json:"CFBundleURLSchemes"`
	} `json:"CFBundleURLTypes"`
}

func inspectDeviceApp(options deviceDoctorOptions, required map[string]bool, devices []deviceRecord,
	execute func(string, ...string) ([]byte, error), now time.Time) []check {
	if options.app == "" {
		return []check{{name: "existing signed physical .app", got: "NOT CHECKED: --app is required for readiness", fix: "supply an existing owner-authorized signed device build; doctor does not build/sign/install"}}
	}
	var checks []check
	add := func(name string, ok bool, got, fix string) {
		checks = append(checks, check{name: name, ok: ok, got: got, fix: fix})
	}
	if _, err := extensionAsset(options.app, "Info.plist", 1<<20); err != nil {
		add("app Info.plist", false, err.Error(), "supply a complete existing .app bundle")
		return checks
	}
	content, err := execute("plutil", "-convert", "json", "-o", "-", filepath.Join(options.app, "Info.plist"))
	var info deviceAppInfo
	if err == nil {
		err = json.Unmarshal(content, &info)
	}
	if err != nil {
		add("app Info.plist", false, err.Error(), "supply a valid existing .app bundle")
		return checks
	}
	physical := len(info.Platforms) == 1 && info.Platforms[0] == "iPhoneOS"
	add("physical iOS bundle identity/platform", physical && info.Identifier != "", fmt.Sprintf("%q %v", info.Identifier, info.Platforms), "use an iphoneos product, not an iphonesimulator build")
	executable := info.Executable
	validExecutable := executable != "" && filepath.Base(executable) == executable && !strings.ContainsAny(executable, `/\\`) && executable != "." && executable != ".."
	if validExecutable {
		path := filepath.Join(options.app, executable)
		metadata, err := os.Lstat(path)
		validExecutable = err == nil && metadata.Mode().IsRegular()
		if validExecutable {
			binary, err := macho.Open(path)
			validExecutable = err == nil
			if err == nil {
				validExecutable = binary.Cpu == macho.CpuArm64 && binary.Type == macho.TypeExec
				binary.Close()
			}
		}
	}
	add("arm64 Mach-O executable", validExecutable, executable, "supply the existing physical arm64 product; doctor will not compile or replace it")
	for _, purpose := range []struct{ name, value string }{{"camera", info.Camera}, {"microphone", info.Microphone}} {
		declared := strings.TrimSpace(purpose.value) != ""
		got := "not declared; feature not checked unless --require includes it"
		if declared {
			got = purpose.value
		}
		add(purpose.name+" purpose string", declared || !required[purpose.name], got, "add a specific purpose string in your normal build before enabling this feature; doctor does not edit bundles or request permission")
	}
	if options.scheme != "" {
		found := false
		for _, group := range info.URLTypes {
			for _, scheme := range group.Schemes {
				found = found || scheme == options.scheme
			}
		}
		add("expected reminder/deep-link scheme", found, options.scheme, "build the app with the expected --url-scheme; notification authorization and tapping remain manual NOT RUN gates")
	}
	_, err = execute("codesign", "--verify", "--deep", "--strict", options.app)
	add("existing code signature verification", err == nil, fmt.Sprint(err), "provide an already signed, unmodified authorized development build; doctor never signs")
	signature, err := execute("codesign", "-d", "--verbose=4", options.app)
	team := signatureTeam(string(signature))
	add("non-ad-hoc team signature", err == nil && team != "" && !strings.Contains(string(signature), "Signature=adhoc"), team, "ad-hoc/simulator signing does not establish physical deployment readiness")
	if _, err := extensionAsset(options.app, "embedded.mobileprovision", 4<<20); err != nil {
		add("development provisioning profile", false, err.Error(), "supply an existing development/ad-hoc device-test build with embedded provisioning; distribution workflows are outside this gate")
		return checks
	}
	profile, err := execute("security", "cms", "-D", "-i", filepath.Join(options.app, "embedded.mobileprovision"))
	if err != nil {
		add("provisioning profile decode", false, err.Error(), "inspect the existing profile in Xcode; doctor never changes it")
		return checks
	}
	profileCheck, err := inspectProvisioning(profile, info.Identifier, team, devices, now)
	add("provisioning expiry/team/app/device match", err == nil, profileCheck, "use an unexpired matching existing profile allowing the selected device; provisioning/signing changes require the owner outside doctor")
	return checks
}

func signatureTeam(signature string) string {
	for _, line := range strings.Split(signature, "\n") {
		if team, found := strings.CutPrefix(line, "TeamIdentifier="); found && team != "not set" {
			return strings.TrimSpace(team)
		}
	}
	return ""
}

type provisioningNode struct {
	XMLName  xml.Name
	Text     string             `xml:",chardata"`
	Children []provisioningNode `xml:",any"`
}

func (node provisioningNode) field(name string) provisioningNode {
	for index := 0; index+1 < len(node.Children); index += 2 {
		if node.Children[index].XMLName.Local == "key" && node.Children[index].Text == name {
			return node.Children[index+1]
		}
	}
	return provisioningNode{}
}

func inspectProvisioning(content []byte, bundleID, team string, devices []deviceRecord, now time.Time) (string, error) {
	if len(content) > 4<<20 {
		return "oversized decoded profile", errors.New("profile exceeds 4 MiB")
	}
	var plist provisioningNode
	if err := xml.NewDecoder(bytes.NewReader(content)).Decode(&plist); err != nil {
		return "invalid profile XML", err
	}
	if plist.XMLName.Local != "plist" || len(plist.Children) != 1 || plist.Children[0].XMLName.Local != "dict" {
		return "invalid profile dictionary", errors.New("profile dictionary missing")
	}
	profile := plist.Children[0]
	expiry, err := time.Parse(time.RFC3339, profile.field("ExpirationDate").Text)
	if err != nil || !expiry.After(now) {
		return "expired or missing profile expiration", errors.New("profile expiration invalid")
	}
	entitlements := profile.field("Entitlements")
	application := entitlements.field("application-identifier").Text
	pattern := team + "." + bundleID
	matches := application == pattern || strings.HasSuffix(application, ".*") && strings.HasPrefix(pattern, strings.TrimSuffix(application, "*"))
	if team == "" || !matches || entitlements.field("com.apple.developer.team-identifier").Text != team {
		return "profile app/team mismatch", errors.New("profile identity mismatch")
	}
	for _, allowed := range profile.field("ProvisionedDevices").Children {
		for _, device := range devices {
			if allowed.XMLName.Local == "string" && allowed.Text == device.Hardware.UDID {
				return "unexpired until " + expiry.UTC().Format(time.RFC3339) + "; app/team match; usable device allowed", nil
			}
		}
	}
	return "no selected usable device in profile allowlist", errors.New("device provisioning not established")
}
