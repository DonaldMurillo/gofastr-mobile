package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"
)

//go:embed inspect_harness.txt
var inspectHarnessSource string

const inspectUsage = `usage: gofastr-native inspect --pkg <import path> [flags]

Opens every reachable screen of a native-renderer app under synthetic
renderer profiles and reports which screens open and which refuse. It
builds a throwaway harness with go build -overlay and -mod=readonly (or
-mod=vendor), so nothing is written into the module. The app's Start runs
against an empty temporary data directory that is removed afterwards.

App code still runs: inspect is not a sandbox. Start, render functions and
close hooks execute with your permissions and can have side effects.

The harness sends only hello, open and close. It never runs actions,
checkpoints, effect receipts or device services.

The report is redacted. Screens are opaque IDs (s1, s2, ...) shared by every
profile in one report. It never prints titles, text, field values, row
content, concrete routes, query strings, raw IDs, file paths or app output.
Route patterns and registered action IDs appear only when the package
exports InspectApp() *nativeui.App, optionally behind the gofastr_inspect
build tag.

  --pkg          package exporting Start, Stop, Root and Command (required)
  --profile      uikit, android, webview, minimal or all (default all)
  --route        first route to open (default: the app's Root)
  --depth        how many row/tab hops to follow (default 4)
  --max-screens  screens to open per profile (default 200)
  --timeout      limit for build and walk (default 3m)
  --json         print JSON instead of text
  --dry-run      print the plan; build and run nothing
`

// inspectTag is passed to go list and go build so an app can keep its
// InspectApp export out of gomobile and release builds.
const inspectTag = "gofastr_inspect"

const nativeuiImportPath = "github.com/DonaldMurillo/gofastr-native/nativeui"

// inspectProfile is a capability set assumed for one renderer build. It
// mirrors what the renderer source advertises with optional build flags
// off and no device services host. It is not a device measurement.
type inspectProfile struct {
	Name     string            `json:"name"`
	Renderer string            `json:"renderer"`
	Caps     map[string]string `json:"capabilities"`
}

func inspectProfiles() []inspectProfile {
	available := func(names ...string) map[string]string {
		caps := map[string]string{}
		for _, name := range names {
			caps[name+"/v1"] = "available"
		}
		return caps
	}
	// render/ios/GofastrNative.swift, without services or extensions.
	uikit := available("navigation", "editor", "drafts", "share", "haptics", "sheets", "announcement", "presentation", "files", "live", "privacy")
	// render/android/GofastrActivity.java, with microphone, incoming shares,
	// secure storage and widgets off.
	android := available("navigation", "editor", "drafts", "share", "haptics", "sheets", "announcement", "presentation", "files", "sharing", "clipboard", "live", "notifications", "privacy")
	for _, name := range []string{"audio", "incoming-shares", "secure-storage", "widgets"} {
		android[name+"/v1"] = "unavailable"
	}
	// render/webview/webview.js in a browser without the device services
	// host, navigator.share, navigator.vibrate or photo primitives.
	webview := available("navigation", "editor", "drafts", "announcement", "presentation", "live")
	for name, support := range map[string]string{"sheets": "fallback", "share": "fallback", "files": "fallback", "haptics": "unavailable", "photos": "unavailable", "camera": "unavailable"} {
		webview[name+"/v1"] = support
	}
	return []inspectProfile{
		{Name: "uikit", Renderer: "uikit", Caps: uikit},
		{Name: "android", Renderer: "android", Caps: android},
		{Name: "webview", Renderer: "webview", Caps: webview},
		{Name: "minimal", Renderer: "webview", Caps: available("navigation", "editor")},
	}
}

// The only capability names, reply codes and field kinds a report may
// echo. They mirror nativeui's built-in capabilities, the codes its trace
// accepts and the field kinds validateFields accepts. Anything else becomes
// "other" or is dropped.
var (
	inspectCapabilities = []string{"navigation/v1", "editor/v1", "drafts/v1", "share/v1", "haptics/v1", "sheets/v1", "announcement/v1", "presentation/v1",
		"files/v1", "photos/v1", "camera/v1", "notifications/v1", "sharing/v1", "clipboard/v1", "audio/v1", "live/v1", "incoming-shares/v1",
		"secure-storage/v1", "widgets/v1", "privacy/v1"}
	inspectCodes = []string{"protocol", "busy", "expired", "unsupported", "pending", "superseded", "stale", "invalid_request", "internal", "unknown",
		"conflict", "validation", "not_found", "forbidden", "draft", "cancelled", "unsaved_changes", "save_pending", "save_unknown", "stale_completion", "invalid_outcome"}
	inspectFieldKinds = []string{"text", "multiline", "toggle", "date", "time", "select"}
	inspectPhases     = []string{"start", "inspect-app", "root", "hello", "open", "close", "stop", "harness", "result"}
)

