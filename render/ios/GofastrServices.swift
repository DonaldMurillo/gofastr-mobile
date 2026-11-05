import Foundation

struct MobileServiceRequest: Codable {
    let id: String
    let notification: MobileNotificationRequest?
    let share: MobileShareRequest?
    let clipboard: MobileClipboardRequest?
    let audio: MobileAudioRequest?
    let commit: Bool?

    func validate() throws {
        guard !id.isEmpty, id.utf8.count <= 256, !id.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              [notification != nil, share != nil, clipboard != nil, audio != nil].filter({ $0 }).count == 1 else {
            throw MobileServiceFailure.invalid
        }
        try notification?.validate()
        try share?.validate()
        if let clipboard, clipboard.text.utf8.count > MobileServiceBounds.maximumText || clipboard.text.contains("\0") { throw MobileServiceFailure.tooLarge }
        try audio?.validate()
    }
}

enum MobileServiceBounds {
    static let maximumBytes = 1_048_576
    static let maximumText = 65_536
    static let maximumJSON = 4_194_304

    static func bytes(_ file: MobileFileExport, maximum: Int = maximumBytes) throws -> Data {
        guard maximum > 0, maximum <= maximumBytes,
              file.data.utf8.count <= ((maximum + 2) / 3) * 4,
              let bytes = Data(base64Encoded: file.data), bytes.count <= maximum else { throw MobileServiceFailure.tooLarge }
        guard !file.name.isEmpty, file.name.utf8.count <= 200,
              ![".", ".."].contains(file.name), !file.name.contains("/"), !file.name.contains("\\"),
              !file.name.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              file.mediaType.contains("/"), file.mediaType.utf8.count <= 128,
              !file.mediaType.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) else { throw MobileServiceFailure.invalid }
        return bytes
    }

    static func read(_ url: URL, maximum: Int) throws -> Data {
        precondition(!Thread.isMainThread)
        guard (1...maximumBytes).contains(maximum), url.isFileURL else { throw MobileServiceFailure.invalid }
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        let bytes = try handle.read(upToCount: maximum + 1) ?? Data()
        guard bytes.count <= maximum else { throw MobileServiceFailure.tooLarge }
        guard !bytes.isEmpty else { throw MobileServiceFailure.invalid }
        return bytes
    }
}

enum MobileServiceFailure: LocalizedError {
    case invalid, tooLarge, unsupported

    var errorDescription: String? {
        switch self {
        case .invalid: return "The device service request or audio data is invalid."
        case .tooLarge: return "The device service exceeds its size or duration limit."
        case .unsupported: return "This device cannot perform the requested service."
        }
    }
}

struct MobileNotificationRequest: Codable {
    let operation: String
    let identifier: String
    let title: String?
    let body: String?
    let route: String?
    let date: String?
    let time: String?

