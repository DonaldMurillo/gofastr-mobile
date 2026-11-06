package ios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenderingComponentsSwiftLifecycleAndEvents(test *testing.T) {
	if runtime.GOOS != "darwin" {
		test.Skip("executable Swift Foundation checks require macOS")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		test.Skip("the Swift toolchain is not installed")
	}
	directory := test.TempDir()
	arguments := []string{"--sdk", "macosx", "swiftc", "-O", "-parse-as-library", "-module-cache-path", filepath.Join(directory, "module-cache")}
	for _, name := range []string{"GofastrComponents.swift", "GofastrComponentExtensions.swift", "GofastrActions.swift", "GofastrFiles.swift", "GofastrPresentation.swift", "GofastrImages.swift", "GofastrAttachments.swift", "GofastrServices.swift", "GofastrIncomingShares.swift", "GofastrSecureStorage.swift", "GofastrWidgets.swift"} {
		source, err := Sources.ReadFile(name)
		if err != nil {
			test.Fatal(err)
		}
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, source, 0o600); err != nil {
			test.Fatal(err)
		}
		arguments = append(arguments, path)
	}
	media, err := os.ReadFile("../../extensions/media/GofastrMedia.swift")
	if err != nil {
		test.Fatal(err)
	}
	mediaPath := filepath.Join(directory, "GofastrMedia.swift")
	if err := os.WriteFile(mediaPath, media, 0o600); err != nil {
		test.Fatal(err)
	}
	native, err := Sources.ReadFile("GofastrNative.swift")
	if err != nil {
		test.Fatal(err)
	}
	start := strings.Index(string(native), "struct MobileDraft:")
	end := strings.Index(string(native), "final class MobileHost {")
	if start < 0 || end <= start {
		test.Fatal("missing actual renderer wire DTOs")
	}
	checks := filepath.Join(directory, "RenderingChecks.swift")
	if err := os.WriteFile(checks, []byte("import Foundation\n"+string(native[start:end])+renderingComponentChecks), 0o600); err != nil {
		test.Fatal(err)
	}
	executable := filepath.Join(directory, "rendering-checks")
	arguments = append(arguments, mediaPath, checks, "-o", executable)
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		test.Fatalf("rendering component executable does not compile: %v\n%s", err, output)
	}
	if output, err := exec.Command(executable).CombinedOutput(); err != nil {
		test.Fatalf("component bounds, keyed retention, event allowlist, or disposal regression: %v\n%s", err, output)
	}
}

func TestRenderingComponentsMediaUIKitOptimizedCompiles(test *testing.T) {
	if runtime.GOOS != "darwin" {
		test.Skip("UIKit compilation requires Xcode on macOS")
	}
	sdk, err := exec.Command("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		test.Skipf("no iOS simulator SDK: %v", err)
	}
	architecture := runtime.GOARCH
	if architecture == "amd64" {
		architecture = "x86_64"
	}
	directory := test.TempDir()
	arguments := []string{"--sdk", "iphonesimulator", "swiftc", "-O", "-parse-as-library", "-emit-library", "-module-name", "GofastrMediaChecks",
		"-target", architecture + "-apple-ios16.0-simulator", "-sdk", strings.TrimSpace(string(sdk)), "-module-cache-path", filepath.Join(directory, "module-cache")}
	entries, err := Sources.ReadDir(".")
	if err != nil {
		test.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".swift") {
			continue
		}
		source, err := Sources.ReadFile(entry.Name())
		if err != nil {
			test.Fatal(err)
		}
		path := filepath.Join(directory, entry.Name())
		if err := os.WriteFile(path, source, 0o600); err != nil {
			test.Fatal(err)
		}
		arguments = append(arguments, path)
	}
	media, err := os.ReadFile("../../extensions/media/GofastrMedia.swift")
	if err != nil {
		test.Fatal(err)
	}
	mediaPath := filepath.Join(directory, "GofastrMedia.swift")
	if err := os.WriteFile(mediaPath, media, 0o600); err != nil {
		test.Fatal(err)
	}
	catalog := filepath.Join(directory, "ComponentCatalog.swift")
	if err := os.WriteFile(catalog, []byte(`
import UIKit
func installGeneratedMediaCatalog() {
    precondition(GofastrMediaComponent.name == "gofastr.media/v1")
    precondition(GofastrMediaComponent.capabilities() == ["gofastr.media/v1": "available"])
    MobileComponentExtensions.install([GofastrMediaComponent.self])
    MobileFileExtensions.install([])
}
`), 0o600); err != nil {
		test.Fatal(err)
	}
	arguments = append(arguments, mediaPath, catalog, "-o", filepath.Join(directory, "libGofastrMediaChecks.dylib"))
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		test.Fatalf("optimized media-enabled UIKit library/catalog must compile and link: %v\n%s", err, output)
	}
}

