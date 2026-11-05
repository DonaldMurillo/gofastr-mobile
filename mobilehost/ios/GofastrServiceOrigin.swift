import Foundation

struct GofastrServiceOrigin {
    let url: URL

    func permits(mainFrame: Bool, scheme: String, host: String, port: Int, current: URL?) -> Bool {
        guard mainFrame, let current else { return false }
        return scheme == url.scheme && host == url.host && port == (url.port ?? 0)
            && current.scheme == url.scheme && current.host == url.host && current.port == url.port
    }

    func destination(_ route: String) -> URL? {
        guard route.hasPrefix("/"), !route.hasPrefix("//"), !route.contains("\\"),
              !route.unicodeScalars.contains(where: { $0.value < 32 || $0.value == 127 }),
              route.utf8.count <= 2048, let destination = URL(string: route, relativeTo: url)?.absoluteURL,
              destination.scheme == url.scheme, destination.host == url.host,
              destination.port == url.port else { return nil }
        return destination
    }
}