type inspectRequirement struct {
	Capability    string `json:"capability"`
	AllowFallback bool   `json:"allowFallback,omitempty"`
}

type inspectComponent struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// inspectOutline is a screen's structure: kinds, counts, flags and opaque
// screen IDs. Without InspectApp it holds no author strings at all. With it,
// ActionIDs, RowActionIDs, ComponentNames and Destinations hold only names
// and patterns the app registered, as nativeui.Outline reports them. Field
// and list keys are never included; they are not registered anywhere.
type inspectOutline struct {
	Content        string               `json:"content"`
	Protected      bool                 `json:"protected,omitempty"`
	Live           bool                 `json:"live,omitempty"`
	Draft          bool                 `json:"draft,omitempty"`
	Requires       []inspectRequirement `json:"requires,omitempty"`
	Actions        int                  `json:"actions,omitempty"`
	ActionIDs      []string             `json:"actionIds,omitempty"`
	Fields         map[string]int       `json:"fields,omitempty"`
	RequiredFields int                  `json:"requiredFields,omitempty"`
	Rows           int                  `json:"rows,omitempty"`
	DisabledRows   int                  `json:"disabledRows,omitempty"`
	RowActionIDs   []string             `json:"rowActionIds,omitempty"`
	Search         bool                 `json:"search,omitempty"`
	Filters        int                  `json:"filters,omitempty"`
	Selection      bool                 `json:"selection,omitempty"`
	Paging         bool                 `json:"paging,omitempty"`
	Refreshable    bool                 `json:"refreshable,omitempty"`
	Tabs           int                  `json:"tabs,omitempty"`
	Components     int                  `json:"components,omitempty"`
	ComponentNames []inspectComponent   `json:"componentNames,omitempty"`
	Images         int                  `json:"images,omitempty"`
	Attachments    int                  `json:"attachments,omitempty"`
	Destinations   []string             `json:"destinations,omitempty"`
	Links          []string             `json:"links,omitempty"`
	LinksOmitted   int                  `json:"linksOmitted,omitempty"`
}

type inspectScreen struct {
	ID         string          `json:"id"`
	Pattern    string          `json:"pattern,omitempty"`
	Parent     string          `json:"parent,omitempty"`
	Depth      int             `json:"depth"`
	Status     string          `json:"status"`
	Code       string          `json:"code,omitempty"`
	Capability string          `json:"capability,omitempty"`
	Outline    *inspectOutline `json:"outline,omitempty"`
}

type inspectResult struct {
	Profile   string          `json:"profile"`
	Renderer  string          `json:"renderer"`
	Connected bool            `json:"connected"`
	Code      string          `json:"code,omitempty"`
	Screens   []inspectScreen `json:"screens"`
	Unvisited int             `json:"unvisited"`
}

// inspectFailure says where the harness stopped. Cause is "panic", a fixed
// harness word, or up to four Go type names from the error's unwrap chain.
// It never holds an error message or a panic value.
type inspectFailure struct {
	Phase   string `json:"phase"`
	Profile string `json:"profile,omitempty"`
	Screen  string `json:"screen,omitempty"`
	Cause   string `json:"cause"`
}

type inspectReport struct {
	Package       string           `json:"package"`
	Synthetic     string           `json:"synthetic"`
	Redaction     string           `json:"redaction"`
	RoutePatterns bool             `json:"routePatterns"`
	Profiles      []inspectProfile `json:"profiles"`
	Results       []inspectResult  `json:"results,omitempty"`
	Failure       *inspectFailure  `json:"failure,omitempty"`
	DryRun        bool             `json:"dryRun,omitempty"`
}

const inspectSyntheticNote = "Capability profiles are assumptions taken from renderer source, not measurements from a device."

