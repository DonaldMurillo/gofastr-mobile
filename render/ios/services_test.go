package ios

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/audio"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/notifications"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestServicesSwiftWireBoundsAndOneShotLifecycle(t *testing.T) {
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
		t.Fatal("missing actual renderer DTOs")
	}
	checks := filepath.Join(directory, "ServiceChecks.swift")
	if err := os.WriteFile(checks, []byte("import Foundation\n"+string(native[start:end])+serviceChecks), 0o600); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(directory, "go-wire.json")
	content := make([]byte, 48)
	copy(content, "RIFF")
	binary.LittleEndian.PutUint32(content[4:], uint32(len(content)-8))
	copy(content[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(content[16:], 16)
	binary.LittleEndian.PutUint16(content[20:], 1)
	binary.LittleEndian.PutUint16(content[22:], 1)
	binary.LittleEndian.PutUint32(content[24:], 16000)
	binary.LittleEndian.PutUint32(content[28:], 32000)
	binary.LittleEndian.PutUint16(content[32:], 2)
	binary.LittleEndian.PutUint16(content[34:], 16)
	copy(content[36:], "data")
	binary.LittleEndian.PutUint32(content[40:], 4)
	file := files.Document{Name: "recording.wav", MediaType: "audio/wav", Data: content}
	fixture := map[string]any{
		"requests": []nativeui.ServiceRequest{
			{ID: "copy", Clipboard: &sharing.Clipboard{Text: "explicit copy"}},
			{ID: "share", Share: &sharing.Request{Title: "Files", Text: "Share these", Files: []files.Document{file}}},
			{ID: "reminder", Notification: &notifications.Request{Operation: notifications.Schedule, Identifier: "notes:1", Title: "Reminder", Route: "/notes/1", Date: "2027-03-14", Time: "03:30"}},
			{ID: "record", Audio: &audio.Request{Title: "Voice note"}, Commit: true},
		},
		"audio":  audio.Document{File: file, DurationMillis: 1},
		"result": nativeui.ServiceResult{Status: nativeui.EffectCompleted, File: &file, DurationMillis: 1},
		"reply":  map[string]any{"version": 1, "instance": "instance", "revision": 0, "serviceId": "opaque-service", "dataVersion": "epoch:2", "changed": true},
	}
	encoded, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "service-checks")
	arguments = append(arguments, checks, "-o", executable)
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("service Foundation checks do not compile: %v\n%s", err, output)
	}
	resultPath := filepath.Join(directory, "swift-result.json")
	if output, err := exec.Command(executable, fixturePath, resultPath).CombinedOutput(); err != nil {
		t.Fatalf("service DTO, local calendar, bounds, cancellation, or stale read regression: %v\n%s", err, output)
	}
	encoded, err = os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var result nativeui.ServiceResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != nativeui.EffectCompleted || result.File == nil {
		t.Fatalf("Swift result is not valid nativeui wire: %s", encoded)
	}
	if err := (audio.Request{Title: "Voice note"}).Accept(*result.File, result.DurationMillis); err != nil {
		t.Fatalf("Swift audio result must pass the actual Go validator: %v", err)
	}
}

