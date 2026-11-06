package main

import (
	"bytes"
	"context"
	"debug/macho"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func readyDevice() deviceRecord {
	var device deviceRecord
	device.Identifier = "core-test-device"
	device.Hardware.UDID = "test-udid"
	device.Hardware.Platform = "iOS"
	device.Hardware.Reality = "physical"
	device.Hardware.Type = "iPhone"
	device.Connection.Pairing = "paired"
	device.Connection.Tunnel = "connected"
	device.Properties.DeveloperMode = "enabled"
	device.Properties.DDI = true
	device.Properties.Name = "Authorized test iPhone"
	return device
}

func deviceReport(devices ...deviceRecord) []byte {
	content, _ := json.Marshal(map[string]any{"info": map[string]any{"outcome": "success"}, "result": map[string]any{"devices": devices}})
	return content
}

func TestDeviceReadinessDoesNotTrustCachedDevices(t *testing.T) {
	for _, state := range []string{"ready", "unavailable", "disabled", "unpaired", "no-ddi", "simulator", "mac", "ipad", "unknown-tunnel"} {
		t.Run(state, func(t *testing.T) {
			device := readyDevice()
			switch state {
			case "unavailable":
				device.Connection.Tunnel = "unavailable"
			case "disabled":
				device.Properties.DeveloperMode = "disabled"
			case "unpaired":
				device.Connection.Pairing = "unpaired"
			case "no-ddi":
				device.Properties.DDI = false
			case "simulator":
				device.Hardware.Reality = "simulated"
			case "mac":
				device.Hardware.Platform = "macOS"
			case "ipad":
				device.Hardware.Type = "iPad"
			case "unknown-tunnel":
				device.Connection.Tunnel = "future-state"
			}
			if usableDevice(device, "") != (state == "ready") {
				t.Fatalf("incorrect readiness for %s", state)
			}
			if state == "ipad" && !usableDevice(device, device.Hardware.UDID) {
				t.Fatal("explicit authorized iPad should be selectable")
			}
		})
	}
	for _, invalid := range []string{`{}`, `{"info":{"outcome":"failure"},"result":{"devices":[]}}`, `{"result":{"devices":[]}}`, "bad JSON"} {
		if _, err := parseDeviceRecords([]byte(invalid)); err == nil {
			t.Fatal("accepted invalid device report", invalid)
		}
	}
	device := readyDevice()
	second := device
	second.Identifier = "aaa"
	devices, err := parseDeviceRecords(deviceReport(device, second))
	if err != nil || len(devices) != 2 || devices[0].Identifier != "aaa" {
		t.Fatal(devices, err)
	}
	if usableDevice(device, "not-selected") {
		t.Fatal("unselected device was accepted")
	}
	var reportPath string
	devices, err = readDeviceReport(func(name string, args ...string) ([]byte, error) {
		if name != "xcrun" || strings.Join(args[:3], " ") != "devicectl list devices" {
			t.Fatal(name, args)
		}
		reportPath = args[len(args)-1]
		return nil, os.WriteFile(reportPath, deviceReport(device), 0600)
	})
	if err != nil || len(devices) != 1 {
		t.Fatal(devices, err)
	}
	if _, err := os.Stat(filepath.Dir(reportPath)); !os.IsNotExist(err) {
		t.Fatal("temporary device report was not cleaned up")
	}
}

func provisioningFixture(expiry, application, team, device string) []byte {
	return []byte(fmt.Sprintf(`<?xml version="1.0"?><plist version="1.0"><dict>
<key>ExpirationDate</key><date>%s</date>
<key>Entitlements</key><dict><key>application-identifier</key><string>%s</string><key>com.apple.developer.team-identifier</key><string>%s</string></dict>
<key>ProvisionedDevices</key><array><string>%s</string></array>
</dict></plist>`, expiry, application, team, device))
}

func TestDeviceProvisioningGate(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"valid", "wildcard", "expired", "wrong-app", "wrong-team", "wrong-device", "no-usable-device"} {
		expiry, application, team, device := "2027-11-05T12:00:00Z", "TEAM.dev.example.test", "TEAM", "test-udid"
		devices := []deviceRecord{readyDevice()}
		switch scenario {
		case "wildcard":
			application = "TEAM.dev.example.*"
		case "expired":
			expiry = "2026-11-05T12:00:00Z"
		case "wrong-app":
			application = "TEAM.other.app"
		case "wrong-team":
			team = "OTHER"
		case "wrong-device":
			device = "another-udid"
		case "no-usable-device":
			devices = nil
		}
		_, err := inspectProvisioning(provisioningFixture(expiry, application, team, device), "dev.example.test", "TEAM", devices, now)
		if (err == nil) != (scenario == "valid" || scenario == "wildcard") {
			t.Fatalf("%s: %v", scenario, err)
		}
	}
	if _, err := inspectProvisioning([]byte(`<plist><array/></plist>`), "app", "TEAM", nil, now); err == nil {
		t.Fatal("invalid profile shape accepted")
	}
	if signatureTeam("TeamIdentifier=not set\n") != "" || signatureTeam("TeamIdentifier=TEAM\n") != "TEAM" {
		t.Fatal("signature team parsing")
	}
}