const (
	inspectOpaqueNote   = "Screens are opaque IDs shared by every profile in this report. Route patterns need an InspectApp() *nativeui.App export (docs/debugging.md)."
	inspectRegistryNote = "Screens are opaque IDs shared by every profile in this report. Route patterns and action IDs come from the app's InspectApp registry."
)

// Limits on what inspect reads back from the toolchain and the harness.
const (
	inspectMaxDiagnostics = 64 << 10
	inspectMaxResult      = 32 << 20
	inspectMaxLinks       = 64
)

var importPathRe = regexp.MustCompile(`^(\./)?[A-Za-z0-9][A-Za-z0-9._~/-]*$`)

// inspectListError is a go list package error. Its text comes from the
// toolchain, not from app code.
type inspectListError struct{ Err string }

type inspectPackage struct {
	ImportPath string
	Name       string
	Dir        string
	GoFiles    []string
	Error      *inspectListError
	DepsErrors []*inspectListError
	Module     *struct {
		Path string
		Dir  string
		Main bool
	}
}

func inspectCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, inspectUsage) }
	pkg := fs.String("pkg", "", "")
	profileName := fs.String("profile", "all", "")
	route := fs.String("route", "", "")
	depth := fs.Int("depth", 4, "")
	maxScreens := fs.Int("max-screens", 200, "")
	timeout := fs.Duration("timeout", 3*time.Minute, "")
	asJSON := fs.Bool("json", false, "")
	dryRun := fs.Bool("dry-run", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	usageErr := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "inspect: "+format+"\n", a...)
		return 2
	}
	if fs.NArg() != 0 {
		return usageErr("unexpected arguments %q", fs.Args())
	}
	if *pkg == "" {
		return usageErr("--pkg is required")
	}
	if !importPathRe.MatchString(*pkg) || strings.Contains(*pkg, "..") {
		return usageErr("--pkg %q: want an import path such as ./examples/gallery/gallerynative or example.com/app/mobile", *pkg)
	}
	if *depth < 0 || *maxScreens < 1 || *timeout <= 0 {
		return usageErr("--depth must be >= 0, --max-screens >= 1 and --timeout positive")
	}
	if *route != "" && !strings.HasPrefix(*route, "/") {
		return usageErr("--route must start with /")
	}
	var profiles []inspectProfile
	for _, candidate := range inspectProfiles() {
		if *profileName == "all" || *profileName == candidate.Name {
			profiles = append(profiles, candidate)
		}
	}
	if len(profiles) == 0 {
		return usageErr("--profile must be uikit, android, webview, minimal or all")
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	target, code := inspectResolve(ctx, *pkg, stderr)
	if code != 0 {
		return code
	}
	report := inspectReport{Package: target.info.ImportPath, Synthetic: inspectSyntheticNote, Redaction: inspectOpaqueNote,
		RoutePatterns: target.registry, Profiles: profiles, DryRun: *dryRun}
	if target.registry {
		report.Redaction = inspectRegistryNote
	}
	if *dryRun {
		return inspectPrint(report, *asJSON, stdout, stderr)
	}
	results, failure, err := inspectRun(ctx, target, profiles, *route, *depth, *maxScreens)
	if err != nil {
		var build *inspectBuildError
		if errors.As(err, &build) {
			fmt.Fprintf(stderr, "inspect: harness build failed (toolchain output only; no app code ran):\n%s", build.diagnostics)
			return 1
		}
		fmt.Fprintln(stderr, "inspect:", err)
		return 1
	}
	report.Results, report.Failure = results, failure
	if code := inspectPrint(report, *asJSON, stdout, stderr); code != 0 {
		return code
	}
	if failure != nil {
		fmt.Fprintf(stderr, "inspect: harness stopped: %s\n", failure.describe())
		return 1
	}
	return 0
}

func (failure *inspectFailure) describe() string {
	text := "phase " + failure.Phase
	if failure.Profile != "" {
		text += ", profile " + failure.Profile
	}
	if failure.Screen != "" {
		text += ", screen " + failure.Screen
	}
	return text + ", cause " + failure.Cause
}

type inspectTarget struct {
	info inspectPackage
	// mod is -mod=readonly or -mod=vendor. Every go command inspect runs
	// passes it, so a GOFLAGS=-mod=mod setting cannot rewrite go.mod or go.sum.
	mod string
	// registry reports an InspectApp() *nativeui.App export.
	registry bool
}