func TestRenderingComponentsHostContracts(test *testing.T) {
	checks := map[string][]string{
		"GofastrNative.swift": {
			`var components: [MobileComponent]?`, `MobileComponentExtensions.capabilities()`, `componentViews?.activate(screen.components ?? [])`,
			`self.screen.components = model.components`, `self.componentViews?.reconcile(model.components ?? [])`,
			`componentViews?.dispose()`, `let destination = event.destination`, `components?.contains(event) == true`,
			`!self.navigationBlocked, stillValid()`, `self.openDestination(destination)`,
		},
		"GofastrComponentExtensions.swift": {
			`protocol MobileComponentExtensionFactory`, `protocol MobileComponentExtensionView`, `static func install(`,
			`func update(payload: String, emit: @escaping (String) -> Void) throws`, `try component.update(payload: model.payload, emit: emit)`,
			`factory.capabilities()[component.name] == "available"`, `entry.generation == generation`,
			`entry.component.destinations[event] != nil`, `guard active else { return }`, `component.dispose()`,
		},
		"GofastrImages.swift":      {`MobileKeyedResources<MobileImageControl>`, `if preview?.data != next.data`, `let action = UIAction { _ in open?(destination) }`},
		"GofastrAttachments.swift": {`MobileKeyedResources<MobileAttachmentItem>`, `MobileRetainedResource<MobileAudioDocument, MobileAudioPreview>`, `playback.update(file.audio)`},
	}
	for name, tokens := range checks {
		source, err := Sources.ReadFile(name)
		if err != nil {
			test.Fatal(err)
		}
		for _, token := range tokens {
			if !strings.Contains(string(source), token) {
				test.Errorf("%s lost rendering component contract: %s", name, token)
			}
		}
	}
	media, err := os.ReadFile("../../extensions/media/GofastrMedia.swift")
	if err != nil {
		test.Fatal(err)
	}
	for _, required := range []string{`func update(payload: String, emit: @escaping (String) -> Void) throws`, `guard self?.generation == current`, `gallery.open = callback`, `attachments.open = callback`} {
		if !strings.Contains(string(media), required) {
			test.Errorf("media library lost retained update or queued-callback fencing: %s", required)
		}
	}
	for _, forbidden := range []string{"pushViewController", "openDestination", "UIApplication.shared.open", "host.", "notes/", "send(\""} {
		if strings.Contains(string(media), forbidden) {
			test.Errorf("media library bypasses generic navigation or owns app-specific behavior: %s", forbidden)
		}
	}
}