func TestDeviceDoctorCommandBoundaryAndDryRun(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("device doctor requires macOS")
	}
	directory := t.TempDir()
	app := filepath.Join(directory, "Existing.app")
	if err := os.Mkdir(app, 0700); err != nil {
		t.Fatal(err)
	}
	var executable bytes.Buffer
	for _, word := range []uint32{macho.Magic64, uint32(macho.CpuArm64), 0, uint32(macho.TypeExec), 0, 0, 0, 0} {
		binary.Write(&executable, binary.LittleEndian, word)
	}
	info := `{"CFBundleIdentifier":"dev.example.test","CFBundleExecutable":"Existing","CFBundleSupportedPlatforms":["iPhoneOS"],"NSCameraUsageDescription":"Attach a photo","NSMicrophoneUsageDescription":"Attach a recording","CFBundleURLTypes":[{"CFBundleURLSchemes":["example-test"]}]}`
	for name, content := range map[string][]byte{"Info.plist": []byte(info), "Existing": executable.Bytes(), "embedded.mobileprovision": []byte("fixture only")} {
		if err := os.WriteFile(filepath.Join(app, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string][]byte{"devices.json": deviceReport(readyDevice()), "profile.xml": provisioningFixture("2099-01-01T00:00:00Z", "TEAM.dev.example.test", "TEAM", "test-udid")} {
		if err := os.WriteFile(filepath.Join(directory, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(directory, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
tool=$(basename "$0")
printf '%s %s\n' "$tool" "$*" >> "$DOCTOR_FIXTURE/commands.log"
case "$tool" in
  xcode-select) [ "$*" = '-p' ] || exit 21; echo /Applications/Xcode.app/Contents/Developer;;
  xcodebuild) [ "$*" = '-version' ] || exit 22; echo 'Xcode fixture';;
  xcrun)
    case "$*" in
      '--sdk iphoneos --show-sdk-path') echo /fixture/iphoneos.sdk;;
      '--sdk iphoneos --find swiftc') echo /fixture/swiftc;;
      'devicectl list devices --quiet --timeout 15 --json-output '*)
        for argument in "$@"; do target="$argument"; done
        cp "$DOCTOR_FIXTURE/devices.json" "$target";;
      *) exit 23;;
    esac;;
  plutil) [ "$*" = "-convert json -o - $DOCTOR_APP/Info.plist" ] || exit 24; cat "$DOCTOR_APP/Info.plist";;
  codesign)
    case "$*" in
      "--verify --deep --strict $DOCTOR_APP") :;;
      "-d --verbose=4 $DOCTOR_APP") echo TeamIdentifier=TEAM; echo "Signature=${DOCTOR_SIGNATURE:-development}";;
      *) exit 25;;
    esac;;
  security) [ "$*" = "cms -D -i $DOCTOR_APP/embedded.mobileprovision" ] || exit 26; cat "$DOCTOR_FIXTURE/profile.xml";;
  *) exit 27;;
