package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDeviceFeaturesAreOptInAndValidatePlatformConfiguration(t *testing.T) {
	a := nativeApp(t)
	if err := validateDeviceFeatures(a, "android-emu"); err != nil {
		t.Fatal(err)
	}
	a.Widgets = true
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("widget without scheme accepted")
	}
	a.URLScheme = "feature-test"
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("iOS widget without group accepted")
	}
	a.AppGroup = "group.dev.gofastr.test"
	a.ReceiveShares, a.SecureStorage = true, true
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("iOS biometrics without purpose accepted")
	}
	a.BiometricUsage = "Authenticate to read the demo secret."
	if err := validateDeviceFeatures(a, "ios-sim"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"templates/android/AndroidManifest.xml.tmpl", "templates/android/widget.xml.tmpl", "templates/ios/Info.plist.tmpl", "templates/ios/ShareExtension.plist.tmpl", "templates/ios/WidgetExtension.plist.tmpl", "templates/ios/DeviceFeatures.entitlements.tmpl"} {
		content, err := render(name, a)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, name, content)
	}
	manifest, _ := render("templates/android/AndroidManifest.xml.tmpl", a)
	for _, expected := range []string{"android.intent.action.SEND", "USE_BIOMETRIC", "HomeWidget", "@xml/gofastr_widget"} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Error("missing", expected)
		}
	}
	plain := nativeApp(t)
	manifest, _ = render("templates/android/AndroidManifest.xml.tmpl", plain)
	for _, absent := range []string{"android.intent.action.SEND", "USE_BIOMETRIC", "HomeWidget"} {
		if bytes.Contains(manifest, []byte(absent)) {
			t.Error("unsolicited feature", absent)
		}
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("native flags accepted by WebView")
	}
}

func TestIOSFeatureExtensionPlanKeepsExecutablesAndKeychainSeparate(t *testing.T) {
	a := nativeApp(t)
	a.ReceiveShares, a.Widgets, a.SecureStorage = true, true, true
	a.AppGroup, a.URLScheme = "group.dev.gofastr.features", "feature-test"
	var plan bytes.Buffer
	directory := filepath.Join(t.TempDir(), "unwritten")
	if err := buildIOSFeatureExtensions(&runner{ctx: context.Background(), dryRun: true, out: &plan}, a, directory, filepath.Join(directory, "Features.app"), "<sdk>", "arm64-apple-ios16.0-simulator"); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"-application-extension", "GofastrShareViewController.swift", "GofastrWidgetExtension.swift", "PlugIns", "--entitlements"} {
		if !strings.Contains(plan.String(), expected) {
			t.Error("extension plan omits", expected)
		}
	}
	if strings.Contains(plan.String(), "-framework Notesnative") {
		t.Fatal("extension links the Go runtime")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("dry run wrote extension output")
	}
	content, _ := render("templates/ios/DeviceFeatures.entitlements.tmpl", a)
	if !bytes.Contains(content, []byte("keychain-access-groups")) {
		t.Fatal("host has no private Keychain group")
	}
	a.SecureStorage = false
	content, _ = render("templates/ios/DeviceFeatures.entitlements.tmpl", a)
	if bytes.Contains(content, []byte("keychain-access-groups")) || bytes.Contains(content, []byte("application-identifier")) {
		t.Fatal("extension inherits host secret access")
	}
}

func TestAndroidWidgetResourcesCompileAndLink(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skip(err)
	}
	a := nativeApp(t).forAndroid(sdk.api)
	a.Widgets, a.ReceiveShares, a.SecureStorage = true, true, true
	a.URLScheme, a.BiometricUsage = "feature-test", "Read a demo secret."
	dir := t.TempDir()
	r := &runner{ctx: context.Background(), out: io.Discard}
	resources, err := prepareAndroidFeatureResources(r, sdk, a, dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "AndroidManifest.xml")
	if err := writeRendered("templates/android/AndroidManifest.xml.tmpl", manifest, a); err != nil {
		t.Fatal(err)
	}
	if err := r.run("link feature resources", sdk.tool("aapt2"), append([]string{"link", "-o", filepath.Join(dir, "features.apk"), "--manifest", manifest, "-I", sdk.androidJar}, resources...)...); err != nil {
		t.Fatal(err)
	}
}

func TestIOSDeviceFeatureExtensionsCompileAgainstSimulatorSDK(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("iOS extension compilation requires macOS and Xcode")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("Xcode toolchain unavailable")
	}
	a := nativeApp(t)
	a.ReceiveShares, a.Widgets = true, true
	a.AppGroup, a.URLScheme = "group.dev.gofastr.featuretest", "feature-test"
	r := &runner{ctx: context.Background(), out: io.Discard}
	sdk, err := r.output("find simulator SDK", "", "xcrun", "--sdk", "iphonesimulator", "--show-sdk-path")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	if err := buildIOSFeatureExtensions(r, a, dir, filepath.Join(dir, "FeatureTest.app"), sdk, simArch()+"-apple-ios16.0-simulator"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Share", "Widget"} {
		bundle := filepath.Join(dir, "FeatureTest.app", "PlugIns", name+".appex")
		if info, err := os.Stat(filepath.Join(bundle, name)); err != nil || info.Size() == 0 {
			t.Fatalf("missing compiled %s extension: %v", name, err)
		}
		if err := r.run("verify extension signature", "codesign", "--verify", "--strict", bundle); err != nil {
			t.Fatal(err)
		}
	}
}