    func validate() throws {
        guard ["schedule", "cancel", "status"].contains(operation), !identifier.isEmpty,
              identifier.utf8.count <= 128,
              identifier.utf8.allSatisfy({ (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || [46, 95, 58, 45].contains($0) }) else { throw MobileServiceFailure.invalid }
        if operation == "schedule" {
            guard let title, !title.isEmpty, title.utf8.count <= 200,
                  !title.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
                  (body ?? "").utf8.count <= 65_536, !(body ?? "").contains("\0"),
                  let route, Self.localRoute(route), date != nil, time != nil else { throw MobileServiceFailure.invalid }
        } else if [title, body, route, date, time].contains(where: { !($0 ?? "").isEmpty }) {
            throw MobileServiceFailure.invalid
        }
    }

    static func localRoute(_ route: String) -> Bool {
        guard route.hasPrefix("/"), !route.hasPrefix("//"), route.utf8.count <= 4_096,
              !route.contains("\\"), !route.contains("#"),
              !route.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              let parts = URLComponents(string: route), parts.scheme == nil, parts.host == nil else { return false }
        return !parts.path.split(separator: "/").contains(where: { $0 == "." || $0 == ".." })
    }

    func scheduledDate(now: Date = Date(), timeZone: TimeZone = .current) throws -> Date {
        guard let date, let time,
              date.count == 10, time.count == 5,
              date.utf8.allSatisfy({ (48...57).contains($0) || $0 == 45 }),
              time.utf8.allSatisfy({ (48...57).contains($0) || $0 == 58 }) else { throw MobileServiceFailure.invalid }
        let dateParts = date.split(separator: "-", omittingEmptySubsequences: false)
        let timeParts = time.split(separator: ":", omittingEmptySubsequences: false)
        guard dateParts.map(\.count) == [4, 2, 2], timeParts.map(\.count) == [2, 2],
              let year = Int(dateParts[0]), let month = Int(dateParts[1]), let day = Int(dateParts[2]),
              let hour = Int(timeParts[0]), let minute = Int(timeParts[1]),
              (1...9999).contains(year), (1...12).contains(month), (1...31).contains(day),
              (0...23).contains(hour), (0...59).contains(minute) else { throw MobileServiceFailure.invalid }
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        let components = DateComponents(timeZone: timeZone, year: year, month: month, day: day, hour: hour, minute: minute, second: 0)
        guard let value = calendar.date(from: components), value > now else { throw MobileServiceFailure.invalid }
        let actual = calendar.dateComponents([.year, .month, .day, .hour, .minute], from: value)
        guard actual.year == year, actual.month == month, actual.day == day,
              actual.hour == hour, actual.minute == minute else { throw MobileServiceFailure.invalid }
        return value
    }
}

struct MobileShareRequest: Codable {
    let title: String?
    let text: String?
    let files: [MobileFileExport]?

    func validate() throws {
        guard (title ?? "").utf8.count <= 200,
              !(title ?? "").unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              (text ?? "").utf8.count <= MobileServiceBounds.maximumText,
              !(text ?? "").contains("\0"),
              (files ?? []).count <= 4,
              !(text ?? "").isEmpty || !(files ?? []).isEmpty else { throw MobileServiceFailure.invalid }
        var total = 0
        for file in files ?? [] {
            total += try MobileServiceBounds.bytes(file).count
            guard total <= MobileServiceBounds.maximumBytes else { throw MobileServiceFailure.tooLarge }
        }
    }

    func prepare() throws -> (URL, [URL]) {
        precondition(!Thread.isMainThread)
        try validate()
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("gofastr-share-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
        do {
            var names = Set<String>()
            var urls: [URL] = []
            for (index, file) in (files ?? []).enumerated() {
                let sanitized = MobileFileAccess.sanitizedName(file.name)
                var name = sanitized
                var suffix = index + 1
                while !names.insert(name.lowercased()).inserted { name = "\(suffix)-\(sanitized)"; suffix += 1 }
                let url = directory.appendingPathComponent(name, isDirectory: false)
                try MobileServiceBounds.bytes(file).write(to: url, options: .atomic)
                try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: url.path)
                urls.append(url)
            }
            return (directory, urls)
        } catch {
            try? FileManager.default.removeItem(at: directory)
            throw error
        }
    }
}

struct MobileClipboardRequest: Codable { let text: String }

struct MobileAudioRequest: Codable {
    let title: String?
    let maxSeconds: Int?
    let maxBytes: Int?

    var seconds: Int { (maxSeconds ?? 0) == 0 ? 60 : maxSeconds! }
    var bytes: Int { (maxBytes ?? 0) == 0 ? MobileServiceBounds.maximumBytes : maxBytes! }

    func validate() throws {
        guard (1...60).contains(seconds), (1...MobileServiceBounds.maximumBytes).contains(bytes),
              let title, !title.isEmpty, title.utf8.count <= 200,
              !title.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) else { throw MobileServiceFailure.invalid }
    }

