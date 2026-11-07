package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Provisioning profiles and signing identities for ios-device builds.
// The doctor's read-only parser (signatureTeam, provisioningNode,
// inspectProvisioning) moved here unchanged; the build reuses it.

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

// Profile kinds the CLI recognises. Only development and ad-hoc builds
// are produced; the other two are refused with what is missing.
const (
	profileDevelopment = "development"
	profileAdHoc       = "ad-hoc"
	profileAppStore    = "app-store"
	profileEnterprise  = "enterprise"
)

// provisioningProfile is the part of a decoded .mobileprovision the build
// checks. It never holds the profile's raw contents.
type provisioningProfile struct {
	Name, UUID     string
	TeamIDs        []string
	Prefix         string
	AppIdentifier  string
	TeamEntitled   string
	Expiry         time.Time
	Devices        []string
	AllDevices     bool
	GetTaskAllow   bool
	KeychainGroups []string
	AppGroups      []string
	CertSHA1s      []string
}

func plistRoot(content []byte) (provisioningNode, error) {
	if len(content) > 4<<20 {
		return provisioningNode{}, errors.New("profile exceeds 4 MiB")
	}
	var plist provisioningNode
	if err := xml.NewDecoder(bytes.NewReader(content)).Decode(&plist); err != nil {
		return provisioningNode{}, fmt.Errorf("invalid plist XML: %w", err)
	}
	if plist.XMLName.Local != "plist" || len(plist.Children) != 1 || plist.Children[0].XMLName.Local != "dict" {
		return provisioningNode{}, errors.New("plist has no top-level dictionary")
	}
	return plist.Children[0], nil
}

func (node provisioningNode) strings() []string {
	var out []string
	for _, child := range node.Children {
		if child.XMLName.Local == "string" {
			out = append(out, strings.TrimSpace(child.Text))
		}
	}
	return out
}

func (node provisioningNode) boolean() bool { return node.XMLName.Local == "true" }

// parseProvisioningProfile reads a decoded profile (security cms -D).
func parseProvisioningProfile(content []byte) (provisioningProfile, error) {
	root, err := plistRoot(content)
	if err != nil {
		return provisioningProfile{}, err
	}
	p := provisioningProfile{
		Name:       strings.TrimSpace(root.field("Name").Text),
		UUID:       strings.TrimSpace(root.field("UUID").Text),
		TeamIDs:    root.field("TeamIdentifier").strings(),
		Devices:    root.field("ProvisionedDevices").strings(),
		AllDevices: root.field("ProvisionsAllDevices").boolean(),
	}
	if prefixes := root.field("ApplicationIdentifierPrefix").strings(); len(prefixes) > 0 {
		p.Prefix = prefixes[0]
	}
	if p.Expiry, err = time.Parse(time.RFC3339, strings.TrimSpace(root.field("ExpirationDate").Text)); err != nil {
		return p, errors.New("profile has no valid ExpirationDate")
	}
	entitlements := root.field("Entitlements")
	p.AppIdentifier = strings.TrimSpace(entitlements.field("application-identifier").Text)
	p.TeamEntitled = strings.TrimSpace(entitlements.field("com.apple.developer.team-identifier").Text)
	p.GetTaskAllow = entitlements.field("get-task-allow").boolean()
	p.KeychainGroups = entitlements.field("keychain-access-groups").strings()
	p.AppGroups = entitlements.field("com.apple.security.application-groups").strings()
	for _, cert := range root.field("DeveloperCertificates").Children {
		if cert.XMLName.Local != "data" {
			continue
		}
		der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(cert.Text), ""))
		if err != nil {
			return p, errors.New("profile has an unreadable DeveloperCertificates entry")
		}
		sum := sha1.Sum(der)
		p.CertSHA1s = append(p.CertSHA1s, strings.ToUpper(hex.EncodeToString(sum[:])))
	}
	if p.UUID == "" || p.Prefix == "" || p.AppIdentifier == "" {
		return p, errors.New("profile lacks UUID, ApplicationIdentifierPrefix or application-identifier")
	}
	return p, nil
}

// kind classifies the profile the way Xcode's distribution methods do.
func (p provisioningProfile) kind() string {
	switch {
	case p.AllDevices:
		return profileEnterprise
	case len(p.Devices) == 0:
		return profileAppStore
	case p.GetTaskAllow:
		return profileDevelopment
	default:
		return profileAdHoc
	}
}

// profileRequirement is what one bundle needs from its profile.
type profileRequirement struct {
	bundleID, teamID, appGroup string
	wantKind                   string // development for debug, ad-hoc for release
	exact                      bool   // an app group or extension is involved
	keychain                   bool   // the bundle gets a private Keychain group
	identitySHA1               string // "" when not resolved (dry run)
}