// inspectModFlag picks a module mode that never writes go.mod or go.sum:
// vendor when the main module vendors, readonly otherwise. go env itself
// reads no module graph.
func inspectModFlag(ctx context.Context) string {
	return readOnlyModuleFlag(ctx, "")
}

// inspectResolve asks go list where the package lives and checks its
// exports from source, so a missing export is a usage error, not a build log.
// go list loads packages without running or compiling app code.
func inspectResolve(ctx context.Context, pkg string, stderr io.Writer) (inspectTarget, int) {
	target := inspectTarget{mod: inspectModFlag(ctx)}
	list := exec.CommandContext(ctx, "go", "list", target.mod, "-tags", inspectTag, "-json", pkg)
	var out bytes.Buffer
	errs := &boundedBuffer{limit: inspectMaxDiagnostics}
	list.Stdout, list.Stderr = &out, errs
	if err := list.Run(); err != nil {
		fmt.Fprintf(stderr, "inspect: go list %s %s failed: %v\n%s", target.mod, pkg, err, errs.String())
		return target, 1
	}
	info := &target.info
	if err := json.Unmarshal(out.Bytes(), info); err != nil {
		fmt.Fprintln(stderr, "inspect: decode go list output:", err)
		return target, 1
	}
	if problems := append([]*inspectListError{info.Error}, info.DepsErrors...); info.Error != nil || len(info.DepsErrors) > 0 {
		fmt.Fprintf(stderr, "inspect: go list %s %s reported errors; inspect never updates go.mod or go.sum, so fix the module first:\n", target.mod, pkg)
		for _, problem := range problems {
			if problem != nil {
				fmt.Fprintf(stderr, "  %s\n", strings.TrimSpace(problem.Err))
			}
		}
		return target, 1
	}
	if info.Name == "main" {
		fmt.Fprintf(stderr, "inspect: %s is a main package; point --pkg at the package gomobile binds\n", info.ImportPath)
		return target, 2
	}
	if info.Module == nil || !info.Module.Main || info.Module.Dir == "" {
		fmt.Fprintf(stderr, "inspect: %s is not in the current module or workspace; run inspect from the module that contains it\n", info.ImportPath)
		return target, 2
	}
	exports := map[string]bool{}
	inspectApp := ""
	files := token.NewFileSet()
	for _, name := range info.GoFiles {
		parsed, err := parser.ParseFile(files, filepath.Join(info.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			fmt.Fprintln(stderr, "inspect:", err)
			return target, 1
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			exports[function.Name.Name] = true
			if function.Name.Name == "InspectApp" {
				inspectApp = "invalid"
				if returnsNativeApp(parsed, function) {
					inspectApp = "ok"
				}
			}
		}
	}
	if !exports["Command"] {
		fmt.Fprintf(stderr, "inspect: %s does not export Command; inspect needs the versioned Command export (see docs/native-presentation.md)\n", info.ImportPath)
		return target, 2
	}
	var missing []string
	for _, name := range []string{"Start", "Stop", "Root"} {
		if !exports[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "inspect: %s does not export %s; inspect needs Start(dataDir), Stop, Root and Command\n", info.ImportPath, strings.Join(missing, ", "))
		return target, 2
	}
	if inspectApp == "invalid" {
		fmt.Fprintf(stderr, "inspect: %s exports InspectApp with the wrong signature; want func InspectApp() *nativeui.App\n", info.ImportPath)
		return target, 2
	}
	target.registry = inspectApp == "ok"
	return target, 0
}

// returnsNativeApp reports whether function is func InspectApp() *X.App
// with X the file's import of nativeui.
func returnsNativeApp(file *ast.File, function *ast.FuncDecl) bool {
	signature := function.Type
	if signature.TypeParams != nil && len(signature.TypeParams.List) != 0 || len(signature.Params.List) != 0 ||
		signature.Results == nil || len(signature.Results.List) != 1 || len(signature.Results.List[0].Names) > 1 {
		return false
	}
	star, ok := signature.Results.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := star.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "App" {
		return false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != nativeuiImportPath {
			continue
		}
		name := "nativeui"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == qualifier.Name {
			return true
		}
	}
	return false
}

// inspectBuildError carries go build's own diagnostics. The build runs no
// app code, so this output cannot hold runtime content. Temporary paths
// are replaced with <harness>.
type inspectBuildError struct{ diagnostics string }

func (err *inspectBuildError) Error() string { return "harness build failed" }

// boundedBuffer keeps the first limit bytes written to it and counts the
// rest, so a chatty toolchain cannot exhaust memory.
type boundedBuffer struct {
	limit   int
	buf     bytes.Buffer
	dropped int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	keep := min(len(p), b.limit-b.buf.Len())
	if keep > 0 {
		b.buf.Write(p[:keep])
	}
	b.dropped += len(p) - max(keep, 0)
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	if b.dropped > 0 {
		return b.buf.String() + fmt.Sprintf("\n[%d more bytes not shown]\n", b.dropped)
	}
	return b.buf.String()
}

func inspectRun(ctx context.Context, target inspectTarget, profiles []inspectProfile, route string, depth, maxScreens int) (results []inspectResult, stopped *inspectFailure, failure error) {
	info := target.info
	work, err := os.MkdirTemp("", "gofastr-inspect-")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil && failure == nil {
			failure = errors.New("could not remove the temporary inspect directory")
		}
	}()
	dataDir := filepath.Join(work, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		return nil, nil, err
	}
	var source bytes.Buffer
	harness := template.Must(template.New("harness").Delims("[[", "]]").Parse(inspectHarnessSource))
	if err := harness.Execute(&source, map[string]any{"Package": info.ImportPath, "Registry": target.registry}); err != nil {
		return nil, nil, err
	}
	sourcePath := filepath.Join(work, "main.go")
	if err := os.WriteFile(sourcePath, source.Bytes(), 0o600); err != nil {
		return nil, nil, err
	}
	// The harness directory exists only in the overlay. A random name keeps
	// two inspections apart; the module never sees a real file.
	virtual := filepath.Join(info.Module.Dir, ".gofastr-inspect-"+filepath.Base(work))
	if _, err := os.Stat(virtual); !errors.Is(err, os.ErrNotExist) {
		return nil, nil, errors.New("refusing to shadow an existing .gofastr-inspect- path in the module")
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(virtual, "main.go"): sourcePath}})
	if err != nil {
		return nil, nil, err
	}
	overlayPath := filepath.Join(work, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		return nil, nil, err
	}
	binary := filepath.Join(work, "harness")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", target.mod, "-tags", inspectTag, "-overlay", overlayPath, "-o", binary, "."+string(filepath.Separator)+filepath.Base(virtual))
	build.Dir = info.Module.Dir
	diagnostics := &boundedBuffer{limit: inspectMaxDiagnostics}
	build.Stderr = diagnostics
	if err := build.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("stopped: %w", ctx.Err())
		}
		text := diagnostics.String()
		for _, path := range []string{virtual, work, filepath.ToSlash(virtual), filepath.ToSlash(work)} {
			text = strings.ReplaceAll(text, path, "<harness>")
		}
		return nil, nil, &inspectBuildError{diagnostics: text}
	}

	resultPath := filepath.Join(work, "result.json")
	type planProfile struct {
		Name  string `json:"name"`
		Hello any    `json:"hello"`
	}
	plan := struct {
		DataDir      string        `json:"dataDir"`
		ResultPath   string        `json:"resultPath"`
		Start        string        `json:"start"`
		Depth        int           `json:"depth"`
		MaxScreens   int           `json:"maxScreens"`
		MaxLinks     int           `json:"maxLinks"`
		Profiles     []planProfile `json:"profiles"`
		Capabilities []string      `json:"capabilities"`
		Codes        []string      `json:"codes"`
		FieldKinds   []string      `json:"fieldKinds"`
	}{DataDir: dataDir, ResultPath: resultPath, Start: route, Depth: depth, MaxScreens: maxScreens, MaxLinks: inspectMaxLinks,
		Capabilities: inspectCapabilities, Codes: inspectCodes, FieldKinds: inspectFieldKinds}
	for _, profile := range profiles {
		plan.Profiles = append(plan.Profiles, planProfile{Name: profile.Name, Hello: map[string]any{
			"minimum": 1, "maximum": 1, "renderer": profile.Renderer, "capabilities": profile.Caps,
		}})
	}
	input, err := json.Marshal(plan)
	if err != nil {
		return nil, nil, err
	}
	// The plan, including the data directory and result path, travels on
	// stdin, never in argv or the environment. Stdout and stderr go to the
	// null device: inspect never relays app output, and nothing buffers it.
	run := exec.CommandContext(ctx, binary)
	run.Dir = work
	run.Stdin = bytes.NewReader(input)
	run.WaitDelay = 5 * time.Second
	_ = run.Run()
	if ctx.Err() != nil {
		return nil, nil, fmt.Errorf("stopped: %w", ctx.Err())
	}
	results, stopped = inspectReadResult(resultPath, profiles, target.registry)
	return results, stopped, nil
}

