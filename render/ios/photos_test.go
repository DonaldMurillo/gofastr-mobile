package ios

import (
	"bytes"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr-native/extensions/photos"
)

func photoTestSources(t *testing.T) fstest.MapFS {
	t.Helper()
	library := fstest.MapFS{}
	entries, err := Sources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		content, err := Sources.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		library[entry.Name()] = &fstest.MapFile{Data: content}
	}
	definition := photos.Definition()
	for _, name := range definition.Manifest.IOS.Sources {
		content, err := definition.Assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		library[name] = &fstest.MapFile{Data: content}
	}
	return library
}

func TestPhotosWireAndImageProcessing(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Swift Foundation/ImageIO checks require the macOS SDK")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("the Swift toolchain is not installed")
	}
	directory := t.TempDir()
	arguments := []string{"--sdk", "macosx", "swiftc", "-parse-as-library", "-module-cache-path", filepath.Join(directory, "module-cache")}
	for _, name := range []string{"GofastrFiles.swift", "GofastrPhotos.swift"} {
		source, err := photoTestSources(t).ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, source, 0o600); err != nil {
			t.Fatal(err)
		}
		arguments = append(arguments, path)
	}
	checksPath := filepath.Join(directory, "PhotosChecks.swift")
	if err := os.WriteFile(checksPath, []byte(photoChecks), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "photo-checks")
	arguments = append(arguments, checksPath, "-o", executable)
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("photo checks do not compile: %v\n%s", err, output)
	}
	fixturePath := filepath.Join(directory, "normalized.jpg")
	if output, err := exec.Command(executable, fixturePath).CombinedOutput(); err != nil {
		t.Fatalf("photo wire, original read, JPEG, orientation, metadata, or cancellation regression: %v\n%s", err, output)
	}
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(fixture))
	if err != nil || decoded.Bounds().Dx() != 72 || decoded.Bounds().Dy() != 36 || len(fixture) > 100_000 {
		t.Fatalf("Swift JPEG must fully decode within the Go handoff bounds: %v", err)
	}
}

func TestPhotosUIKitTypechecks(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("UIKit typechecking requires Xcode on macOS")
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
	arguments := []string{"--sdk", "iphonesimulator", "swiftc", "-parse-as-library", "-typecheck", "-module-name", "GofastrPhotoChecks",
		"-target", architecture + "-apple-ios16.0-simulator", "-sdk", strings.TrimSpace(string(sdk)), "-module-cache-path", filepath.Join(directory, "module-cache")}
	entries, err := photoTestSources(t).ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".swift") {
			continue
		}
		source, err := photoTestSources(t).ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, entry.Name())
		if err := os.WriteFile(path, source, 0o600); err != nil {
			t.Fatal(err)
		}
		arguments = append(arguments, path)
	}
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("the photo adapter must compile without Go bindings: %v\n%s", err, output)
	}
}

