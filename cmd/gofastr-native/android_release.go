package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The Android release path. Passwords never appear in an argument, the
// runner environment, a file or the output: every tool reads them from the
// owner's environment variables (apksigner env:NAME, keytool and jarsigner
// -storepass:env NAME).

// keystoreCert is the public half of the owner's release key.
type keystoreCert struct {
	sha256  string // lowercase hex of the DER certificate, as apksigner prints it
	subject string
	expires time.Time
}

// parseKeystoreListing reads `keytool -list -rfc -alias A`: one
// PrivateKeyEntry and its PEM certificate chain. It refuses the SDK debug
// certificate and an expired one.
func parseKeystoreListing(listing []byte, now time.Time) (keystoreCert, error) {
	if !strings.Contains(string(listing), "PrivateKeyEntry") {
		return keystoreCert{}, errors.New("the alias is not a private key entry (keytool -list shows no PrivateKeyEntry)")
	}
	rest := listing
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return keystoreCert{}, errors.New("keytool -list printed no certificate for the alias")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return keystoreCert{}, fmt.Errorf("read the signing certificate: %w", err)
		}
		if cert.Subject.CommonName == "Android Debug" {
			return keystoreCert{}, errors.New("the key is an Android debug key (CN=Android Debug); a release needs your own upload key")
		}
		if !now.Before(cert.NotAfter) {
			return keystoreCert{}, fmt.Errorf("the signing certificate expired on %s", cert.NotAfter.UTC().Format("2006-01-02"))
		}
		if now.Before(cert.NotBefore) {
			return keystoreCert{}, fmt.Errorf("the signing certificate is not valid until %s", cert.NotBefore.UTC().Format("2006-01-02"))
		}
		sum := sha256.Sum256(cert.Raw)
		return keystoreCert{sha256: hex.EncodeToString(sum[:]), subject: cert.Subject.String(), expires: cert.NotAfter}, nil
	}
}

// preflightKeystore checks the key before the slow build so a wrong
// password, alias or a debug key fails in seconds.
func preflightKeystore(r *runner, sdk androidSDK, s androidSigning, now time.Time) (keystoreCert, error) {
	listing, err := r.outputBytes("check the release key (keytool reads the password from $"+s.storePassEnv+")", nil,
		sdk.keytool, "-list", "-rfc", "-keystore", s.keystore, "-alias", s.alias, "-storepass:env", s.storePassEnv)
	if err != nil {
		return keystoreCert{}, fmt.Errorf("%w\n    (check --keystore, --key-alias and the password in $%s)", err, s.storePassEnv)
	}
	if r.dryRun {
		fmt.Fprintln(r.out, "    NOT CHECKED: signing secret presence, key alias and certificate (dry run)")
		return keystoreCert{}, nil
	}
	return parseKeystoreListing(listing, now)
}

// badging is what `aapt2 dump badging` says about an APK.
type badging struct {
	pkg, versionCode, versionName, targetSDK string
	nativeCode                               []string
	debuggable                               bool
}

var badgingAttr = regexp.MustCompile(`(\w+)='([^']*)'`)

func parseBadging(text string) badging {
	var b badging
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "package:"):
			for _, m := range badgingAttr.FindAllStringSubmatch(line, -1) {
				switch m[1] {
				case "name":
					b.pkg = m[2]
				case "versionCode":
					b.versionCode = m[2]
				case "versionName":
					b.versionName = m[2]
				}
			}
		case strings.HasPrefix(line, "targetSdkVersion:"):
			b.targetSDK = strings.Trim(strings.TrimPrefix(line, "targetSdkVersion:"), "'")
		case strings.HasPrefix(line, "native-code:"):
			for _, field := range strings.Fields(strings.TrimPrefix(line, "native-code:")) {
				b.nativeCode = append(b.nativeCode, strings.Trim(field, "'"))
			}
		case strings.HasPrefix(line, "application-debuggable"):
			b.debuggable = true
		}
	}
	return b
}

