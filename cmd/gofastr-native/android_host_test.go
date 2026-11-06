package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	renderandroid "github.com/DonaldMurillo/gofastr-native/render/android"
)

func TestNativeAndroidCompilesAgainstSDKAndGeneratedBindings(t *testing.T) {
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	a := nativeApp(t).forAndroid(sdk.api)
	a.Extensions, err = discoverPlatformExtensions(&runner{ctx: context.Background(), out: io.Discard}, a.PkgPath, "android")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	sources, err := prepareAndroidHost(&runner{ctx: context.Background(), out: io.Discard}, a, directory)
	if err != nil {
		t.Fatal(err)
	}
	// Only go.Seq/Universe need stand-ins. All Android APIs are checked against
	// the real SDK, and binding names/signatures against the pinned gobind.
	fixture, err := os.ReadFile(filepath.Join("testdata", "androidstubs.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range strings.Split(strings.ReplaceAll(string(fixture), "\r\n", "\n"), "\n--- ")[1:] {
		name, body, _ := strings.Cut(chunk, "\n")
		if !strings.HasPrefix(name, "go/") {
			continue
		}
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := writeSource(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	bindings := gobindPackageOut(t, a.PkgPath)
	java, err := listFiles(filepath.Join(bindings, "java"), ".java")
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, java...)
	for _, kind := range []string{"file", "component"} {
		files, err := scaffoldFiles("example.android-"+kind+"/v1", kind)
		if err != nil {
			t.Fatal(err)
		}
		for name, content := range files {
			if strings.HasSuffix(name, ".java") {
				path := filepath.Join(directory, "scaffold-"+kind, name)
				if err := writeSource(path, content); err != nil {
					t.Fatal(err)
				}
				sources = append(sources, path)
			}
		}
	}
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", sdk.androidJar, "-d", filepath.Join(directory, "classes")}, sources...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("native Android SDK compile: %v\n%s", err, output)
	}
	entries, _ := renderandroid.Sources.ReadDir(".")
	for _, entry := range entries {
		want, _ := renderandroid.Sources.ReadFile(entry.Name())
		got, err := os.ReadFile(filepath.Join(directory, "renderer", entry.Name()))
		if err != nil || !bytes.Equal(want, got) {
			t.Fatalf("renderer source changed: %s %v", entry.Name(), err)
		}
	}
	bootstrap, _ := os.ReadFile(sources[0])
	if len(bootstrap) > 4096 || bytes.Contains(bootstrap, []byte("EditText")) || bytes.Contains(bootstrap, []byte("WebView")) {
		t.Fatal("native Android bootstrap contains rendering logic")
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "native Android manifest", manifest)
	for _, expected := range []string{"TransferProvider", "ReminderReceiver", "POST_NOTIFICATIONS"} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Errorf("manifest omits %s", expected)
		}
	}
}

func TestNativeAndroidDryRunAndPermissionPackaging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	directory := filepath.Join(t.TempDir(), "unwritten")
	args := []string{"build", "--platform", "android-emu", "--renderer", "native", "--pkg", "../../examples/notes/notesnative", "--url-scheme", "notes-test", "--microphone-usage", "Record private audio.", "--out", directory, "--dry-run"}
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, &stderr)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("dry run wrote output")
	}
	if !strings.Contains(stdout.String(), "GofastrActivity.java") || !strings.Contains(stdout.String(), "Android SQLite") {
		t.Fatal("native Android plan omits renderer or SQLite compatibility")
	}
	a := nativeApp(t).forAndroid(36)
	a.MicrophoneUsage = "Record audio."
	a.URLScheme = "notes-test"
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"android.permission.RECORD_AUDIO", `android:scheme="notes-test"`} {
		if !bytes.Contains(manifest, []byte(expected)) {
			t.Errorf("manifest omits %s", expected)
		}
	}
	a.MicrophoneUsage = ""
	a.URLScheme = ""
	manifest, _ = render("templates/android/AndroidManifest.xml.tmpl", a)
	if bytes.Contains(manifest, []byte("RECORD_AUDIO")) || bytes.Contains(manifest, []byte("BROWSABLE")) {
		t.Fatal("optional permission/link registration was not removed")
	}
}

func TestDefaultAndroidAVDPrefersPhoneToTV(t *testing.T) {
	if got := preferredAndroidAVD([]string{"Homi_TV_API34", "Pixel_5_API_34"}); got != "Pixel_5_API_34" {
		t.Fatalf("phone APK would boot incompatible TV image: %s", got)
	}
	if got := preferredAndroidAVD([]string{"custom_avd"}); got != "custom_avd" {
		t.Fatal(got)
	}
}
