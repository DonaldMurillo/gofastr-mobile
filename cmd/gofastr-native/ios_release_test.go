package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testTeam = "ABCDE12345"

// fakeProfile describes a synthetic decoded provisioning profile. The
// "certificate" is random bytes; only its SHA-1 matters to the checks.
type fakeProfile struct {
	name, appID, team string
	devices           []string
	allDevices        bool
	getTaskAllow      bool
	keychain, groups  []string
	expiry            time.Time
	cert              []byte
}

func newFakeProfile(appID string) fakeProfile {
	cert := make([]byte, 64)
	_, _ = rand.Read(cert)
	return fakeProfile{name: "test " + appID, appID: testTeam + "." + appID, team: testTeam,
		devices: []string{"00008110-000A1B2C3D4E5F60"}, getTaskAllow: true,
		keychain: []string{testTeam + ".*"}, expiry: time.Now().AddDate(0, 1, 0), cert: cert}
}

func (f fakeProfile) sha1() string {
	sum := sha1.Sum(f.cert)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func (f fakeProfile) xml() []byte {
	arr := func(items []string) string {
		var b strings.Builder
		b.WriteString("<array>")
		for _, item := range items {
			fmt.Fprintf(&b, "<string>%s</string>", item)
		}
		b.WriteString("</array>")
		return b.String()
	}
	boolean := func(v bool) string {
		if v {
			return "<true/>"
		}
		return "<false/>"
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>`)
	fmt.Fprintf(&b, "<key>Name</key><string>%s</string>", f.name)
	b.WriteString("<key>UUID</key><string>2B1C6D0E-0000-4000-8000-000000000001</string>")
	fmt.Fprintf(&b, "<key>TeamIdentifier</key>%s", arr([]string{f.team}))
	fmt.Fprintf(&b, "<key>ApplicationIdentifierPrefix</key>%s", arr([]string{f.team}))
	fmt.Fprintf(&b, "<key>ExpirationDate</key><date>%s</date>", f.expiry.UTC().Format(time.RFC3339))
	if f.allDevices {
		b.WriteString("<key>ProvisionsAllDevices</key><true/>")
	} else if len(f.devices) > 0 {
		fmt.Fprintf(&b, "<key>ProvisionedDevices</key>%s", arr(f.devices))
	}
	fmt.Fprintf(&b, "<key>DeveloperCertificates</key><array><data>%s</data></array>", base64.StdEncoding.EncodeToString(f.cert))
	b.WriteString("<key>Entitlements</key><dict>")
	fmt.Fprintf(&b, "<key>application-identifier</key><string>%s</string>", f.appID)
	fmt.Fprintf(&b, "<key>com.apple.developer.team-identifier</key><string>%s</string>", f.team)
	fmt.Fprintf(&b, "<key>get-task-allow</key>%s", boolean(f.getTaskAllow))
	if f.keychain != nil {
		fmt.Fprintf(&b, "<key>keychain-access-groups</key>%s", arr(f.keychain))
	}
	if f.groups != nil {
		fmt.Fprintf(&b, "<key>com.apple.security.application-groups</key>%s", arr(f.groups))
	}
	b.WriteString("</dict></dict></plist>")
	return []byte(b.String())
}

func parsedFake(t *testing.T, f fakeProfile) provisioningProfile {
	t.Helper()
	p, err := parseProvisioningProfile(f.xml())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProvisioningProfileKinds(t *testing.T) {
	dev := newFakeProfile("dev.gofastr.x")
	adhoc := dev
	adhoc.getTaskAllow = false
	store := adhoc
	store.devices = nil
	enterprise := adhoc
	enterprise.devices = nil
	enterprise.allDevices = true
	for _, tc := range []struct {
		f    fakeProfile
		want string
	}{{dev, profileDevelopment}, {adhoc, profileAdHoc}, {store, profileAppStore}, {enterprise, profileEnterprise}} {
		if got := parsedFake(t, tc.f).kind(); got != tc.want {
			t.Errorf("kind %s, want %s", got, tc.want)
		}
	}
	p := parsedFake(t, dev)
	if len(p.CertSHA1s) != 1 || p.CertSHA1s[0] != dev.sha1() || p.Prefix != testTeam {
		t.Fatalf("parsed %+v", p)
	}
}

func TestProvisioningProfileCheck(t *testing.T) {
	now := time.Now()
	base := newFakeProfile("dev.gofastr.x")
	base.groups = []string{"group.dev.gofastr.x"}
	req := profileRequirement{bundleID: "dev.gofastr.x", teamID: testTeam, wantKind: profileDevelopment, identitySHA1: base.sha1()}
	if err := parsedFake(t, base).check(req, now); err != nil {
		t.Fatal(err)
	}
	mutate := func(fn func(*fakeProfile)) provisioningProfile {
		f := base
		fn(&f)
		return parsedFake(t, f)
	}
	for _, tc := range []struct {
		name string
		p    provisioningProfile
		req  profileRequirement
		want string
	}{
		{"ad hoc for debug", mutate(func(f *fakeProfile) { f.getTaskAllow = false }), req, "needs a development profile"},
		{"development for release", parsedFake(t, base), func() profileRequirement { r := req; r.wantKind = profileAdHoc; return r }(), "needs an ad hoc distribution profile"},
		{"app store", mutate(func(f *fakeProfile) { f.devices = nil; f.getTaskAllow = false }), req, "App Store profiles are not supported yet"},
		{"enterprise", mutate(func(f *fakeProfile) { f.allDevices = true; f.devices = nil }), req, "enterprise"},
		{"expired", mutate(func(f *fakeProfile) { f.expiry = now.AddDate(0, 0, -1) }), req, "expired"},
		{"wrong team", mutate(func(f *fakeProfile) { f.team = "ZZZZZ99999" }), req, "does not match --team-id"},
		{"other app", mutate(func(f *fakeProfile) { f.appID = testTeam + ".dev.other" }), req, "does not cover"},
		{"wildcard with group", mutate(func(f *fakeProfile) { f.appID = testTeam + ".*" }), func() profileRequirement { r := req; r.exact = true; return r }(), "wildcard App ID"},
		{"missing group", parsedFake(t, base), func() profileRequirement { r := req; r.appGroup = "group.dev.other"; return r }(), "does not allow app group"},
		{"missing keychain", mutate(func(f *fakeProfile) { f.keychain = []string{testTeam + ".dev.other"} }), func() profileRequirement { r := req; r.keychain = true; return r }(), "keychain-access-groups do not cover"},
		{"cert mismatch", parsedFake(t, base), func() profileRequirement { r := req; r.identitySHA1 = strings.Repeat("A", 40); return r }(), "is not one of the profile's certificates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.p.check(tc.req, now); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
	// A wildcard App ID is fine for a plain app.
	if err := mutate(func(f *fakeProfile) { f.appID = testTeam + ".*" }).check(req, now); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceEntitlementRules(t *testing.T) {
	app := newFakeProfile("dev.gofastr.x")
	app.groups = []string{"group.dev.gofastr.x"}
	p := parsedFake(t, app)
	appEnt := deviceEntitlements(p, "dev.gofastr.x", testTeam, "group.dev.gofastr.x", true)
	if got := appEnt[entKeychainGroups]; fmt.Sprint(got) != "["+testTeam+".dev.gofastr.x]" {
		t.Fatalf("app keychain group %v", got)
	}
	if err := appEnt.allowedBy(p); err != nil {
		t.Fatal(err)
	}
	ext := deviceEntitlements(p, "dev.gofastr.x.Share", testTeam, "group.dev.gofastr.x", false)
	if _, ok := ext[entKeychainGroups]; ok {
		t.Fatal("an extension received keychain-access-groups")
	}
	// An entitlement the profile does not grant is caught.
	p.AppGroups = nil
	if err := appEnt.allowedBy(p); err == nil || !strings.Contains(err.Error(), "application-groups") {
		t.Fatalf("ungranted app group passed: %v", err)
	}
	round, err := parseEntitlements(append([]byte("Executable=/x\n"), appEnt.plist()...))
	if err != nil {
		t.Fatal(err)
	}
	if !round.equal(appEnt) {
		t.Fatalf("round trip %v != %v", round, appEnt)
	}
	if !bytes.Equal(appEnt.plist(), round.plist()) {
		t.Fatal("plist output is not deterministic")
	}
	delete(round, entGetTaskAllow)
	if round.equal(appEnt) {
		t.Fatal("equal ignored a missing key")
	}
}

func TestParseFindIdentity(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("B", 40)
	listing := fmt.Sprintf("  1) %s \"Apple Development: Dev (X)\"\n  2) %s \"Apple Distribution: Co (Y)\"\n  3) %s \"Apple Distribution: Co (Y)\"\n     3 valid identities found\n", a, b, strings.Repeat("c", 40))
	if got, err := parseFindIdentity(listing, "Apple Development: Dev (X)"); err != nil || got != strings.ToUpper(a) {
		t.Fatal(got, err)
	}
	if _, err := parseFindIdentity(listing, "Apple Development"); err == nil {
		t.Fatal("a partial name matched")
	}
	if _, err := parseFindIdentity(listing, "Apple Distribution: Co (Y)"); err == nil || !strings.Contains(err.Error(), "2 identities") {
		t.Fatalf("ambiguous name: %v", err)
	}
}

func TestPackIPA(t *testing.T) {
	appDir := filepath.Join(t.TempDir(), "Notes.app")
	files := map[string]string{"Notes": "bin", "Info.plist": "plist", "PlugIns/Share.appex/Share": "ext", "PlugIns/Share.appex/Info.plist": "p"}
	for name, content := range files {
		p := filepath.Join(appDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	first, second := filepath.Join(dir, "a.ipa"), filepath.Join(dir, "b.ipa")
	exec := []string{"Notes", "PlugIns/Share.appex/Share"}
	if err := packIPA(appDir, first, exec); err != nil {
		t.Fatal(err)
	}
	if err := packIPA(appDir, second, exec); err != nil {
		t.Fatal(err)
	}
	x, _ := os.ReadFile(first)
	y, _ := os.ReadFile(second)
	if !bytes.Equal(x, y) {
		t.Fatal("the IPA is not deterministic")
	}
	zr, err := zip.OpenReader(first)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	modes := map[string]os.FileMode{}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		modes[f.Name] = f.Mode()
	}
	if names[0] != "Payload/" || names[1] != "Payload/Notes.app/" {
		t.Fatalf("entries %v", names)
	}
	for name, want := range map[string]os.FileMode{
		"Payload/Notes.app/Notes":                     0o755,
		"Payload/Notes.app/PlugIns/Share.appex/Share": 0o755,
		"Payload/Notes.app/Info.plist":                0o644,
	} {
		if modes[name].Perm() != want {
			t.Errorf("%s mode %v, want %v", name, modes[name], want)
		}
	}
	if !modes["Payload/Notes.app/PlugIns/"].IsDir() {
		t.Error("directory entries lack the directory mode")
	}
	if fileExists(first + ".partial") {
		t.Fatal("the partial file was left behind")
	}
	if err := os.Symlink("Notes", filepath.Join(appDir, "link")); err != nil {
		t.Logf("symlink check skipped: %v", err)
		return
	}
	if err := packIPA(appDir, filepath.Join(dir, "c.ipa"), exec); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("a symlink was packed: %v", err)
	}
	if fileExists(filepath.Join(dir, "c.ipa")) || fileExists(filepath.Join(dir, "c.ipa.partial")) {
		t.Fatal("a refused IPA left a file")
	}
}

// The ios-device dry run, with a test decoder standing in for security
// cms, plans a device build signed inside-out with every profile checked.
func TestIOSDeviceDryRunPlan(t *testing.T) {
	const id = "dev.gofastr.notes.releasetest"
	const group = "group.dev.gofastr.notes.releasetest"
	dir := t.TempDir()
	profiles := map[string]fakeProfile{}
	app := newFakeProfile(id)
	app.groups = []string{group}
	paths := map[string]string{}
	for label, f := range map[string]fakeProfile{"app": app, "share": newFakeProfile(id + ".Share"), "widget": newFakeProfile(id + ".Widget")} {
		f.cert = app.cert
		f.keychain = app.keychain
		if label != "app" {
			f.groups = []string{group}
			f.keychain = nil
		}
		path := filepath.Join(dir, label+".mobileprovision")
		if err := os.WriteFile(path, []byte("cms-wrapped stand-in"), 0o600); err != nil {
			t.Fatal(err)
		}
		profiles[path] = f
		paths[label] = path
	}
	restore, injected := decodeProvisioningProfile, profileDecoderInjected
	t.Cleanup(func() { decodeProvisioningProfile, profileDecoderInjected = restore, injected })
	var decoded []string
	decodeProvisioningProfile = func(r *runner, path string) ([]byte, error) {
		f, ok := profiles[path]
		if !ok {
			return nil, fmt.Errorf("unexpected profile %s", path)
		}
		decoded = append(decoded, filepath.Base(path))
		return f.xml(), nil
	}
	profileDecoderInjected = true
	out := t.TempDir()
	code, stdout, stderr := runCLI(t, "build", "--platform", "ios-device", "--pkg", nativePkg, "--renderer", "native", "--dry-run",
		"--id", id, "--out", out, "--team-id", testTeam, "--sign-identity", app.sha1(),
		"--provisioning-profile", paths["app"], "--receive-shares", "--share-provisioning-profile", paths["share"],
		"--widgets", "--url-scheme", "notes-release", "--widget-provisioning-profile", paths["widget"],
		"--app-group", group, "--secure-storage", "--biometric-usage", "Unlock your notes")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout, stderr)
	}
	plan := planText(stdout)
	if len(decoded) != 3 {
		t.Fatalf("decoded %v", decoded)
	}
	for _, want := range []string{"-target=ios ", "--sdk iphoneos", "arm64-apple-ios", "embedded.mobileprovision", ".ipa",
		"--sign " + app.sha1(), "codesign --verify --deep --strict"} {
		if !strings.Contains(plan, want) {
			t.Errorf("plan lacks %q", want)
		}
	}
	if strings.Contains(plan, "iphonesimulator") || strings.Contains(plan, "--sign -") {
		t.Error("the device plan builds for the simulator or signs ad hoc")
	}
	share := strings.Index(plan, "sign the Share extension with the owner identity")
	widget := strings.Index(plan, "sign the Widget extension with the owner identity")
	appSign := strings.Index(plan, "sign the app with the owner identity")
	if share < 0 || widget < 0 || appSign < 0 || !(share < appSign && widget < appSign) {
		t.Fatalf("signing order share=%d widget=%d app=%d", share, widget, appSign)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 0 {
		t.Fatalf("a dry run wrote %d entries to --out", len(entries))
	}
	// A development profile is refused for a release build.
	code, _, stderr = runCLI(t, "build", "--platform", "ios-device", "--pkg", nativePkg, "--renderer", "native", "--dry-run",
		"--id", id, "--out", t.TempDir(), "--team-id", testTeam, "--sign-identity", app.sha1(),
		"--provisioning-profile", paths["app"], "--profile", "release", "--build-number", "4")
	if code == 0 || !strings.Contains(stderr, "needs an ad hoc distribution profile") {
		t.Fatalf("release with a development profile: exit %d\n%s", code, stderr)
	}
}
