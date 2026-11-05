import Foundation
import UniformTypeIdentifiers

struct MobileFiles: Decodable {
    let id: String
    let pick: MobileFilePick?
    let export: MobileFileExport?
    let photo: MobilePhoto?

    var isValid: Bool {
        !id.isEmpty && [pick != nil, export != nil, photo != nil].filter { $0 }.count == 1 &&
            (pick.map { (0...MobileFileAccess.maximumBytes).contains($0.maxBytes) } ?? true) &&
            (photo?.isValid ?? true)
    }
}

struct MobilePhoto: Decodable {
    enum Source: String, Decodable { case library, camera }
    let title: String
    let source: Source
    let maxBytes: Int
    let maxDimension: Int

    var isValid: Bool {
        (1...MobileFileAccess.maximumBytes).contains(maxBytes) && (1...2048).contains(maxDimension)
    }
}

struct MobileFilePick: Decodable {
    let title: String
    let extensions: [String]?
    let maxBytes: Int
}

struct MobileFileExport: Codable, Equatable {
    let name: String
    let mediaType: String
    let data: String
}

enum MobileFileStatus: String {
    case completed, cancelled, denied, unsupported, failed
}

struct MobileFilePayload {
    let name: String
    let mediaType: String
    let data: String
}

struct MobileFileResult {
    let status: MobileFileStatus
    var file: MobileFilePayload?

    var wire: [String: Any] {
        var result: [String: Any] = ["status": status.rawValue]
        if status == .completed, let file {
            result["file"] = ["name": file.name, "mediaType": file.mediaType, "data": file.data]
        }
        return result
    }
}

struct MobileFileTaskState {
    enum Phase { case preparing, presented, reading, reported, finished }
    private(set) var phase = Phase.preparing

    mutating func present() -> Bool {
        guard phase == .preparing else { return false }
        phase = .presented
        return true
    }

    mutating func receive() -> Bool {
        guard phase == .presented else { return false }
        phase = .reading
        return true
    }

    mutating func report() -> Bool {
        guard phase != .reported && phase != .finished else { return false }
        phase = .reported
        return true
    }

    mutating func finish() { phase = .finished }
}

enum MobileFileAccess {
    static let maximumBytes = 1_048_576

    enum Failure: Error { case invalid, unsupported, tooLarge }

    static func sanitizedName(_ value: String) -> String {
        let component = (value.replacingOccurrences(of: "\\", with: "/") as NSString).lastPathComponent
        let cleaned = component.unicodeScalars.filter { !CharacterSet.controlCharacters.contains($0) && $0 != ":" }
        var name = ""
        for scalar in cleaned {
            guard name.utf8.count + scalar.utf8.count <= 200 else { break }
            name.unicodeScalars.append(scalar)
        }
        name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return name.isEmpty || name == "." || name == ".." || name == "/" ? "document" : name
    }

    static func extensions(_ pick: MobileFilePick) throws -> [String] {
        try (pick.extensions ?? []).map { value in
            let normalized = value.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
            let suffix = normalized.hasPrefix(".") ? String(normalized.dropFirst()) : normalized
            guard !suffix.isEmpty, suffix != ".", !suffix.hasPrefix("."),
                  !suffix.unicodeScalars.contains(where: { CharacterSet.controlCharacters.contains($0) || "/\\:".unicodeScalars.contains($0) }) else {
                throw Failure.invalid
            }
            return suffix
        }
    }

    static func contentTypes(_ pick: MobileFilePick) throws -> [UTType] {
        let extensions = try extensions(pick)
        if extensions.isEmpty { return [.data] }
        return try extensions.map {
            guard let type = UTType(filenameExtension: $0) else { throw Failure.unsupported }
            return type
        }
    }

    static func read(_ url: URL, pick: MobileFilePick) throws -> MobileFilePayload {
        precondition(!Thread.isMainThread)
        guard (0...maximumBytes).contains(pick.maxBytes), url.isFileURL else { throw Failure.invalid }
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        var coordinationError: NSError?
        var result: Result<MobileFilePayload, Error>?
        NSFileCoordinator(filePresenter: nil).coordinate(readingItemAt: url, options: .withoutChanges, error: &coordinationError) { coordinated in
            result = Result {
                let metadata = try coordinated.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .contentTypeKey])
                guard metadata.isRegularFile == true, metadata.isSymbolicLink != true else { throw Failure.unsupported }
                let name = sanitizedName(url.lastPathComponent)
                let allowed = try extensions(pick)
                guard allowed.isEmpty || allowed.contains(where: { name.lowercased().hasSuffix(".\($0)") }) else { throw Failure.unsupported }
                let handle = try FileHandle(forReadingFrom: coordinated)
                defer { try? handle.close() }
                var bytes = Data()
                while bytes.count <= pick.maxBytes {
                    guard let chunk = try handle.read(upToCount: pick.maxBytes + 1 - bytes.count), !chunk.isEmpty else { break }
                    bytes.append(chunk)
                }
                guard bytes.count <= pick.maxBytes else { throw Failure.tooLarge }
                let mediaType = metadata.contentType?.preferredMIMEType ?? UTType(filenameExtension: url.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
                return MobileFilePayload(name: name, mediaType: mediaType, data: bytes.base64EncodedString())
            }
        }
        if let coordinationError { throw coordinationError }
        guard let result else { throw Failure.invalid }
        return try result.get()
    }

    static func prepareExport(_ model: MobileFileExport) throws -> URL {
        precondition(!Thread.isMainThread)
        guard model.data.utf8.count <= ((maximumBytes + 2) / 3) * 4,
              let bytes = Data(base64Encoded: model.data), bytes.count <= maximumBytes else { throw Failure.tooLarge }
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("gofastr-file-export-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        do {
            let url = directory.appendingPathComponent(sanitizedName(model.name), isDirectory: false)
            try bytes.write(to: url, options: [.atomic])
            return url
        } catch {
            try? FileManager.default.removeItem(at: directory)
            throw error
        }
    }

    static func status(for error: Error) -> MobileFileStatus {
        if case Failure.unsupported = error { return .unsupported }
        var underlying = error as NSError
        for _ in 0..<8 {
            if underlying.domain == NSCocoaErrorDomain && [CocoaError.fileReadNoPermission.rawValue, CocoaError.fileWriteNoPermission.rawValue].contains(underlying.code) { return .denied }
            if underlying.domain == NSPOSIXErrorDomain && [1, 13].contains(underlying.code) { return .denied }
            guard let next = underlying.userInfo[NSUnderlyingErrorKey] as? NSError else { break }
            underlying = next
        }
        return .failed
    }
}
