import Foundation

struct MobileIncomingShareRequest: Codable {
    let operation: String
    let identifier: String?
    func validate() throws {
        guard operation == "next" && (identifier ?? "").isEmpty || operation == "acknowledge" && MobileIncomingShareItem.validIdentifier(identifier ?? "") else { throw MobileIncomingShareError.invalid }
    }
}

struct MobileIncomingShareFile: Codable {
    let name: String
    let mediaType: String
    let data: String
    func bytes() throws -> Data {
        guard !name.isEmpty, name.utf8.count <= 200, name != ".", name != "..", !name.contains("/"), !name.contains("\\"), !name.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              mediaType.utf8.count <= 128, mediaType.range(of: #"^[A-Za-z0-9!#$&^_.+\-]+/[A-Za-z0-9!#$&^_.+\-]+$"#, options: .regularExpression) != nil,
              data.utf8.count <= 1_398_104, let decoded = Data(base64Encoded: data), decoded.count <= 1_048_576 else { throw MobileIncomingShareError.invalid }
        return decoded
    }
}

struct MobileIncomingShareItem: Codable {
    let identifier: String
    var title: String?
    var text: String?
    var files: [MobileIncomingShareFile]?
    static func validIdentifier(_ value: String) -> Bool { value.utf8.count == 32 && value.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) } }
    func validate() throws {
        guard Self.validIdentifier(identifier), (title ?? "").utf8.count <= 200, !(title ?? "").unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              (text ?? "").utf8.count <= 65_536, !(text ?? "").contains("\0"), (files ?? []).count <= 4,
              !(text ?? "").isEmpty || !(files ?? []).isEmpty else { throw MobileIncomingShareError.invalid }
        var total = 0
        for file in files ?? [] { total += try file.bytes().count }
        guard total <= 1_048_576 else { throw MobileIncomingShareError.invalid }
    }
    var wire: [String: Any] {
        var result: [String: Any] = ["identifier": identifier]
        if let title, !title.isEmpty { result["title"] = title }
        if let text, !text.isEmpty { result["text"] = text }
        if let files, !files.isEmpty { result["files"] = files.map { ["name": $0.name, "mediaType": $0.mediaType, "data": $0.data] } }
        return result
    }
}

enum MobileIncomingShareError: Error { case invalid, unavailable, full }

