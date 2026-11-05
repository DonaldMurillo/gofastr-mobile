import Foundation
import UIKit
import WebKit

public final class GofastrWebViewServices: NSObject, WKScriptMessageHandlerWithReply {
    private weak var presenter: UIViewController?
    private weak var webView: WKWebView?
    private let origin: URL
    private var runner: GofastrServiceRunner?
    private var pendingRoute: String?
    private var active = false
    private var generation = 0

    public init(presenter: UIViewController, origin: URL) {
        self.presenter = presenter
        self.origin = origin
        super.init()
    }

    public func attach(_ webView: WKWebView) {
        self.webView = webView
        guard let presenter else { return }
        runner = GofastrServiceRunner(presenter: presenter, openRoute: { [weak self] route in
            self?.open(route) ?? false
        })
    }

    public func navigationStarted() {
        generation += 1
        runner?.cancel()
    }

    public func navigationFinished() {
        guard let route = pendingRoute else { return }
        pendingRoute = nil
        _ = open(route)
    }

    private func trusted(_ message: WKScriptMessage) -> Bool {
        let source = message.frameInfo.securityOrigin
        return GofastrServiceOrigin(url: origin).permits(mainFrame: message.frameInfo.isMainFrame,
            scheme: source.protocol, host: source.host, port: source.port, current: webView?.url)
    }

    private func open(_ route: String) -> Bool {
        guard GofastrServiceOrigin(url: origin).destination(route) != nil, let webView else { return false }
        guard webView.url != nil, !webView.isLoading else {
            pendingRoute = route
            return true
        }
        webView.callAsyncJavaScript("if (window.__gofastr && typeof window.__gofastr.navigate === 'function') { await window.__gofastr.navigate(route); return true; } return false;",
            arguments: ["route": route], in: nil, in: .page) { _ in }
        return true
    }

    public func userContentController(_ userContentController: WKUserContentController,
                                     didReceive message: WKScriptMessage,
                                     replyHandler: @escaping (Any?, String?) -> Void) {
        guard trusted(message), let body = message.body as? [String: Any], let kind = body["kind"] as? String else {
            replyHandler(nil, "Native services require the trusted app main frame.")
            return
        }
        if kind == "capabilities", body.count == 1 {
            replyHandler(GofastrServiceRunner.capabilities(), nil)
            return
        }
        guard kind == "perform", body.count == 2, let request = body["request"] as? [String: Any],
              JSONSerialization.isValidJSONObject(request),
              let data = try? JSONSerialization.data(withJSONObject: request), data.count <= 2 << 20,
              let encoded = String(data: data, encoding: .utf8), let runner else {
            replyHandler(nil, "Invalid native service request.")
            return
        }
        guard !active else {
            replyHandler(["status": "failed", "message": "Another native service is already active."], nil)
            return
        }
        active = true
        let admitted = generation
        runner.perform(requestJSON: encoded) { [weak self] result in
            guard let self else {
                replyHandler(nil, "The app window closed.")
                return
            }
            self.active = false
            guard admitted == self.generation, let current = self.webView?.url,
                  current.scheme == self.origin.scheme, current.host == self.origin.host,
                  current.port == self.origin.port else {
                replyHandler(nil, "The screen changed. The native action was not retried.")
                return
            }
            guard let data = result.data(using: .utf8), data.count <= 2 << 20,
                  let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
                replyHandler(nil, "Invalid native service result. The action was not retried.")
                return
            }
            replyHandler(value, nil)
        }
    }
}
