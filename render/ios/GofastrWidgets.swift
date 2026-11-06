import Foundation

enum MobileWidgetFailure: Error { case invalid, unavailable }

struct MobileWidgetItem: Codable {
    let id: String
    let title: String
    let subtitle: String?
}

struct MobileWidgetSnapshot: Codable {
    let title: String
    let text: String?
    let route: String
    let items: [MobileWidgetItem]?

    func validate() throws {
        guard !title.isEmpty, Self.text(title, maximum: 200),
              Self.text(text ?? "", maximum: 2_048, multiline: true),
              Self.localRoute(route), (items ?? []).count <= 4 else { throw MobileWidgetFailure.invalid }
        var identifiers = Set<String>()
        for item in items ?? [] {
            guard !item.id.isEmpty, item.id.utf8.count <= 64,
                  item.id.utf8.allSatisfy({ (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || [46, 95, 58, 45].contains($0) }),
                  identifiers.insert(item.id).inserted,
                  !item.title.isEmpty, Self.text(item.title, maximum: 200),
                  Self.text(item.subtitle ?? "", maximum: 512, multiline: true) else { throw MobileWidgetFailure.invalid }
        }
        guard try JSONEncoder().encode(self).count <= 16_384 else { throw MobileWidgetFailure.invalid }
    }

    static func text(_ value: String, maximum: Int, multiline: Bool = false) -> Bool {
        value.utf8.count <= maximum && !value.unicodeScalars.contains(where: {
            CharacterSet.controlCharacters.contains($0) && !(multiline && ($0.value == 9 || $0.value == 10))
        })
    }

    static func localRoute(_ route: String) -> Bool {
        guard route.hasPrefix("/"), !route.hasPrefix("//"), text(route, maximum: 2_048),
              !route.unicodeScalars.contains(where: CharacterSet.whitespacesAndNewlines.contains),
              !route.contains("\\"), !route.contains("#"),
              let components = URLComponents(string: route), components.scheme == nil, components.host == nil,
              let path = components.percentEncodedPath.removingPercentEncoding,
              !path.contains("//"), !path.contains("\\"), !path.contains("?"), !path.contains("#"),
              !components.percentEncodedPath.lowercased().contains("%2f"),
              let decoded = route.removingPercentEncoding, text(decoded, maximum: 2_048) else { return false }
        // URLComponents can repair stray percent signs; reject these rather than
        // opening a route that differs from the admitted Go request.
        let bytes = Array(route.utf8)
        for index in bytes.indices where bytes[index] == 37 {
            guard index + 2 < bytes.count, hex(bytes[index + 1]), hex(bytes[index + 2]) else { return false }
        }
        return !path.split(separator: "/").contains(where: { $0 == "." || $0 == ".." })
    }

    private static func hex(_ value: UInt8) -> Bool {
        (48...57).contains(value) || (65...70).contains(value) || (97...102).contains(value)
    }
}

struct MobileWidgetRequest: Codable {
    let operation: String
    let snapshot: MobileWidgetSnapshot?

    func validate() throws {
        switch operation {
        case "publish":
            guard let snapshot else { throw MobileWidgetFailure.invalid }
            try snapshot.validate()
        case "clear", "status":
            guard snapshot == nil else { throw MobileWidgetFailure.invalid }
        default: throw MobileWidgetFailure.invalid
        }
    }
}

struct MobileWidgetState: Codable {
    let published: Bool
}

#if canImport(WidgetKit) && os(iOS)
import WidgetKit

/// Stores one public summary in the app group. WidgetKit consumes only this
/// bounded JSON file; the widget extension never starts the Go runtime.
final class MobileWidgetStore {
    static let filename = "gofastr-widget-v1.json"
    private let appGroup: String
    private let kind: String
    private let queue = DispatchQueue(label: "dev.gofastr.widget", qos: .utility)

    init(appGroup: String, kind: String = "dev.gofastr.summary") {
        self.appGroup = appGroup
        self.kind = kind
    }

    var available: Bool {
        !appGroup.isEmpty && FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: appGroup) != nil
    }

    /// Completion returns on main exactly once. A completed publish means the
    /// file was saved and a reload requested, not that WidgetKit displayed it.
    func perform(_ request: MobileWidgetRequest, completion: @escaping ([String: Any]) -> Void) {
        queue.async {
            let result: [String: Any]
            do {
                guard self.available, let directory = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: self.appGroup) else {
                    DispatchQueue.main.async { completion(["status": "unsupported"]) }
                    return
                }
                try request.validate()
                let file = directory.appendingPathComponent(Self.filename, isDirectory: false)
                switch request.operation {
                case "publish":
                    guard let snapshot = request.snapshot else { throw MobileWidgetFailure.invalid }
                    let content = try JSONEncoder().encode(snapshot)
                    try content.write(to: file, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
                case "clear":
                    do { try FileManager.default.removeItem(at: file) }
                    catch let error as CocoaError where error.code == .fileNoSuchFile { }
                default: break
                }
                let published: Bool
                if FileManager.default.fileExists(atPath: file.path) {
                    let bytes = try Self.read(file)
                    let snapshot = try JSONDecoder().decode(MobileWidgetSnapshot.self, from: bytes)
                    try snapshot.validate()
                    published = true
                } else { published = false }
                if request.operation != "status" { WidgetCenter.shared.reloadTimelines(ofKind: self.kind) }
                result = ["status": "completed", "widget": ["published": published]]
            } catch {
                result = ["status": "failed", "message": "The widget snapshot could not be updated."]
            }
            DispatchQueue.main.async { completion(result) }
        }
    }

    static func read(_ file: URL) throws -> Data {
        let handle = try FileHandle(forReadingFrom: file)
        defer { try? handle.close() }
        let content = try handle.read(upToCount: 16_385) ?? Data()
        guard !content.isEmpty, content.count <= 16_384 else { throw MobileWidgetFailure.invalid }
        return content
    }
}
#endif
