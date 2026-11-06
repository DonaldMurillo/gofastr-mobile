package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

const extensionUsage = `Usage:
  gofastr-native extension check --pkg <Go package> [--dry-run]
  gofastr-native extension init --name <namespace/v1> --out <new-directory> [--kind file|component] [--dry-run]

check reads the compiled iOS dependency graph and bounded manifests/assets.
It does not execute package init, compile Swift/JS, or prove per-app registration.
init creates a library package inside an existing Go module, not an app template.
The Go package is adapter; factory and asset filenames are fixed, not name-derived.
`

func extensionCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, extensionUsage)
		return 0
	}
	flags := flag.NewFlagSet("extension "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, extensionUsage) }
	dry := flags.Bool("dry-run", false, "print steps without commands or writes")
	var pkg, name, directory, kind string
	switch args[0] {
	case "check":
		flags.StringVar(&pkg, "pkg", "", "compiled Go package")
	case "init":
		flags.StringVar(&name, "name", "", "versioned extension identity")
		flags.StringVar(&directory, "out", "", "new library directory")
		flags.StringVar(&kind, "kind", "file", "file or component")
	default:
		fmt.Fprintf(stderr, "unknown extension command %q\n%s", args[0], extensionUsage)
		return 2
	}
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected extension arguments:", flags.Args())
		return 2
	}
	runner := &runner{ctx: ctx, out: stdout, dryRun: *dry}
	if args[0] == "init" {
		if err := scaffoldExtension(runner, name, kind, directory); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if pkg == "" || strings.HasPrefix(pkg, "-") {
		fmt.Fprintln(stderr, "extension check requires --pkg <Go package>, not a go list flag")
		return 2
	}
	definitions, err := discoverExtensions(runner, pkg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *dry {
		fmt.Fprintln(stdout, "NOT RUN: manifest, asset, identity, capability and factory checks (dry run; no commands or writes)")
		return 0
	}
	printExtensionReport(stdout, definitions)
	return 0
}

func printExtensionReport(output io.Writer, definitions []compiledExtension) {
	for _, definition := range definitions {
		manifest := definition.manifest
		kind := manifest.Kind
		if kind == "" {
			kind = extension.FileKind
		}
		fmt.Fprintf(output, "extension %s kind=%s\n", manifest.Name, kind)
		fmt.Fprintf(output, "  manifest %s/%s\n  factory %s\n", definition.directory, extension.ManifestName, manifest.IOS.Factory)
		capabilities := append([]string(nil), manifest.Capabilities...)
		sort.Strings(capabilities)
		for _, capability := range capabilities {
			fmt.Fprintf(output, "  capability %s\n", capability)
		}
		for _, source := range manifest.IOS.Sources {
			fmt.Fprintf(output, "  swift %s\n", source)
		}
		fmt.Fprintf(output, "  webview %s\n", manifest.WebView.Script)
	}
	fmt.Fprintf(output, "PASS: %d compiled extensions; manifests/assets valid, no identity/capability/factory collisions\n", len(definitions))
	fmt.Fprintln(output, "NOT CHECKED: adapter behavior, runtime availability, static webassets publication and per-app selection; build/test the consuming app")
}