// inspectReadResult reads the harness result with a size limit and checks
// every field against the redaction contract. A result that breaks it is
// discarded whole: app code shares the harness process and could have
// written the file.
func inspectReadResult(path string, profiles []inspectProfile, registry bool) ([]inspectResult, *inspectFailure) {
	file, err := os.Open(path)
	if err != nil {
		return nil, &inspectFailure{Phase: "harness", Cause: "exited-without-result"}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, inspectMaxResult+1))
	if err != nil || len(data) > inspectMaxResult {
		return nil, &inspectFailure{Phase: "result", Cause: "unreadable"}
	}
	var decoded struct {
		Results []inspectResult `json:"results"`
		Failure *inspectFailure `json:"failure"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || !validInspectOutput(decoded.Results, decoded.Failure, profiles, registry) {
		return nil, &inspectFailure{Phase: "result", Cause: "invalid"}
	}
	return decoded.Results, decoded.Failure
}

var (
	inspectScreenID = regexp.MustCompile(`^s[1-9][0-9]{0,6}$`)
	inspectToken    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
	inspectTypeName = regexp.MustCompile(`^[A-Za-z0-9_.*\[\]/,()-]{1,200}$`)
)

func inspectPattern(pattern string) bool {
	return len(pattern) <= 256 && strings.HasPrefix(pattern, "/") && !strings.ContainsAny(pattern, "?#\\ ") && strings.IndexFunc(pattern, unicode.IsControl) < 0
}

func validInspectOutput(results []inspectResult, failure *inspectFailure, profiles []inspectProfile, registry bool) bool {
	in := func(value string, values []string) bool {
		for _, candidate := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	code := func(value string) bool { return value == "" || value == "other" || in(value, inspectCodes) }
	id := func(value string, optional bool) bool {
		return optional && value == "" || inspectScreenID.MatchString(value)
	}
	tokens := func(values []string, check func(string) bool) bool {
		if len(values) != 0 && !registry {
			return false
		}
		for _, value := range values {
			if !check(value) {
				return false
			}
		}
		return true
	}
	if failure != nil {
		if !in(failure.Phase, inspectPhases) || failure.Profile != "" && !in(failure.Profile, []string{"uikit", "android", "webview", "minimal"}) || !id(failure.Screen, true) {
			return false
		}
		for _, part := range strings.Split(failure.Cause, ">") {
			if !inspectTypeName.MatchString(part) {
				return false
			}
		}
	}
	if len(results) > len(profiles) {
		return false
	}
	for index, result := range results {
		if result.Profile != profiles[index].Name || result.Renderer != profiles[index].Renderer || !code(result.Code) || result.Unvisited < 0 {
			return false
		}
		for _, screen := range result.Screens {
			if !id(screen.ID, false) || !id(screen.Parent, true) || screen.Depth < 0 || !code(screen.Code) ||
				screen.Capability != "" && !in(screen.Capability, inspectCapabilities) ||
				screen.Pattern != "" && (!registry || !inspectPattern(screen.Pattern)) {
				return false
			}
			switch screen.Status {
			case "ok":
				if screen.Outline == nil {
					return false
				}
			case "refused", "empty":
				if screen.Outline != nil {
					return false
				}
			default:
				return false
			}
			outline := screen.Outline
			if outline == nil {
				continue
			}
			if !in(outline.Content, []string{"list", "form", "file-preview", "text", "components", "none"}) ||
				!tokens(outline.ActionIDs, inspectToken.MatchString) || !tokens(outline.RowActionIDs, inspectToken.MatchString) ||
				!tokens(outline.Destinations, inspectPattern) || len(outline.ComponentNames) != 0 && !registry {
				return false
			}
			for kind := range outline.Fields {
				if kind != "other" && !in(kind, inspectFieldKinds) {
					return false
				}
			}
			for _, need := range outline.Requires {
				if !in(need.Capability, inspectCapabilities) {
					return false
				}
			}
			for _, named := range outline.ComponentNames {
				if !capabilityLike.MatchString(named.Name) {
					return false
				}
			}
			for _, link := range outline.Links {
				if !id(link, false) {
					return false
				}
			}
		}
	}
	return true
}

var capabilityLike = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}/v[0-9]{1,4}$`)

