package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Build profiles. debug is what build and run always produced: the debug
// signing key, WebView inspection on, build number 1. release signs with
// an owner-supplied identity, compiles the debug hooks out, verifies the
// artifact and writes release-report.json. Nothing here creates a signing
// key or certificate, downloads a tool, uploads, or changes the machine.

const (
	profileDebug   = "debug"
	profileRelease = "release"
	maxBuildNumber = 2100000000
	reportName     = "release-report.json"
	bundletoolEnv  = "GOFASTR_BUNDLETOOL"
)

// supportedABIs are the Android ABIs gomobile builds and the SQLite
// syscall overlay (android_libc.go) has been checked against. The order is
// today's -target order.
var supportedABIs = []struct{ abi, gomobile string }{
	{"arm64-v8a", "android/arm64"},
	{"x86_64", "android/amd64"},
}

var (
	envNameRe = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
	teamIDRe  = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	sha1HexRe = regexp.MustCompile(`^[0-9A-Fa-f]{40}$`)
)

// releaseFlags are the profile, packaging and signing flags of build and
// run. main registers them with one addReleaseFlags call.
type releaseFlags struct {
	profile, buildNumber, abi, packages, bundletool string
	keystore, keyAlias, storePassEnv, keyPassEnv    string
	teamID, signIdentity, keychain                  string
	appProfile, shareProfile, widgetProfile         string
	set                                             map[string]bool
	fs                                              *flag.FlagSet
}

func addReleaseFlags(fs *flag.FlagSet) *releaseFlags {
	f := &releaseFlags{fs: fs}
	fs.StringVar(&f.profile, "profile", profileDebug, "debug or release")
	fs.StringVar(&f.buildNumber, "build-number", "", "Android versionCode / iOS CFBundleVersion (required for release)")
	fs.StringVar(&f.abi, "abi", "", "Android ABIs: arm64-v8a,x86_64")
	fs.StringVar(&f.packages, "package", "", "Android artifacts: apk, aab, or apk,aab")
	fs.StringVar(&f.bundletool, "bundletool", "", "owner-supplied bundletool.jar (or "+bundletoolEnv+")")
	fs.StringVar(&f.keystore, "keystore", "", "Android release keystore")
	fs.StringVar(&f.keyAlias, "key-alias", "", "Android release key alias")
	fs.StringVar(&f.storePassEnv, "keystore-pass-env", "", "environment variable holding the keystore password")
	fs.StringVar(&f.keyPassEnv, "key-pass-env", "", "environment variable holding the key password")
	fs.StringVar(&f.teamID, "team-id", "", "Apple team identifier (ios-device)")
	fs.StringVar(&f.signIdentity, "sign-identity", "", "codesigning identity SHA-1 or exact name (ios-device)")
	fs.StringVar(&f.keychain, "keychain", "", "keychain holding the identity (ios-device)")
	fs.StringVar(&f.appProfile, "provisioning-profile", "", "app provisioning profile (ios-device)")
	fs.StringVar(&f.shareProfile, "share-provisioning-profile", "", "share extension provisioning profile (ios-device)")
	fs.StringVar(&f.widgetProfile, "widget-provisioning-profile", "", "widget extension provisioning profile (ios-device)")
	return f
}

// given reports which release flags appeared on the command line.
func (f *releaseFlags) given() map[string]bool {
	if f.set == nil {
		f.set = map[string]bool{}
		f.fs.Visit(func(fl *flag.Flag) { f.set[fl.Name] = true })
	}
	return f.set
}

var androidSigningFlags = []string{"keystore", "key-alias", "keystore-pass-env", "key-pass-env", "bundletool"}
var iosSigningFlags = []string{"team-id", "sign-identity", "keychain", "provisioning-profile", "share-provisioning-profile", "widget-provisioning-profile"}