func TestPhotosAcquisitionContract(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		source, err := photoTestSources(t).ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		return string(source)
	}
	controller := read("GofastrPhotoController.swift")
	for _, required := range []string{
		"PHPickerViewController(configuration:", "configuration.filter = .images", "configuration.selectionLimit = 1",
		"loadFileRepresentation(forTypeIdentifier:", "MobilePhotoAccess.readOriginal(url, work: work)",
		"UIImagePickerController.isSourceTypeAvailable(.camera)", "NSCameraUsageDescription",
		"#if targetEnvironment(simulator)\n        return false\n        #else",
		"AVCaptureDevice.authorizationStatus(for: .video)", "AVCaptureDevice.requestAccess(for: .video)",
		"case .denied, .restricted:", "status: .unsupported", "status: .cancelled", "status: .failed",
		"state.receive()", "guard state.report() else { return }", "guard state.phase == .reading else { return }",
		"self.state.phase == .preparing", "work.cancel()", "progress?.cancel()", "provider = nil", "controller = nil",
		"MobileForegroundDismissal.dismiss(controller)", "focus.restore(source)", "queue.async", "DispatchQueue.main.async",
	} {
		if !strings.Contains(controller, required) {
			t.Errorf("missing photo acquisition boundary: %s", required)
		}
	}
	for _, forbidden := range []string{"loadObject(", "UIImageWriteToSavedPhotosAlbum", "PHPhotoLibrary", "requestAccess(for: .audio)", "Data(contentsOf:", "send(\"file-result\""} {
		if strings.Contains(controller, forbidden) {
			t.Errorf("photo controller must not bypass bounded acquisition/settlement: %s", forbidden)
		}
	}
	foreground := read("GofastrForeground.swift")
	for _, required := range []string{"private var active: MobileForegroundTask?", "if let operation = model.operation", "MobileFileExtensions.make(", "task = MobileFileTask(", "active = task"} {
		if !strings.Contains(foreground, required) {
			t.Errorf("photos must reuse the existing foreground factory: %s", required)
		}
	}
	native := read("GofastrNative.swift")
	start := strings.Index(native, "func connect(")
	end := strings.Index(native, "func open(")
	if start < 0 || end <= start {
		t.Fatal("missing hello boundary")
	}
	hello := native[start:end]
	for _, required := range []string{"MobileFileExtensions.capabilities()", "guard Thread.isMainThread else", "DispatchQueue.main.async"} {
		if !strings.Contains(hello, required) {
			t.Errorf("missing main-thread photo capability negotiation: %s", required)
		}
	}
	if strings.Contains(hello, "requestAccess") || strings.Contains(hello, "MobilePhotoPlatform") || strings.Index(hello, "guard Thread.isMainThread") > strings.Index(hello, "MobileFileExtensions.capabilities()") {
		t.Error("hello must check hardware/purpose on main without requesting permission")
	}
}

