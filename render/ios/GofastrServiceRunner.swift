import UIKit
import UserNotifications
import AVFoundation

public final class GofastrServiceRunner: NSObject {
    private weak var presenter: UIViewController?
    private let openRoute: (String) -> Bool
    private let queue = DispatchQueue(label: "gofastr.native.services", qos: .userInitiated)
    private var settlement = MobileServiceSettlement()
    private var completion: ((String) -> Void)?
    private var controller: UIViewController?
    private var directory: URL?
    private var dismissing = false
    private var scheduling = false
    private var cancellingSchedule = false
    private let secureStorage = GofastrSecureStorage()
    private var secureStorageActive = false
    private var durableServiceActive = false
    private lazy var dismissalDelegate = MobileServiceDismissalDelegate { [weak self] in self?.cancel() }

    public init(presenter: UIViewController, openRoute: @escaping (String) -> Bool) {
        self.presenter = presenter
        self.openRoute = openRoute
        super.init()
        MobileNotificationRouter.shared.install(openRoute)
    }

    public static func capabilities() -> [String: String] {
        let purpose = Bundle.main.object(forInfoDictionaryKey: "NSMicrophoneUsageDescription") as? String
        return ["notifications/v1": "available", "sharing/v1": "available", "clipboard/v1": "available",
                "secure-storage/v1": GofastrSecureStorage.enabled ? "available" : "unavailable",
                "incoming-shares/v1": Bundle.main.object(forInfoDictionaryKey: "GofastrReceiveShares") as? Bool == true ? "available" : "unavailable",
                "widgets/v1": Bundle.main.object(forInfoDictionaryKey: "GofastrWidgets") as? Bool == true ? "available" : "unavailable",
                "audio/v1": purpose?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false ? "available" : "unavailable"]
    }

    public func perform(requestJSON: String, completion: @escaping (String) -> Void) {
        guard Thread.isMainThread else {
            DispatchQueue.main.async { self.perform(requestJSON: requestJSON, completion: completion) }
            return
        }
        guard !dismissing, let token = settlement.begin() else {
            completion(MobileServiceResult(status: "failed", message: "Another device service is active.").json)
            return
        }
        self.completion = completion
        guard requestJSON.utf8.count <= MobileServiceBounds.maximumJSON else {
            finish(token, MobileServiceResult(status: "failed", message: MobileServiceFailure.tooLarge.localizedDescription))
            return
        }
        MobileNotificationRouter.shared.flush()
        queue.async {
            let decoded = Result { () throws -> MobileServiceRequest in
                let request = try JSONDecoder().decode(MobileServiceRequest.self, from: Data(requestJSON.utf8))
                try request.validate()
                return request
            }
            DispatchQueue.main.async {
                guard self.settlement.active == token else { return }
                switch decoded {
                case .failure(let error): self.finish(token, MobileServiceResult(status: "failed", message: error.localizedDescription))
                case .success(let request): self.execute(request, token: token)
                }
            }
        }
    }

    public func cancel() {
        guard Thread.isMainThread else { DispatchQueue.main.async { self.cancel() }; return }
        guard let token = settlement.active else { return }
        if secureStorageActive { secureStorage.cancel(); return }
        if durableServiceActive { return }
        if scheduling { cancellingSchedule = true; return }
        finish(token, MobileServiceResult(status: "cancelled", message: "Device service cancelled."))
    }

    private func execute(_ request: MobileServiceRequest, token: UUID) {
        if let secure = request.secureStorage {
            secureStorageActive = true
            secureStorage.perform(secure) { [weak self] result in
                self?.secureStorageActive = false
                self?.finish(token, result)
            }
            return
        }
        if let incoming = request.incomingShare {
            guard Self.capabilities()["incoming-shares/v1"] == "available" else { finish(token, MobileServiceResult(status: "unsupported")); return }
            durableServiceActive = true
            MobileIncomingShares(groupIdentifier: Bundle.main.object(forInfoDictionaryKey: "GofastrAppGroup") as? String ?? "").perform(incoming) { [weak self] result in self?.finishDictionary(token, result) }
            return
        }
        if let widget = request.widget {
            guard Self.capabilities()["widgets/v1"] == "available" else { finish(token, MobileServiceResult(status: "unsupported")); return }
            durableServiceActive = true
            MobileWidgetStore(appGroup: Bundle.main.object(forInfoDictionaryKey: "GofastrAppGroup") as? String ?? "").perform(widget) { [weak self] result in self?.finishDictionary(token, result) }
            return
        }
        if let notification = request.notification { performNotification(notification, token: token); return }
        if let clipboard = request.clipboard {
            UIPasteboard.general.string = clipboard.text
            finish(token, MobileServiceResult(status: "completed"))
            return
        }
        guard let presenter, presenter.viewIfLoaded?.window != nil, presenter.presentedViewController == nil else {
            finish(token, MobileServiceResult(status: "failed", message: "The service has no available foreground presenter."))
            return
        }
        if let share = request.share { performShare(share, token: token, presenter: presenter) }
        else if let audio = request.audio { performAudio(audio, token: token, presenter: presenter) }
    }