// anyGiven names the first release flag set, for commands that take none.
func (f *releaseFlags) anyGiven() string {
	names := make([]string, 0)
	for name := range f.given() {
		switch name {
		case "profile", "build-number", "abi", "package":
		default:
			if !contains(androidSigningFlags, name) && !contains(iosSigningFlags, name) {
				continue
			}
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// releaseContext is what resolve needs from the shared flags.
type releaseContext struct {
	sub, platform, out, device, screenshot string
	dryRun                                 bool
}

type androidSigning struct {
	keystore, alias, storePassEnv, keyPassEnv string
}

type iosSigning struct {
	teamID, identity, keychain              string
	appProfile, shareProfile, widgetProfile string
}

// releaseOptions is the validated profile and signing configuration.
type releaseOptions struct {
	profile     string
	buildNumber int
	abis        []string
	apk, aab    bool
	bundletool  string
	android     androidSigning
	ios         iosSigning
}

func (o releaseOptions) release() bool { return o.profile == profileRelease }

// debugOptions is today's behaviour: debug key, both ABIs, APK only.
func debugOptions() releaseOptions {
	o := releaseOptions{profile: profileDebug, buildNumber: 1, apk: true}
	for _, s := range supportedABIs {
		o.abis = append(o.abis, s.abi)
	}
	return o
}

// normalizePlatform maps the android alias onto the canonical name.
func normalizePlatform(p string) string {
	if p == "android" {
		return "android-emu"
	}
	return p
}

// resolve validates every profile, packaging and signing rule before any
// command runs, applies the profile to a, and returns the absolute output
// directory. Every error is a usage error (exit 2).
func (f *releaseFlags) resolve(c releaseContext, a *app) (releaseOptions, string, error) {
	given := f.given()
	o := debugOptions()
	switch f.profile {
	case profileDebug, profileRelease:
		o.profile = f.profile
	default:
		return o, "", errors.New("--profile must be debug or release")
	}
	android := c.platform == "android-emu"
	if c.platform == "ios-sim" && o.release() {
		return o, "", errors.New("--profile release needs a signed device build: use --platform ios-device; a simulator build is never a release artifact")
	}
	if f.buildNumber != "" {
		n, err := strconv.Atoi(f.buildNumber)
		if err != nil || n < 1 || n > maxBuildNumber || strconv.Itoa(n) != f.buildNumber {
			return o, "", fmt.Errorf("--build-number must be an integer from 1 to %d", maxBuildNumber)
		}
		o.buildNumber = n
	} else if o.release() {
		return o, "", errors.New("--profile release requires --build-number (Android versionCode / iOS CFBundleVersion); each upload needs a higher number than the last")
	}
	if !android {
		for _, name := range []string{"abi", "package", "bundletool", "keystore", "key-alias", "keystore-pass-env", "key-pass-env"} {
			if given[name] {
				return o, "", fmt.Errorf("--%s applies only to --platform android-emu", name)
			}
		}
	}
	if c.platform != "ios-device" {
		for _, name := range iosSigningFlags {
			if given[name] {
				return o, "", fmt.Errorf("--%s applies only to --platform ios-device", name)
			}
		}
	}
	if android {
		if err := f.resolveAndroid(c, &o, given); err != nil {
			return o, "", err
		}
	}
	if c.platform == "ios-device" {
		if err := f.resolveIOS(c, &o, *a); err != nil {
			return o, "", err
		}
	}
	dir := c.out
	if dir == "" {
		dir = filepath.Join("dist", "mobile", c.platform)
		if o.release() {
			dir += "-release"
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return o, "", err
	}
	if android && o.release() {
		if inside(dir, o.android.keystore) {
			return o, "", errors.New("--keystore must not live inside --out: output directories get cleaned and shared")
		}
	}
	if c.platform == "ios-device" {
		for _, p := range []string{o.ios.appProfile, o.ios.shareProfile, o.ios.widgetProfile, o.ios.keychain} {
			if p != "" && inside(dir, p) {
				return o, "", errors.New("signing inputs must not live inside --out: output directories get cleaned and shared")
			}
		}
	}
	a.applyProfile(o, c.platform)
	return o, dir, nil
}

func (f *releaseFlags) resolveAndroid(c releaseContext, o *releaseOptions, given map[string]bool) error {
	if f.abi != "" {
		seen := map[string]bool{}
		for _, name := range strings.Split(f.abi, ",") {
			name = strings.TrimSpace(name)
			switch name {
			case "armeabi-v7a", "x86":
				return fmt.Errorf("--abi %s is not supported: 32-bit Go and the SQLite syscall overlay have not been validated; use arm64-v8a and/or x86_64", name)
			}
			if gomobileABI(name) == "" {
				return fmt.Errorf("--abi %q: want arm64-v8a, x86_64, or both separated by a comma", name)
			}
			if seen[name] {
				return fmt.Errorf("--abi lists %s twice", name)
			}
			seen[name] = true
		}
		// Keep the canonical order so the plan and the report are stable.
		o.abis = nil
		for _, s := range supportedABIs {
			if seen[s.abi] {
				o.abis = append(o.abis, s.abi)
			}
		}
	}
	if f.packages != "" {
		o.apk, o.aab = false, false
		for _, kind := range strings.Split(f.packages, ",") {
			switch strings.TrimSpace(kind) {
			case "apk":
				o.apk = true
			case "aab":
				o.aab = true
			default:
				return errors.New("--package must be apk, aab, or apk,aab")
			}
		}
	}
	if !o.release() {
		for _, name := range androidSigningFlags {
			if given[name] {
				return fmt.Errorf("--%s requires --profile release (debug builds always use the debug key)", name)
			}
		}
		if o.aab {
			return errors.New("--package aab requires --profile release")
		}
		return nil
	}
	if o.aab {
		o.bundletool = f.bundletool
		if o.bundletool == "" {
			o.bundletool = os.Getenv(bundletoolEnv)
		}
		if o.bundletool == "" {
			return fmt.Errorf("--package aab needs bundletool: pass --bundletool <bundletool.jar> or set %s (gofastr-native never downloads it)", bundletoolEnv)
		}
		abs, err := filepath.Abs(o.bundletool)
		if err != nil {
			return err
		}
		o.bundletool = abs
		if !c.dryRun && !fileExists(abs) {
			return fmt.Errorf("--bundletool %s does not exist", abs)
		}
	} else if given["bundletool"] {
		return errors.New("--bundletool applies only with --package aab")
	}
	s := androidSigning{keystore: f.keystore, alias: f.keyAlias, storePassEnv: f.storePassEnv, keyPassEnv: f.keyPassEnv}
	switch {
	case s.keystore == "":
		return errors.New("--profile release on Android requires --keystore <owner-created keystore>")
	case s.alias == "":
		return errors.New("--profile release on Android requires --key-alias")
	case s.storePassEnv == "":
		return errors.New("--profile release on Android requires --keystore-pass-env NAME (the password stays in that environment variable)")
	}
	if s.keyPassEnv == "" {
		s.keyPassEnv = s.storePassEnv
	}
	for _, name := range []string{s.storePassEnv, s.keyPassEnv} {
		if !envNameRe.MatchString(name) {
			return fmt.Errorf("password variable %q: want an environment variable name such as GOFASTR_ANDROID_STORE_PASSWORD, not the password", name)
		}
	}
	if s.alias == "androiddebugkey" || strings.ContainsFunc(s.alias, unicode.IsControl) || len(s.alias) > 128 || strings.HasPrefix(s.alias, "-") {
		return errors.New("--key-alias must be your release key's alias, not androiddebugkey")
	}
	abs, err := filepath.Abs(s.keystore)
	if err != nil {
		return err
	}
	s.keystore = abs
	if isDebugKeystore(abs) {
		return errors.New("--keystore is the SDK debug keystore; a release needs your own upload key")
	}
	if !c.dryRun {
		if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("--keystore %s is not a readable file", abs)
		}
		for _, name := range []string{s.storePassEnv, s.keyPassEnv} {
			if os.Getenv(name) == "" {
				return fmt.Errorf("environment variable %s is not set or empty", name)
			}
		}
	}
	o.android = s
	return nil
}

func (f *releaseFlags) resolveIOS(c releaseContext, o *releaseOptions, a app) error {
	s := iosSigning{teamID: f.teamID, identity: f.signIdentity, keychain: f.keychain,
		appProfile: f.appProfile, shareProfile: f.shareProfile, widgetProfile: f.widgetProfile}
	switch {
	case !teamIDRe.MatchString(s.teamID):
		return errors.New("--platform ios-device requires --team-id (10 uppercase letters and digits)")
	case s.identity == "":
		return errors.New("--platform ios-device requires --sign-identity (the certificate SHA-1 from security find-identity, or its exact name)")
	case s.appProfile == "":
		return errors.New("--platform ios-device requires --provisioning-profile <app .mobileprovision>")
	}
	if !sha1HexRe.MatchString(s.identity) && (strings.ContainsFunc(s.identity, unicode.IsControl) || len(s.identity) > 200 || strings.HasPrefix(s.identity, "-")) {
		return errors.New("--sign-identity must be a 40-hex SHA-1 or an exact identity name")
	}
	if (a.ReceiveShares || a.Widgets) && a.AppGroup == "" {
		return errors.New("iOS share and widget extensions require --app-group")
	}
	if a.SecureStorage && a.BiometricUsage == "" {
		return errors.New("iOS secure storage requires --biometric-usage for Face ID authentication")
	}
	if a.ReceiveShares && s.shareProfile == "" {
		return errors.New("--receive-shares on ios-device requires --share-provisioning-profile for " + a.ID + ".Share")
	}
	if !a.ReceiveShares && s.shareProfile != "" {
		return errors.New("--share-provisioning-profile requires --receive-shares")
	}
	if a.Widgets && s.widgetProfile == "" {
		return errors.New("--widgets on ios-device requires --widget-provisioning-profile for " + a.ID + ".Widget")
	}
	if !a.Widgets && s.widgetProfile != "" {
		return errors.New("--widget-provisioning-profile requires --widgets")
	}
	if c.sub == "run" && c.device == "" {
		return errors.New("run --platform ios-device requires --device <exact CoreDevice identifier or UDID>; there is no default pick")
	}
	if c.screenshot != "" {
		return errors.New("--screenshot is not supported on ios-device")
	}
	for _, p := range []*string{&s.appProfile, &s.shareProfile, &s.widgetProfile, &s.keychain} {
		if *p == "" {
			continue
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}
	if !c.dryRun {
		for _, p := range []string{s.appProfile, s.shareProfile, s.widgetProfile} {
			if p != "" && !fileExists(p) {
				return fmt.Errorf("provisioning profile %s does not exist", p)
			}
		}
	}
	if sha1HexRe.MatchString(s.identity) {
		s.identity = strings.ToUpper(s.identity)
	}
	o.ios = s
	return nil
}

// applyProfile writes the profile-dependent template values.
func (a *app) applyProfile(o releaseOptions, platform string) {
	a.Profile = o.profile
	a.Debuggable = !o.release()
	a.ABIs = append([]string(nil), o.abis...)
	a.VersionCode = o.buildNumber
	a.Build = strconv.Itoa(o.buildNumber)
	if platform == "ios-device" {
		a.IOSPlatform, a.IOSSupportedPlatform, a.IOSDevice = "iphoneos", "iPhoneOS", true
	}
}

func gomobileABI(abi string) string {
	for _, s := range supportedABIs {
		if s.abi == abi {
			return s.gomobile
		}
	}
	return ""
}

// gomobileAndroidTarget is the -target value for the selected ABIs.
func gomobileAndroidTarget(abis []string) string {
	var targets []string
	for _, abi := range abis {
		targets = append(targets, gomobileABI(abi))
	}
	return "-target=" + strings.Join(targets, ",")
}

// goLDFlags keeps today's Android page-size flag and, for release, strips symbol and DWARF tables from release binaries.
func goLDFlags(release bool, android bool) []string {
	var flags []string
	ld := ""
	if release {
		ld = "-s -w"
	}
	if android {
		ld = strings.TrimSpace(ld + " -extldflags=-Wl,-z,max-page-size=16384")
	}
	if ld != "" {
		flags = append(flags, "-ldflags="+ld)
	}
	if release {
		flags = append(flags, "-trimpath")
	}
	return flags
}

func isDebugKeystore(path string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	debug := filepath.Join(home, ".android", "debug.keystore")
	if strings.EqualFold(filepath.Clean(path), filepath.Clean(debug)) {
		return true
	}
	a, errA := os.Stat(path)
	b, errB := os.Stat(debug)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

// inside reports whether path is dir or below it.
func inside(dir, path string) bool {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// redactWriter replaces secret values in everything a release build
// prints. The tools never echo passwords; this is the backstop.
//
// Child processes write in arbitrary chunks, so a secret can arrive split
// across Write calls. The writer holds back only the bytes that could still
// be the start of a secret, plus up to longest-1 bytes of already decided
// context so an occurrence that began in an earlier chunk is still found.
// Every byte covered by any occurrence of any secret, overlapping ones
// included, is replaced; each covered run prints as one "[redacted]".
// Call Flush when the output ends to release the held-back tail.
// It is safe for concurrent use by a command's stdout and stderr.
type redactWriter struct {
	mu      sync.Mutex
	w       io.Writer
	secrets []string // nonempty, distinct, longest first
	maxLen  int
	buf     []byte // context (already decided) followed by undecided bytes
	decided int    // buf[:decided] is context that was already written
	inRun   bool   // the last decided byte was redacted
}

const redactedMark = "[redacted]"

// newRedactWriter wraps w. Empty secrets are ignored.
func newRedactWriter(w io.Writer, secrets []string) *redactWriter {
	rw := &redactWriter{w: w, secrets: normalizeSecrets(secrets)}
	for _, s := range rw.secrets {
		rw.maxLen = max(rw.maxLen, len(s))
	}
	return rw
}

func normalizeSecrets(secrets []string) []string {
	var out []string
	for _, s := range secrets {
		if s != "" && !contains(out, s) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func (w *redactWriter) Write(p []byte) (int, error) {
	if len(w.secrets) == 0 {
		return w.w.Write(p)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if err := w.emit(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush writes every held-back byte, redacting complete secrets.
func (w *redactWriter) Flush() error {
	if len(w.secrets) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.emit(true)
}

// emit decides as many undecided bytes as it can and writes them.
func (w *redactWriter) emit(final bool) error {
	hold := len(w.buf)
	if !final {
		hold = holdFrom(w.buf, w.decided, w.secrets)
	}
	var out []byte
	out, w.inRun = redactRange(nil, w.buf, w.decided, hold, w.secrets, w.inRun)
	// Keep longest-1 bytes of decided context before the undecided tail.
	start := max(0, hold-(w.maxLen-1))
	w.buf = append(w.buf[:0], w.buf[start:]...)
	w.decided = hold - start
	if len(out) == 0 {
		return nil
	}
	_, err := w.w.Write(out)
	return err
}

// holdFrom returns the first index at or after from where buf's remainder
// is a proper prefix of some secret: those bytes could still become one.
func holdFrom(buf []byte, from int, secrets []string) int {
	for j := max(from, len(buf)-len(secrets[0])+1); j < len(buf); j++ {
		rest := buf[j:]
		for _, s := range secrets {
			if len(rest) < len(s) && strings.HasPrefix(s, string(rest)) {
				return j
			}
		}
	}
	return len(buf)
}

// redactRange appends buf[from:to] to dst with every byte that any secret
// occurrence in buf covers replaced. inRun says whether the byte before
// from was redacted, so a run split across calls prints one marker.
func redactRange(dst, buf []byte, from, to int, secrets []string, inRun bool) ([]byte, bool) {
	if from >= to {
		return dst, inRun
	}
	covered := make([]bool, to-from)
	for _, s := range secrets {
		for i := 0; i+len(s) <= len(buf); {
			k := bytes.Index(buf[i:], []byte(s))
			if k < 0 {
				break
			}
			start := i + k
			for p := max(start, from); p < min(start+len(s), to); p++ {
				covered[p-from] = true
			}
			i = start + 1
		}
	}
	for p := from; p < to; p++ {
		if covered[p-from] {
			if !inRun {
				dst = append(dst, redactedMark...)
			}
			inRun = true
			continue
		}
		dst = append(dst, buf[p])
		inRun = false
	}
	return dst, inRun
}

// redactString redacts every occurrence of every nonempty secret in s.
func redactString(s string, secrets []string) (string, bool) {
	secrets = normalizeSecrets(secrets)
	changed := false
	for _, secret := range secrets {
		if strings.Contains(s, secret) {
			changed = true
			break
		}
	}
	if !changed {
		return s, false
	}
	out, _ := redactRange(nil, []byte(s), 0, len(s), secrets, false)
	return string(out), true
}

// secretValues reads the named variables so output can be redacted.
// Every nonempty value counts, however short: an imported keystore can
// have any password, and a short one is still a secret.
func secretValues(names ...string) []string {
	var values []string
	for _, name := range names {
		if v := os.Getenv(name); v != "" && !contains(values, v) {
			values = append(values, v)
		}
	}
	return values
}

// redactedError is an error whose text had a secret removed. It answers
// errors.Is for the original chain (so context.Canceled still maps to
// exit 130) but has no Unwrap, so errors.As cannot reach the original text.
type redactedError struct {
	msg  string
	orig error
}

func (e *redactedError) Error() string { return e.msg }

func (e *redactedError) Is(target error) bool { return errors.Is(e.orig, target) }

// redactError returns err itself when its text holds no secret.
func redactError(err error, secrets []string) error {
	if err == nil {
		return nil
	}
	msg, changed := redactString(err.Error(), secrets)
	if !changed {
		return err
	}
	return &redactedError{msg: msg, orig: err}
}

// redactOutput routes r.out through a redactWriter until the returned
// function runs. Call it deferred with the address of the named error
// result: it flushes the held-back tail, restores the original writer on
// success and failure, and redacts the error the caller returns.
func redactOutput(r *runner, secrets []string) func(*error) {
	orig := r.out
	rw := newRedactWriter(orig, secrets)
	r.out = rw
	return func(errp *error) {
		r.out = orig
		if ferr := rw.Flush(); ferr != nil {
			if *errp == nil {
				*errp = ferr
			} else {
				*errp = errors.Join(*errp, ferr)
			}
		}
		*errp = redactError(*errp, rw.secrets)
	}
}

// Release report.

type reportCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // PASS, FAIL, NOT RUN
	Detail string `json:"detail,omitempty"`
}

type reportArtifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type reportAndroid struct {
	SignerCertificateSHA256 string `json:"signerCertificateSha256"`
	MinSDK                  int    `json:"minSdk"`
	TargetSDK               int    `json:"targetSdk"`
}

type reportIOSProfile struct {
	Bundle string `json:"bundle"`
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Expiry string `json:"expiry"`
}

type reportIOS struct {
	TeamID          string                    `json:"teamId"`
	CertificateSHA1 string                    `json:"certificateSha1"`
	MinimumOS       string                    `json:"minimumOs"`
	Profiles        []reportIOSProfile        `json:"profiles"`
	Entitlements    map[string]map[string]any `json:"entitlements"`
}

type releaseReport struct {
	Schema       int              `json:"schema"`
	GeneratedAt  string           `json:"generatedAt"`
	Platform     string           `json:"platform"`
	Profile      string           `json:"profile"`
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Version      string           `json:"version"`
	BuildNumber  int              `json:"buildNumber"`
	Renderer     string           `json:"renderer"`
	HostTemplate bool             `json:"callerHostTemplate,omitempty"`
	ABIs         []string         `json:"abis,omitempty"`
	Features     []string         `json:"features"`
	Artifacts    []reportArtifact `json:"artifacts"`
	Android      *reportAndroid   `json:"android,omitempty"`
	IOS          *reportIOS       `json:"ios,omitempty"`
	Checks       []reportCheck    `json:"checks"`
}

func newReleaseReport(a app, platform string) *releaseReport {
	rep := &releaseReport{Schema: 1, Platform: platform, Profile: a.Profile, ID: a.ID, Name: a.Name,
		Version: a.Version, BuildNumber: a.VersionCode, Renderer: a.Renderer, HostTemplate: a.HostTemplate != "",
		Features: []string{}}
	if platform == "android-emu" {
		rep.ABIs = a.ABIs
	}
	for _, f := range []struct {
		on   bool
		name string
	}{{a.CameraUsage != "", "camera"}, {a.MicrophoneUsage != "", "microphone"}, {a.URLScheme != "", "url-scheme"},
		{a.ReceiveShares, "receive-shares"}, {a.SecureStorage, "secure-storage"}, {a.Widgets, "widgets"}, {a.AppGroup != "", "app-group"}} {
		if f.on {
			rep.Features = append(rep.Features, f.name)
		}
	}
	for _, e := range a.Extensions {
		rep.Features = append(rep.Features, "extension:"+e.manifest.Name)
	}
	return rep
}

func (rep *releaseReport) check(name string, err error, detail string) {
	c := reportCheck{Name: name, Status: "PASS", Detail: detail}
	if err != nil {
		c.Status, c.Detail = "FAIL", err.Error()
	}
	rep.Checks = append(rep.Checks, c)
}

func (rep *releaseReport) notRun(name, why string) {
	rep.Checks = append(rep.Checks, reportCheck{Name: name, Status: "NOT RUN", Detail: why})
}

func (rep *releaseReport) addArtifact(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	rep.Artifacts = append(rep.Artifacts, reportArtifact{Name: filepath.Base(path), SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: n})
	return nil
}

// write stores the report as 0600 JSON. It is written last, after every
// verification passed, so its presence means the artifacts beside it
// were checked.
func (rep *releaseReport) write(dir string, now time.Time) error {
	rep.GeneratedAt = now.UTC().Format(time.RFC3339)
	for _, c := range rep.Checks {
		if c.Status == "FAIL" {
			return fmt.Errorf("release check %q failed: %s", c.Name, c.Detail)
		}
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return writeOwnerOnly(filepath.Join(dir, reportName), append(b, '\n'))
}

// printChecks lists every check the way doctor does.
func (rep *releaseReport) printChecks(w io.Writer) {
	fmt.Fprintln(w, "\nRelease checks:")
	for _, c := range rep.Checks {
		if c.Detail != "" {
			fmt.Fprintf(w, "  %s: %s (%s)\n", c.Status, c.Name, c.Detail)
		} else {
			fmt.Fprintf(w, "  %s: %s\n", c.Status, c.Name)
		}
	}
}

// removeStale deletes a previous artifact and report before a release
// build, so a failed or interrupted build leaves nothing that looks
// finished at the artifact path.
func removeStale(r *runner, dir string, names ...string) error {
	return r.do("remove previous release artifacts and report", func() error {
		for _, name := range append(names, reportName) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		return nil
	})
}