const photoChecks = `
import Foundation
import CoreGraphics
import ImageIO
import UniformTypeIdentifiers

private final class PhotoBackgroundResult<Value> {
    var result: Result<Value, Error>?
}

@main
enum PhotoChecks {
    static func background<Value>(_ body: @escaping () throws -> Value) throws -> Value {
        let semaphore = DispatchSemaphore(value: 0)
        let result = PhotoBackgroundResult<Value>()
        DispatchQueue.global(qos: .userInitiated).async {
            result.result = Result(catching: body)
            semaphore.signal()
        }
        semaphore.wait()
        return try result.result!.get()
    }

    static func expectFailure(_ status: MobileFileStatus, _ body: () throws -> Void) {
        do { try body(); preconditionFailure("unexpected photo success") }
        catch { precondition(MobilePhotoAccess.status(for: error) == status, "unexpected photo error: \(error)") }
    }

    static func fixture(noise: Bool = false) -> CGImage {
        let width = noise ? 960 : 96
        let height = noise ? 640 : 48
        let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                                space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        if noise {
            let pixels = context.data!.assumingMemoryBound(to: UInt8.self)
            var seed: UInt32 = 42
            for index in 0..<(width * height) {
                for channel in 0..<3 {
                    seed = seed &* 1664525 &+ 1013904223
                    pixels[index * 4 + channel] = UInt8(seed >> 24)
                }
                pixels[index * 4 + 3] = 255
            }
        } else {
            let colors: [[CGFloat]] = [[1, 0, 0, 1], [0, 1, 0, 1], [0, 0, 1, 1], [1, 1, 0, 1]]
            for index in 0..<4 {
                context.setFillColor(CGColor(colorSpace: CGColorSpace(name: CGColorSpace.sRGB)!, components: colors[index])!)
                context.fill(CGRect(x: (index % 2) * width / 2, y: (index / 2) * height / 2, width: width / 2, height: height / 2))
            }
        }
        return context.makeImage()!
    }

    static func encoded(_ image: CGImage, orientation: CGImagePropertyOrientation = .up) -> Data {
        let bytes = NSMutableData()
        let destination = CGImageDestinationCreateWithData(bytes, UTType.tiff.identifier as CFString, 1, nil)!
        CGImageDestinationAddImage(destination, image, [
            kCGImagePropertyOrientation: orientation.rawValue,
            kCGImagePropertyGPSDictionary: [kCGImagePropertyGPSLatitude: 37.4, kCGImagePropertyGPSLongitude: 122.1],
            kCGImagePropertyExifDictionary: [kCGImagePropertyExifUserComment: "private original comment"],
            kCGImagePropertyTIFFDictionary: [kCGImagePropertyTIFFMake: "private original device"]
        ] as CFDictionary)
        precondition(CGImageDestinationFinalize(destination))
        return bytes as Data
    }

    static func corners(_ image: CGImage) -> [Int] {
        let context = CGContext(data: nil, width: image.width, height: image.height, bitsPerComponent: 8, bytesPerRow: image.width * 4,
                                space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        context.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
        let bytes = context.data!.assumingMemoryBound(to: UInt8.self)
        return [(1, 1), (3, 1), (1, 3), (3, 3)].map { horizontal, vertical in
            let offset = ((vertical * image.height / 4) * image.width + horizontal * image.width / 4) * 4
            return (bytes[offset] > 127 ? 4 : 0) + (bytes[offset + 1] > 127 ? 2 : 0) + (bytes[offset + 2] > 127 ? 1 : 0)
        }
    }

    static func inspect(_ file: MobileFilePayload, photo: MobilePhoto) -> CGImage {
        precondition(file.mediaType == "image/jpeg" && ["jpg", "jpeg"].contains((file.name as NSString).pathExtension))
        let bytes = Data(base64Encoded: file.data)!
        precondition(bytes.count <= photo.maxBytes && bytes.prefix(2) == Data([255, 216]) && bytes.suffix(2) == Data([255, 217]))
        let source = CGImageSourceCreateWithData(bytes as CFData, nil)!
        precondition(CGImageSourceGetType(source) as String? == UTType.jpeg.identifier)
        let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil)! as NSDictionary
        precondition(properties[kCGImagePropertyGPSDictionary] == nil)
        let exif = properties[kCGImagePropertyExifDictionary] as? NSDictionary
        precondition(exif?[kCGImagePropertyExifUserComment] == nil)
        let tiff = properties[kCGImagePropertyTIFFDictionary] as? NSDictionary
        precondition(tiff?[kCGImagePropertyTIFFMake] == nil)
        precondition((properties[kCGImagePropertyOrientation] as? Int ?? 1) == 1)
        let image = CGImageSourceCreateImageAtIndex(source, 0, nil)!
        precondition(image.width > 0 && image.height > 0 && max(image.width, image.height) <= photo.maxDimension)
        return image
    }

    static func main() throws {
        let decoder = JSONDecoder()
        let photoJSON = #"{"id":"declared-continuation","photo":{"title":"Choose photo","source":"library","maxBytes":1048576,"maxDimension":2048}}"#
        let request = try decoder.decode(MobileFiles.self, from: Data(photoJSON.utf8))
        precondition(request.isValid && request.pick == nil && request.export == nil && request.photo?.source == .library)
        for oldJSON in [#"{"id":"old","pick":{"title":"Import","maxBytes":0}}"#, #"{"id":"old","export":{"name":"a.txt","mediaType":"text/plain","data":""}}"#] {
            let old = try decoder.decode(MobileFiles.self, from: Data(oldJSON.utf8))
            precondition(old.isValid && old.photo == nil)
        }
        for fields in [#""pick":{"title":"Import","maxBytes":1}"#, #""export":{"name":"x","mediaType":"text/plain","data":""}"#, #""pick":{"title":"Import","maxBytes":1},"export":{"name":"x","mediaType":"text/plain","data":""}"#] {
            let multiple = photoJSON.dropLast() + "," + fields + "}"
            let decoded = try decoder.decode(MobileFiles.self, from: Data(multiple.utf8))
            precondition(!decoded.isValid)
        }
        for invalidJSON in [#"{"id":"empty"}"#, photoJSON.replacingOccurrences(of: "1048576", with: "0"), photoJSON.replacingOccurrences(of: "1048576", with: "1048577"), photoJSON.replacingOccurrences(of: "2048", with: "0"), photoJSON.replacingOccurrences(of: "2048", with: "2049")] {
            let decoded = try decoder.decode(MobileFiles.self, from: Data(invalidJSON.utf8))
            precondition(!decoded.isValid)
        }
        for invalidJSON in [photoJSON.replacingOccurrences(of: "library", with: "unknown"), photoJSON.replacingOccurrences(of: "1048576", with: "1.5"), photoJSON.replacingOccurrences(of: #","maxDimension":2048"#, with: "")] {
            precondition(invalidJSON != photoJSON)
            precondition((try? decoder.decode(MobileFiles.self, from: Data(invalidJSON.utf8))) == nil)
        }
        let photo = MobilePhoto(title: "Choose", source: .library, maxBytes: 100_000, maxDimension: 72)
        let original = encoded(fixture())
        let originalSource = CGImageSourceCreateWithData(original as CFData, nil)!
        let originalProperties = CGImageSourceCopyPropertiesAtIndex(originalSource, 0, nil)! as NSDictionary
        precondition(originalProperties[kCGImagePropertyGPSDictionary] != nil && originalProperties[kCGImagePropertyExifDictionary] != nil)
        let file = try background { try MobilePhotoAccess.jpeg(original, photo: photo, work: MobilePhotoWork()) }
        let decoded = inspect(file, photo: photo)
        precondition(decoded.width == 72 && decoded.height == 36)
        try Data(base64Encoded: file.data)!.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
        let jpeg = Data(base64Encoded: file.data)!
        let reencoded = try background { try MobilePhotoAccess.jpeg(jpeg, photo: photo, work: MobilePhotoWork()) }
        _ = inspect(reencoded, photo: photo)
        expectFailure(.failed) { _ = try background { try MobilePhotoAccess.jpeg(Data(jpeg.prefix(jpeg.count / 2)), photo: photo, work: MobilePhotoWork()) } }
        let wire = MobileFileResult(status: .completed, file: file).wire
        precondition(Set((wire["file"] as! [String: String]).keys) == Set(["name", "mediaType", "data"]))
        for status: MobileFileStatus in [.cancelled, .denied, .unsupported, .failed] {
            precondition(MobileFileResult(status: status, file: file).wire["file"] == nil)
        }
        for orientation in [CGImagePropertyOrientation.up, .upMirrored, .down, .downMirrored, .leftMirrored, .right, .rightMirrored, .left] {
            let image = fixture()
            let bytes = encoded(image, orientation: orientation)
            let camera = MobilePhoto(title: "Capture", source: .camera, maxBytes: 100_000, maxDimension: 72)
            let library = try background { try MobilePhotoAccess.jpeg(bytes, photo: photo, work: MobilePhotoWork()) }
            let capture = try background { try MobilePhotoAccess.jpeg(image, orientation: orientation, photo: camera, work: MobilePhotoWork()) }
            let expected = inspect(library, photo: photo)
            let actual = inspect(capture, photo: camera)
            let rotated = [.left, .leftMirrored, .right, .rightMirrored].contains(orientation)
            precondition(expected.width == (rotated ? 36 : 72) && expected.height == (rotated ? 72 : 36))
            precondition(actual.width == expected.width && actual.height == expected.height && corners(actual) == corners(expected), "orientation \(orientation.rawValue)")
        }
        let small = MobilePhoto(title: "Small", source: .library, maxBytes: 1200, maxDimension: 512)
        let noisy = encoded(fixture(noise: true))
        let reduced = try background { try MobilePhotoAccess.jpeg(noisy, photo: small, work: MobilePhotoWork()) }
        _ = inspect(reduced, photo: small)
        let minimal = MobilePhoto(title: "Tiny", source: .library, maxBytes: 4096, maxDimension: 1)
        let onePixel = try background { try MobilePhotoAccess.jpeg(original, photo: minimal, work: MobilePhotoWork()) }
        let pixel = inspect(onePixel, photo: minimal)
        precondition(pixel.width == 1 && pixel.height == 1)
        let impossible = MobilePhoto(title: "Impossible", source: .camera, maxBytes: 1, maxDimension: 72)
        expectFailure(.failed) { _ = try background { try MobilePhotoAccess.jpeg(original, photo: impossible, work: MobilePhotoWork()) } }
        for bad in [Data(), Data("not an image".utf8), Data([255, 216, 255, 217]), Data(repeating: 0, count: MobilePhotoAccess.maximumOriginalBytes + 1)] {
            expectFailure(.failed) { _ = try background { try MobilePhotoAccess.jpeg(bad, photo: photo, work: MobilePhotoWork()) } }
        }
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("photo-check-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false)
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("original.tiff")
        try original.write(to: url)
        let read = try background { try MobilePhotoAccess.readOriginal(url, work: MobilePhotoWork()) }
        precondition(read == original)
        let link = directory.appendingPathComponent("symlink.tiff")
        try FileManager.default.createSymbolicLink(at: link, withDestinationURL: url)
        expectFailure(.unsupported) { _ = try background { try MobilePhotoAccess.readOriginal(link, work: MobilePhotoWork()) } }
        expectFailure(.unsupported) { _ = try background { try MobilePhotoAccess.readOriginal(directory, work: MobilePhotoWork()) } }
        expectFailure(.failed) { _ = try background { try MobilePhotoAccess.readOriginal(URL(string: "https://example.invalid/image.jpg")!, work: MobilePhotoWork()) } }
        let handle = try FileHandle(forWritingTo: url)
        try handle.truncate(atOffset: UInt64(MobilePhotoAccess.maximumOriginalBytes))
        try handle.close()
        let largest = try background { try MobilePhotoAccess.readOriginal(url, work: MobilePhotoWork()) }
        precondition(largest.count == MobilePhotoAccess.maximumOriginalBytes)
        let oversized = try FileHandle(forWritingTo: url)
        try oversized.truncate(atOffset: UInt64(MobilePhotoAccess.maximumOriginalBytes + 1))
        try oversized.close()
        expectFailure(.failed) { _ = try background { try MobilePhotoAccess.readOriginal(url, work: MobilePhotoWork()) } }
        try Data().write(to: url)
        expectFailure(.failed) { _ = try background { try MobilePhotoAccess.readOriginal(url, work: MobilePhotoWork()) } }
        let work = MobilePhotoWork()
        try work.check()
        work.cancel()
        work.cancel()
        expectFailure(.cancelled) { try work.check() }
        expectFailure(.cancelled) { _ = try background { try MobilePhotoAccess.jpeg(original, photo: photo, work: work) } }
        expectFailure(.cancelled) { _ = try background { try MobilePhotoAccess.readOriginal(url, work: work) } }
        precondition(MobilePhotoAccess.status(for: NSError(domain: NSCocoaErrorDomain, code: CocoaError.fileReadNoPermission.rawValue)) == .denied)
        precondition(MobilePhotoAccess.status(for: NSError(domain: "provider", code: 1, userInfo: [NSUnderlyingErrorKey: CocoaError(.userCancelled)])) == .cancelled)
        precondition(MobilePhotoAccess.status(for: URLError(.cancelled)) == .cancelled)
        for events in [["select", "complete", "dismiss", "cancel"], ["select", "dismiss", "complete", "cancel"], ["cancel", "select", "dismiss", "complete"], ["finish", "select", "complete", "dismiss"]] {
            var state = MobileFileTaskState()
            precondition(state.present())
            var submissions = 0
            for event in events {
                switch event {
                case "select": _ = state.receive()
                case "cancel": if state.receive() && state.report() { submissions += 1 }
                case "dismiss": if state.report() { submissions += 1 }
                case "complete": if state.phase == .reading && state.report() { submissions += 1 }
                case "finish": state.finish()
                default: preconditionFailure()
                }
            }
            precondition(submissions == (events.first == "finish" ? 0 : 1))
        }
    }
}
`
