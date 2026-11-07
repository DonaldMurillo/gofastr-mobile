package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"text/template"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

//go:embed scaffolds/app
var appScaffolds embed.FS

const (
	nativeModulePath = "github.com/DonaldMurillo/gofastr-native"
	// placeholderVersion is what go.mod requires when a replace supplies the module.
	placeholderVersion = "v0.0.0-00010101000000-000000000000"
)

// appScaffold is everything init writes into a new app. Every value has
// been validated before it reaches a template; Go source receives values
// only through strconv.Quote.
type appScaffold struct {
	Module     string // module path of the new app
	ImportBase string // import path of the app root (Module, or a subdirectory for the in-repo copy)
	Package    string // bind package name and directory
	ID, Name   string // application identity, defaults as build derives them
	CustomID   bool   // --id differs from build's default
	CustomName bool   // --name differs from build's default
	Native     nativeSource
	GoVersion  string // go directive
}

// nativeSource says how the new module gets gofastr-native: a local
// checkout through replace, or a published module version.
type nativeSource struct {
	dir     string // absolute checkout directory (replace mode)
	version string // module version (version mode)
	origin  string // where the answer came from, for the notice
}

var (
	initPackageRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,30}$`)
	goVersionRe   = regexp.MustCompile(`1\.[0-9]+(\.[0-9]+)?`)
	// Names that compile in Go but break the bind or the layout: main cannot
	// be bound, screens is the sibling package, and the rest are Java or
	// Android package roots the bound Java package would shadow.
	initReservedPackages = map[string]bool{"main": true, "screens": true, "java": true, "javax": true, "android": true, "androidx": true, "dalvik": true, "kotlin": true, "go": true}
)

// validateInitPackage applies the bind package grammar init promises: a
// short lowercase Go identifier that is neither a Go nor a Java keyword.
func validateInitPackage(name, source string) error {
	switch {
	case !initPackageRe.MatchString(name):
		return fmt.Errorf("--package %q%s: want 1-31 lowercase letters and digits, starting with a letter", name, source)
	case token.IsKeyword(name), javaReserved[name], initReservedPackages[name]:
		return fmt.Errorf("--package %q%s: reserved name (Go or Java keyword, main, screens, or a Java package root); choose another", name, source)
	}
	return nil
}

// defaultInitPackage derives a package name from the --out directory.
func defaultInitPackage(out string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(filepath.Base(out)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validateInitModule(path string) error {
	if err := module.CheckPath(path); err != nil {
		return fmt.Errorf("--module %q: %v", path, err)
	}
	if path == nativeModulePath || strings.HasPrefix(path, nativeModulePath+"/") {
		return fmt.Errorf("--module %q: choose a path outside %s", path, nativeModulePath)
	}
	return nil
}

// readOnlyOutput runs a read-only query even in a dry run, as resolvePkg
// does, because the plan needs the real answer.
func readOnlyOutput(r *runner, desc, name string, args ...string) (string, error) {
	dry := r.dryRun
	r.dryRun = false
	defer func() { r.dryRun = dry }()
	return r.output(desc, "", name, args...)
}

type goListModule struct {
	Path    string
	Version string
	Main    bool
	Dir     string
	GoMod   string
	Replace *goListModule
}

// resolveNativeSource implements --native. Empty asks the go tool which
// gofastr-native the current module uses.
func resolveNativeSource(r *runner, flagValue string) (nativeSource, error) {
	if flagValue != "" {
		if info, err := os.Stat(flagValue); err == nil && info.IsDir() {
			dir, err := filepath.Abs(flagValue)
			if err != nil {
				return nativeSource{}, err
			}
			if err := checkNativeCheckout(dir); err != nil {
				return nativeSource{}, err
			}
			return nativeSource{dir: dir, origin: "--native checkout"}, nil
		}
		if semver.IsValid(flagValue) && semver.Canonical(flagValue) == flagValue || module.IsPseudoVersion(flagValue) {
			return nativeSource{version: flagValue, origin: "--native version"}, nil
		}
		return nativeSource{}, fmt.Errorf("--native %q: want a gofastr-native checkout directory or a module version such as v0.1.0", flagValue)
	}
	out, err := readOnlyOutput(r, "find the gofastr-native module this CLI belongs to", "go", "list", "-m", "-json", nativeModulePath)
	if err == nil {
		var m goListModule
		if json.Unmarshal([]byte(out), &m) == nil {
			switch {
			case m.Main && m.Dir != "":
				return nativeSource{dir: m.Dir, origin: "this checkout (the current main module)"}, nil
			case m.Replace != nil && m.Replace.Version == "" && m.Replace.Dir != "":
				dir := m.Replace.Dir
				if !filepath.IsAbs(dir) && m.GoMod != "" {
					dir = filepath.Join(filepath.Dir(m.GoMod), dir)
				}
				return nativeSource{dir: dir, origin: "the current module's local replace"}, nil
			case m.Replace == nil && m.Version != "":
				return nativeSource{version: m.Version, origin: "the current module's requirement"}, nil
			}
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path == nativeModulePath && semver.IsValid(info.Main.Version) {
		return nativeSource{version: info.Main.Version, origin: "this CLI's build"}, nil
	}
	return nativeSource{}, errors.New("cannot tell which gofastr-native to use here; pass --native <checkout-dir> (for example --native ../gofastr-native) or --native <module version>")
}

func checkNativeCheckout(dir string) error {
	content, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return fmt.Errorf("--native %s: no go.mod: %w", dir, err)
	}
	if modfile.ModulePath(content) != nativeModulePath {
		return fmt.Errorf("--native %s: go.mod declares %q, not %s", dir, modfile.ModulePath(content), nativeModulePath)
	}
	return nil
}

// initGoVersion picks the go directive: the checkout's own, else the
// running toolchain's.
func initGoVersion(r *runner, native nativeSource) (string, error) {
	if native.dir != "" {
		content, err := os.ReadFile(filepath.Join(native.dir, "go.mod"))
		if err != nil {
			return "", err
		}
		parsed, err := modfile.ParseLax("go.mod", content, nil)
		if err == nil && parsed.Go != nil {
			return parsed.Go.Version, nil
		}
	}
	out, err := readOnlyOutput(r, "read the Go version", "go", "env", "GOVERSION")
	if err != nil {
		return "", err
	}
	if v := goVersionRe.FindString(out); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("cannot read a Go version from %q", out)
}

func appGoMod(s appScaffold) ([]byte, error) {
	f := new(modfile.File)
	if err := f.AddModuleStmt(s.Module); err != nil {
		return nil, err
	}
	if err := f.AddGoStmt(s.GoVersion); err != nil {
		return nil, err
	}
	version := s.Native.version
	if s.Native.dir != "" {
		version = placeholderVersion
	}
	if err := f.AddRequire(nativeModulePath, version); err != nil {
		return nil, err
	}
	if s.Native.dir != "" {
		if err := f.AddReplace(nativeModulePath, "", filepath.ToSlash(s.Native.dir), ""); err != nil {
			return nil, err
		}
	}
	for _, tool := range []string{nativeModulePath + "/cmd/gofastr-native", "golang.org/x/mobile/cmd/gobind", "golang.org/x/mobile/cmd/gomobile"} {
		if err := f.AddTool(tool); err != nil {
			return nil, err
		}
	}
	f.Cleanup()
	return f.Format()
}

// appScaffoldSources renders the Go packages. The in-repo starter is the
// same output with another import base, so the two cannot drift.
func appScaffoldSources(s appScaffold) (map[string][]byte, error) {
	data := map[string]string{
		"Package":        s.Package,
		"AppName":        strconv.Quote(s.Name),
		"DraftPrefix":    strconv.Quote(s.Package + ".item.draft."),
		"ScreensImport":  strconv.Quote(s.ImportBase + "/screens"),
		"AlreadyStarted": strconv.Quote(s.Package + ": already started"),
		"NotAbsolute":    strconv.Quote(s.Package + ": data directory must be absolute"),
		"NotStarted":     strconv.Quote(s.Package + ": not started"),
	}
	files := map[string][]byte{}
	for source, target := range map[string]string{
		"screens.go.tmpl":      "screens/screens.go",
		"screens_test.go.tmpl": "screens/screens_test.go",
		"bind.go.tmpl":         s.Package + "/" + s.Package + ".go",
		"bind_test.go.tmpl":    s.Package + "/" + s.Package + "_test.go",
	} {
		rendered, err := renderAppScaffold(source, data)
		if err != nil {
			return nil, err
		}
		if rendered, err = format.Source(rendered); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		files[target] = rendered
	}
	return files, nil
}

// appScaffoldFiles is the complete new module.
func appScaffoldFiles(s appScaffold) (map[string][]byte, error) {
	files, err := appScaffoldSources(s)
	if err != nil {
		return nil, err
	}
	if files["go.mod"], err = appGoMod(s); err != nil {
		return nil, err
	}
	files[".gitignore"] = []byte("/dist/\n")
	note := "It requires gofastr-native " + s.Native.version + "."
	if s.Native.dir != "" {
		note = "It uses the gofastr-native checkout at `" + filepath.ToSlash(s.Native.dir) + "` through a `replace` in go.mod, because gofastr-native has no published version yet. Once one exists, replace it with a normal requirement: `go mod edit -dropreplace " + nativeModulePath + "` then `go get " + nativeModulePath + "@<version>`."
	}
	readme, err := renderAppScaffold("readme.md.tmpl", map[string]string{
		"Name": s.Name, "Module": s.Module, "Package": s.Package, "NativeNote": note, "IdentityFlags": s.identityFlags(),
	})
	if err != nil {
		return nil, err
	}
	files["README.md"] = readme
	return files, nil
}

// identityFlags repeats a non-default --id or --name, which every build
// command must receive to target the same app.
func (s appScaffold) identityFlags() string {
	flags := ""
	if s.CustomID {
		flags += " --id " + s.ID
	}
	if s.CustomName {
		flags += ` --name "` + s.Name + `"`
	}
	return flags
}

func renderAppScaffold(name string, data map[string]string) ([]byte, error) {
	content, err := appScaffolds.ReadFile("scaffolds/app/" + name)
	if err != nil {
		return nil, err
	}
	parsed, err := template.New(name).Delims("<%", "%>").Option("missingkey=error").Parse(string(content))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := parsed.Execute(&out, data); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// starterScaffold is the checked-in copy under examples/starter.
func starterScaffold() appScaffold {
	return appScaffold{Module: nativeModulePath, ImportBase: nativeModulePath + "/examples/starter", Package: "starter", ID: "dev.gofastr.starter", Name: "Starter"}
}