func TestServiceRunnerAndVisibleLiveContracts(t *testing.T) {
	checks := map[string][]string{
		"GofastrServiceRunner.swift": {
			"public final class GofastrServiceRunner", "public init(presenter: UIViewController, openRoute: @escaping (String) -> Bool)",
			"public static func capabilities() -> [String: String]", "public func perform(requestJSON: String, completion: @escaping (String) -> Void)", "public func cancel()",
			"NSMicrophoneUsageDescription", "UNUserNotificationCenterDelegate", "UNNotificationDefaultActionIdentifier", "self.pending = route", "completionHandler([.banner, .sound])",
			"request.scheduledDate()", "requestAuthorization(options: [.alert, .sound])", "getPendingNotificationRequests", "removePendingNotificationRequests",
			"UIActivityViewController(activityItems: items", "completionWithItemsHandler", "removeItem(at: temporary)", "UIPasteboard.general.string = clipboard.text",
			"settlement.finish(token)", "queue.async", "DispatchQueue.main.async",
		},
		"GofastrAudio.swift": {
			"AVAudioRecorder", "requestRecordPermission", "recorder?.pause()", "record(forDuration:", "recorder.stop()", "configure(previewButton,", "configure(useButton,",
			"AVAudioSession.interruptionNotification", "UIApplication.didEnterBackgroundNotification", "MobileServiceBounds.read", "self.request.accept(file, durationMillis: duration)",
			"status: \"cancelled\"", "status: \"completed\"", "file: payload, durationMillis: durationMillis", "self.document.validatedBytes()", "player?.currentTime = Double(seek.value)",
		},
		"GofastrNative.swift": {
			"let service: MobileServiceRequest?", "let serviceId: String?", "let live: MobileLive?", "let dataVersion: String?", "let changed: Bool?",
			`send("service-result", extra: ["requestId": request, "serviceId": identifier, "serviceResult": result.wire])`,
			"serviceID: reply.serviceId", "observedServices.insert(serviceID).inserted", "reply.response?.service != nil", "response.service == nil",
			`"live/v1": "available"`, "GofastrServiceRunner.capabilities()", "liveVisible", "screen.form == nil, let list = screen.list", "watchGeneration.accepts(token)",
			`"kind": "watch"`, `"values": query, "dataVersion": version`, "self.instance == instance", "self.selection.reconcile(model.list?.rows ?? [])",
			"restoreListScroll(anchor)", "private func cancelListRead()", "private func cancelWatch()", "operation.cancel()", "watchWorkers", `"native.list.retry"`, `"native.list.cancel"`,
			"liveReadRecovery.recover(error.code)", "self.reloadList()", "liveReadRecovery.reset()",
		},
		"GofastrForeground.swift":  {"final class MobileServiceTask: MobileForegroundTask", "runner.perform(requestJSON: json)", "runner.cancel()", "focus.restore(source)"},
		"GofastrAttachments.swift": {"let audio: MobileAudioDocument?", "MobileAudioPreview(audio, identifier: file.id)", "players.forEach { $0.stop() }"},
	}
	for name, tokens := range checks {
		source, err := Sources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range tokens {
			if !strings.Contains(string(source), token) {
				t.Errorf("%s lost native services contract: %s", name, token)
			}
		}
	}
	runner, _ := Sources.ReadFile("GofastrServiceRunner.swift")
	if strings.Contains(string(runner), "UIPasteboard.general.string\n") || strings.Contains(string(runner), "import Go") {
		t.Fatal("runner must not read clipboard or import application bindings")
	}
	native, _ := Sources.ReadFile("GofastrNative.swift")
	start := strings.Index(string(native), "private func serviceResult(")
	if start < 0 {
		t.Fatal("missing one-shot service result boundary")
	}
	end := strings.Index(string(native)[start:], "private func finishForeground(") + start
	if end <= start {
		t.Fatal("missing one-shot service result boundary")
	}
	if strings.Count(string(native)[start:end], `send("service-result"`) != 1 || !strings.Contains(string(native)[start:end], "self?.complete(request, foregroundID: identifier)") {
		t.Fatal("service results must write once and recover through read-only completion")
	}
}

