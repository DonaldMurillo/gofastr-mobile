package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// The physical-device build: the simulator pipeline against the iphoneos
// SDK, signed inside-out with the owner's identity and one provisioning
// profile per bundle, verified, then zipped into an IPA. Nothing here
// creates an identity or profile, unlocks a keychain, or talks to Apple.

// deviceBundle is one signed bundle: the app or an extension.
type deviceBundle struct {
	label, id, profilePath string
	keychain               bool
	profile                *provisioningProfile // nil when not decoded (dry run)
	entitlements           entitlementSet
}

// deviceSigner signs with one identity and per-bundle profiles.
type deviceSigner struct {
	identity, keychain string // identity is a SHA-1 once resolved
	work               string
	bundles            map[string]*deviceBundle
	order              []string // bundle IDs in signing order
}

func (s *deviceSigner) sign(r *runner, label, bundleID, bundle string) error {
	b := s.bundles[bundleID]
	if b == nil {
		return fmt.Errorf("no provisioning profile configured for %s", bundleID)
	}
	if err := r.do("embed the "+label+" provisioning profile as embedded.mobileprovision", func() error {
		content, err := os.ReadFile(b.profilePath)
		if err != nil {
			return err
		}
		return writeOwnerOnly(filepath.Join(bundle, "embedded.mobileprovision"), content)
	}); err != nil {
		return err
	}
	entitlements := filepath.Join(s.work, "ios-device", strings.ReplaceAll(label, " ", "-")+".entitlements")
	if err := r.do("write the "+label+" entitlements", func() error {
		if b.entitlements == nil {
			return errors.New("entitlements were not generated")
		}
		if err := os.MkdirAll(filepath.Dir(entitlements), 0o700); err != nil {
			return err
		}
		return writeOwnerOnly(entitlements, b.entitlements.plist())
	}); err != nil {
		return err
	}
	args := []string{"--force", "--sign", s.identity}
	if s.keychain != "" {
		args = append(args, "--keychain", s.keychain)
	}
	// --generate-entitlement-der and --timestamp=none are unconfirmed on a
	// Mac for this tool; see docs/release.md.
	args = append(args, "--entitlements", entitlements, "--generate-entitlement-der", "--timestamp=none", bundle)
	return r.run("sign the "+label+" with the owner identity", "codesign", args...)
}

// buildIOSDevice builds, signs, verifies and packages an ios-device app.
// It returns the IPA path.
func buildIOSDevice(r *runner, a app, out string, o releaseOptions) (string, error) {
	a = a.forIOS()
	if !r.dryRun && runtime.GOOS != "darwin" {
		return "", errors.New("--platform ios-device needs macOS with Xcode; use --dry-run to print the plan here")
	}
	if err := removeStale(r, out, a.Exe+".ipa"); err != nil {
		return "", err
	}
	s := o.ios
	signer := &deviceSigner{identity: s.identity, keychain: s.keychain, work: filepath.Join(out, "work"), bundles: map[string]*deviceBundle{}}
	add := func(label, id, profile string, keychain bool) {
		signer.bundles[id] = &deviceBundle{label: label, id: id, profilePath: profile, keychain: keychain}
		signer.order = append(signer.order, id)
	}
	if a.ReceiveShares {
		add("Share extension", a.ID+".Share", s.shareProfile, false)
	}
	if a.Widgets {
		add("Widget extension", a.ID+".Widget", s.widgetProfile, false)
	}
	add("app", a.ID, s.appProfile, a.SecureStorage)

	identity, err := resolveSigningIdentity(r, s)
	if err != nil {
		return "", err
	}
	signer.identity = identity
	wantKind := profileDevelopment
	if o.release() {
		wantKind = profileAdHoc
	}
	rep := newReleaseReport(a, "ios-device")
	rep.IOS = &reportIOS{TeamID: s.teamID, MinimumOS: a.MinOS, Entitlements: map[string]map[string]any{}}
	if sha1HexRe.MatchString(identity) {
		rep.IOS.CertificateSHA1 = identity
	}
	decode := !r.dryRun || profileDecoderInjected
	exact := a.AppGroup != "" || a.ReceiveShares || a.Widgets
	for _, id := range signer.order {
		b := signer.bundles[id]
		if !decode {
			fmt.Fprintf(r.out, "    NOT CHECKED: %s provisioning profile kind, team, expiry, App ID, groups and certificate (dry run)\n", b.label)
			continue
		}
		content, err := decodeProvisioningProfile(r, b.profilePath)
		if err != nil {
			return "", fmt.Errorf("decode the %s provisioning profile: %w", b.label, err)
		}
		profile, err := parseProvisioningProfile(content)
		if err != nil {
			return "", fmt.Errorf("%s provisioning profile: %w", b.label, err)
		}
		req := profileRequirement{bundleID: id, teamID: s.teamID, appGroup: a.AppGroup, wantKind: wantKind, exact: exact, keychain: b.keychain}
		if sha1HexRe.MatchString(identity) {
			req.identitySHA1 = identity
		}
		if err := profile.check(req, time.Now()); err != nil {
			return "", err
		}
		b.profile = &profile
		b.entitlements = deviceEntitlements(profile, id, s.teamID, a.AppGroup, b.keychain)
		if id != a.ID {
			if _, leaked := b.entitlements[entKeychainGroups]; leaked {
				return "", fmt.Errorf("%s would carry keychain-access-groups; extensions must never share the app's private Keychain group", id)
			}
		}
		if err := b.entitlements.allowedBy(profile); err != nil {
			return "", fmt.Errorf("%s entitlements exceed its profile: %w", b.label, err)
		}
		rep.IOS.Profiles = append(rep.IOS.Profiles, reportIOSProfile{Bundle: id, UUID: profile.UUID, Name: profile.Name, Kind: profile.kind(), Expiry: profile.Expiry.UTC().Format(time.RFC3339)})
		rep.IOS.Entitlements[id] = b.entitlements
		rep.check(b.label+" profile kind, team, expiry, App ID, groups and certificate", nil, profile.kind())
	}

	appDir, err := buildIOSFor(r, a, out, deviceTarget(a.MinOS), signer, o.release())
	if err != nil {
		return "", err
	}
	if err := verifyDeviceApp(r, a, appDir, signer, rep); err != nil {
		return "", err
	}
	ipa := filepath.Join(out, a.Exe+".ipa")
	executables := []string{a.Exe}
	for _, id := range signer.order {
		if id != a.ID {
			name := strings.TrimPrefix(id, a.ID+".")
			executables = append(executables, path.Join("PlugIns", name+".appex", name))
		}
	}
	if err := r.do("package "+ipa, func() error {
		if err := packIPA(appDir, ipa, executables); err != nil {
			return err
		}
		return rep.addArtifact(ipa)
	}); err != nil {
		return "", err
	}
	rep.notRun("install and launch on a registered iPhone", "run --platform ios-device --device <id>, then check the app by hand")
	if a.ReceiveShares || a.Widgets {
		rep.notRun("share extension and widget on device through the app-group container", "manual device gate")
	}
	if a.SecureStorage {
		rep.notRun("Keychain isolation on device (extension cannot read the app's item)", "manual device gate")
	}
	if err := r.do("write "+reportName, func() error {
		rep.printChecks(r.out)
		if !o.release() {
			return nil // the report belongs to release builds; the checks still printed
		}
		return rep.write(out, time.Now())
	}); err != nil {
		return "", err
	}
	return ipa, nil
}

