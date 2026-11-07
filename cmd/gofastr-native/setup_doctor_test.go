package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeAndroidSDK lays out the files findAndroidSDK looks for. The files
// are empty: doctor only checks that they exist, and the probes that would
// run them are faked.
func fakeAndroidSDK(t *testing.T) (sdk, jdk string) {
	t.Helper()
	root := t.TempDir()
	sdk, jdk = filepath.Join(root, "sdk"), filepath.Join(root, "jdk")
	exe := func(name string) string { return executableName(name) }
	bat := func(name string) string {
		if runtime.GOOS == "windows" {
			return name + ".bat"
		}
		return name
	}
	for _, path := range []string{
		filepath.Join(sdk, "build-tools", "35.0.0", bat("d8")),
		filepath.Join(sdk, "build-tools", "35.0.0", exe("aapt2")),
		filepath.Join(sdk, "build-tools", "35.0.0", exe("zipalign")),
		filepath.Join(sdk, "build-tools", "35.0.0", bat("apksigner")),
		filepath.Join(sdk, "platforms", "android-35", "android.jar"),
		filepath.Join(sdk, "ndk", "27.2.12479018", "source.properties"),
		filepath.Join(sdk, "platform-tools", exe("adb")),
		filepath.Join(sdk, "emulator", exe("emulator")),
		filepath.Join(jdk, "bin", exe("javac")),
		filepath.Join(jdk, "bin", exe("keytool")),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("ANDROID_HOME", sdk)
	t.Setenv("ANDROID_SDK_ROOT", "")
	t.Setenv("ANDROID_NDK_HOME", "")
	t.Setenv("JAVA_HOME", jdk)
	t.Setenv(bundletoolEnv, "")
	return sdk, jdk
}

func TestDoctorReleaseToolsNeverFailADebugSetup(t *testing.T) {
	_, jdk := fakeAndroidSDK(t)
	t.Setenv("PATH", t.TempDir()) // jdkTool also searches PATH; every probe here is faked
	code, out, _ := runDoctor(t, "javac 17.0.9", doctorOptions{platform: "android-emu", goos: "windows"})
	if code != 0 || !strings.Contains(out, "[warn] jarsigner") || !strings.Contains(out, "[info] bundletool: not configured") {
		t.Fatalf("code %d\n%s", code, out)
	}
	jarsigner := filepath.Join(jdk, "bin", executableName("jarsigner"))
	if err := os.WriteFile(jarsigner, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(bundletoolEnv, filepath.Join(jdk, "missing-bundletool.jar"))
	code, out, _ = runDoctor(t, "javac 17.0.9", doctorOptions{platform: "android-emu", goos: "windows"})
	if code != 1 || !strings.Contains(out, "[info] jarsigner") || !strings.Contains(out, "[MISS] "+bundletoolEnv+" names a file") {
		t.Fatalf("a %s naming no file must fail doctor: code %d\n%s", bundletoolEnv, code, out)
	}
}

type doctorProbe struct {
	javac string
	calls []string
}

func (p *doctorProbe) run(name string, args ...string) (string, error) {
	call := filepath.Base(name) + " " + strings.Join(args, " ")
	p.calls = append(p.calls, call)
	base := strings.TrimSuffix(filepath.Base(name), ".exe")
	switch {
	case base == "go" && len(args) > 0 && args[0] == "version":
		return "go version go1.27.0 windows/amd64\n", nil
	case base == "go" && len(args) > 0 && args[0] == "env":
		return "go1.27.0\nauto\n\n\n", nil // not in a module
	case base == "emulator" && args[0] == "-list-avds":
		return "Pixel_9\n", nil
	case base == "emulator" && args[0] == "-accel-check":
		return "accel:\n0\nWHPX(10.0.19045) is installed and usable.\naccel\n", nil
	case base == "javac":
		return p.javac + "\n", nil
	}
	return "", errors.New("unexpected probe: " + call)
}

func runDoctor(t *testing.T, javac string, o doctorOptions) (int, string, *doctorProbe) {
	t.Helper()
	probe := &doctorProbe{javac: javac}
	var out bytes.Buffer
	r := &runner{ctx: context.Background(), out: &out, probe: probe.run}
	code := doctorWithOptions(r, o)
	return code, out.String(), probe
}

func TestDoctorRequiresJavac17(t *testing.T) {
	fakeAndroidSDK(t)
	code, out, _ := runDoctor(t, "javac 11.0.2", doctorOptions{platform: "android-emu", goos: "windows"})
	if code != 1 || !strings.Contains(out, "[MISS] javac 17 or newer: javac 11.0.2") || !strings.Contains(out, "from JAVA_HOME") {
		t.Fatalf("javac 11: code %d\n%s", code, out)
	}
	code, out, _ = runDoctor(t, "javac 17.0.9", doctorOptions{platform: "android-emu", goos: "windows"})
	if code != 0 || !strings.Contains(out, "[ok  ] javac 17 or newer") {
		t.Fatalf("javac 17: code %d\n%s", code, out)
	}
	if !strings.Contains(out, "next: gofastr-native init") {
		t.Errorf("outside a module, a passing doctor should point at init:\n%s", out)
	}
	for _, major := range []struct {
		in   string
		want int
	}{{"javac 1.8.0_292", 8}, {"javac 21", 21}, {"javac 17.0.9", 17}, {"no javac", 0}} {
		if got := javacMajor(major.in); got != major.want {
			t.Errorf("javacMajor(%q) = %d, want %d", major.in, got, major.want)
		}
	}
}

func TestDoctorReportsTheSDKSourceAndConflicts(t *testing.T) {
	sdk, _ := fakeAndroidSDK(t)
	code, out, _ := runDoctor(t, "javac 17.0.9", doctorOptions{platform: "android-emu", goos: "windows"})
	if code != 0 || !strings.Contains(out, "[info] Android SDK root: "+sdk+" (from ANDROID_HOME)") {
		t.Fatalf("code %d\n%s", code, out)
	}
	t.Setenv("ANDROID_SDK_ROOT", filepath.Join(sdk, "elsewhere"))
	code, out, _ = runDoctor(t, "javac 17.0.9", doctorOptions{platform: "android-emu", goos: "windows"})
	if code != 0 || !strings.Contains(out, "[warn] ANDROID_HOME and ANDROID_SDK_ROOT differ") {
		t.Fatalf("a warning must not fail doctor: code %d\n%s", code, out)
	}
	t.Setenv("ANDROID_NDK_HOME", filepath.Join(sdk, "no-such-ndk"))
	if code, out, _ = runDoctor(t, "javac 17.0.9", doctorOptions{platform: "android-emu", goos: "windows"}); code != 1 || !strings.Contains(out, "[MISS] ANDROID_NDK_HOME") {
		t.Fatalf("a missing ANDROID_NDK_HOME: code %d\n%s", code, out)
	}
}

func TestDoctorStartsNoADBServerWithoutDevices(t *testing.T) {
	fakeAndroidSDK(t)
	_, out, probe := runDoctor(t, "javac 17.0.9", doctorOptions{platform: "android-emu", goos: "windows"})
	for _, call := range probe.calls {
		if strings.HasPrefix(call, executableName("adb")) {
			t.Errorf("doctor ran %q without --devices", call)
		}
	}
	if !strings.Contains(out, "doctor --devices") {
		t.Errorf("no hint about --devices:\n%s", out)
	}
	if !strings.Contains(out, "[info] emulator acceleration: WHPX") {
		t.Errorf("acceleration summary:\n%s", out)
	}
}

func TestAndroidFixMatchesTheHostShell(t *testing.T) {
	windows := androidFixFor("windows")
	if !strings.Contains(windows, "$env:ANDROID_HOME = ") || !strings.Contains(windows, "SetEnvironmentVariable") || strings.Contains(windows, "export ") {
		t.Errorf("windows fix: %s", windows)
	}
	for _, goos := range []string{"darwin", "linux"} {
		if fix := androidFixFor(goos); !strings.Contains(fix, "export ANDROID_HOME=") || strings.Contains(fix, "$env:") {
			t.Errorf("%s fix: %s", goos, fix)
		}
	}
}