    func accept(_ file: MobileFileExport, durationMillis: Int) throws {
        try validate()
        guard (1...(seconds * 1_000)).contains(durationMillis) else { throw MobileServiceFailure.invalid }
        let content = try MobileServiceBounds.bytes(file, maximum: bytes)
        guard Self.hasAudioSignature(content, mediaType: file.mediaType) else { throw MobileServiceFailure.invalid }
    }

    static func hasAudioSignature(_ data: Data, mediaType: String) -> Bool {
        let bytes = [UInt8](data)
        func contains(_ text: String, at offset: Int) -> Bool {
            let signature = Array(text.utf8)
            return bytes.count >= offset + signature.count && Array(bytes[offset..<(offset + signature.count)]) == signature
        }
        func integer(_ offset: Int, littleEndian: Bool = false) -> Int {
            guard offset >= 0, offset + 4 <= bytes.count else { return -1 }
            let chunk = Array(bytes[offset..<(offset + 4)])
            let ordered = littleEndian ? Array(chunk.reversed()) : chunk
            return ordered.reduce(0) { ($0 << 8) | Int($1) }
        }
        let parts = mediaType.lowercased().split(separator: ";").map { $0.trimmingCharacters(in: .whitespaces) }
        if parts.dropFirst().contains(where: { !["codecs=opus", "codecs=\"opus\"", "codecs=mp4a.40.2", "codecs=\"mp4a.40.2\""].contains($0) }) { return false }
        switch parts.first ?? "" {
        case "audio/mp4", "audio/x-m4a":
            var offset = 0
            var brand = false
            var codec = false
            var payload = false
            while offset + 8 <= bytes.count {
                let length = integer(offset)
                guard length >= 8, length <= bytes.count - offset else { return false }
                if contains("ftyp", at: offset + 4) {
                    guard length >= 16, length % 4 == 0 else { return false }
                    for position in stride(from: offset + 8, to: offset + length, by: 4) where position != offset + 12 {
                        if ["M4A ", "mp42", "isom", "iso2"].contains(where: { contains($0, at: position) }) { brand = true }
                    }
                }
                if contains("moov", at: offset + 4) { codec = data.subdata(in: (offset + 8)..<(offset + length)).range(of: Data("mp4a".utf8)) != nil }
                if contains("mdat", at: offset + 4) { payload = length > 8 }
                offset += length
            }
            return offset == bytes.count && brand && codec && payload
        case "audio/aac":
            var offset = 0
            while offset < bytes.count {
                guard offset + 7 <= bytes.count, bytes[offset] == 0xff, bytes[offset + 1] & 0xf6 == 0xf0,
                      bytes[offset + 2] >> 2 & 15 <= 12 else { return false }
                let header = bytes[offset + 1] & 1 == 0 ? 9 : 7
                let length = Int(bytes[offset + 3] & 3) << 11 | Int(bytes[offset + 4]) << 3 | Int(bytes[offset + 5] >> 5)
                guard length > header, length <= bytes.count - offset else { return false }
                offset += length
            }
            return !bytes.isEmpty
        case "audio/wav", "audio/x-wav", "audio/wave":
            guard bytes.count >= 44, contains("RIFF", at: 0), contains("WAVE", at: 8), integer(4, littleEndian: true) + 8 == bytes.count else { return false }
            var offset = 12
            var format = false
            var payload = false
            var alignment = 0
            while offset + 8 <= bytes.count {
                let length = integer(offset + 4, littleEndian: true)
                guard length >= 0, length <= bytes.count - offset - 8 else { return false }
                if contains("fmt ", at: offset) {
                    guard length >= 16 else { return false }
                    let encoding = Int(bytes[offset + 8]) | Int(bytes[offset + 9]) << 8
                    let channels = Int(bytes[offset + 10]) | Int(bytes[offset + 11]) << 8
                    let bits = Int(bytes[offset + 22]) | Int(bytes[offset + 23]) << 8
                    alignment = Int(bytes[offset + 20]) | Int(bytes[offset + 21]) << 8
                    format = [1, 3].contains(encoding) && (1...2).contains(channels) && (8...32).contains(bits) && bits % 8 == 0 && alignment == channels * bits / 8 && integer(offset + 12, littleEndian: true) > 0
                }
                if contains("data", at: offset) { payload = format && alignment > 0 && length > 0 && length % alignment == 0 }
                offset += 8 + length + length % 2
            }
            return offset == bytes.count && format && payload
        case "audio/ogg", "application/ogg":
            var offset = 0
            var first = true
            var payload = false
            while offset < bytes.count {
                guard offset + 27 <= bytes.count, contains("OggS", at: offset), bytes[offset + 4] == 0 else { return false }
                let segments = Int(bytes[offset + 26])
                let start = offset + 27 + segments
                guard start <= bytes.count else { return false }
                let size = bytes[(offset + 27)..<start].reduce(0) { $0 + Int($1) }
                guard size > 0, size <= bytes.count - start else { return false }
                if first {
                    guard bytes[offset + 5] & 2 != 0, size >= 19, contains("OpusHead", at: start), bytes[start + 8] == 1, (1...2).contains(bytes[start + 9]) else { return false }
                    first = false
                } else if !contains("OpusTags", at: start) { payload = true }
                offset = start + size
            }
            return !first && payload
        case "audio/webm":
            return bytes.count > 32 && Array(bytes.prefix(4)) == [0x1a, 0x45, 0xdf, 0xa3] && data.range(of: Data("webm".utf8)) != nil && data.range(of: Data("OpusHead".utf8)) != nil && data.range(of: Data([0x18, 0x53, 0x80, 0x67])) != nil
        default: return false
        }
    }
}