const serviceChecks = `
import AVFoundation

struct ServiceFixtures: Decodable {
    let requests: [MobileServiceRequest]
    let audio: MobileAudioDocument
    let result: MobileServiceResult
    let reply: MobileReply
}

@main
enum ServiceChecks {
    static func rejected(_ operation: () throws -> Void) {
        do { try operation(); preconditionFailure("expected rejected service input") } catch {}
    }

    static func background<T>(_ operation: @escaping () throws -> T) throws -> T {
        let semaphore = DispatchSemaphore(value: 0)
        var result: Result<T, Error>?
        DispatchQueue.global().async { result = Result(catching: operation); semaphore.signal() }
        semaphore.wait()
        return try result!.get()
    }

    static func notification(_ date: String, _ time: String) -> MobileNotificationRequest {
        MobileNotificationRequest(operation: "schedule", identifier: "notes:1", title: "Reminder", body: nil, route: "/notes/1", date: date, time: time)
    }

    static func main() throws {
        let decoder = JSONDecoder()
        let fixture = try decoder.decode(ServiceFixtures.self, from: Data(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1])))
        for request in fixture.requests { try request.validate() }
        precondition(fixture.requests[0].commit == nil, "false commit is omitted by canonical Go JSON")
        precondition(fixture.requests[3].commit == true)
        precondition(fixture.reply.serviceId == "opaque-service" && fixture.reply.dataVersion == "epoch:2" && fixture.reply.changed == true)
        let roundTrip = try decoder.decode(MobileServiceResult.self, from: Data(fixture.result.json.utf8))
        precondition(roundTrip.file == fixture.audio.file && roundTrip.durationMillis == 1)
        _ = try fixture.audio.validatedBytes()
        try Data(fixture.result.json.utf8).write(to: URL(fileURLWithPath: CommandLine.arguments[2]))
        let oldReply = try decoder.decode(MobileReply.self, from: Data(#"{"version":1,"revision":0}"#.utf8))
        precondition(oldReply.serviceId == nil && oldReply.changed == nil)
        let attachment = try decoder.decode(MobileAttachment.self, from: Data(#"{"id":"file","name":"voice","mediaType":"audio/wav","size":48,"destination":"/files/1"}"#.utf8))
        precondition(attachment.audio == nil)

        for json in [#"{"id":"bad"}"#, #"{"id":"bad","clipboard":{"text":"x"},"audio":{"title":"Record"}}"#, #"{"id":"","clipboard":{"text":"x"}}"#] {
            rejected { try decoder.decode(MobileServiceRequest.self, from: Data(json.utf8)).validate() }
        }
        let emptyClipboard = try decoder.decode(MobileServiceRequest.self, from: Data(#"{"id":"clear","clipboard":{"text":""}}"#.utf8))
        try emptyClipboard.validate()
        let hugeClipboard = try decoder.decode(MobileServiceRequest.self, from: JSONSerialization.data(withJSONObject: ["id":"copy", "clipboard":["text":String(repeating:"é", count:32769)]]))
        rejected { try hugeClipboard.validate() }

        let zone = TimeZone(identifier: "America/New_York")!
        let now = ISO8601DateFormatter().date(from: "2027-01-01T00:00:00Z")!
        let scheduled = try notification("2027-03-14", "03:30").scheduledDate(now: now, timeZone: zone)
        precondition(ISO8601DateFormatter().string(from: scheduled) == "2027-03-14T07:30:00Z")
        for pair in [("2027-03-14", "02:30"), ("2027-02-29", "10:00"), ("2027-04-31", "10:00"), ("2027-01-01", "24:00"), ("2027-1-01", "00:00"), ("2027-01-01", "0:00"), ("2026-12-31", "01:00"), ("2027-01-01", "00:60")] {
            rejected { _ = try notification(pair.0, pair.1).scheduledDate(now: now, timeZone: zone) }
        }
        rejected { _ = try notification("2027-03-14", "03:30").scheduledDate(now: scheduled, timeZone: zone) }
        _ = try notification("2027-11-07", "01:30").scheduledDate(now: now, timeZone: zone)
        for route in ["https://example.com", "//example.com/x", "/notes/../private", "/notes/%2e%2e/private", "/notes#code", "/notes\\private"] {
            precondition(!MobileNotificationRequest.localRoute(route))
        }

        let file = fixture.audio.file
        let oversized = MobileFileExport(name: "large.wav", mediaType: "audio/wav", data: Data(repeating: 0, count: 1_048_577).base64EncodedString())
        rejected { _ = try MobileServiceBounds.bytes(oversized) }
        rejected { _ = try MobileServiceBounds.bytes(MobileFileExport(name: "../../escape", mediaType: "text/plain", data: "")) }
        let duplicate = MobileFileExport(name: "recording.wav", mediaType: file.mediaType, data: file.data)
        let collision = MobileFileExport(name: "2-recording.wav", mediaType: file.mediaType, data: file.data)
        let share = MobileShareRequest(title: "Share", text: "Details", files: [file, duplicate, collision])
        let prepared = try background { try share.prepare() }
        defer { try? FileManager.default.removeItem(at: prepared.0) }
        precondition(prepared.1.count == 3 && Set(prepared.1).count == 3)
        for url in prepared.1 {
            precondition(url.deletingLastPathComponent() == prepared.0)
            let sharedBytes = try Data(contentsOf: url)
            precondition(sharedBytes == Data(base64Encoded: file.data))
        }
        rejected { try MobileShareRequest(title: nil, text: nil, files: Array(repeating: file, count: 5)).validate() }
        let half = MobileFileExport(name: "half.txt", mediaType: "text/plain", data: Data(repeating: 1, count: 524289).base64EncodedString())
        rejected { try MobileShareRequest(title: nil, text: nil, files: [half, half]).validate() }
        rejected { try MobileShareRequest(title: nil, text: nil, files: []).validate() }
        let maximumFile = MobileFileExport(name: "maximum.bin", mediaType: "application/octet-stream", data: Data(repeating: 255, count: 1_048_576).base64EncodedString())
        let maximumShare = MobileServiceRequest(id: "maximum", notification: nil, share: MobileShareRequest(title: "Share", text: String(repeating: "\u{1}", count: 65_536), files: [maximumFile]), clipboard: nil, audio: nil, commit: nil)
        try maximumShare.validate()
        let maximumJSON = try JSONEncoder().encode(maximumShare)
        precondition(maximumJSON.count <= MobileServiceBounds.maximumJSON, "the bounded JSON envelope must allow escaped slashes and legal 64 KiB text plus a full 1 MiB file")

        let audio = MobileAudioRequest(title: "Voice", maxSeconds: 0, maxBytes: 0)
        precondition(audio.seconds == 60 && audio.bytes == 1_048_576)
        try audio.accept(file, durationMillis: 1)
        for duration in [0, -1, 60001] { rejected { try audio.accept(file, durationMillis: duration) } }
        for seconds in [-1, 61] { rejected { try MobileAudioRequest(title: "Voice", maxSeconds: seconds, maxBytes: nil).validate() } }
        for maximum in [-1, 1048577] { rejected { try MobileAudioRequest(title: "Voice", maxSeconds: nil, maxBytes: maximum).validate() } }
        rejected { try MobileAudioRequest(title: "Voice", maxSeconds: 1, maxBytes: 2).accept(file, durationMillis: 1) }
        rejected { try audio.accept(MobileFileExport(name: "fake.m4a", mediaType: "audio/mp4", data: Data("0000ftypM4A ".utf8).base64EncodedString()), durationMillis: 1) }
        var wav = Data(base64Encoded: file.data)!
        wav[4] = 0
        precondition(!MobileAudioRequest.hasAudioSignature(wav, mediaType: "audio/wav"))
        precondition(!MobileAudioRequest.hasAudioSignature(Data(base64Encoded: file.data)!, mediaType: "audio/ogg"))
        rejected { try audio.accept(MobileFileExport(name: "empty.wav", mediaType: "audio/wav", data: ""), durationMillis: 1) }
        let local = prepared.0.appendingPathComponent("bounded")
        try Data(repeating: 1, count: 33).write(to: local)
        rejected { _ = try background { try MobileServiceBounds.read(local, maximum: 32) } }
        let boundedBytes = try background { try MobileServiceBounds.read(local, maximum: 33) }
        precondition(boundedBytes.count == 33)

        let aacURL = prepared.0.appendingPathComponent("encoded.m4a")
        let encodedAudio = try background { () throws -> MobileFileExport in
            let format = AVAudioFormat(standardFormatWithSampleRate: 16000, channels: 1)!
            let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: 1600)!
            buffer.frameLength = 1600
            for frame in 0..<1600 { buffer.floatChannelData![0][frame] = 0 }
            do {
                let output = try AVAudioFile(forWriting: aacURL, settings: [AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: 16000, AVNumberOfChannelsKey: 1, AVEncoderBitRateKey: 32000], commonFormat: .pcmFormatFloat32, interleaved: false)
                try output.write(from: buffer)
            }
            let bytes = try MobileServiceBounds.read(aacURL, maximum: 1_048_576)
            let file = MobileFileExport(name: "encoded.m4a", mediaType: "audio/mp4", data: bytes.base64EncodedString())
            try audio.accept(file, durationMillis: 100)
            let player = try AVAudioPlayer(data: bytes)
            precondition(player.duration.isFinite && player.duration > 0 && player.duration <= 1)
            return file
        }
        let encodedResult = MobileServiceResult(status: "completed", permission: "granted", file: encodedAudio, durationMillis: 100)
        try Data(encodedResult.json.utf8).write(to: URL(fileURLWithPath: CommandLine.arguments[2]))

        for status in ["cancelled", "denied", "unsupported", "failed"] {
            let result = MobileServiceResult(status: status, message: "line\nbreak" + String(repeating:"x", count:1024), file: file, durationMillis: 1)
            precondition(result.wire["file"] == nil && result.wire["durationMillis"] == nil)
            let message = result.wire["message"] as! String
            precondition(message.utf8.count <= 512 && !message.contains("\n"))
        }
        var settlement = MobileServiceSettlement()
        let first = settlement.begin()!
        precondition(settlement.begin() == nil)
        precondition(settlement.finish(first))
        precondition(!settlement.finish(first), "permission/share/record callbacks cannot settle twice")
        let second = settlement.begin()!
        precondition(!settlement.finish(first) && settlement.active == second)
        precondition(settlement.finish(second))
        var read = MobileReadGeneration()
        let old = read.begin()
        read.cancel()
        precondition(!read.accepts(old))
        let fresh = read.begin()
        precondition(read.accepts(fresh) && !read.accepts(old))
        var recovery = MobileReadRecovery()
        for code in ["forbidden", "unknown", "internal", "invalid_request", "pending", "cancelled"] {
            precondition(!recovery.recover(code), "Only obsolete read sessions can recover automatically")
        }
        precondition(recovery.recover("stale"))
        for code in ["pending", "cancelled"] {
            recovery.reset()
            precondition(recovery.retry(code))
            precondition(recovery.retry(code))
            precondition(recovery.retry(code))
            precondition(!recovery.retry(code), "Transient watch retries must be bounded")
        }
        precondition(!recovery.retry("forbidden"))
        recovery.reset()
        precondition(recovery.recover("stale"))
        precondition(!recovery.recover("expired"), "Recovery must not loop without a successful watch or explicit retry")
        recovery.reset()
        precondition(recovery.recover("expired"))
        precondition(!recovery.recover("stale"))
        recovery.reset()
        precondition(recovery.recover("stale"))
        let list = try decoder.decode(MobileList.self, from: Data(#"{"rows":[{"id":"kept","title":"Kept","destination":"/notes/kept"}],"selection":{"key":"selected","selected":["kept","deleted"],"maximum":10}}"#.utf8))
        var selection = MobileListSelectionState(list.selection)
        selection.reconcile(list.rows)
        precondition(selection.selected == ["kept"])
        let live = try decoder.decode(MobileScreen.self, from: Data(#"{"route":"/notes","title":"Notes","list":{"rows":[]},"live":{"version":"epoch:1"}}"#.utf8))
        precondition(live.live?.version == "epoch:1" && live.form == nil)
    }
}
`
