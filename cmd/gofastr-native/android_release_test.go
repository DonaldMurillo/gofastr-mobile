package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// releaseSDK returns the Android SDK, or skips when the SDK or JDK the
// release path needs is missing.
func releaseSDK(t *testing.T) androidSDK {
	t.Helper()
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skip(err)
	}
	if jdkTool("java") == "" || jdkTool("jarsigner") == "" {
		t.Skip("no java/jarsigner")
	}
	return sdk
}

// testKeystore creates a throwaway PKCS12 keystore in a temp dir. It is a
// test fixture, never a production credential; the password is random and
// lives only in the named environment variable.
func testKeystore(t *testing.T, sdk androidSDK, env, alias, dname string) (string, string) {
	t.Helper()
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	password := "gf-test-" + hex.EncodeToString(raw)
	t.Setenv(env, password)
	path := filepath.Join(t.TempDir(), "fixture.p12")
	cmd := exec.Command(sdk.keytool, "-genkeypair", "-keystore", path, "-storetype", "PKCS12", "-alias", alias,
		"-keyalg", "RSA", "-keysize", "2048", "-validity", "2", "-dname", dname,
		"-storepass:env", env, "-keypass:env", env)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("keytool: %v\n%s", err, out)
	}
	return path, password
}

// minimalReleaseInputs writes a manifest, a stand-in dex file and stand-in
// native libraries: enough for aapt2, apksigner and badging, which do not
// load the code.
func minimalReleaseInputs(t *testing.T, sdk androidSDK, a app, abis []string) androidReleaseInputs {
	t.Helper()
	work := t.TempDir()
	dir := filepath.Join(work, "android")
	manifest := filepath.Join(dir, "AndroidManifest.xml")
	if err := writeRendered("templates/android/AndroidManifest.xml.tmpl", manifest, a); err != nil {
		t.Fatal(err)
	}
	dex := filepath.Join(dir, "classes.dex")
	if err := os.WriteFile(dex, []byte("dex\n035\x00stand-in"), 0o600); err != nil {
		t.Fatal(err)
	}
	jni := filepath.Join(work, "aar", "jni")
	for _, abi := range abis {
		if err := os.MkdirAll(filepath.Join(jni, abi), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(jni, abi, "libgojni.so"), []byte("\x7fELF stand-in "+abi), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return androidReleaseInputs{work: work, manifest: manifest, dexFile: dex, jniDir: jni}
}

func releaseApp(t *testing.T, sdk androidSDK, abis []string) app {
	t.Helper()
	a := nativeApp(t)
	a.ID = "dev.gofastr.releasetest"
	a.Version = "1.2.3"
	a.applyProfile(releaseOptions{profile: profileRelease, buildNumber: 77, abis: abis, apk: true}, "android-emu")
	return a.forAndroid(sdk.api)
}

func TestAndroidReleaseSignsAndVerifiesWithOwnerKey(t *testing.T) {
	sdk := releaseSDK(t)
	abis := []string{"arm64-v8a", "x86_64"}
	a := releaseApp(t, sdk, abis)
	keystore, password := testKeystore(t, sdk, "GOFASTR_TEST_RELEASE_PASS", "upload", "CN=gofastr test")
	var log bytes.Buffer
	redact := newRedactWriter(&log, []string{password})
	r := &runner{ctx: context.Background(), out: redact}
	signing := androidSigning{keystore: keystore, alias: "upload", storePassEnv: "GOFASTR_TEST_RELEASE_PASS", keyPassEnv: "GOFASTR_TEST_RELEASE_PASS"}
	cert, err := preflightKeystore(r, sdk, signing, time.Now())
	if err != nil {
		t.Fatalf("preflight: %v\n%s", err, log.String())
	}
	if len(cert.sha256) != 64 {
		t.Fatalf("certificate digest %q", cert.sha256)
	}
	in := minimalReleaseInputs(t, sdk, a, abis)
	out := t.TempDir()
	o := releaseOptions{profile: profileRelease, buildNumber: 77, abis: abis, apk: true, android: signing}
	apk, err := releaseAndroid(r, sdk, a, o, out, in, cert)
	if err != nil {
		t.Fatalf("release: %v\n%s", err, log.String())
	}
	if apk != filepath.Join(out, a.Exe+".apk") || !fileExists(apk) {
		t.Fatalf("artifact %s", apk)
	}
	if err := redact.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log.String(), password) {
		t.Fatal("the password reached the output")
	}
	verify, err := exec.Command(jdkTool("java"), "-jar", filepath.Join(sdk.buildTools, "lib", "apksigner.jar"), "verify", "--print-certs", apk).CombinedOutput()
	if err != nil {
		t.Fatalf("independent verify: %v\n%s", err, verify)
	}
	if d := parseSignerDigests(string(verify)); len(d) != 1 || d[0] != cert.sha256 {
		t.Fatalf("signer %v, want %s", d, cert.sha256)
	}
	badgingOut, err := exec.Command(sdk.tool("aapt2"), "dump", "badging", apk).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if b := parseBadging(string(badgingOut)); b.debuggable || b.versionCode != "77" || !sameSet(b.nativeCode, abis) {
		t.Fatalf("badging %+v", b)
	}
	content, err := os.ReadFile(filepath.Join(out, reportName))
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Join(out, reportName)); info != nil && info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("report mode %v", info.Mode())
	}
	var rep releaseReport
	if err := json.Unmarshal(content, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Android == nil || rep.Android.SignerCertificateSHA256 != cert.sha256 || rep.BuildNumber != 77 || len(rep.Artifacts) != 1 {
		t.Fatalf("report %+v", rep)
	}
	for _, leak := range []string{password, "GOFASTR_TEST_RELEASE_PASS", keystore, filepath.Base(keystore)} {
		if strings.Contains(string(content), leak) {
			t.Fatalf("report contains %q", leak)
		}
	}
	notRun := 0
	for _, c := range rep.Checks {
		if c.Status == "FAIL" {
			t.Fatalf("failed check in a written report: %+v", c)
		}
		if c.Status == "NOT RUN" {
			notRun++
		}
	}
	if notRun == 0 {
		t.Fatal("the report claims hardware acceptance")
	}
	if fileExists(filepath.Join(in.work, "android", "signed.apk")) {
		t.Fatal("the signed APK was copied, not moved")
	}
}