const renderingComponentChecks = `
final class RenderingProbe {
    var emit: (String) -> Void
    var disposals = 0
    var updates = 0
    var playing = true
    init(_ emit: @escaping (String) -> Void) { self.emit = emit }
    func update(_ emit: @escaping (String) -> Void) { updates += 1; self.emit = emit }
    func dispose() { disposals += 1; playing = false; emit("image.0") }
}

@main struct RenderingChecks {
    static func component(_ id: String, payload: String = "{}", name: String = "gofastr.media/v1", destinations: [String: String] = ["image.0": "/allowed"]) throws -> MobileComponent {
        let bytes = try JSONSerialization.data(withJSONObject: ["id": id, "name": name, "payload": payload, "destinations": destinations])
        return try JSONDecoder().decode(MobileComponent.self, from: bytes)
    }

    static func rejects(_ body: () throws -> Void) {
        do { try body() } catch { return }
        preconditionFailure("invalid component was accepted")
    }

    static func main() throws {
        let legacy = try JSONDecoder().decode(MobileScreen.self, from: Data(#"{"route":"/legacy","title":"Legacy"}"#.utf8))
        precondition(legacy.components == nil)
        let screenData = try JSONSerialization.data(withJSONObject: ["route": "/editor", "title": "Editor", "components": [["id": "media", "name": "gofastr.media/v1", "payload": "{}"]]])
        let screen = try JSONDecoder().decode(MobileScreen.self, from: screenData)
        precondition(screen.components?.first?.destinations == [:] && screen.images == nil && screen.attachments == nil)
        let emptyMedia = try screen.components!.first!.decodePayload(GofastrMediaPayload.self)
        precondition(emptyMedia.images == nil)

        let limit = MobileComponent.maximumPayloadBytes
        _ = try component("max", payload: "\"" + String(repeating: "a", count: limit - 2) + "\"")
        rejects { _ = try component("large", payload: "\"" + String(repeating: "a", count: limit - 1) + "\"") }
        rejects { _ = try component("utf8", payload: "\"" + String(repeating: "é", count: limit / 2) + "\"") }
        rejects { _ = try component("empty", payload: "") }
        rejects { _ = try component("broken", payload: "{bad") }
        rejects { _ = try component("") }
        rejects { _ = try component("missing", name: "") }
        rejects { _ = try component("routes", destinations: ["image.0": ""]) }
        rejects { _ = try component("routes", destinations: ["image.0": String(repeating: "x", count: 4097)]) }
        rejects { _ = try component("routes", destinations: Dictionary(uniqueKeysWithValues: (0..<129).map { ("event.\($0)", "/allowed") })) }
        rejects { _ = try JSONDecoder().decode(MobileComponent.self, from: Data(#"{"id":"x","name":"gofastr.media/v1","payload":{}}"#.utf8)) }
        rejects { _ = try GofastrMediaPayload.decode(String(repeating: " ", count: limit + 1)) }

        var created: [RenderingProbe] = []
        var events: [MobileComponentEvent] = []
        let reconciler = MobileComponentReconciler<RenderingProbe>(make: { _, emit in
            let probe = RenderingProbe(emit)
            created.append(probe)
            return probe
        }, update: { probe, _, emit in probe.update(emit) }, dispose: { $0.dispose() }, emit: { events.append($0) })
        let first = try component("first")
        let second = try component("second")
        try reconciler.reconcile([first, second])
        let original = reconciler.resources[0]
        original.emit("https://untrusted.invalid")
        original.emit("file.99")
        precondition(events.isEmpty, "unknown keys and direct routes must be ignored")
        original.emit("image.0")
        let checkpointEvent = events.last!
        precondition(checkpointEvent.destination == "/allowed" && reconciler.contains(checkpointEvent))
        try reconciler.reconcile([second, first])
        precondition(created.count == 2 && reconciler.resources[1] === original && original.playing)
        try reconciler.reconcile([second, first])
        precondition(original.disposals == 0 && reconciler.contains(checkpointEvent))
        let queuedBeforeRemap = original.emit
        let remapped = try component("first", destinations: ["image.0": "/updated"])
        try reconciler.reconcile([remapped, second])
        precondition(created.count == 2 && reconciler.resources[0] === original && original.playing)
        precondition(!reconciler.contains(checkpointEvent), "an in-flight checkpoint must not use an outdated route map")
        let countBeforeRemapCallback = events.count
        queuedBeforeRemap("image.0")
        precondition(events.count == countBeforeRemapCallback, "queued callbacks from the old route map are revoked")
        original.emit("image.0")
        precondition(events.last!.destination == "/updated")
        let queuedBeforeUpdate = original.emit
        let countBeforeUpdate = events.count
        try reconciler.reconcile([try component("first", payload: #"{"changed":true}"#), second])
        precondition(original.disposals == 0 && original.playing && created.count == 2)
        precondition(reconciler.resources[0] === original && original.updates == 2 && created[1].updates == 0)
        queuedBeforeUpdate("image.0")
        precondition(events.count == countBeforeUpdate, "payload updates revoke old queued emitters without replacing the resource")
        original.emit("image.0")
        precondition(events.count == countBeforeUpdate + 1 && events.last!.destination == "/allowed")
        let countBeforeRemove = events.count
        try reconciler.reconcile([second])
        precondition(original.disposals == 1 && events.count == countBeforeRemove)
        original.emit("image.0")
        precondition(events.count == countBeforeRemove, "removed callbacks cannot navigate")
        rejects { _ = try reconciler.reconcile([second, second]) }
        precondition(reconciler.resources.count == 1 && reconciler.resources[0] === created[1])
        created[1].emit("image.0")
        let beforeDisappearance = events.last!
        reconciler.disposeAll()
        reconciler.disposeAll()
        precondition(created.allSatisfy { $0.disposals == 1 } && reconciler.resources.isEmpty)
        try reconciler.reconcile([second])
        precondition(!reconciler.contains(beforeDisappearance), "reappearing identical content must not revive prior callbacks")
        let last = reconciler.resources[0]
        let countBeforeFactoryReplace = events.count
        try reconciler.reconcile([try component("second", name: "other.component/v1")])
        precondition(last.disposals == 1 && events.count == countBeforeFactoryReplace)
        reconciler.disposeAll()

        var released: RenderingProbe?
        do {
            let temporary = MobileComponentReconciler<RenderingProbe>(make: { _, emit in
                let probe = RenderingProbe(emit); released = probe; return probe
            }, update: { probe, _, emit in probe.update(emit) }, dispose: { $0.dispose() }, emit: { _ in preconditionFailure("retired event escaped") })
            try temporary.reconcile([first])
        }
        precondition(released?.disposals == 1)
        let failing = MobileComponentReconciler<RenderingProbe>(make: { _, _ in throw MobileComponentFailure.unavailable("absent") }, update: { _, _, _ in }, dispose: { $0.dispose() }, emit: { _ in })
        let unavailable = try failing.reconcile([first])
        precondition(unavailable.count == 1 && unavailable[0].id == first.id && failing.resources.isEmpty)
        failing.disposeAll()

        var updateResources: [RenderingProbe] = []
        var updateEvents = 0
        let updateFailure = MobileComponentReconciler<RenderingProbe>(make: { _, emit in
            let probe = RenderingProbe(emit); updateResources.append(probe); return probe
        }, update: { probe, model, emit in
            if model.payload == #"{"fail":true}"# { throw MobileComponentFailure.invalidPayload }
            probe.update(emit)
        }, dispose: { $0.dispose() }, emit: { _ in updateEvents += 1 })
        try updateFailure.reconcile([first, second])
        let failed = updateResources[0]
        let queuedBeforeFailure = failed.emit
        let issues = try updateFailure.reconcile([try component("first", payload: #"{"fail":true}"#), second])
        precondition(issues.count == 1 && failed.disposals == 1 && updateFailure.resources[0] === updateResources[1])
        precondition(updateResources[1].playing && updateResources[1].disposals == 0)
        queuedBeforeFailure("image.0")
        precondition(updateEvents == 0, "failed update events are revoked before disposal")
        try updateFailure.reconcile([first, second])
        precondition(updateResources.count == 3 && updateResources[1].disposals == 0)
        updateFailure.disposeAll()

        let children = MobileKeyedResources<RenderingProbe>(dispose: { $0.dispose() })
        children.reconcile(["photo", "audio"]) { _ in RenderingProbe { _ in } }
        let photo = children["photo"]!
        let audio = children["audio"]!
        children.reconcile(["audio", "new-photo", "photo"]) { _ in RenderingProbe { _ in } }
        precondition(children.resources[0] === audio && children.resources[2] === photo && audio.playing && photo.disposals == 0)
        children.reconcile(["audio", "new-photo"]) { _ in RenderingProbe { _ in } }
        precondition(photo.disposals == 1 && audio.disposals == 0 && audio.playing)

        let audioState = MobileRetainedResource<MobileAudioDocument, RenderingProbe>(dispose: { $0.dispose() })
        let document = MobileAudioDocument(file: MobileFileExport(name: "one.wav", mediaType: "audio/wav", data: "AQ=="), durationMillis: 1)
        let firstPlayer = audioState.update(document) { _ in RenderingProbe { _ in } }!
        let unchangedPlayer = audioState.update(document) { _ in preconditionFailure("unchanged audio was recreated") }!
        precondition(unchangedPlayer === firstPlayer && firstPlayer.playing && firstPlayer.disposals == 0)
        let changedDocument = MobileAudioDocument(file: MobileFileExport(name: "one.wav", mediaType: "audio/wav", data: "Ag=="), durationMillis: 1)
        let replacementPlayer = audioState.update(changedDocument) { _ in RenderingProbe { _ in } }!
        precondition(firstPlayer.disposals == 1 && replacementPlayer !== firstPlayer)
        audioState.update(nil) { _ in preconditionFailure("removed audio was created") }
        precondition(replacementPlayer.disposals == 1 && audioState.resource == nil)

        let media = try GofastrMediaPayload.decode(#"{"images":[{"id":"photo","alt":"Photo","data":"AQ==","width":1,"height":1,"destination":"https://untrusted.invalid"},{"id":"photo2","alt":"Second","data":"AQ==","width":1,"height":1}],"attachments":[{"id":"file","name":"Private file","mediaType":"text/plain","size":1,"destination":"/payload-route"}],"preview":{"file":{"id":"preview","name":"Preview","mediaType":"text/plain","size":1,"destination":"/payload-preview"},"text":"Content"}}"#)
        precondition(media.eventImages.map { $0.destination! } == ["image.0", "image.1"])
        precondition(media.eventAttachments.map(\.destination) == ["file.0"])
        precondition(media.eventImages[0].id == "photo" && media.eventAttachments[0].id == "file")
        precondition(media.preview?.file.name == "Preview" && media.preview?.text == "Content")
        print("Rendering component wire bounds, keyed playback retention, event allowlist, and disposal checks passed")
    }
}
`
