package ios

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFilesWireAndBoundedAccess(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Swift Foundation checks require the macOS SDK")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("the Swift toolchain is not installed")
	}
	directory := t.TempDir()
	arguments := []string{"--sdk", "macosx", "swiftc", "-parse-as-library", "-module-cache-path", filepath.Join(directory, "module-cache")}
	for _, name := range []string{"GofastrComponents.swift", "GofastrFiles.swift", "GofastrPresentation.swift", "GofastrActions.swift", "GofastrImages.swift", "GofastrAttachments.swift", "GofastrServices.swift"} {
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
		t.Fatal("could not locate the real binding-independent wire DTOs")
	}
	checksPath := filepath.Join(directory, "FilesChecks.swift")
	checks := "import Foundation\n" + string(native[start:end]) + filesChecks
	if err := os.WriteFile(checksPath, []byte(checks), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "files-checks")
	arguments = append(arguments, checksPath, "-o", executable)
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("file checks do not compile: %v\n%s", err, output)
	}
	if output, err := exec.Command(executable).CombinedOutput(); err != nil {
		t.Fatalf("file wire, bounded I/O, or result lifecycle regression: %v\n%s", err, output)
	}
}

func TestFilesForegroundAndSettlementContract(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		content, err := Sources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}
	native := read("GofastrNative.swift")
	for _, required := range []string{
		`"files/v1": "available"`, "let files: MobileFiles?", "let fileId: String?",
		"let foreground = MobileForegroundCoordinator()", "guard !busy, !host.foreground.isActive",
		`["kind": kind, "instance": instance, "revision": bufferRevision, "values": values]`,
		"let chained = reply.response?.presentation != nil || reply.response?.files != nil",
		"if !editor.dirty && response.presentation == nil && response.files == nil",
		"fileID: reply.fileId", "self.finishForeground(foregroundID, restoreFocus: restoreFocus, preserveFocus: chained)",
	} {
		if !strings.Contains(native, required) {
			t.Errorf("missing file foreground/wire contract: %s", required)
		}
	}
	start := strings.Index(native, "private func fileResult(")
	end := strings.Index(native, "private func finishForeground(")
	if start < 0 || end <= start {
		t.Fatal("missing file result submission boundary")
	}
	submission := native[start:end]
	for _, required := range []string{
		"let request = UUID().uuidString", `send("file-result", extra: ["requestId": request, "fileId": identifier, "fileResult": result.wire])`,
		"self?.complete(request, foregroundID: identifier)",
	} {
		if !strings.Contains(submission, required) {
			t.Errorf("file results must write once then settle: %s", required)
		}
	}
	if strings.Count(submission, `send("file-result"`) != 1 || strings.Contains(submission, "self?.fileResult(") || strings.Contains(submission, "self.fileResult(") {
		t.Error("file-result writes must never retry")
	}
	controller := read("GofastrFileController.swift")
	for _, required := range []string{
		"UIDocumentPickerViewController(forOpeningContentTypes:", "asCopy: true", "UIDocumentPickerViewController(forExporting: [url], asCopy: true)",
		"func documentPickerWasCancelled(", "queue.async", "MobileFileAccess.read(url, pick: pick)",
		"MobileForegroundDismissal.dismiss(controller)", "guard state.report() else { return }", "cleanExport()",
	} {
		if !strings.Contains(controller, required) {
			t.Errorf("missing picker lifecycle boundary: %s", required)
		}
	}
	if strings.Contains(native, "files.id") || strings.Contains(native, "fileId: files.id") {
		t.Error("the declared file continuation id is not a runtime task identity")
	}
	presentation := read("GofastrPresentationController.swift")
	if strings.Count(presentation, "if !(controller is UIAlertController) { controller.presentationController?.delegate = self }") != 2 {
		t.Error("do not reintroduce the system alert presentationController.delegate trap")
	}
	foreground := read("GofastrForeground.swift")
	if strings.Count(foreground, "guard active == nil, !identifier.isEmpty") != 3 || !strings.Contains(foreground, "private var active: MobileForegroundTask?") {
		t.Error("file tasks, presentations, and device services must reserve the same foreground slot")
	}
}