func TestAndroidReleaseRefusesDebugKeyAndBadSecrets(t *testing.T) {
	sdk := releaseSDK(t)
	debugKS, _ := testKeystore(t, sdk, "GOFASTR_TEST_DEBUG_PASS", "androiddebugkey", "CN=Android Debug,O=Android,C=US")
	var log bytes.Buffer
	r := &runner{ctx: context.Background(), out: &log}
	if _, err := preflightKeystore(r, sdk, androidSigning{keystore: debugKS, alias: "androiddebugkey", storePassEnv: "GOFASTR_TEST_DEBUG_PASS"}, time.Now()); err == nil || !strings.Contains(err.Error(), "debug key") {
		t.Fatalf("debug certificate accepted: %v", err)
	}
	keystore, password := testKeystore(t, sdk, "GOFASTR_TEST_GOOD_PASS", "upload", "CN=gofastr test")
	t.Setenv("GOFASTR_TEST_WRONG_PASS", "wrong-"+password)
	_, err := preflightKeystore(r, sdk, androidSigning{keystore: keystore, alias: "upload", storePassEnv: "GOFASTR_TEST_WRONG_PASS"}, time.Now())
	if err == nil {
		t.Fatal("a wrong password passed the preflight")
	}
	_, errAlias := preflightKeystore(r, sdk, androidSigning{keystore: keystore, alias: "missing", storePassEnv: "GOFASTR_TEST_GOOD_PASS"}, time.Now())
	if errAlias == nil {
		t.Fatal("a missing alias passed the preflight")
	}
	for _, text := range []string{log.String(), err.Error(), errAlias.Error()} {
		if strings.Contains(text, password) {
			t.Fatal("a password reached the output")
		}
	}
	// Expired certificates are refused too.
	listing, err := exec.Command(sdk.keytool, "-list", "-rfc", "-keystore", keystore, "-alias", "upload", "-storepass:env", "GOFASTR_TEST_GOOD_PASS").Output()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseKeystoreListing(listing, time.Now().AddDate(0, 0, 10)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired certificate accepted: %v", err)
	}
	if _, err := parseKeystoreListing([]byte("Entry type: trustedCertEntry\n"), time.Now()); err == nil {
		t.Fatal("a certificate-only entry was accepted")
	}
}

