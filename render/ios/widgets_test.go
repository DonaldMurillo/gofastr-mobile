package ios

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/widgets"
)

func TestWidgetSwiftContract(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("executable Swift checks require macOS")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("Swift toolchain unavailable")
	}
	directory := t.TempDir()
	source, err := Sources.ReadFile("GofastrWidgets.swift")
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(directory, "GofastrWidgets.swift")
	if err := os.WriteFile(sourcePath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal([]widgets.Request{
		{Operation: widgets.Publish, Snapshot: &widgets.Snapshot{Title: "Recent notes", Text: "Published by Go", Route: "/notes?filter=recent%20notes", Items: []widgets.Item{{ID: "1", Title: "Shopping", Subtitle: "Milk\nBread"}}}},
		{Operation: widgets.Clear}, {Operation: widgets.Status},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(directory, "widget-wire.json")
	if err := os.WriteFile(fixturePath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	checkPath := filepath.Join(directory, "WidgetChecks.swift")
	if err := os.WriteFile(checkPath, []byte(widgetSwiftChecks), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "widget-checks")
	if output, err := exec.Command("xcrun", "--sdk", "macosx", "swiftc", "-O", "-parse-as-library", "-module-cache-path", filepath.Join(directory, "cache"), sourcePath, checkPath, "-o", executable).CombinedOutput(); err != nil {
		t.Fatalf("compile Widget DTOs: %v\n%s", err, output)
	}
	if output, err := exec.Command(executable, fixturePath).CombinedOutput(); err != nil {
		t.Fatalf("execute Widget DTOs: %v\n%s", err, output)
	}
}

const widgetSwiftChecks = `
import Foundation
@main enum WidgetChecks {
    static func main() throws {
        let data = try Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1]))
        let requests = try JSONDecoder().decode([MobileWidgetRequest].self, from: data)
        precondition(requests.count == 3)
        for request in requests { try request.validate() }
        precondition(requests[0].snapshot?.items?.first?.subtitle == "Milk\nBread")
        for route in ["https://evil.test", "//evil.test", "/notes/../private", "/%2e%2e/private", "/notes%2fprivate", "/notes%0aother", "/notes?q=%0d", "/notes%5cprivate", "/notes#secret", "/notes?bad=%", "/white space"] {
            precondition(!MobileWidgetSnapshot.localRoute(route), route)
        }
        for route in ["/", "/notes/1", "/notes?query=hello%20world"] { precondition(MobileWidgetSnapshot.localRoute(route), route) }
        let invalid = MobileWidgetSnapshot(title: "Notes", text: nil, route: "/notes", items: [MobileWidgetItem(id: "1", title: "One", subtitle: nil), MobileWidgetItem(id: "1", title: "Two", subtitle: nil)])
        do { try invalid.validate(); fatalError("accepted duplicate widget items") } catch MobileWidgetFailure.invalid { }
        let state = try JSONEncoder().encode(MobileWidgetState(published: false))
        precondition(String(data: state, encoding: .utf8) == "{\"published\":false}")
    }
}
`