struct MobileAudioDocument: Codable, Equatable {
    let file: MobileFileExport
    let durationMillis: Int

    func validatedBytes() throws -> Data {
        try MobileAudioRequest(title: "Playback", maxSeconds: nil, maxBytes: nil).accept(file, durationMillis: durationMillis)
        return try MobileServiceBounds.bytes(file)
    }
}

struct MobileServiceResult: Codable {
    let status: String
    var message: String?
    var permission: String?
    var identifier: String?
    var file: MobileFileExport?
    var durationMillis: Int?

    var wire: [String: Any] {
        var value: [String: Any] = ["status": status]
        if let message { value["message"] = Self.safeMessage(message) }
        if let permission { value["permission"] = permission }
        if let identifier { value["identifier"] = identifier }
        if status == "completed", let file, let durationMillis {
            value["file"] = ["name": file.name, "mediaType": file.mediaType, "data": file.data]
            value["durationMillis"] = durationMillis
        }
        return value
    }

    private static func safeMessage(_ value: String) -> String {
        var result = ""
        for scalar in value.unicodeScalars {
            let text = CharacterSet.controlCharacters.contains(scalar) ? " " : String(scalar)
            guard result.utf8.count + text.utf8.count <= 512 else { break }
            result += text
        }
        return result
    }

    var json: String {
        String(decoding: (try? JSONSerialization.data(withJSONObject: wire)) ?? Data(#"{"status":"failed"}"#.utf8), as: UTF8.self)
    }
}

struct MobileServiceSettlement {
    private(set) var active: UUID?
    mutating func begin() -> UUID? {
        guard active == nil else { return nil }
        let token = UUID()
        active = token
        return token
    }
    mutating func finish(_ token: UUID) -> Bool {
        guard active == token else { return false }
        active = nil
        return true
    }
}

struct MobileLive: Decodable { let version: String }

struct MobileReadGeneration {
    private(set) var generation = 0
    private(set) var active = false
    mutating func begin() -> Int { generation += 1; active = true; return generation }
    mutating func cancel() { generation += 1; active = false }
    func accepts(_ token: Int) -> Bool { active && token == generation }
}