// The CLI refuses a real build with a bad key before gomobile bind, and
// leaves no artifact.
func TestAndroidReleaseCLIStopsBeforeBindOnBadKey(t *testing.T) {
	sdk := releaseSDK(t)
	keystore, password := testKeystore(t, sdk, "GOFASTR_TEST_CLI_PASS", "upload", "CN=gofastr test")
	t.Setenv("GOFASTR_TEST_CLI_WRONG", password+"-nope")
	out := t.TempDir()
	code, stdout, stderr := runCLI(t, "build", "--platform", "android", "--pkg", nativePkg, "--renderer", "native", "--profile", "release",
		"--build-number", "2", "--out", out, "--keystore", keystore, "--key-alias", "upload", "--keystore-pass-env", "GOFASTR_TEST_CLI_WRONG")
	if code != 1 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, password) {
		t.Fatal("the password reached the output")
	}
	if strings.Contains(stdout, "gomobile") {
		t.Fatal("the build ran gomobile before checking the key")
	}
	if fileExists(filepath.Join(out, "Notesnative.apk")) || fileExists(filepath.Join(out, reportName)) {
		t.Fatal("a failed release left an artifact")
	}
}

func TestBadgingParserAndMismatch(t *testing.T) {
	text := "package: name='dev.gofastr.x' versionCode='7' versionName='1.0.0' platformBuildVersionName='16'\n" +
		"targetSdkVersion:'36'\nnative-code: 'arm64-v8a' 'x86_64'\n"
	b := parseBadging(text)
	a := app{ID: "dev.gofastr.x", VersionCode: 7, Version: "1.0.0", ABIs: []string{"x86_64", "arm64-v8a"}}
	if err := checkBadging(b, a); err != nil || b.targetSDK != "36" {
		t.Fatal(err, b)
	}
	a.ABIs = []string{"x86_64"}
	if err := checkBadging(b, a); err == nil || !strings.Contains(err.Error(), "native-code") {
		t.Fatal("ABI mismatch not detected")
	}
	b = parseBadging(text + "application-debuggable\n")
	a.ABIs = []string{"arm64-v8a", "x86_64"}
	if err := checkBadging(b, a); err == nil || !strings.Contains(err.Error(), "debuggable") {
		t.Fatal("debuggable APK not detected")
	}
	if d := parseSignerDigests("Signer #1 certificate DN: CN=x\nSigner #1 certificate SHA-256 digest: ABCD\n"); len(d) != 1 || d[0] != "abcd" {
		t.Fatal(d)
	}
}

