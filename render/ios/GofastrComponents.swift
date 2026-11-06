import Foundation

struct MobileOption: Decodable {
    let value: String
    let label: String
    let disabled: Bool?
}

struct MobileField: Decodable {
    let key: String
    let label: String
    let placeholder: String?
    let value: String
    let kind: String
    let style: String?
    let required: Bool?
    let disabled: Bool?
    let options: [MobileOption]?
}

struct MobileRowAction: Decodable {
    let id: String
    let label: String
    let role: String?
    let fullSwipe: Bool?

    var allowsFullSwipe: Bool { fullSwipe == true && role != "destructive" }
}

struct MobileRow: Decodable {
    let id: String
    let title: String
    let subtitle: String?
    let metadata: String?
    let icon: String?
    let destination: String
    let disabled: Bool?
    let leadingActions: [MobileRowAction]?
    let trailingActions: [MobileRowAction]?

    var actions: [MobileRowAction] { (leadingActions ?? []) + (trailingActions ?? []) }
}

struct MobileListSelection: Decodable {
    let key: String
    let selected: [String]?
    let maximum: Int?
}

struct MobileListPaging: Decodable {
    let key: String
    let next: String?
    let label: String?
}

struct MobileList: Decodable {
    let header: String?
    let footer: String?
    let empty: String?
    let searchKey: String?
    let searchPlaceholder: String?
    let refreshable: Bool?
    let rows: [MobileRow]
    let filters: [MobileField]?
    let selection: MobileListSelection?
    let paging: MobileListPaging?

    func queryValues(_ values: [String: String]) -> [String: String] {
        let keys = Set((filters ?? []).map(\.key) + [searchKey, paging?.key].compactMap { $0 })
        return values.filter { keys.contains($0.key) }
    }

    func refreshedValues(_ query: [String: String], authoritative: [String: String]?) -> [String: String] {
        queryValues(query).merging(authoritative ?? [:]) { _, current in current }
    }
}

struct MobileListSelectionState {
    private(set) var selected: Set<String>

    init(_ selection: MobileListSelection?) {
        selected = Set(selection?.selected ?? [])
    }

    mutating func toggle(_ row: MobileRow, maximum: Int) -> Bool {
        guard row.disabled != true else { return false }
        if selected.contains(row.id) { selected.remove(row.id); return true }
        guard maximum <= 0 || selected.count < maximum else { return false }
        selected.insert(row.id)
        return true
    }

    mutating func reset() { selected.removeAll() }

    mutating func reconcile(_ rows: [MobileRow]) {
        selected.formIntersection(rows.map(\.id))
    }

    var wire: String {
        String(decoding: (try? JSONEncoder().encode(selected.sorted())) ?? Data("[]".utf8), as: UTF8.self)
    }
}

struct MobileTab: Decodable {
    let id: String
    let label: String
    let route: String
    let icon: String?
}

struct MobileRequirement: Decodable {
    let capability: String
    let allowFallback: Bool?
}

enum MobileLocalRoute {
    static func normalize(_ url: URL) -> String? {
        guard url.baseURL == nil, let parts = URLComponents(url: url, resolvingAgainstBaseURL: false),
              parts.fragment == nil, parts.user == nil, parts.password == nil, parts.port == nil else { return nil }
        let path: String
        if let scheme = parts.scheme {
            guard !["http", "https", "file", "ftp", "data", "javascript", "mailto", "tel", "sms", "about"].contains(scheme.lowercased()),
                  parts.host == nil || parts.host?.range(of: "^[A-Za-z0-9._~-]+$", options: .regularExpression) != nil else { return nil }
            if let host = parts.host { path = "/" + host + parts.percentEncodedPath }
            else { path = parts.percentEncodedPath }
        } else {
            guard parts.host == nil else { return nil }
            path = parts.percentEncodedPath
        }
        return validate(path + (parts.percentEncodedQuery.map { "?" + $0 } ?? ""))
    }

    static func validate(_ route: String) -> String? {
        guard !route.isEmpty, route.utf8.count <= 2048, route.hasPrefix("/"), !route.hasPrefix("//"),
              !route.contains("#"), !route.contains("\\"),
              route.unicodeScalars.allSatisfy({ !$0.properties.isWhitespace && !CharacterSet.controlCharacters.contains($0) }),
              let parts = URLComponents(string: route), parts.scheme == nil, parts.host == nil,
              let decoded = parts.percentEncodedPath.removingPercentEncoding,
              !decoded.contains("\\"), !decoded.contains("//"), !decoded.contains("?"), !decoded.contains("#"),
              !parts.percentEncodedPath.lowercased().contains("%2f"),
              decoded.unicodeScalars.allSatisfy({ !CharacterSet.controlCharacters.contains($0) }),
              !decoded.split(separator: "/").contains(where: { $0 == "." || $0 == ".." }),
              let decodedRoute = route.removingPercentEncoding,
              decodedRoute.unicodeScalars.allSatisfy({ !CharacterSet.controlCharacters.contains($0) }) else { return nil }
        let bytes = Array(route.utf8)
        for index in bytes.indices where bytes[index] == 37 {
            guard index + 2 < bytes.count,
                  bytes[(index + 1)...(index + 2)].allSatisfy({ (48...57).contains($0) || (65...70).contains($0) || (97...102).contains($0) }) else { return nil }
        }
        return route
    }
}

struct MobileNavigationMetadata: Codable {
    let version: Int
    let activeTab: String
    let stacks: [String: [String]]

    static let maximumTabs = 16
    static let maximumDepth = 6

    func routes(for tab: MobileTab) -> [String] {
        guard version == 1, let stored = stacks[tab.id], stored.first == tab.route,
              stored.count <= Self.maximumDepth else { return [tab.route] }
        var routes: [String] = []
        for route in stored {
            guard let valid = MobileLocalRoute.validate(route), !routes.contains(valid) else { break }
            routes.append(valid)
        }
        return routes.isEmpty ? [tab.route] : routes
    }
}

enum MobileFieldDate {
    static func formatter(_ kind: String) -> DateFormatter {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = Calendar(identifier: .gregorian)
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = kind == "date" ? "yyyy-MM-dd" : "HH:mm"
        formatter.isLenient = false
        return formatter
    }

    static func parse(_ value: String, kind: String) -> Date? {
        let formatter = formatter(kind)
        guard let date = formatter.date(from: value), formatter.string(from: date) == value else { return nil }
        return date
    }
}
