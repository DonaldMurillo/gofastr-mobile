package ios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrustedOriginAndLocalRoutePolicy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Swift execution requires macOS")
	}
	compiler, err := exec.LookPath("swiftc")
	if err != nil {
		t.Skip("Swift compiler is unavailable")
	}
	source, err := Sources.ReadFile("GofastrServiceOrigin.swift")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	main := filepath.Join(directory, "main.swift")
	harness := `
let policy = GofastrServiceOrigin(url: URL(string: "gofastr://app")!)
let current = URL(string: "gofastr://app/notes")!
precondition(policy.permits(mainFrame: true, scheme: "gofastr", host: "app", port: 0, current: current))
precondition(!policy.permits(mainFrame: false, scheme: "gofastr", host: "app", port: 0, current: current))
precondition(!policy.permits(mainFrame: true, scheme: "https", host: "app", port: 0, current: current))
precondition(!policy.permits(mainFrame: true, scheme: "gofastr", host: "other", port: 0, current: current))
precondition(!policy.permits(mainFrame: true, scheme: "gofastr", host: "app", port: 443, current: current))
precondition(!policy.permits(mainFrame: true, scheme: "gofastr", host: "app", port: 0, current: nil))
precondition(!policy.permits(mainFrame: true, scheme: "gofastr", host: "app", port: 0, current: URL(string: "https://app/notes")!))
precondition(!policy.permits(mainFrame: true, scheme: "gofastr", host: "app", port: 0, current: URL(string: "gofastr://app:444/notes")!))
precondition(policy.destination("/notes/123?view=active")?.host == "app")
for route in ["", "notes", "//example.com/notes", "https://example.com/notes", "/notes\\escape", "/notes\n", String(repeating: "/", count: 2049)] {
    precondition(policy.destination(route) == nil, "admitted unsafe route")
}
print("trusted service origin and local route policy passed")
`
	if err := os.WriteFile(main, append(source, []byte(harness)...), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "policy")
	if output, err := exec.Command(compiler, "-O", main, "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("policy: %v\n%s", err, output)
	}
}

func TestWebViewServiceAdapterTypechecksWithoutBindings(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("WebKit SDK requires Xcode on macOS")
	}
	sdk, err := exec.Command("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		t.Skipf("iOS simulator SDK unavailable: %v", err)
	}
	architecture := "arm64"
	if runtime.GOARCH == "amd64" {
		architecture = "x86_64"
	}
	directory := t.TempDir()
	stub := filepath.Join(directory, "Runner.swift")
	if err := os.WriteFile(stub, []byte(`import UIKit
public final class GofastrServiceRunner {
    public init(presenter: UIViewController, openRoute: @escaping (String) -> Bool) {}
    public static func capabilities() -> [String: String] { [:] }
    public func perform(requestJSON: String, completion: @escaping (String) -> Void) {}
    public func cancel() {}
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--sdk", "iphonesimulator", "swiftc", "-parse-as-library", "-typecheck", "-target", architecture + "-apple-ios16.0-simulator", "-sdk", strings.TrimSpace(string(sdk)), stub}
	entries, err := Sources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		source, err := Sources.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(directory, entry.Name())
		if err := os.WriteFile(destination, source, 0o600); err != nil {
			t.Fatal(err)
		}
		args = append(args, destination)
	}
	if output, err := exec.Command("xcrun", args...).CombinedOutput(); err != nil {
		t.Fatalf("WebView service adapter: %v\n%s", err, output)
	}
}
