package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const initUsage = `Usage:
  gofastr-native init --out <new-directory> [--module <path>] [--package <name>]
                      [--id <reverse-dns>] [--name <display name>]
                      [--native <checkout-dir|module-version>] [--dry-run]

init creates a new Go module with a working native app: Go-defined screens,
a private item store, durable editor drafts, tests, and the CLI pinned as a
go tool. It never writes into an existing directory.

  --out      new directory; its parent must exist
  --module   module path (default example.com/<package>)
  --package  bind package name (default: --out's base name, lowercased,
             letters and digits only)
  --id       application id (default dev.gofastr.<package>)
  --name     home-screen name (default: the package name, capitalized)
  --native   gofastr-native checkout directory (used through a replace) or
             a published module version. Default: whatever the current
             module uses. gofastr-native has no published version yet, so
             today this means a checkout.
  --dry-run  print the files and commands without creating anything
`

// initCommand is the hook for "gofastr-native init". It parses its own
// flags, like extensionCommand, so it never sees build flags.
func initCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, initUsage)
		return 0
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, initUsage) }
	out := fs.String("out", "", "new directory")
	modulePath := fs.String("module", "", "module path")
	pkg := fs.String("package", "", "bind package name")
	id := fs.String("id", "", "application id")
	name := fs.String("name", "", "home-screen name")
	native := fs.String("native", "", "gofastr-native checkout or version")
	dry := fs.Bool("dry-run", false, "print the plan only")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}
	r := &runner{ctx: ctx, out: stdout, dryRun: *dry}
	plan, code, err := planInit(r, initRequest{out: *out, module: *modulePath, pkg: *pkg, id: *id, name: *name, native: *native})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return code
	}
	if err := writeInit(r, plan, writeNewScaffoldFile); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return tidyInit(r, plan, stderr)
}

type initRequest struct{ out, module, pkg, id, name, native string }

type initPlan struct {
	dir      string
	scaffold appScaffold
	files    map[string][]byte
}

// planInit validates every input before anything is created. It runs only
// read-only go queries. Usage errors return code 2.
func planInit(r *runner, req initRequest) (initPlan, int, error) {
	if req.out == "" {
		return initPlan{}, 2, errors.New("init requires --out <new-directory>")
	}
	pkg, source := req.pkg, ""
	if pkg == "" {
		pkg, source = defaultInitPackage(req.out), " (derived from --out; pass --package to choose one)"
	}
	if err := validateInitPackage(pkg, source); err != nil {
		return initPlan{}, 2, err
	}
	modulePath := req.module
	if modulePath == "" {
		modulePath = "example.com/" + pkg
		fmt.Fprintf(r.out, "module path: %s (pass --module to choose the path you will publish under)\n", modulePath)
	}
	if err := validateInitModule(modulePath); err != nil {
		return initPlan{}, 2, err
	}
	// Share build's identity grammar so every README command works as written.
	a, err := newApp(modulePath+"/"+pkg, pkg, req.id, req.name, "")
	if err != nil {
		return initPlan{}, 2, err
	}
	dir, err := newOutputDir(req.out)
	if err != nil {
		return initPlan{}, 2, err
	}
	native, err := resolveNativeSource(r, req.native)
	if err != nil {
		return initPlan{}, 2, err
	}
	goVersion, err := initGoVersion(r, native)
	if err != nil {
		return initPlan{}, 1, err
	}
	defaults, _ := newApp(modulePath+"/"+pkg, pkg, "", "", "")
	s := appScaffold{Module: modulePath, ImportBase: modulePath, Package: pkg, ID: a.ID, Name: a.Name,
		CustomID: a.ID != defaults.ID, CustomName: a.Name != defaults.Name, Native: native, GoVersion: goVersion}
	files, err := appScaffoldFiles(s)
	if err != nil {
		return initPlan{}, 1, err
	}
	return initPlan{dir: dir, scaffold: s, files: files}, 0, nil
}

// newOutputDir resolves --out to a directory that does not exist yet under
// a parent that does. The parent's symlinks are resolved once, so the
// directory created is the one reported.
func newOutputDir(out string) (string, error) {
	abs, err := filepath.Abs(out)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("--out parent must already exist: %w", err)
	}
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", fmt.Errorf("--out parent %s is not a directory", parent)
	}
	dir := filepath.Join(parent, filepath.Base(abs))
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("--out %s already exists or cannot be checked; choose a new directory", dir)
	}
	return dir, nil
}

func writeInit(r *runner, plan initPlan, write func(string, []byte) error) error {
	s := plan.scaffold
	fmt.Fprintf(r.out, "gofastr-native init: %s (%s), package %s, module %s\n", s.Name, s.ID, s.Package, s.Module)
	if s.Native.dir != "" {
		fmt.Fprintf(r.out, "gofastr-native: checkout %s through replace (%s)\n", filepath.ToSlash(s.Native.dir), s.Native.origin)
	} else {
		fmt.Fprintf(r.out, "gofastr-native: %s (%s); only published versions resolve\n", s.Native.version, s.Native.origin)
	}
	if err := r.do("create "+plan.dir, func() error { return writeScaffold(plan.dir, plan.files, write) }); err != nil {
		return err
	}
	for _, name := range sortedScaffoldFiles(plan.files) {
		fmt.Fprintf(r.out, "  %s\n", filepath.Join(plan.dir, filepath.FromSlash(name)))
	}
	return nil
}

// tidyInit resolves dependencies in the new module. A failure keeps the
// files, which are valid source, and says how to finish by hand.
func tidyInit(r *runner, plan initPlan, stderr io.Writer) int {
	tidy := *r
	tidy.dir = plan.dir
	tidy.env = append(append([]string(nil), r.env...), "GOWORK=off", "GOFLAGS=-mod=mod")
	if err := tidy.run("resolve dependencies (GOWORK=off GOFLAGS=-mod=mod)", "go", "mod", "tidy"); err != nil {
		fmt.Fprintf(stderr, "\n%v\nThe files are in place. Finish with:\n  cd %s\n  go mod tidy\n", err, plan.dir)
		if plan.scaffold.Native.version != "" {
			fmt.Fprintf(stderr, "If %s@%s is not published, use --native <checkout-dir> instead.\n", nativeModulePath, plan.scaffold.Native.version)
		} else {
			fmt.Fprintln(stderr, "Offline? go mod tidy also wants test-only dependencies of your dependencies. go mod tidy -e records what the module cache holds; the app still builds and tests, and a later online go mod tidy fills in the rest.")
		}
		return 1
	}
	r.step = tidy.step
	if r.dryRun {
		fmt.Fprintln(r.out, "\nNOT RUN: no directories, files, or commands besides read-only go queries (dry run)")
		return 0
	}
	query := *r
	query.dir = plan.dir
	if work, err := query.quietOutput("go", "env", "GOWORK"); err == nil {
		if work = strings.TrimSpace(work); work != "" && work != "off" {
			fmt.Fprintf(r.out, "\nwarning: %s sits under the workspace %s. Add it there with go work use, or run commands with GOWORK=off.\n", plan.dir, work)
		}
	}
	pkg := plan.scaffold.Package
	fmt.Fprintf(r.out, "\nCreated %s. Next:\n  cd %s\n  go test ./...\n  go tool gofastr-native doctor --platform android-emu\n  go tool gofastr-native dev --platform android-emu --renderer native --pkg ./%s%s\n", plan.scaffold.Name, plan.dir, pkg, plan.scaffold.identityFlags())
	return 0
}