/// Shared by the host and its separate extension executable. No Go runtime is
/// loaded in the extension. The same App Group entitlement is required in both.
final class MobileIncomingShareInbox {
    private let directory: URL
    static let maximumJSON = 1_500_000
    init(directory: URL) throws {
        self.directory = directory
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        var url = directory
        var values = URLResourceValues(); values.isExcludedFromBackup = true
        try url.setResourceValues(values)
    }
    #if os(iOS)
    convenience init(groupIdentifier: String) throws {
        guard !groupIdentifier.isEmpty, let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: groupIdentifier) else { throw MobileIncomingShareError.unavailable }
        try self.init(directory: container.appendingPathComponent("gofastr-incoming-shares", isDirectory: true))
    }
    #endif
    // Call from a worker. Directory coordination serializes quota, publication,
    // and acknowledgements across the app and the share extension processes.
    private func coordinated<T>(_ action: () throws -> T) throws -> T {
        var coordinationError: NSError?
        var outcome: Result<T, Error>?
        NSFileCoordinator().coordinate(writingItemAt: directory, options: .forMerging, error: &coordinationError) { _ in outcome = Result { try action() } }
        if let coordinationError { throw coordinationError }
        guard let outcome else { throw MobileIncomingShareError.unavailable }
        return try outcome.get()
    }
    private func entries() throws -> [URL] {
        let entries = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: [.contentModificationDateKey, .isRegularFileKey, .isSymbolicLinkKey])
        var current: [(URL, Date)] = []
        for url in entries where url.pathExtension == "ack" && MobileIncomingShareItem.validIdentifier(url.deletingPathExtension().lastPathComponent) {
            if ((try? url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast) < Date().addingTimeInterval(-7 * 24 * 60 * 60) { try FileManager.default.removeItem(at: url) }
        }
        for url in entries where url.pathExtension == "json" && MobileIncomingShareItem.validIdentifier(url.deletingPathExtension().lastPathComponent) {
            if FileManager.default.fileExists(atPath: url.deletingPathExtension().appendingPathExtension("ack").path) { try FileManager.default.removeItem(at: url); continue }
            let values = try url.resourceValues(forKeys: [.contentModificationDateKey, .isRegularFileKey, .isSymbolicLinkKey])
            guard values.isRegularFile == true, values.isSymbolicLink != true else { throw MobileIncomingShareError.invalid }
            let date = values.contentModificationDate ?? .distantPast
            if date < Date().addingTimeInterval(-7 * 24 * 60 * 60) { try FileManager.default.removeItem(at: url) }
            else { current.append((url, date)) }
        }
        return current.sorted { $0.1 == $1.1 ? $0.0.lastPathComponent < $1.0.lastPathComponent : $0.1 < $1.1 }.map { $0.0 }
    }
    func append(_ item: MobileIncomingShareItem) throws {
        try item.validate()
        let data = try JSONEncoder().encode(item)
        guard data.count <= Self.maximumJSON else { throw MobileIncomingShareError.invalid }
        try coordinated {
            let current = try entries()
            let destination = directory.appendingPathComponent(item.identifier + ".json")
            if current.contains(destination) || FileManager.default.fileExists(atPath: directory.appendingPathComponent(item.identifier + ".ack").path) { return }
            guard current.count < 16 else { throw MobileIncomingShareError.full }
            #if os(iOS)
            try data.write(to: destination, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
            #else
            try data.write(to: destination, options: .atomic)
            #endif
            let published = try FileHandle(forWritingTo: destination)
            defer { try? published.close() }
            try published.synchronize()
        }
    }
    func next() throws -> MobileIncomingShareItem? {
        try coordinated {
            guard let first = try entries().first else { return nil }
            let stream = try FileHandle(forReadingFrom: first)
            defer { try? stream.close() }
            let data = try stream.read(upToCount: Self.maximumJSON + 1) ?? Data()
            guard data.count <= Self.maximumJSON else { throw MobileIncomingShareError.invalid }
            let item = try JSONDecoder().decode(MobileIncomingShareItem.self, from: data)
            try item.validate()
            guard first.deletingPathExtension().lastPathComponent == item.identifier else { throw MobileIncomingShareError.invalid }
            return item
        }
    }
    func acknowledge(_ identifier: String) throws {
        guard MobileIncomingShareItem.validIdentifier(identifier) else { throw MobileIncomingShareError.invalid }
        try coordinated {
            let file = directory.appendingPathComponent(identifier + ".json")
            let tombstone = directory.appendingPathComponent(identifier + ".ack")
            try Data().write(to: tombstone, options: .atomic)
            let published = try FileHandle(forWritingTo: tombstone)
            try published.synchronize()
            try published.close()
            if FileManager.default.fileExists(atPath: file.path) { try FileManager.default.removeItem(at: file) }
            let acknowledgements = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: [.contentModificationDateKey]).filter { $0.pathExtension == "ack" && MobileIncomingShareItem.validIdentifier($0.deletingPathExtension().lastPathComponent) }.sorted { ((try? $0.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast) < ((try? $1.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast) }
            for (index, url) in acknowledgements.enumerated() where index < acknowledgements.count - 256 || ((try? url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast) < Date().addingTimeInterval(-7 * 24 * 60 * 60) { try FileManager.default.removeItem(at: url) }
        }
    }
}

final class MobileIncomingShares {
    private let groupIdentifier: String
    private let queue = DispatchQueue(label: "dev.gofastr.incoming-shares")
    init(groupIdentifier: String) { self.groupIdentifier = groupIdentifier }
    func perform(_ request: MobileIncomingShareRequest, completion: @escaping ([String: Any]) -> Void) {
        queue.async {
            var response: [String: Any]
            do {
                try request.validate()
                #if os(iOS)
                let inbox = try MobileIncomingShareInbox(groupIdentifier: self.groupIdentifier)
                if request.operation == "next" {
                    response = ["status": "completed"]
                    if let item = try inbox.next() { response["incomingShare"] = item.wire }
                } else {
                    try inbox.acknowledge(request.identifier ?? "")
                    response = ["status": "completed"]
                }
                #else
                response = ["status": "unsupported"]
                #endif
            } catch { response = ["status": "failed", "message": "The share inbox could not be read."] }
            let result = response
            DispatchQueue.main.async { completion(result) }
        }
    }
}