// patternMatches applies the profile wildcard rule: "ABC.*" covers
// "ABC.anything", anything else must match exactly.
func patternMatches(pattern, value string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}

// check lists every way the profile fails the requirement.
func (p provisioningProfile) check(req profileRequirement, now time.Time) error {
	var problems []string
	switch kind := p.kind(); {
	case kind == profileEnterprise:
		problems = append(problems, "enterprise (ProvisionsAllDevices) profiles are not supported")
	case kind == profileAppStore:
		problems = append(problems, "App Store profiles are not supported yet: that path also needs an asset-catalog app icon (actool), a privacy manifest, DT* build metadata and an upload tool")
	case kind != req.wantKind:
		want := "--profile debug needs a development profile"
		if req.wantKind == profileAdHoc {
			want = "--profile release needs an ad hoc distribution profile"
		}
		problems = append(problems, fmt.Sprintf("this is a %s profile; %s", kind, want))
	}
	if !contains(p.TeamIDs, req.teamID) || p.TeamEntitled != req.teamID {
		problems = append(problems, fmt.Sprintf("profile team %v does not match --team-id %s", p.TeamIDs, req.teamID))
	}
	if !now.Before(p.Expiry) {
		problems = append(problems, "profile expired on "+p.Expiry.UTC().Format("2006-01-02"))
	}
	wantApp := p.Prefix + "." + req.bundleID
	switch {
	case strings.HasSuffix(p.AppIdentifier, "*") && req.exact:
		problems = append(problems, fmt.Sprintf("wildcard App ID %s cannot carry app groups or extensions; use an explicit App ID for %s", p.AppIdentifier, req.bundleID))
	case !patternMatches(p.AppIdentifier, wantApp):
		problems = append(problems, fmt.Sprintf("profile App ID %s does not cover %s", p.AppIdentifier, wantApp))
	}
	if req.appGroup != "" && !contains(p.AppGroups, req.appGroup) {
		problems = append(problems, fmt.Sprintf("profile does not allow app group %s", req.appGroup))
	}
	if req.keychain {
		covered := false
		for _, pattern := range p.KeychainGroups {
			covered = covered || patternMatches(pattern, wantApp)
		}
		if !covered {
			problems = append(problems, "profile keychain-access-groups do not cover "+wantApp)
		}
	}
	if req.identitySHA1 != "" && !contains(p.CertSHA1s, req.identitySHA1) {
		problems = append(problems, "--sign-identity "+req.identitySHA1+" is not one of the profile's certificates")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s profile %q: %s", req.bundleID, p.Name, strings.Join(problems, "; "))
	}
	return nil
}

// entitlementSet is one bundle's signing entitlements. Values are
// string, bool or []string.
type entitlementSet map[string]any

const (
	entApplicationID  = "application-identifier"
	entTeamID         = "com.apple.developer.team-identifier"
	entGetTaskAllow   = "get-task-allow"
	entKeychainGroups = "keychain-access-groups"
	entAppGroups      = "com.apple.security.application-groups"
)

// deviceEntitlements builds the entitlements for one bundle. Only the app
// gets keychain-access-groups: without it an extension's default
// Keychain group is its own identifier, so it cannot read the app's
// private items.
func deviceEntitlements(p provisioningProfile, bundleID, teamID, appGroup string, keychain bool) entitlementSet {
	e := entitlementSet{
		entApplicationID: p.Prefix + "." + bundleID,
		entTeamID:        teamID,
		entGetTaskAllow:  p.GetTaskAllow,
	}
	if keychain {
		e[entKeychainGroups] = []string{p.Prefix + "." + bundleID}
	}
	if appGroup != "" {
		e[entAppGroups] = []string{appGroup}
	}
	return e
}

