package ios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestComponentsWireRoutesAndState(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("executable Swift Foundation checks require macOS")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("the Swift toolchain is not installed")
	}
	directory := t.TempDir()
	arguments := []string{"--sdk", "macosx", "swiftc", "-O", "-parse-as-library", "-module-cache-path", filepath.Join(directory, "module-cache")}
	for _, name := range []string{"GofastrComponents.swift", "GofastrComponentExtensions.swift", "GofastrActions.swift", "GofastrFiles.swift", "GofastrPresentation.swift", "GofastrImages.swift", "GofastrAttachments.swift", "GofastrServices.swift"} {
		source, err := Sources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, source, 0o600); err != nil {
			t.Fatal(err)
		}
		arguments = append(arguments, path)
	}
	native, err := Sources.ReadFile("GofastrNative.swift")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(native), "struct MobileDraft:")
	end := strings.Index(string(native), "final class MobileHost {")
	if start < 0 || end <= start {
		t.Fatal("missing actual renderer DTO boundary")
	}
	checks := filepath.Join(directory, "ComponentsChecks.swift")
	if err := os.WriteFile(checks, []byte("import Foundation\n"+string(native[start:end])+componentChecks), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "component-checks")
	arguments = append(arguments, checks, "-o", executable)
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("component checks do not compile: %v\n%s", err, output)
	}
	if output, err := exec.Command(executable).CombinedOutput(); err != nil {
		t.Fatalf("wire, selection, date, route, or metadata regression: %v\n%s", err, output)
	}
}