// checkBadging compares the packaged APK against what was asked for.
func checkBadging(b badging, a app) error {
	var problems []string
	if b.pkg != a.ID {
		problems = append(problems, fmt.Sprintf("package %q, want %q", b.pkg, a.ID))
	}
	if b.versionCode != strconv.Itoa(a.VersionCode) {
		problems = append(problems, fmt.Sprintf("versionCode %q, want %d", b.versionCode, a.VersionCode))
	}
	if b.versionName != a.Version {
		problems = append(problems, fmt.Sprintf("versionName %q, want %q", b.versionName, a.Version))
	}
	if !sameSet(b.nativeCode, a.ABIs) {
		problems = append(problems, fmt.Sprintf("native-code %v, want %v", b.nativeCode, a.ABIs))
	}
	if b.debuggable {
		problems = append(problems, "the APK is debuggable")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// parseSignerDigests reads the signer SHA-256 digests from
// `apksigner verify --print-certs`.
func parseSignerDigests(text string) []string {
	var digests []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Signer #") && strings.Contains(line, "certificate SHA-256 digest:") {
			_, digest, _ := strings.Cut(line, "digest:")
			digests = append(digests, strings.ToLower(strings.TrimSpace(digest)))
		}
	}
	return digests
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

// checkAARABIs confirms gomobile wrote exactly the requested jni/<abi>
// directories.
func checkAARABIs(jniDir string, abis []string) error {
	entries, err := os.ReadDir(jniDir)
	if err != nil {
		return err
	}
	var got []string
	for _, e := range entries {
		if e.IsDir() {
			got = append(got, e.Name())
		}
	}
	if !sameSet(got, abis) {
		return fmt.Errorf("gomobile wrote native libraries for %v, want %v", got, abis)
	}
	return nil
}

// buildToolsMajor is the major version of the build-tools in use.
func buildToolsMajor(sdk androidSDK) int {
	major, _, _ := strings.Cut(filepath.Base(sdk.buildTools), ".")
	n, _ := strconv.Atoi(major)
	return n
}

// androidReleaseInputs are the shared outputs of the build that both the
// APK and the bundle are assembled from.
type androidReleaseInputs struct {
	work, manifest, dexFile, jniDir string
	resourceArgs                    []string
}

func aapt2LinkArgs(sdk androidSDK, a app, manifest, output string, resourceArgs []string, proto bool) []string {
	args := []string{"link"}
	if proto {
		args = append(args, "--proto-format")
	}
	args = append(args, "-o", output, "--manifest", manifest, "-I", sdk.androidJar,
		"--min-sdk-version", strconv.Itoa(a.MinSDK), "--target-sdk-version", strconv.Itoa(a.TargetSDK),
		"--version-code", strconv.Itoa(a.VersionCode), "--version-name", a.Version)
	return append(args, resourceArgs...)
}

// releaseAndroid assembles, signs and verifies the requested release
// artifacts, then writes the report. It returns the primary artifact.
func releaseAndroid(r *runner, sdk androidSDK, a app, o releaseOptions, out string, in androidReleaseInputs, cert keystoreCert) (string, error) {
	rep := newReleaseReport(a, "android-emu")
	rep.Android = &reportAndroid{SignerCertificateSHA256: cert.sha256, MinSDK: a.MinSDK, TargetSDK: a.TargetSDK}
	rep.check("release key is not the debug key and has not expired", nil, "expires "+cert.expires.UTC().Format("2006-01-02"))
	var primary string
	if o.apk {
		apk, err := releaseAndroidAPK(r, sdk, a, o, out, in, cert, rep)
		if err != nil {
			return "", err
		}
		primary = apk
	}
	if o.aab {
		aab, err := releaseAndroidAAB(r, sdk, a, o, out, in, rep)
		if err != nil {
			return "", err
		}
		if primary == "" {
			primary = aab
		}
	}
	rep.notRun("ARM64 hardware launch", "install the APK on an owner test phone")
	if o.aab {
		rep.notRun("Play Console internal-track upload and pre-launch review", "owner uploads manually; gofastr-native never uploads")
	}
	if err := r.do("write "+reportName, func() error {
		rep.printChecks(r.out)
		return rep.write(out, time.Now())
	}); err != nil {
		return "", err
	}
	return primary, nil
}

func releaseAndroidAPK(r *runner, sdk androidSDK, a app, o releaseOptions, out string, in androidReleaseInputs, cert keystoreCert, rep *releaseReport) (string, error) {
	dir := filepath.Join(in.work, "android")
	base := filepath.Join(dir, "base.apk")
	if err := r.run("link the manifest into a base APK", sdk.tool("aapt2"), aapt2LinkArgs(sdk, a, in.manifest, base, in.resourceArgs, false)...); err != nil {
		return "", err
	}
	unsigned := filepath.Join(dir, "unsigned.apk")
	if err := r.do("add classes.dex and the native libraries", func() error {
		return addToAPK(base, unsigned, in.dexFile, in.jniDir)
	}); err != nil {
		return "", err
	}
	aligned := filepath.Join(dir, "aligned.apk")
	pageAlign := buildToolsMajor(sdk) >= 35 || r.dryRun
	alignArgs := []string{"-p", "-f", "4", unsigned, aligned}
	if pageAlign {
		alignArgs = []string{"-P", "16", "-f", "4", unsigned, aligned}
	}
	if err := r.run("align the APK", sdk.tool("zipalign"), alignArgs...); err != nil {
		return "", err
	}
	s := o.android
	signed := filepath.Join(dir, "signed.apk")
	if err := r.do("clear the previous signed APK", func() error { return removeIfExists(signed) }); err != nil {
		return "", err
	}
	if err := r.run("sign the APK with the release key", sdk.tool("apksigner"), "sign",
		"--ks", s.keystore, "--ks-key-alias", s.alias, "--ks-pass", "env:"+s.storePassEnv, "--key-pass", "env:"+s.keyPassEnv,
		"--out", signed, aligned); err != nil {
		return "", err
	}
	if pageAlign {
		err := r.run("check zip and 16 KiB page alignment", sdk.tool("zipalign"), "-c", "-P", "16", "-v", "4", signed)
		if err != nil {
			return "", err
		}
		rep.check("zipalign -c -P 16 4", nil, "")
	} else {
		rep.notRun("zipalign -c -P 16 4", "build-tools "+filepath.Base(sdk.buildTools)+" predates -P; install build-tools 35 or newer")
	}
	verify, err := r.output("verify the APK signature", "", sdk.tool("apksigner"), "verify", "--print-certs", "-Werr", signed)
	if err != nil {
		return "", err
	}
	if !r.dryRun {
		digests := parseSignerDigests(verify)
		if len(digests) != 1 || digests[0] != cert.sha256 {
			return "", fmt.Errorf("apksigner reports signer digests %v, want the release key %s", digests, cert.sha256)
		}
	}
	rep.check("apksigner verify --print-certs -Werr; signer is the release key", nil, "")
	dump, err := r.output("read the APK's package, version, ABIs and debuggable flag", "", sdk.tool("aapt2"), "dump", "badging", signed)
	if err != nil {
		return "", err
	}
	if !r.dryRun {
		b := parseBadging(dump)
		if err := checkBadging(b, a); err != nil {
			return "", fmt.Errorf("aapt2 dump badging: %w", err)
		}
		if b.targetSDK != "" {
			rep.Android.TargetSDK, _ = strconv.Atoi(b.targetSDK)
		}
	}
	rep.check("aapt2 badging: package, versionCode, versionName, native-code, not debuggable", nil, strings.Join(a.ABIs, ","))
	apk := filepath.Join(out, a.Exe+".apk")
	if err := r.do("move the verified APK to "+apk, func() error {
		if err := os.Rename(signed, apk); err != nil {
			return err
		}
		return rep.addArtifact(apk)
	}); err != nil {
		return "", err
	}
	return apk, nil
}

func releaseAndroidAAB(r *runner, sdk androidSDK, a app, o releaseOptions, out string, in androidReleaseInputs, rep *releaseReport) (string, error) {
	dir := filepath.Join(in.work, "android")
	java := jdkTool("java")
	jarsigner := jdkTool("jarsigner")
	if java == "" || jarsigner == "" {
		if !r.dryRun {
			return "", errors.New("a JDK with java and jarsigner is required to build and sign an app bundle")
		}
		java, jarsigner = "java", "jarsigner"
	}
	proto := filepath.Join(dir, "proto.apk")
	if err := r.run("link the manifest and resources in protobuf format", sdk.tool("aapt2"), aapt2LinkArgs(sdk, a, in.manifest, proto, in.resourceArgs, true)...); err != nil {
		return "", err
	}
	module := filepath.Join(dir, "base.zip")
	if err := r.do("assemble the base bundle module", func() error {
		return buildBundleModule(proto, in.dexFile, in.jniDir, a.ABIs, module)
	}); err != nil {
		return "", err
	}
	config := filepath.Join(dir, "BundleConfig.json")
	if err := r.do("write BundleConfig.json (uncompressed, 16 KiB-aligned native libraries)", func() error {
		return writeOwnerOnly(config, []byte(bundleConfigJSON))
	}); err != nil {
		return "", err
	}
	unsigned := filepath.Join(dir, "unsigned.aab")
	signed := filepath.Join(dir, "signed.aab")
	if err := r.do("clear the previous bundle", func() error {
		if err := removeIfExists(unsigned); err != nil {
			return err
		}
		return removeIfExists(signed)
	}); err != nil {
		return "", err
	}
	bt := []string{"-jar", o.bundletool}
	if err := r.run("build the app bundle with the owner-supplied bundletool", java,
		append(bt, "build-bundle", "--modules="+module, "--config="+config, "--output="+unsigned)...); err != nil {
		return "", err
	}
	s := o.android
	if err := r.run("sign the bundle with the release key", jarsigner,
		"-keystore", s.keystore, "-storepass:env", s.storePassEnv, "-keypass:env", s.keyPassEnv,
		"-signedjar", signed, unsigned, s.alias); err != nil {
		return "", err
	}
	verified, err := r.output("verify the bundle's JAR signature", "", jarsigner, "-verify", signed)
	if err != nil {
		return "", err
	}
	if !r.dryRun && !strings.Contains(verified, "jar verified.") {
		return "", fmt.Errorf("jarsigner -verify did not report \"jar verified.\":\n%s", verified)
	}
	rep.check("jarsigner -verify", nil, "")
	if err := r.run("validate the bundle", java, append(bt, "validate", "--bundle="+signed)...); err != nil {
		return "", err
	}
	rep.check("bundletool validate", nil, "")
	dumped, err := r.output("read the bundle config", "", java, append(bt, "dump", "config", "--bundle="+signed)...)
	if err != nil {
		return "", err
	}
	if !r.dryRun && !strings.Contains(dumped, "PAGE_ALIGNMENT_16K") {
		return "", errors.New("bundletool dump config shows no PAGE_ALIGNMENT_16K: this bundletool ignored the 16 KiB native-library alignment")
	}
	rep.check("bundle config keeps PAGE_ALIGNMENT_16K", nil, "")
	aab := filepath.Join(out, a.Exe+".aab")
	if err := r.do("move the verified bundle to "+aab, func() error {
		if err := os.Rename(signed, aab); err != nil {
			return err
		}
		return rep.addArtifact(aab)
	}); err != nil {
		return "", err
	}
	return aab, nil
}

// bundleConfigJSON asks bundletool to store native libraries
// uncompressed and 16 KiB aligned. config.proto marks the alignment field
// experimental, so the build reads it back with `dump config`.
const bundleConfigJSON = `{"optimizations":{"uncompressNativeLibraries":{"enabled":true,"alignment":"PAGE_ALIGNMENT_16K"}}}` + "\n"

// localBundleAPK turns a bundle into a universal APK for a local install.
// bundletool signs it with its own debug key: this checks the bundle on
// an emulator, it is not how Play delivers the app.
func localBundleAPK(r *runner, o releaseOptions, aab string) (string, error) {
	java := jdkTool("java")
	if java == "" {
		java = "java"
	}
	dir := filepath.Join(filepath.Dir(aab), "work", "android")
	apks := filepath.Join(dir, "universal.apks")
	if err := r.do("clear the previous universal APK set", func() error { return removeIfExists(apks) }); err != nil {
		return "", err
	}
	if err := r.run("build a universal APK from the bundle (local testing only; bundletool's debug signing)", java,
		"-jar", o.bundletool, "build-apks", "--bundle="+aab, "--output="+apks, "--mode=universal"); err != nil {
		return "", err
	}
	apk := filepath.Join(dir, "universal.apk")
	if err := r.do("extract universal.apk", func() error { return extractZipEntry(apks, "universal.apk", apk) }); err != nil {
		return "", err
	}
	return apk, nil
}

func extractZipEntry(archive, name, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		w, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, io.LimitReader(rc, 1<<30)); err != nil {
			w.Close()
			return err
		}
		return w.Close()
	}
	return fmt.Errorf("%s has no %s", archive, name)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// runAndroidProfile installs a build. An APK installs as today; a
// bundle-only build installs a local universal APK made from the bundle.
func runAndroidProfile(r *runner, a app, artifact string, opts runOpts, o releaseOptions) error {
	if strings.HasSuffix(artifact, ".aab") {
		apk, err := localBundleAPK(r, o, artifact)
		if err != nil {
			return err
		}
		artifact = apk
	}
	return runAndroid(r, a, artifact, opts)
}