// allowedBy checks that the profile grants every entitlement.
func (e entitlementSet) allowedBy(p provisioningProfile) error {
	var problems []string
	for _, key := range e.keys() {
		switch v := e[key].(type) {
		case string:
			switch key {
			case entApplicationID:
				if !patternMatches(p.AppIdentifier, v) {
					problems = append(problems, key+" "+v+" not granted")
				}
			case entTeamID:
				if p.TeamEntitled != v {
					problems = append(problems, key+" "+v+" not granted")
				}
			default:
				problems = append(problems, key+" is not a known entitlement")
			}
		case bool:
			if key != entGetTaskAllow || v != p.GetTaskAllow {
				problems = append(problems, fmt.Sprintf("%s=%t not granted", key, v))
			}
		case []string:
			for _, item := range v {
				granted := false
				switch key {
				case entKeychainGroups:
					for _, pattern := range p.KeychainGroups {
						granted = granted || patternMatches(pattern, item)
					}
				case entAppGroups:
					granted = contains(p.AppGroups, item)
				}
				if !granted {
					problems = append(problems, key+" "+item+" not granted")
				}
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (e entitlementSet) keys() []string {
	keys := make([]string, 0, len(e))
	for key := range e {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// plist renders the entitlements as a deterministic XML plist.
func (e entitlementSet) plist() []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	esc := func(s string) string {
		var out bytes.Buffer
		_ = xml.EscapeText(&out, []byte(s))
		return out.String()
	}
	for _, key := range e.keys() {
		fmt.Fprintf(&b, "\t<key>%s</key>\n", esc(key))
		switch v := e[key].(type) {
		case string:
			fmt.Fprintf(&b, "\t<string>%s</string>\n", esc(v))
		case bool:
			fmt.Fprintf(&b, "\t<%t/>\n", v)
		case []string:
			b.WriteString("\t<array>\n")
			for _, item := range v {
				fmt.Fprintf(&b, "\t\t<string>%s</string>\n", esc(item))
			}
			b.WriteString("\t</array>\n")
		}
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

// parseEntitlements reads a plist dictionary of entitlements, such as
// codesign -d --entitlements - --xml prints.
func parseEntitlements(content []byte) (entitlementSet, error) {
	start := bytes.Index(content, []byte("<?xml"))
	if start < 0 {
		start = bytes.Index(content, []byte("<plist"))
	}
	if start < 0 {
		return nil, errors.New("no entitlements plist in the output")
	}
	root, err := plistRoot(content[start:])
	if err != nil {
		return nil, err
	}
	e := entitlementSet{}
	for i := 0; i+1 < len(root.Children); i += 2 {
		if root.Children[i].XMLName.Local != "key" {
			return nil, errors.New("malformed entitlements dictionary")
		}
		key, value := root.Children[i].Text, root.Children[i+1]
		switch value.XMLName.Local {
		case "string":
			e[key] = value.Text
		case "true", "false":
			e[key] = value.boolean()
		case "array":
			e[key] = value.strings()
		default:
			return nil, fmt.Errorf("entitlement %s has an unsupported %s value", key, value.XMLName.Local)
		}
	}
	return e, nil
}

// equal compares two entitlement sets exactly.
func (e entitlementSet) equal(other entitlementSet) bool {
	if len(e) != len(other) {
		return false
	}
	for key, v := range e {
		w, ok := other[key]
		if !ok {
			return false
		}
		switch v := v.(type) {
		case []string:
			ws, ok := w.([]string)
			if !ok || strings.Join(v, "\x00") != strings.Join(ws, "\x00") {
				return false
			}
		default:
			if v != w {
				return false
			}
		}
	}
	return true
}

// findIdentityLine is one row of security find-identity -v -p codesigning.
var findIdentityLine = regexp.MustCompile(`^\s*\d+\)\s+([0-9A-Fa-f]{40})\s+"(.*)"\s*$`)

// parseFindIdentity resolves an identity name to its SHA-1. The name must
// match exactly one identity.
func parseFindIdentity(listing, name string) (string, error) {
	var matches []string
	for _, line := range strings.Split(listing, "\n") {
		m := findIdentityLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m != nil && m[2] == name && !contains(matches, strings.ToUpper(m[1])) {
			matches = append(matches, strings.ToUpper(m[1]))
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no valid codesigning identity named %q (security find-identity -v -p codesigning lists them)", name)
	case 1:
		return matches[0], nil
	}
	return "", fmt.Errorf("%d identities are named %q; pass --sign-identity <SHA-1> instead", len(matches), name)
}

// decodeProvisioningProfile decodes a .mobileprovision with
// security cms -D into a private temporary file that is removed
// afterwards. Tests replace it.
var decodeProvisioningProfile = func(r *runner, profile string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("decoding a provisioning profile needs macOS (security cms)")
	}
	dir, err := os.MkdirTemp("", "gofastr-profile-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	decoded := filepath.Join(dir, "profile.plist")
	if err := r.run("decode "+filepath.Base(profile)+" (read-only)", "security", "cms", "-D", "-i", profile, "-o", decoded); err != nil {
		return nil, err
	}
	return os.ReadFile(decoded)
}

// profileDecoderInjected lets a dry run use a test decoder; a real dry
// run never decodes.
var profileDecoderInjected = false