func inspectPrint(report inspectReport, asJSON bool, stdout, stderr io.Writer) int {
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(stderr, "inspect:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "package %s\n%s\n%s\n", report.Package, report.Synthetic, report.Redaction)
	if report.DryRun {
		fmt.Fprintln(stdout, "dry run: nothing built, started or written")
		for _, profile := range report.Profiles {
			names := make([]string, 0, len(profile.Caps))
			for name, support := range profile.Caps {
				names = append(names, name+"="+support)
			}
			sort.Strings(names)
			fmt.Fprintf(stdout, "profile %s (renderer %s): %s\n", profile.Name, profile.Renderer, strings.Join(names, " "))
		}
		return 0
	}
	for _, result := range report.Results {
		fmt.Fprintf(stdout, "\nprofile %s (renderer %s, assumed)\n", result.Profile, result.Renderer)
		if !result.Connected {
			fmt.Fprintf(stdout, "  hello refused: %s\n", result.Code)
			continue
		}
		opened, refused := 0, 0
		for _, screen := range result.Screens {
			name := screen.ID
			if screen.Pattern != "" {
				name += " " + screen.Pattern
			}
			where := fmt.Sprintf("depth %d", screen.Depth)
			if screen.Parent != "" {
				where += " from " + screen.Parent
			}
			detail := ""
			switch screen.Status {
			case "refused":
				refused++
				detail = screen.Code
				if screen.Capability != "" {
					detail += ", needs " + screen.Capability
				}
			case "ok":
				opened++
				detail = screen.Outline.summary()
			}
			fmt.Fprintf(stdout, "  %-8s %s  %s  %s\n", screen.Status, name, where, detail)
		}
		fmt.Fprintf(stdout, "  %d opened, %d refused", opened, refused)
		if result.Unvisited > 0 {
			fmt.Fprintf(stdout, ", %d not visited (--max-screens)", result.Unvisited)
		}
		fmt.Fprintln(stdout)
	}
	if report.Failure != nil {
		fmt.Fprintf(stdout, "\nstopped: %s\n", report.Failure.describe())
	}
	return 0
}

// summary prints an outline as one line of words and counts.
func (outline *inspectOutline) summary() string {
	parts := []string{outline.Content}
	count := func(name string, value int) {
		if value > 0 {
			parts = append(parts, name+"="+strconv.Itoa(value))
		}
	}
	flag := func(name string, on bool) {
		if on {
			parts = append(parts, name)
		}
	}
	list := func(name string, values []string) {
		if len(values) > 0 {
			parts = append(parts, name+"="+strings.Join(values, ","))
		}
	}
	count("rows", outline.Rows)
	count("disabled", outline.DisabledRows)
	fields := 0
	kinds := make([]string, 0, len(outline.Fields))
	for kind, n := range outline.Fields {
		fields += n
		kinds = append(kinds, kind+":"+strconv.Itoa(n))
	}
	sort.Strings(kinds)
	if fields > 0 {
		parts = append(parts, "fields="+strings.Join(kinds, ","))
	}
	count("required", outline.RequiredFields)
	count("filters", outline.Filters)
	count("actions", outline.Actions)
	count("tabs", outline.Tabs)
	count("components", outline.Components)
	count("images", outline.Images)
	count("attachments", outline.Attachments)
	flag("protected", outline.Protected)
	flag("live", outline.Live)
	flag("draft", outline.Draft)
	flag("search", outline.Search)
	flag("selection", outline.Selection)
	flag("paging", outline.Paging)
	flag("refreshable", outline.Refreshable)
	var needs []string
	for _, need := range outline.Requires {
		text := need.Capability
		if need.AllowFallback {
			text += "~fallback"
		}
		needs = append(needs, text)
	}
	list("requires", needs)
	list("action-ids", outline.ActionIDs)
	list("row-actions", outline.RowActionIDs)
	list("destinations", outline.Destinations)
	list("links", outline.Links)
	count("links-omitted", outline.LinksOmitted)
	return strings.Join(parts, " ")
}