    private func finishDictionary(_ token: UUID, _ value: [String: Any]) {
        guard settlement.active == token else { return }
        durableServiceActive = false
        guard let data = try? JSONSerialization.data(withJSONObject: value),
              let result = try? JSONDecoder().decode(MobileServiceResult.self, from: data) else {
            finish(token, MobileServiceResult(status: "failed")); return
        }
        finish(token, result)
    }

    private func performShare(_ request: MobileShareRequest, token: UUID, presenter: UIViewController) {
        queue.async {
            let prepared = Result { try request.prepare() }
            DispatchQueue.main.async {
                guard self.settlement.active == token else {
                    if case .success(let result) = prepared { self.remove(result.0) }
                    return
                }
                switch prepared {
                case .failure(let error): self.finish(token, MobileServiceResult(status: "failed", message: error.localizedDescription))
                case .success(let result):
                    self.directory = result.0
                    var items: [Any] = []
                    if let text = request.text, !text.isEmpty { items.append(text) }
                    items.append(contentsOf: result.1)
                    let sheet = UIActivityViewController(activityItems: items, applicationActivities: nil)
                    sheet.title = request.title
                    sheet.popoverPresentationController?.sourceView = presenter.view
                    sheet.popoverPresentationController?.sourceRect = CGRect(x: presenter.view.bounds.midX, y: presenter.view.bounds.midY, width: 1, height: 1)
                    sheet.completionWithItemsHandler = { [weak self] _, completed, _, error in
                        DispatchQueue.main.async {
                            self?.finish(token, MobileServiceResult(status: error != nil ? "failed" : completed ? "completed" : "cancelled", message: error?.localizedDescription))
                        }
                    }
                    self.controller = sheet
                    presenter.present(sheet, animated: true)
                    sheet.presentationController?.delegate = self.dismissalDelegate
                }
            }
        }
    }

    private func performAudio(_ request: MobileAudioRequest, token: UUID, presenter: UIViewController) {
        guard Self.capabilities()["audio/v1"] == "available" else {
            finish(token, MobileServiceResult(status: "unsupported", message: "Microphone purpose text is missing."))
            return
        }
        let audio = MobileAudioRecorderController(request: request, queue: queue) { [weak self] result in
            self?.finish(token, result)
        }
        let navigation = UINavigationController(rootViewController: audio)
        navigation.isModalInPresentation = true
        controller = navigation
        presenter.present(navigation, animated: true)
        navigation.presentationController?.delegate = dismissalDelegate
    }

    private func finish(_ token: UUID, _ result: MobileServiceResult) {
        precondition(Thread.isMainThread)
        guard settlement.finish(token) else { return }
        scheduling = false
        cancellingSchedule = false
        let completion = self.completion
        self.completion = nil
        let presented = controller
        controller = nil
        let recording = (presented as? UINavigationController)?.viewControllers.first as? MobileAudioRecorderController
        recording?.shutdown()
        let temporary = directory
        directory = nil
        dismissing = true
        MobileForegroundDismissal.dismiss(presented) {
            self.queue.async {
                if let temporary { try? FileManager.default.removeItem(at: temporary) }
                let encoded = result.json
                DispatchQueue.main.async {
                    self.dismissing = false
                    completion?(encoded)
                }
            }
        }
    }

    private func remove(_ directory: URL) {
        queue.async { try? FileManager.default.removeItem(at: directory) }
    }