esac
`
	for _, tool := range []string{"xcode-select", "xcodebuild", "xcrun", "plutil", "codesign", "security"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCTOR_FIXTURE", directory)
	t.Setenv("DOCTOR_APP", app)
	args := []string{"doctor", "--platform", "ios-device", "--app", app, "--require", "camera,microphone,notifications", "--url-scheme", "example-test"}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), append(args, "--dry-run"), &stdout, &stderr); code != 0 {
		t.Fatal(code, &stderr)
	}
	if _, err := os.Stat(filepath.Join(directory, "commands.log")); !os.IsNotExist(err) {
		t.Fatal("dry-run executed commands")
	}
	stdout.Reset()
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("%d\n%s\n%s", code, &stdout, &stderr)
	}
	if strings.Contains(stdout.String(), "FAIL:") || !strings.Contains(stdout.String(), "NOT RUN: physical camera/microphone") {
		t.Fatal(stdout.String())
	}
	t.Setenv("DOCTOR_SIGNATURE", "adhoc")
	if code := run(context.Background(), args, io.Discard, &stderr); code == 0 {
		t.Fatal("ad-hoc signature passed readiness")
	}
	unavailable := readyDevice()
	unavailable.Connection.Tunnel = "unavailable"
	if err := os.WriteFile(filepath.Join(directory, "devices.json"), deviceReport(unavailable), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCTOR_SIGNATURE", "development")
	if code := run(context.Background(), args, io.Discard, &stderr); code == 0 {
		t.Fatal("unavailable device passed readiness")
	}
	after, err := os.ReadFile(filepath.Join(app, "Info.plist"))
	if err != nil || string(after) != info {
		t.Fatal("doctor modified the app")
	}
}

func TestDeviceDoctorUsageErrors(t *testing.T) {
	for _, extra := range [][]string{{"--require", "unknown"}, {"--require", "notifications"}, {"--require", "extensions"}, {"--url-scheme", "https"}, {"--pkg=-exec=oops"}} {
		args := append([]string{"doctor", "--platform", "ios-device", "--dry-run"}, extra...)
		if code := run(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
	if code := run(context.Background(), []string{"doctor", "--platform", "unknown"}, io.Discard, io.Discard); code != 2 {
		t.Fatal("unknown platform passed doctor")
	}
	if checks := inspectDeviceApp(deviceDoctorOptions{}, nil, nil, nil, time.Now()); len(checks) != 1 || checks[0].ok {
		t.Fatal("missing app was treated as ready", checks)
	}
}

func TestDeviceAppRequiresDeclaredPurposesAndExactScheme(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "Info.plist"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	info := []byte(`{"CFBundleIdentifier":"dev.example.test","CFBundleExecutable":"../escape","CFBundleSupportedPlatforms":["iPhoneSimulator"],"NSCameraUsageDescription":" ","CFBundleURLTypes":[{"CFBundleURLSchemes":["other-app"]}]}`)
	checks := inspectDeviceApp(deviceDoctorOptions{app: directory, scheme: "example-test"}, map[string]bool{"camera": true, "microphone": true}, nil,
		func(name string, args ...string) ([]byte, error) {
			if name == "plutil" {
				return info, nil
			}
			return []byte("TeamIdentifier=not set\nSignature=adhoc\n"), nil
		}, time.Now())
	wanted := map[string]bool{"physical iOS bundle identity/platform": false, "arm64 Mach-O executable": false,
		"camera purpose string": false, "microphone purpose string": false, "expected reminder/deep-link scheme": false, "non-ad-hoc team signature": false}
	for _, item := range checks {
		if _, expected := wanted[item.name]; expected {
			if item.ok {
				t.Fatalf("invalid app passed %s", item.name)
			}
			wanted[item.name] = true
		}
	}
	for name, observed := range wanted {
		if !observed {
			t.Fatalf("missing gate %s", name)
		}
	}
}
