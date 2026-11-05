import UIKit

protocol MobileForegroundTask: AnyObject {
    var identifier: String { get }
    func show()
    func finish(restoreFocus: Bool, completion: @escaping () -> Void)
}

final class MobileForegroundCoordinator {
    private var active: MobileForegroundTask?
    private var focus: MobileForegroundFocus?
    private let fileQueue = DispatchQueue(label: "gofastr.native.files", qos: .userInitiated)

    var isActive: Bool { active != nil }

    func cancel() {
        guard let active else { return }
        finish(active.identifier, restoreFocus: false) {}
    }

    @discardableResult
    func show(_ model: MobilePresentation, identifier: String, from source: UIViewController,
              respond: @escaping (String, [String]) -> Void) -> Bool {
        guard active == nil, !identifier.isEmpty,
              ["alert", "confirmation", "menu", "dialog", "sheet"].contains(model.kind),
              source.presentedViewController == nil else { return false }
        let focus = self.focus ?? MobileForegroundFocus(source)
        self.focus = focus
        let task = MobilePresentationTask(model, identifier: identifier, source: source, focus: focus, respond: respond)
        active = task
        task.show()
        return true
    }

    @discardableResult
    func show(_ model: MobileFiles, identifier: String, from source: UIViewController,
              respond: @escaping (MobileFileResult) -> Void) -> Bool {
        guard active == nil, !identifier.isEmpty, source.presentedViewController == nil else { return false }
        let focus = self.focus ?? MobileForegroundFocus(source)
        let task: MobileForegroundTask
        if let operation = model.operation {
            guard model.isValid, let adapter = MobileFileExtensions.make(operation, identifier: identifier, source: source, focus: focus, queue: fileQueue, respond: respond) else {
                respond(MobileFileResult(status: .unsupported))
                return true
            }
            task = adapter
        } else {
            task = MobileFileTask(model, identifier: identifier, source: source, focus: focus, queue: fileQueue, respond: respond)
        }
        self.focus = focus
        active = task
        task.show()
        return true
    }

    func reject(_ identifier: String, message: String) {
        guard let task = active as? MobilePresentationTask, task.identifier == identifier else { return }
        task.reject(message)
    }

    @discardableResult
    func show(_ model: MobileServiceRequest, identifier: String, from source: UIViewController,
              openRoute: @escaping (String) -> Bool, respond: @escaping (MobileServiceResult) -> Void) -> Bool {
        guard active == nil, !identifier.isEmpty, source.presentedViewController == nil else { return false }
        let focus = self.focus ?? MobileForegroundFocus(source)
        self.focus = focus
        let task = MobileServiceTask(model, identifier: identifier, source: source, focus: focus, openRoute: openRoute, respond: respond)
        active = task
        task.show()
        return true
    }

    func finish(_ identifier: String, restoreFocus: Bool = true, preserveFocus: Bool = false, completion: @escaping () -> Void) {
        guard let task = active, task.identifier == identifier else { completion(); return }
        task.finish(restoreFocus: restoreFocus) { [weak self] in
            if self?.active === task { self?.active = nil }
            if !preserveFocus { self?.focus = nil }
            completion()
        }
    }
}

final class MobileServiceTask: MobileForegroundTask {
    private static let queue = DispatchQueue(label: "gofastr.native.service-wire", qos: .userInitiated)
    let identifier: String
    private let model: MobileServiceRequest
    private weak var source: UIViewController?
    private let focus: MobileForegroundFocus
    private let runner: GofastrServiceRunner
    private let respond: (MobileServiceResult) -> Void
    private var reported = false

    init(_ model: MobileServiceRequest, identifier: String, source: UIViewController, focus: MobileForegroundFocus,
         openRoute: @escaping (String) -> Bool, respond: @escaping (MobileServiceResult) -> Void) {
        self.model = model
        self.identifier = identifier
        self.source = source
        self.focus = focus
        self.respond = respond
        runner = GofastrServiceRunner(presenter: source, openRoute: openRoute)
    }

    func show() {
        Self.queue.async {
            let encoded = Result { String(decoding: try JSONEncoder().encode(self.model), as: UTF8.self) }
            DispatchQueue.main.async {
                guard !self.reported else { return }
                switch encoded {
                case .failure(let error):
                    self.reported = true
                    self.respond(MobileServiceResult(status: "failed", message: error.localizedDescription))
                case .success(let json):
                    self.runner.perform(requestJSON: json) { [weak self] json in
                        Self.queue.async {
                            let result = (try? JSONDecoder().decode(MobileServiceResult.self, from: Data(json.utf8))) ?? MobileServiceResult(status: "failed", message: "The device returned an invalid service result.")
                            DispatchQueue.main.async {
                                guard let self, !self.reported else { return }
                                self.reported = true
                                self.respond(result)
                            }
                        }
                    }
                }
            }
        }
    }

    func finish(restoreFocus: Bool, completion: @escaping () -> Void) {
        reported = true
        runner.cancel()
        if restoreFocus { focus.restore(source) }
        completion()
    }
}

final class MobileForegroundFocus {
    private weak var input: UIView?
    private let element: Any?

    init(_ source: UIViewController) {
        input = Self.firstResponder(in: source.view)
        element = UIAccessibility.focusedElement(using: .notificationVoiceOver)
    }

    func restore(_ source: UIViewController?) {
        guard source?.viewIfLoaded?.window != nil else { return }
        input?.becomeFirstResponder()
        UIAccessibility.post(notification: .screenChanged, argument: element ?? source?.view)
    }

    private static func firstResponder(in view: UIView) -> UIView? {
        if view.isFirstResponder { return view }
        for child in view.subviews {
            if let input = firstResponder(in: child) { return input }
        }
        return nil
    }
}

enum MobileForegroundDismissal {
    static func dismiss(_ controller: UIViewController?, completion: @escaping () -> Void) {
        guard let controller else { completion(); return }
        if (controller.isBeingDismissed || controller.isBeingPresented), let transition = controller.transitionCoordinator {
            if transition.animate(alongsideTransition: nil, completion: { _ in
                DispatchQueue.main.async { dismiss(controller, completion: completion) }
            }) { return }
        }
        if controller.presentingViewController != nil { controller.dismiss(animated: true, completion: completion) }
        else { completion() }
    }
}