func TestComponentsUIKitOptimizedCompiles(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("optimized UIKit compilation requires Xcode on macOS")
	}
	sdk, err := exec.Command("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		t.Skipf("no iOS simulator SDK: %v", err)
	}
	architecture := runtime.GOARCH
	if architecture == "amd64" {
		architecture = "x86_64"
	}
	directory := t.TempDir()
	arguments := []string{"--sdk", "iphonesimulator", "swiftc", "-O", "-parse-as-library", "-emit-library", "-module-name", "GofastrComponentsChecks",
		"-emit-module", "-emit-module-path", filepath.Join(directory, "GofastrComponentsChecks.swiftmodule"),
		"-target", architecture + "-apple-ios16.0-simulator", "-sdk", strings.TrimSpace(string(sdk)), "-module-cache-path", filepath.Join(directory, "module-cache")}
	entries, err := Sources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".swift") {
			continue
		}
		source, err := Sources.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, entry.Name())
		if err := os.WriteFile(path, source, 0o600); err != nil {
			t.Fatal(err)
		}
		arguments = append(arguments, path)
	}
	arguments = append(arguments, "-o", filepath.Join(directory, "libGofastrComponentsChecks.dylib"))
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("binding-independent optimized UIKit library does not compile/link: %v\n%s", err, output)
	}
	client := filepath.Join(directory, "PublicServiceClient.swift")
	if err := os.WriteFile(client, []byte(`
import UIKit
import GofastrComponentsChecks
func usePublicServiceRunner(_ presenter: UIViewController, route: @escaping (String) -> Bool, request: String, completion: @escaping (String) -> Void) {
    let runner = GofastrServiceRunner(presenter: presenter, openRoute: route)
    let capabilities: [String: String] = GofastrServiceRunner.capabilities()
    precondition(!capabilities.isEmpty)
    runner.perform(requestJSON: request, completion: completion)
    runner.cancel()
}
func usePublicRendererLifecycle(_ renderer: GofastrNativeRenderer, completion: @escaping (Bool) -> Void) {
    renderer.checkpoint()
    renderer.checkpoint(completion: completion)
    renderer.suspend()
    renderer.resume()
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	clientArguments := []string{"--sdk", "iphonesimulator", "swiftc", "-typecheck", "-parse-as-library", "-I", directory,
		"-target", architecture + "-apple-ios16.0-simulator", "-sdk", strings.TrimSpace(string(sdk)), "-module-cache-path", filepath.Join(directory, "module-cache"), client}
	if output, err := exec.Command("xcrun", clientArguments...).CombinedOutput(); err != nil {
		t.Fatalf("public runner interface cannot be reused by a separate hosted/native module: %v\n%s", err, output)
	}
}

func TestComponentsUIKitContracts(t *testing.T) {
	checks := map[string][]string{
		"GofastrNative.swift": {
			`public func openURL(_ url: URL) -> Bool`, `public func checkpoint()`, `MobileLocalRoute.normalize(url)`, `host.queue.sync`, `self.host.backend.render(route, "{}")`,
			`host.open(screen.route, values: queryValues)`, `host.render(screen.route, values: queryValues)`, `refreshedValues(queryValues, authoritative: model.values)`, `selection.wire`, `selection.reset()`, `selection.reconcile(model.list?.rows ?? [])`,
			`listQuery[paging.key] = next`, `listQuery[paging.key] = ""`, `"native.load-more"`, `"native.select.\(row.id)"`,
			`extraValues: ["row": identifier]`, `leadingSwipeActionsConfigurationForRowAt`, `trailingSwipeActionsConfigurationForRowAt`,
			`configuration.performsFirstActionWithFullSwipe = declared.first?.allowsFullSwipe == true`, `showFieldErrors(editor?.fields ?? error.fields ?? [:])`,
			`if field.disabled == true { result[field.key] = field.value }`, `text.markedTextRange == nil`, `current == self.bufferRevision`, `navigationStillSafe`,
			`"native.list-empty"`, `max(1, $0.rows.count)`, `rows.indices.contains(indexPath.row)`, `sizeListFooter()`, `table?.backgroundView = nil`,
		},
		"GofastrFieldControl.swift": {
			`UISwitch()`, `UIDatePicker()`, `select.menu = UIMenu(children: options)`, `option.disabled == true ? [.disabled] : []`,
			`toggle.isOn ? "true" : "false"`, `"native.field.\(field.key)"`, `"native.row-action.\(row.id).\(action.id)"`, `preferredFont(forTextStyle: .body)`,
		},
		"GofastrNavigation.swift": {
			`UITabBarController`, `UINavigationController(rootViewController: placeholder)`, `"native.tab.\(tab.id)"`,
			`screen.prepareForNavigation`, `openRestorationRoute`, `self.host.open(route, completion: completion)`, `self.navigations[index].tabBarItem.isEnabled = false`,
			`MobileNavigationMetadata.maximumDepth`, `MobileNavigationMetadata.maximumTabs`, `hasUncertainEditor`, `MobileRestorationSafety.canOpen`, `view.isUserInteractionEnabled = false`,
			`"compose": "square.and.pencil"`, `"share": "square.and.arrow.up"`, `UIImage(systemName: "square.grid.2x2")`,
		},
	}
	for name, required := range checks {
		source, err := Sources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range required {
			if !strings.Contains(string(source), token) {
				t.Errorf("%s lost component/lifecycle contract: %s", name, token)
			}
		}
	}
}

const componentChecks = `
@main
enum ComponentChecks {
    static func main() throws {
        let decoder = JSONDecoder()
        let oldField = try decoder.decode(MobileField.self, from: Data(#"{"key":"body","label":"Body","value":"keep IME","kind":"multiline"}"#.utf8))
        precondition(oldField.options == nil && oldField.disabled == nil && oldField.required == nil)
        let field = try decoder.decode(MobileField.self, from: Data(#"{"key":"tag","label":"Tag","value":"work","kind":"select","required":true,"disabled":false,"options":[{"value":"work","label":"Work"},{"value":"locked","label":"Locked","disabled":true}]}"#.utf8))
        precondition(field.required == true && field.disabled == false)
        precondition(field.options?.first?.value == "work" && field.options?.last?.disabled == true)
        for kind in ["text", "multiline", "toggle", "select", "date", "time"] {
            let encoded = #"{"key":"field","label":"Field","value":"","kind":"KIND"}"#.replacingOccurrences(of: "KIND", with: kind)
            let decoded = try decoder.decode(MobileField.self, from: Data(encoded.utf8))
            precondition(decoded.kind == kind)
        }

        let oldRow = try decoder.decode(MobileRow.self, from: Data(#"{"id":"record-a","title":"A","destination":"/notes/a"}"#.utf8))
        let row = try decoder.decode(MobileRow.self, from: Data(#"{"id":"record-b","title":"B","destination":"/notes/b","disabled":false,"leadingActions":[{"id":"pin","label":"Pin","fullSwipe":true}],"trailingActions":[{"id":"trash","label":"Trash","role":"destructive","fullSwipe":true}]}"#.utf8))
        let locked = try decoder.decode(MobileRow.self, from: Data(#"{"id":"locked","title":"Locked","destination":"","disabled":true}"#.utf8))
        precondition(oldRow.disabled == nil && oldRow.actions.isEmpty)
        precondition(row.leadingActions?.first?.allowsFullSwipe == true)
        precondition(row.trailingActions?.first?.allowsFullSwipe == false, "destructive full swipe remains prohibited even for bad input")
        let selection = try decoder.decode(MobileListSelection.self, from: Data(#"{"key":"picked","selected":["record-b","record-b"],"maximum":2}"#.utf8))
        var state = MobileListSelectionState(selection)
        precondition(state.selected == ["record-b"] && state.wire == #"["record-b"]"#)
        precondition(!state.toggle(locked, maximum: 2))
        precondition(state.toggle(oldRow, maximum: 2))
        precondition(state.wire == #"["record-a","record-b"]"#)
        state.reconcile([row, oldRow])
        precondition(state.wire == #"["record-a","record-b"]"#, "stable IDs survive row reorder and pending snapshots")
        let third = try decoder.decode(MobileRow.self, from: Data(#"{"id":"third","title":"Third","destination":""}"#.utf8))
        precondition(!state.toggle(third, maximum: 2))
        precondition(state.toggle(row, maximum: 2))
        precondition(state.toggle(third, maximum: 0))
        state.reconcile([third])
        precondition(state.selected == ["third"], "deleted IDs must not be resubmitted")
        state.reset()
        precondition(state.wire == "[]")
        precondition(MobileListSelectionState(nil).selected.isEmpty)

        let screen = try decoder.decode(MobileScreen.self, from: Data(#"{"route":"/notes","title":"Notes","activeTab":"notes","tabs":[{"id":"notes","label":"Notes","route":"/notes","icon":"notes"},{"id":"files","label":"Files","route":"/files"}],"list":{"rows":[],"filters":[{"key":"archived","label":"Archived","kind":"toggle","value":"false"}],"selection":{"key":"selected","selected":[],"maximum":10},"paging":{"key":"cursor","next":"next-token","label":"More notes"}}}"#.utf8))
        precondition(screen.tabs?.count == 2 && screen.activeTab == "notes")
        precondition(screen.list?.filters?.first?.kind == "toggle")
        precondition(screen.list?.paging?.next == "next-token")
        let list = try decoder.decode(MobileList.self, from: Data(#"{"rows":[],"searchKey":"q","filters":[{"key":"archived","label":"Archived","kind":"toggle","value":"false"}],"selection":{"key":"selected"},"paging":{"key":"cursor"}}"#.utf8))
        let staleValues = ["q": "edited locally", "archived": "true", "cursor": "page-2", "selected": #"["deleted-record"]"#, "row": "deleted-record", "undo": "old-token"]
        let query = list.queryValues(staleValues)
        precondition(query == ["q": "edited locally", "archived": "true", "cursor": "page-2"], "read refresh must not submit deleted selection, row action context, or stale undo metadata")
        let refreshed = list.refreshedValues(query, authoritative: ["q": "edited locally", "undo": "fresh-token", "selected": "[]"])
        precondition(refreshed["undo"] == "fresh-token" && refreshed["row"] == nil)
        precondition(refreshed["cursor"] == "page-2" && refreshed["archived"] == "true", "legacy partial values must not reset live queries or accumulated pages")
        let withoutUndo = list.refreshedValues(refreshed, authoritative: ["selected": "[]"])
        precondition(withoutUndo["undo"] == nil && withoutUndo["selected"] == "[]", "removed authoritative action metadata must disappear")
        precondition(list.refreshedValues(query, authoritative: ["cursor": "canonical-page"])["cursor"] == "canonical-page")
        let oldScreen = try decoder.decode(MobileScreen.self, from: Data(#"{"route":"/notes","title":"Notes","list":{"rows":[]}}"#.utf8))
        precondition(oldScreen.tabs == nil && oldScreen.activeTab == nil && oldScreen.list?.filters == nil)
        let oldResponse = try decoder.decode(MobileResponse.self, from: Data(#"{"version":1}"#.utf8))
        precondition(oldResponse.screen == nil && oldResponse.files == nil)

        for value in ["2024-02-29", "2026-11-05", "2000-01-01"] {
            let date = MobileFieldDate.parse(value, kind: "date")!
            precondition(MobileFieldDate.formatter("date").string(from: date) == value)
        }
        for value in ["", "2026-02-29", "2026-13-01", "2026-1-1", "2026-11-05T12:00:00Z"] {
            precondition(MobileFieldDate.parse(value, kind: "date") == nil)
        }
        for value in ["00:00", "09:05", "23:59"] {
            let date = MobileFieldDate.parse(value, kind: "time")!
            precondition(MobileFieldDate.formatter("time").string(from: date) == value)
        }
        for value in ["", "24:00", "12:60", "9:05", "12:05:00"] {
            precondition(MobileFieldDate.parse(value, kind: "time") == nil)
        }

        for (url, expected) in [
            ("notesapp://notes/new?tag=work", "/notes/new?tag=work"),
            ("notesapp://files", "/files"),
            ("/notes?term=hello%20world", "/notes?term=hello%20world"),
            ("/", "/")
        ] { precondition(MobileLocalRoute.normalize(URL(string: url)!) == expected, url) }
        for url in ["https://notes/new", "http://notes", "file:///notes", "mailto:a@example.com", "notesapp:/notes", "notesapp://user@notes/new", "notesapp://notes:22/new", "notesapp://notes/new#effect", "//external/notes", "notes", "notesapp://notes/../files", "notesapp://notes/%2e%2e/files", "notesapp://notes/%2Ffiles", "/notes?value=%0a"] {
            precondition(MobileLocalRoute.normalize(URL(string: url)!) == nil, url)
        }
        for route in ["/notes/%", "/notes/%xz", "/notes//new", "/notes/./new", "/notes/../new", "/notes/\\new", "/notes\n", "/notes/%3fnew", String(repeating: "/", count: 2050)] {
            precondition(MobileLocalRoute.validate(route) == nil, route)
        }
        let tab = screen.tabs!.first!
        let metadata = MobileNavigationMetadata(version: 1, activeTab: "notes", stacks: ["notes": ["/notes", "/notes/a"]])
        precondition(metadata.routes(for: tab) == ["/notes", "/notes/a"])
        let encoded = try JSONEncoder().encode(metadata)
        let object = try JSONSerialization.jsonObject(with: encoded) as! [String: Any]
        precondition(Set(object.keys) == ["version", "activeTab", "stacks"], "persist only route metadata, never fields, effects, requests, or pending writes")
        let decodedMetadata = try decoder.decode(MobileNavigationMetadata.self, from: encoded)
        precondition(decodedMetadata.routes(for: tab) == ["/notes", "/notes/a"])
        for routes in [["/files", "/notes/a"], Array(repeating: "/notes", count: 7)] {
            precondition(MobileNavigationMetadata(version: 1, activeTab: "notes", stacks: ["notes": routes]).routes(for: tab) == ["/notes"])
        }
        precondition(MobileNavigationMetadata(version: 2, activeTab: "notes", stacks: metadata.stacks).routes(for: tab) == ["/notes"])
        precondition(MobileNavigationMetadata(version: 1, activeTab: "notes", stacks: ["notes": ["/notes", "https://bad", "/notes/b"]]).routes(for: tab) == ["/notes"])
        precondition(MobileNavigationMetadata(version: 1, activeTab: "notes", stacks: ["notes": ["/notes", "/notes/a", "/notes/a"]]).routes(for: tab) == ["/notes", "/notes/a"])
    }
}
`