// resolveSigningIdentity returns the identity SHA-1. A name is looked up
// with security find-identity; a dry run keeps the name.
func resolveSigningIdentity(r *runner, s iosSigning) (string, error) {
	if sha1HexRe.MatchString(s.identity) {
		return strings.ToUpper(s.identity), nil
	}
	args := []string{"find-identity", "-v", "-p", "codesigning"}
	if s.keychain != "" {
		args = append(args, s.keychain)
	}
	listing, err := r.output("find the codesigning identity "+s.identity, "", "security", args...)
	if err != nil {
		return "", err
	}
	if r.dryRun {
		fmt.Fprintln(r.out, "    NOT CHECKED: identity name resolves to exactly one certificate (dry run)")
		return s.identity, nil
	}
	return parseFindIdentity(listing, s.identity)
}

// verifyDeviceApp checks the signature, each bundle's entitlements, and
// runs the doctor's own inspection of a signed device app.
func verifyDeviceApp(r *runner, a app, appDir string, signer *deviceSigner, rep *releaseReport) error {
	if err := r.run("verify the signature", "codesign", "--verify", "--deep", "--strict", appDir); err != nil {
		return err
	}
	rep.check("codesign --verify --deep --strict", nil, "")
	for _, id := range signer.order {
		b := signer.bundles[id]
		bundle := appDir
		if id != a.ID {
			name := strings.TrimPrefix(id, a.ID+".")
			bundle = filepath.Join(appDir, "PlugIns", name+".appex")
		}
		dumped, err := r.outputBytes("read the signed "+b.label+" entitlements", nil, "codesign", "-d", "--entitlements", "-", "--xml", bundle)
		if err != nil {
			return err
		}
		if r.dryRun {
			continue
		}
		got, err := parseEntitlements(dumped)
		if err != nil {
			return fmt.Errorf("%s: %w", b.label, err)
		}
		if !got.equal(b.entitlements) {
			return fmt.Errorf("the signed %s carries entitlements %v, want %v", b.label, got, b.entitlements)
		}
		rep.check(b.label+" signed entitlements equal the generated set", nil, "")
	}
	if r.dryRun {
		rep.notRun("doctor inspection of the signed app", "dry run")
		return nil
	}
	app := signer.bundles[a.ID]
	var devices []deviceRecord
	if app.profile != nil {
		for _, udid := range app.profile.Devices {
			var d deviceRecord
			d.Hardware.UDID = udid
			devices = append(devices, d)
		}
	}
	execute := func(name string, args ...string) ([]byte, error) {
		return r.outputBytes("self-check: "+name, nil, name, args...)
	}
	for _, c := range inspectDeviceApp(deviceDoctorOptions{app: appDir, scheme: a.URLScheme}, map[string]bool{}, devices, execute, time.Now()) {
		var err error
		if !c.ok {
			err = fmt.Errorf("%s (fix: %s)", c.got, c.fix)
		}
		rep.check("doctor: "+c.name, err, "")
		if err != nil {
			return fmt.Errorf("self-check %s failed: %w", c.name, err)
		}
	}
	return nil
}