    private func performNotification(_ request: MobileNotificationRequest, token: UUID) {
        let center = UNUserNotificationCenter.current()
        let identifier = MobileNotificationRouter.prefix + request.identifier
        if request.operation == "cancel" {
            center.removePendingNotificationRequests(withIdentifiers: [identifier])
            center.removeDeliveredNotifications(withIdentifiers: [identifier])
            finish(token, MobileServiceResult(status: "completed", identifier: request.identifier))
            return
        }
        if request.operation == "status" {
            center.getNotificationSettings { settings in
                center.getPendingNotificationRequests { requests in
                    let scheduled = requests.contains { $0.identifier == identifier }
                    DispatchQueue.main.async {
                        self.finish(token, MobileServiceResult(status: "completed", message: scheduled ? "scheduled" : "not scheduled",
                                                              permission: Self.permission(settings.authorizationStatus), identifier: request.identifier))
                    }
                }
            }
            return
        }
        do { _ = try request.scheduledDate() }
        catch { finish(token, MobileServiceResult(status: "failed", message: "Choose a valid future local date and time.")); return }
        center.requestAuthorization(options: [.alert, .sound]) { granted, error in
            DispatchQueue.main.async {
                guard self.settlement.active == token else { return }
                guard error == nil, granted else {
                    self.finish(token, MobileServiceResult(status: error == nil ? "denied" : "failed", message: error?.localizedDescription,
                                                          permission: granted ? "granted" : "denied", identifier: request.identifier))
                    return
                }
                do {
                    let date = try request.scheduledDate()
                    let content = UNMutableNotificationContent()
                    content.title = request.title ?? ""
                    content.body = request.body ?? ""
                    content.sound = .default
                    content.userInfo = [MobileNotificationRouter.routeKey: request.route ?? ""]
                    var calendar = Calendar(identifier: .gregorian)
                    calendar.timeZone = .current
                    var components = calendar.dateComponents([.year, .month, .day, .hour, .minute], from: date)
                    components.calendar = calendar
                    components.timeZone = calendar.timeZone
                    let notification = UNNotificationRequest(identifier: identifier, content: content,
                        trigger: UNCalendarNotificationTrigger(dateMatching: components, repeats: false))
                    self.scheduling = true
                    center.add(notification) { error in
                        DispatchQueue.main.async {
                            guard self.settlement.active == token else { return }
                            if self.cancellingSchedule {
                                center.removePendingNotificationRequests(withIdentifiers: [identifier])
                                center.removeDeliveredNotifications(withIdentifiers: [identifier])
                                self.finish(token, MobileServiceResult(status: "cancelled", message: "Reminder scheduling cancelled.", identifier: request.identifier))
                                return
                            }
                            self.finish(token, MobileServiceResult(status: error == nil ? "completed" : "failed", message: error?.localizedDescription,
                                                                  permission: "granted", identifier: request.identifier))
                        }
                    }
                } catch {
                    self.finish(token, MobileServiceResult(status: "failed", message: "The chosen local time is no longer in the future."))
                }
            }
        }
    }

    private static func permission(_ status: UNAuthorizationStatus) -> String {
        switch status {
        case .authorized, .provisional, .ephemeral: return "granted"
        case .denied: return "denied"
        case .notDetermined: return "prompt"
        @unknown default: return "unsupported"
        }
    }

    deinit {
        if let directory { queue.async { try? FileManager.default.removeItem(at: directory) } }
        let presented = controller
        let recording = (presented as? UINavigationController)?.viewControllers.first as? MobileAudioRecorderController
        if Thread.isMainThread {
            recording?.shutdown()
            MobileForegroundDismissal.dismiss(presented) {}
        } else {
            DispatchQueue.main.async {
                recording?.shutdown()
                MobileForegroundDismissal.dismiss(presented) {}
            }
        }
    }
}

private final class MobileServiceDismissalDelegate: NSObject, UIAdaptivePresentationControllerDelegate {
    private let cancel: () -> Void
    init(cancel: @escaping () -> Void) { self.cancel = cancel }
    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) { cancel() }
}

final class MobileNotificationRouter: NSObject, UNUserNotificationCenterDelegate {
    static let shared = MobileNotificationRouter()
    static let prefix = "gofastr.notification."
    static let routeKey = "gofastr.route"
    private weak var previous: UNUserNotificationCenterDelegate?
    private var openRoute: ((String) -> Bool)?
    private var pending: String?
    private var observer: NSObjectProtocol?

    func install(_ openRoute: @escaping (String) -> Bool) {
        precondition(Thread.isMainThread)
        self.openRoute = openRoute
        let center = UNUserNotificationCenter.current()
        if center.delegate !== self { previous = center.delegate; center.delegate = self }
        if observer == nil {
            observer = NotificationCenter.default.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in self?.flush() }
        }
        flush()
    }

    func flush() {
        guard let route = pending, openRoute?(route) == true else { return }
        pending = nil
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                withCompletionHandler completionHandler: @escaping () -> Void) {
        guard response.notification.request.identifier.hasPrefix(Self.prefix),
              response.actionIdentifier == UNNotificationDefaultActionIdentifier,
              let route = response.notification.request.content.userInfo[Self.routeKey] as? String,
              MobileNotificationRequest.localRoute(route) else {
            if let previous, previous.responds(to: #selector(userNotificationCenter(_:didReceive:withCompletionHandler:))) {
                previous.userNotificationCenter?(center, didReceive: response, withCompletionHandler: completionHandler)
            } else { completionHandler() }
            return
        }
        DispatchQueue.main.async {
            self.pending = route
            self.flush()
            completionHandler()
        }
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        if notification.request.identifier.hasPrefix(Self.prefix) { completionHandler([.banner, .sound]) }
        else if let previous, previous.responds(to: #selector(userNotificationCenter(_:willPresent:withCompletionHandler:))) {
            previous.userNotificationCenter?(center, willPresent: notification, withCompletionHandler: completionHandler)
        } else { completionHandler([]) }
    }
}