const filesChecks = `
private final class BackgroundResult<Value> {
    var result: Result<Value, Error>?
}

@main
enum FilesChecks {
    static func background<Value>(_ work: @escaping () throws -> Value) throws -> Value {
        let state = BackgroundResult<Value>()
        let finished = DispatchSemaphore(value: 0)
        DispatchQueue(label: "files.checks").async {
            state.result = Result { try work() }
            finished.signal()
        }
        finished.wait()
        return try state.result!.get()
    }

    static func expectFailure(_ status: MobileFileStatus, _ work: () throws -> Void) {
        do { try work(); preconditionFailure("operation unexpectedly succeeded") }
        catch { precondition(MobileFileAccess.status(for: error) == status) }
    }

    static func main() throws {
        let decoder = JSONDecoder()
        let withFile = try decoder.decode(MobileReply.self, from: Data(#"{"version":1,"screen":{"route":"/notes/1","title":"Note","text":"Content","attachments":[{"id":"opaque","name":"brief.txt","mediaType":"text/plain","size":18,"destination":"/notes/1/files/opaque"}]}}"#.utf8))
        precondition(withFile.screen?.attachments?.first?.name == "brief.txt")
        precondition(withFile.screen?.attachments?.first?.detail == "File · 18 B")
        precondition(withFile.screen?.filePreview == nil)
        let preview = try decoder.decode(MobileReply.self, from: Data(##"{"version":1,"screen":{"route":"/notes/1/files/opaque","title":"Attachment","filePreview":{"file":{"id":"opaque","name":"Thoughts.md","mediaType":"text/markdown","size":18,"destination":"/notes/1/files/opaque","detail":"Markdown · 18 B","icon":"file"},"caption":"Private attachment","previewTitle":"Text preview","text":"# Thoughts"}}}"##.utf8))
        precondition(preview.screen?.filePreview?.file.name == "Thoughts.md")
        precondition(preview.screen?.filePreview?.file.detail == "Markdown · 18 B")
        precondition(preview.screen?.filePreview?.text == "# Thoughts")
        let actions = try decoder.decode([MobileAction].self, from: Data(#"[{"id":"save","label":"Done","primary":true},{"id":"share","label":"Share note","icon":"share"},{"id":"export","label":"Export"},{"id":"attachments","label":"Attachments"}]"#.utf8))
        let toolbar = MobileToolbarPlan(actions)
        precondition(toolbar.primary?.id == "save", "Done must never be hidden in automatic overflow")
        precondition(toolbar.overflow.map(\.id) == ["share", "export", "attachments"])
        let filesActions = try decoder.decode([MobileAction].self, from: Data(#"[{"id":"attach","label":"Add file","primary":true},{"id":"photo","label":"Choose photo"},{"id":"camera","label":"Take photo"}]"#.utf8))
        let filesToolbar = MobileToolbarPlan(filesActions)
        precondition(filesToolbar.primary?.id == "attach")
        precondition(filesToolbar.overflow.map(\.id) == ["photo", "camera"])
        let fallback = MobileToolbarPlan(Array(actions.dropFirst()))
        precondition(fallback.primary?.id == "share" && fallback.overflow.map(\.id) == ["export", "attachments"])
        precondition(MobileToolbarPlan([]).primary == nil)
        let old = try decoder.decode(MobileReply.self, from: Data(#"{"version":1,"response":{"version":1}}"#.utf8))
        precondition(old.fileId == nil && old.presentationId == nil && old.response?.files == nil)
        let pickJSON = #"{"id":"app-continuation","pick":{"title":"Import","extensions":["txt",".MD"],"maxBytes":1048576}}"#
        let request = try decoder.decode(MobileFiles.self, from: Data(pickJSON.utf8))
        precondition(request.isValid && request.export == nil)
        let normalizedExtensions = try MobileFileAccess.extensions(request.pick!)
        precondition(normalizedExtensions == ["txt", "md"])
        let replyJSON = #"{"version":1,"fileId":"opaque-runtime-task","response":{"version":1,"files":${files}}}"#.replacingOccurrences(of: "${files}", with: pickJSON)
        let reply = try decoder.decode(MobileReply.self, from: Data(replyJSON.utf8))
        precondition(reply.fileId == "opaque-runtime-task" && reply.fileId != reply.response?.files?.id)
        for json in [
            #"{"id":"none"}"#,
            #"{"id":"both","pick":{"title":"Import","maxBytes":1},"export":{"name":"a","mediaType":"text/plain","data":""}}"#,
            #"{"id":"oversized","pick":{"title":"Import","maxBytes":1048577}}"#,
            #"{"id":"negative","pick":{"title":"Import","maxBytes":-1}}"#,
            #"{"id":"","pick":{"title":"Import","maxBytes":1}}"#
        ] {
            let invalid = try decoder.decode(MobileFiles.self, from: Data(json.utf8))
            precondition(!invalid.isValid)
        }
        let export = try decoder.decode(MobileFiles.self, from: Data(#"{"id":"export","export":{"name":"report.txt","mediaType":"text/plain","data":"aGVsbG8="}}"#.utf8))
        precondition(export.isValid && export.pick == nil)
        precondition(MobileFileResult(status: .completed).wire["file"] == nil)
        let unexpected = MobileFilePayload(name: "ignored", mediaType: "text/plain", data: "")
        for status in [MobileFileStatus.cancelled, .denied, .unsupported, .failed] {
            let wire = MobileFileResult(status: status, file: unexpected).wire
            precondition(wire.count == 1 && wire["status"] as? String == status.rawValue)
        }

        var state = MobileFileTaskState()
        precondition(!state.receive() && state.present() && !state.present())
        precondition(state.receive() && !state.receive())
        precondition(state.report() && !state.report())
        state.finish()
        precondition(!state.receive() && !state.report() && !state.present())
        var preparation = MobileFileTaskState()
        precondition(preparation.report() && !preparation.report())
        var abandoned = MobileFileTaskState()
        abandoned.finish()
        precondition(!abandoned.report() && !abandoned.present())

        precondition(MobileFileAccess.sanitizedName("../../report.txt") == "report.txt")
        precondition(MobileFileAccess.sanitizedName(#"C:\private\report.txt"#) == "report.txt")
        precondition(MobileFileAccess.sanitizedName("bad\u{0000}:name.txt") == "badname.txt")
        for name in ["", ".", "..", "/"] { precondition(MobileFileAccess.sanitizedName(name) == "document") }
        precondition(MobileFileAccess.sanitizedName(String(repeating: "é", count: 300)).utf8.count <= 200)
        for suffix in ["", ".", "../txt", "bad/name", "bad\\name", "bad:extension", "bad\u{0000}"] {
            expectFailure(.failed) { _ = try MobileFileAccess.extensions(MobileFilePick(title: "", extensions: [suffix], maxBytes: 1)) }
        }
        let defaults = MobileFilePick(title: "Import", extensions: nil, maxBytes: MobileFileAccess.maximumBytes)
        let defaultTypes = try MobileFileAccess.contentTypes(defaults)
        precondition(!defaultTypes.isEmpty)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false)
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("Chosen.TXT")
        let bytes = Data(repeating: 65, count: MobileFileAccess.maximumBytes)
        try bytes.write(to: url)
        let picked = try background { try MobileFileAccess.read(url, pick: request.pick!) }
        precondition(picked.name == "Chosen.TXT" && !picked.mediaType.isEmpty)
        precondition(Data(base64Encoded: picked.data) == bytes)
        let wire = MobileFileResult(status: .completed, file: picked).wire
        let encoded = try JSONSerialization.data(withJSONObject: wire)
        precondition(!String(decoding: encoded, as: UTF8.self).contains(directory.path))
        let payload = wire["file"] as! [String: String]
        precondition(Set(payload.keys) == Set(["name", "mediaType", "data"]))
        try (bytes + Data([66])).write(to: url)
        expectFailure(.failed) { _ = try background { try MobileFileAccess.read(url, pick: defaults) } }
        try Data([65, 66]).write(to: url)
        expectFailure(.failed) { _ = try background { try MobileFileAccess.read(url, pick: MobileFilePick(title: "", extensions: nil, maxBytes: 1)) } }
        expectFailure(.unsupported) { _ = try background { try MobileFileAccess.read(url, pick: MobileFilePick(title: "", extensions: ["md"], maxBytes: 100)) } }
        try Data().write(to: url)
        let empty = try background { try MobileFileAccess.read(url, pick: MobileFilePick(title: "", extensions: [], maxBytes: 0)) }
        precondition(empty.data.isEmpty)
        let link = directory.appendingPathComponent("link.txt")
        try FileManager.default.createSymbolicLink(at: link, withDestinationURL: url)
        expectFailure(.unsupported) { _ = try background { try MobileFileAccess.read(link, pick: defaults) } }
        expectFailure(.unsupported) { _ = try background { try MobileFileAccess.read(directory, pick: defaults) } }
        expectFailure(.failed) { _ = try background { try MobileFileAccess.read(URL(string: "https://example.invalid/file")!, pick: defaults) } }

        let exported = try background { try MobileFileAccess.prepareExport(MobileFileExport(name: "../../report.txt", mediaType: "text/plain", data: bytes.base64EncodedString())) }
        defer { try? FileManager.default.removeItem(at: exported.deletingLastPathComponent()) }
        precondition(exported.lastPathComponent == "report.txt")
        precondition(exported.deletingLastPathComponent().lastPathComponent.hasPrefix("gofastr-file-export-"))
        let exportedBytes = try Data(contentsOf: exported)
        precondition(exportedBytes == bytes)
        for data in ["%%%", (bytes + Data([66])).base64EncodedString(), String(repeating: "A", count: 1_398_108)] {
            expectFailure(.failed) { _ = try background { try MobileFileAccess.prepareExport(MobileFileExport(name: "report.txt", mediaType: "text/plain", data: data)) } }
        }
        precondition(MobileFileAccess.status(for: NSError(domain: NSCocoaErrorDomain, code: CocoaError.fileReadNoPermission.rawValue)) == .denied)
        precondition(MobileFileAccess.status(for: NSError(domain: NSPOSIXErrorDomain, code: 13)) == .denied)
        precondition(MobileFileAccess.status(for: NSError(domain: "provider", code: 1, userInfo: [NSUnderlyingErrorKey: NSError(domain: NSPOSIXErrorDomain, code: 1)])) == .denied)
    }
}
`