// packIPA zips appDir as Payload/<name>.app. Entries are sorted, carry a
// fixed time and Unix modes (0755 for directories and executables, 0644
// otherwise), so the same bundle always gives the same bytes. Symlinks and
// special files are refused. The IPA is written beside its final path and
// renamed into place.
func packIPA(appDir, ipa string, executables []string) error {
	root := "Payload/" + filepath.Base(appDir)
	type item struct {
		name, src string
		dir       bool
		mode      fs.FileMode
	}
	items := []item{{name: "Payload/", dir: true}, {name: root + "/", dir: true}}
	exec := map[string]bool{}
	for _, e := range executables {
		exec[e] = true
	}
	err := filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == appDir {
			return nil
		}
		rel, err := filepath.Rel(appDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("%s is a symlink; the IPA packer refuses links", rel)
		case info.IsDir():
			items = append(items, item{name: root + "/" + rel + "/", dir: true})
		case info.Mode().IsRegular():
			mode := fs.FileMode(0o644)
			if exec[rel] || info.Mode()&0o111 != 0 {
				mode = 0o755
			}
			items = append(items, item{name: root + "/" + rel, src: p, mode: mode})
		default:
			return fmt.Errorf("%s is not a regular file or directory", rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	tmp := ipa + ".partial"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	zw := zip.NewWriter(f)
	stamp := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, it := range items {
		h := &zip.FileHeader{Name: it.name, Modified: stamp}
		if it.dir {
			h.SetMode(fs.ModeDir | 0o755)
			if _, err := zw.CreateHeader(h); err != nil {
				return fail(err)
			}
			continue
		}
		h.Method = zip.Deflate
		h.SetMode(it.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return fail(err)
		}
		src, err := os.Open(it.src)
		if err != nil {
			return fail(err)
		}
		_, err = io.Copy(w, io.LimitReader(src, 2<<30))
		src.Close()
		if err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, ipa)
}

// runIOSDevice installs the signed app on the exact --device and
// launches it. It never uninstalls, resets, pairs or enables Developer
// Mode. An install whose outcome is unclear is reported UNKNOWN and not
// retried.
func runIOSDevice(r *runner, a app, ipa string, opts runOpts) error {
	appDir := filepath.Join(filepath.Dir(ipa), a.Exe+".app")
	device := opts.device
	if !r.dryRun {
		execute := func(name string, args ...string) ([]byte, error) {
			return r.outputBytes("read-only: "+name, nil, name, args...)
		}
		devices, err := readDeviceReport(execute)
		if err != nil {
			return fmt.Errorf("list devices: %w", err)
		}
		found := false
		for _, d := range devices {
			found = found || usableDevice(d, device)
		}
		if !found {
			return fmt.Errorf("device %q is not a connected, paired iPhone with Developer Mode on (check: gofastr-native doctor --platform ios-device --device %s)", device, device)
		}
	}
	check := shellJoin("xcrun", []string{"devicectl", "device", "info", "apps", "--device", device, "--bundle-id", a.ID})
	dir := ""
	if !r.dryRun {
		var err error
		if dir, err = os.MkdirTemp("", "gofastr-install-"); err != nil {
			return err
		}
		defer os.RemoveAll(dir)
	}
	report := filepath.Join(dir, "install.json")
	runErr := r.run("install the app on "+device, "xcrun", "devicectl", "device", "install", "app", "--device", device, "--json-output", report, appDir)
	if !r.dryRun {
		outcome := ""
		if content, err := os.ReadFile(report); err == nil {
			var parsed struct {
				Info struct {
					Outcome string `json:"outcome"`
				} `json:"info"`
			}
			if json.Unmarshal(content, &parsed) == nil {
				outcome = parsed.Info.Outcome
			}
		}
		switch {
		case outcome == "success" && runErr == nil:
		case outcome == "":
			fmt.Fprintf(r.out, "UNKNOWN: the install outcome is unclear (%v). Not retrying. Check it with:\n    %s\n", runErr, check)
			return errors.New("install outcome UNKNOWN")
		default:
			return fmt.Errorf("install failed (outcome %q): %v", outcome, runErr)
		}
	}
	if err := r.run("launch the app", "xcrun", "devicectl", "device", "process", "launch", "--device", device, a.ID); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "\nRunning on device %s.\n  Installed apps: %s\n", device, check)
	return nil
}