func TestAARABICheck(t *testing.T) {
	dir := t.TempDir()
	for _, abi := range []string{"arm64-v8a", "x86_64"} {
		if err := os.MkdirAll(filepath.Join(dir, abi), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkAARABIs(dir, []string{"x86_64", "arm64-v8a"}); err != nil {
		t.Fatal(err)
	}
	if err := checkAARABIs(dir, []string{"x86_64"}); err == nil {
		t.Fatal("an extra ABI was accepted")
	}
}

// aapt2's --proto-format output repacks into the module layout bundletool
// reads, with a protobuf (not binary XML) manifest and exactly the
// selected native libraries.
func TestBundleModuleFromRealProtoLink(t *testing.T) {
	sdk := releaseSDK(t)
	abis := []string{"x86_64"}
	a := releaseApp(t, sdk, abis)
	in := minimalReleaseInputs(t, sdk, a, abis)
	proto := filepath.Join(in.work, "android", "proto.apk")
	r := &runner{ctx: context.Background(), out: &bytes.Buffer{}}
	if err := r.run("proto link", sdk.tool("aapt2"), aapt2LinkArgs(sdk, a, in.manifest, proto, nil, true)...); err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(in.work, "base.zip")
	if err := buildBundleModule(proto, in.dexFile, in.jniDir, abis, module); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(module)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]*zip.File{}
	for _, f := range zr.File {
		names[f.Name] = f
		if !(f.Name == "manifest/AndroidManifest.xml" || f.Name == "resources.pb" || f.Name == "dex/classes.dex" ||
			strings.HasPrefix(f.Name, "res/") || strings.HasPrefix(f.Name, "lib/x86_64/")) {
			t.Errorf("unexpected module entry %s", f.Name)
		}
	}
	for _, want := range []string{"manifest/AndroidManifest.xml", "resources.pb", "dex/classes.dex", "lib/x86_64/libgojni.so"} {
		if names[want] == nil {
			t.Errorf("module lacks %s", want)
		}
	}
	rc, err := names["manifest/AndroidManifest.xml"].Open()
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 4)
	_, _ = rc.Read(head)
	rc.Close()
	if bytes.Equal(head, []byte{0x03, 0x00, 0x08, 0x00}) {
		t.Fatal("the manifest is binary XML, not protobuf")
	}
	// A wrong ABI selection and an unexpected proto entry are errors.
	if err := buildBundleModule(proto, in.dexFile, in.jniDir, []string{"arm64-v8a"}, module); err == nil {
		t.Fatal("libraries outside --abi were accepted")
	}
	bad := filepath.Join(in.work, "bad.apk")
	writeZip(t, bad, map[string]string{"AndroidManifest.xml": "m", "resources.pb": "r", "assets/secret.txt": "x"})
	if err := buildBundleModule(bad, in.dexFile, in.jniDir, abis, module); err == nil || !strings.Contains(err.Error(), "unexpected proto APK entry") {
		t.Fatalf("an unexpected entry was dropped or accepted: %v", err)
	}
	escape := filepath.Join(in.work, "escape.apk")
	writeZip(t, escape, map[string]string{"AndroidManifest.xml": "m", "resources.pb": "r", "res/../../x": "x"})
	if err := buildBundleModule(escape, in.dexFile, in.jniDir, abis, module); err == nil {
		t.Fatal("an escaping entry name was accepted")
	}
}

// With an owner-supplied bundletool (GOFASTR_BUNDLETOOL), the whole AAB
// path runs: build, jarsigner sign and verify, validate, and the 16 KiB
// alignment survives in the bundle config. Without it the test skips; the
// report marks these checks NOT RUN.
func TestAndroidBundleWithOwnerBundletool(t *testing.T) {
	sdk := releaseSDK(t)
	bt := os.Getenv(bundletoolEnv)
	if bt == "" || !fileExists(bt) {
		t.Skip("set " + bundletoolEnv + " to an owner-supplied bundletool.jar to run the bundle path (never downloaded)")
	}
	abis := []string{"arm64-v8a", "x86_64"}
	a := releaseApp(t, sdk, abis)
	keystore, _ := testKeystore(t, sdk, "GOFASTR_TEST_AAB_PASS", "upload", "CN=gofastr test")
	signing := androidSigning{keystore: keystore, alias: "upload", storePassEnv: "GOFASTR_TEST_AAB_PASS", keyPassEnv: "GOFASTR_TEST_AAB_PASS"}
	var log bytes.Buffer
	r := &runner{ctx: context.Background(), out: &log}
	cert, err := preflightKeystore(r, sdk, signing, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	in := minimalReleaseInputs(t, sdk, a, abis)
	out := t.TempDir()
	o := releaseOptions{profile: profileRelease, buildNumber: 77, abis: abis, aab: true, bundletool: bt, android: signing}
	aab, err := releaseAndroid(r, sdk, a, o, out, in, cert)
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if !strings.HasSuffix(aab, ".aab") || !fileExists(aab) {
		t.Fatal(aab)
	}
}
