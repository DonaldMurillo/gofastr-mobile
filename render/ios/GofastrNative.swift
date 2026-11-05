import UIKit

public struct GofastrNativeBackend {
    let start: (String) throws -> Void
    let stop: () throws -> Void
    let root: () -> String
    let render: (String, String) throws -> String
    let dispatch: (String, String, String) throws -> String
    let command: ((String) throws -> String)?

    public init(
        start: @escaping (String) throws -> Void,
        stop: @escaping () throws -> Void,
        root: @escaping () -> String,
        render: @escaping (String, String) throws -> String,
        dispatch: @escaping (String, String, String) throws -> String,
        command: ((String) throws -> String)? = nil
    ) {
        self.start = start
        self.stop = stop
        self.root = root
        self.render = render
        self.dispatch = dispatch
        self.command = command
    }
}

struct MobileDraft: Decodable {
    var key: String
    let changed: String
    let restored: String
    let backLabel: String
    let title: String
    let message: String
    let keepEditing: String
    let keepDraft: String
    let discard: String
}

struct MobileForm: Decodable {
    let fields: [MobileField]
    let status: String?
    var draft: MobileDraft?
    let style: String?
    let detailsTitle: String?
}

struct MobileScreen: Decodable {
    var route: String
    let title: String
    let prompt: String?
    let largeTitle: Bool?
    let values: [String: String]?
    let actions: [MobileAction]?
    let list: MobileList?
    var form: MobileForm?
    let text: String?
    let filePreview: MobileFilePreview?
    var images: [MobileImage]?
    var attachments: [MobileAttachment]?
    let tabs: [MobileTab]?
    let activeTab: String?
    let requires: [MobileRequirement]?
    let live: MobileLive?
}

struct MobileNavigation: Decodable {
    let kind: String
    let route: String?
}

struct MobileResponse: Decodable {
    let version: Int
    let screen: MobileScreen?
    let navigation: MobileNavigation?
    let share: String?
    let haptic: String?
    let announcement: String?
    let clearDraft: Bool?
    let saved: MobileSaved?
    let presentation: MobilePresentation?
    let files: MobileFiles?
    let service: MobileServiceRequest?
}

struct MobileSaved: Decodable {
    let route: String
    let values: [String: String]
    let draftKey: String?
}

struct MobileEditor: Decodable {
    let baseline: [String: String]
    let values: [String: String]
    let dirty: Bool
    let pending: Bool
    let unknown: Bool
    let restored: Bool
    let draftStored: Bool
    let canLeave: Bool
    let canNavigate: Bool
    let fields: [String: String]?
    let message: String?
}

struct MobileFailure: Decodable, LocalizedError {
    let code: String
    let message: String
    let fields: [String: String]?

    var errorDescription: String? { message }
}

struct MobileReply: Decodable {
    let version: Int
    let connection: String?
    let instance: String?
    let revision: Int?
    let screen: MobileScreen?
    let response: MobileResponse?
    let editor: MobileEditor?
    let error: MobileFailure?
    let effectIds: [String: String]?
    let presentationId: String?
    let fileId: String?
    let serviceId: String?
    let dataVersion: String?
    let changed: Bool?
}

final class MobileHost {
    let queue = DispatchQueue(label: "gofastr.native.commands", qos: .userInitiated)
    let backend: GofastrNativeBackend
    let foreground = MobileForegroundCoordinator()
    var navigationChanged: (() -> Void)?
    var openServiceRoute: ((String) -> Bool)?
    var mutationRevision = 0
    var active = true
    private let commandSequencer = MobileCommandSequencer()
    private let workers: OperationQueue = {
        let workers = OperationQueue()
        workers.maxConcurrentOperationCount = 4
        workers.qualityOfService = .userInitiated
        return workers
    }()
    private var connection = ""
    private let watchWorkers: OperationQueue = {
        let queue = OperationQueue()
        queue.maxConcurrentOperationCount = 1
        queue.qualityOfService = .utility
        return queue
    }()

    init(_ backend: GofastrNativeBackend) {
        self.backend = backend
    }

    static func failure(_ error: NSError?) -> Error {
        error ?? NSError(domain: "GoFastrMobile", code: 1, userInfo: [NSLocalizedDescriptionKey: "Couldn’t finish. Please try again."])
    }

    func call(_ command: @escaping () throws -> String, completion: @escaping (Result<MobileResponse, Error>) -> Void) {
        queue.async {
            let result = Result { () throws -> MobileResponse in
                let encoded = try command()
                let response = try JSONDecoder().decode(MobileResponse.self, from: Data(encoded.utf8))
                guard response.version == 1 else { throw Self.failure(nil) }
                if self.backend.command == nil && response.presentation != nil {
                    throw MobileFailure(code: "unsupported", message: "This backend does not support presentation/v1. A versioned mobile command backend is required.", fields: nil)
                }
                if self.backend.command == nil && response.files != nil {
                    throw MobileFailure(code: "unsupported", message: "This backend does not support files/v1. A versioned mobile command backend is required.", fields: nil)
                }
                if self.backend.command == nil && response.service != nil {
                    throw MobileFailure(code: "unsupported", message: "Device services require a versioned mobile command backend.", fields: nil)
                }
                return response
            }
            DispatchQueue.main.async { completion(result) }
        }
    }

    func render(_ route: String, values: [String: String], completion: @escaping (Result<MobileResponse, Error>) -> Void) {
        call({
            let payload = String(decoding: try JSONEncoder().encode(values), as: UTF8.self)
            return try self.backend.render(route, payload)
        }, completion: completion)
    }

    func dispatch(_ route: String, action: String, values: [String: String], completion: @escaping (Result<MobileResponse, Error>) -> Void) {
        call({
            let payload = String(decoding: try JSONEncoder().encode(values), as: UTF8.self)
            return try self.backend.dispatch(route, action, payload)
        }, completion: completion)
    }

    @discardableResult
    func command(_ fields: [String: Any], completion: @escaping (Result<MobileReply, Error>) -> Void) -> Operation? {
        var payload = fields
        payload["version"] = 1
        payload["connection"] = connection
        let outgoing = payload
        let operation = BlockOperation()
        operation.addExecutionBlock { [weak operation] in
            guard operation?.isCancelled == false else { return }
            let result = Result { () throws -> MobileReply in
                let encoded = String(decoding: try JSONSerialization.data(withJSONObject: outgoing), as: UTF8.self)
                guard let command = self.backend.command else { throw Self.failure(nil) }
                let response = try command(encoded)
                let reply = try JSONDecoder().decode(MobileReply.self, from: Data(response.utf8))
                guard reply.version == 1 else { throw Self.failure(nil) }
                return reply
            }
            DispatchQueue.main.async { completion(result) }
        }
        commandSequencer.prepare(operation, kind: fields["kind"] as? String ?? "", instance: fields["instance"] as? String)
        (fields["kind"] as? String == "watch" ? watchWorkers : workers).addOperation(operation)
        return operation
    }

    func connect(completion: @escaping (Result<MobileReply, Error>) -> Void) {
        guard Thread.isMainThread else {
            DispatchQueue.main.async { self.connect(completion: completion) }
            return
        }
        let capabilities = [
            "navigation/v1": "available", "editor/v1": "available", "drafts/v1": "available",
            "share/v1": "available", "haptics/v1": "available", "sheets/v1": "available", "announcement/v1": "available",
            "presentation/v1": "available", "files/v1": "available", "photos/v1": "available",
            "camera/v1": MobilePhotoPlatform.cameraAvailable() ? "available" : "unavailable", "live/v1": "available"
        ].merging(GofastrServiceRunner.capabilities()) { _, service in service }
        command(["kind": "hello", "hello": ["minimum": 1, "maximum": 1, "renderer": "uikit", "capabilities": capabilities]]) { result in
            if case .success(let reply) = result { self.connection = reply.connection ?? "" }
            completion(result)
        }
    }

    func open(_ route: String, values: [String: String] = [:], completion: @escaping (Result<MobileReply, Error>) -> Void) {
        command(["kind": "open", "route": route, "values": values], completion: completion)
    }
}

public final class GofastrNativeRenderer {
    private let host: MobileHost
    private weak var window: UIWindow?
    private var starting = true
    private var stopped = false
    private var queuedRoutes: [(route: String, draftKey: String?)] = []
    private var lifecycleObservers: [NSObjectProtocol] = []
    private var checkpointBatches: [UUID: MobileCheckpointBatch] = [:]
    private var backgroundTask: UIBackgroundTaskIdentifier = .invalid

    public init(backend: GofastrNativeBackend) {
        host = MobileHost(backend)
        host.openServiceRoute = { [weak self] route in
            guard let self, let url = URL(string: route) else { return false }
            return self.openURL(url)
        }
        MobileNotificationRouter.shared.install { [weak self] route in self?.host.openServiceRoute?(route) ?? false }
    }

    public func makeWindow() -> UIWindow {
        let window = UIWindow(frame: UIScreen.main.bounds)
        window.tintColor = MobileAppearance.accent
        self.window = window
        let launch = UIViewController()
        launch.view.backgroundColor = .systemBackground
        let spinner = UIActivityIndicatorView(style: .large)
        spinner.translatesAutoresizingMaskIntoConstraints = false
        launch.view.addSubview(spinner)
        NSLayoutConstraint.activate([spinner.centerXAnchor.constraint(equalTo: launch.view.centerXAnchor), spinner.centerYAnchor.constraint(equalTo: launch.view.centerYAnchor)])
        spinner.startAnimating()
        let navigation = UINavigationController(rootViewController: launch)
        navigation.navigationBar.prefersLargeTitles = true
        window.rootViewController = navigation
        window.makeKeyAndVisible()
        MobileNotificationRouter.shared.flush()
        let directory = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("app", isDirectory: true).path
        let host = self.host
        host.call({
            try host.backend.start(directory)
            return try host.backend.render(host.backend.root(), "{}")
        }) { result in
            spinner.stopAnimating()
            switch result {
            case .success(let response):
                guard let screen = response.screen else { launch.showFailure(MobileHost.failure(nil)); return }
                if host.backend.command == nil {
                    navigation.setViewControllers([NativeScreenController(screen, host: host)], animated: false)
                } else {
                    host.connect { result in
                        guard case .success(let hello) = result, hello.error == nil else { launch.showFailure(MobileHost.failure(nil)); return }
                        host.open(host.backend.root()) { result in
                            guard case .success(let reply) = result, reply.error == nil, let model = reply.screen else { launch.showFailure(MobileHost.failure(nil)); return }
                            if model.tabs?.isEmpty == false {
                                let tabs = MobileTabController(screen: model, host: host, reply: reply)
                                window.rootViewController = tabs
                                tabs.ready = { [weak self] in self?.finishStartup() }
                                tabs.restore()
                            } else {
                                navigation.setViewControllers([NativeScreenController(model, host: host, reply: reply)], animated: false)
                                self.finishStartup()
                            }
                        }
                    }
                }
            case .failure(let error): launch.showFailure(error)
            }
        }
        for name in [UIApplication.willResignActiveNotification, UIApplication.didEnterBackgroundNotification] {
            lifecycleObservers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in self?.suspend() })
        }
        lifecycleObservers.append(NotificationCenter.default.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in self?.resume() })
        lifecycleObservers.append(NotificationCenter.default.addObserver(forName: UIApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in self?.checkpoint() })
        return window
    }

    @discardableResult
    public func openURL(_ url: URL) -> Bool {
        guard Thread.isMainThread else { return DispatchQueue.main.sync { self.openURL(url) } }
        guard !stopped, window != nil, host.backend.command != nil,
              let route = MobileLocalRoute.normalize(url), queuedRoutes.isEmpty else { return false }
        if !starting {
            if let tabs = window?.rootViewController as? MobileTabController {
                guard tabs.canOpenRoute else { return false }
            } else if let navigation = window?.rootViewController as? UINavigationController {
                guard navigation.presentedViewController == nil, let screen = navigation.topViewController as? NativeScreenController,
                      !screen.navigationBlocked, screen.presentedViewController == nil else { return false }
            } else { return false }
        }
        let cameraAvailable = MobilePhotoPlatform.cameraAvailable()
        let target = host.queue.sync { () -> MobileScreen? in
            guard let encoded = try? self.host.backend.render(route, "{}"),
                  let response = try? JSONDecoder().decode(MobileResponse.self, from: Data(encoded.utf8)),
                  response.version == 1, let screen = response.screen, response.navigation == nil,
                  response.presentation == nil, response.files == nil, response.service == nil, response.share == nil else { return nil }
            let services = GofastrServiceRunner.capabilities().filter { $0.value == "available" }.map(\.key)
            let available = Set(["navigation/v1", "editor/v1", "drafts/v1", "share/v1", "haptics/v1", "sheets/v1", "announcement/v1", "presentation/v1", "files/v1", "photos/v1", "live/v1"] + services)
            return (screen.requires ?? []).allSatisfy { available.contains($0.capability) || ($0.capability == "camera/v1" && cameraAvailable) } ? screen : nil
        }
        guard let target else { return false }
        if starting { queuedRoutes.append((route, target.form?.draft?.key)) }
        else { openRoute(route, draftKey: target.form?.draft?.key) }
        return true
    }

    public func checkpoint() {
        checkpoint { _ in }
    }

    public func checkpoint(completion: @escaping (Bool) -> Void) {
        guard Thread.isMainThread else { DispatchQueue.main.async { self.checkpoint(completion: completion) }; return }
        guard !stopped else { completion(false); return }
        (window?.rootViewController as? MobileTabController)?.persist()
        let controllers = retainedScreens.filter { $0.isViewLoaded }
        guard !controllers.isEmpty else { completion(true); return }
        let identifier = UUID()
        let batch = MobileCheckpointBatch(count: controllers.count) { [weak self] stored in
            if let self {
                self.checkpointBatches.removeValue(forKey: identifier)
                if self.checkpointBatches.isEmpty { self.endBackgroundTask() }
            }
            completion(stored)
        }
        checkpointBatches[identifier] = batch
        if backgroundTask == .invalid {
            backgroundTask = UIApplication.shared.beginBackgroundTask(withName: "GoFastr draft checkpoint") { [weak self] in
                guard Thread.isMainThread else { DispatchQueue.main.async { self?.expireCheckpoints() }; return }
                self?.expireCheckpoints()
            }
        }
        controllers.forEach { $0.checkpointForBackground(completion: batch.complete) }
        if backgroundTask == .invalid { batch.expire() }
    }

    public func suspend() {
        guard Thread.isMainThread else { DispatchQueue.main.async { self.suspend() }; return }
        guard !stopped else { return }
        host.active = false
        retainedScreens.forEach { $0.suspendReads() }
        checkpoint()
    }

    public func resume() {
        guard Thread.isMainThread else { DispatchQueue.main.async { self.resume() }; return }
        guard !stopped, UIApplication.shared.applicationState == .active else { return }
        host.active = true
        retainedScreens.forEach { $0.resumeVisibleReads() }
    }

    private var retainedScreens: [NativeScreenController] {
        if let tabs = window?.rootViewController as? MobileTabController { return tabs.retainedScreens }
        return (window?.rootViewController as? UINavigationController)?.viewControllers.compactMap { $0 as? NativeScreenController } ?? []
    }

    private func endBackgroundTask() {
        let task = backgroundTask
        backgroundTask = .invalid
        if task != .invalid { UIApplication.shared.endBackgroundTask(task) }
    }

    private func expireCheckpoints() {
        let batches = Array(checkpointBatches.values)
        checkpointBatches.removeAll()
        endBackgroundTask()
        batches.forEach { $0.expire() }
    }

    private func finishStartup() {
        starting = false
        let route = queuedRoutes.last
        queuedRoutes.removeAll()
        if let target = route { openRoute(target.route, draftKey: target.draftKey) }
    }

    private func openRoute(_ route: String, draftKey: String?) {
        guard !stopped else { return }
        if let tabs = window?.rootViewController as? MobileTabController { tabs.open(route, draftKey: draftKey); return }
        guard let navigation = window?.rootViewController as? UINavigationController, let source = navigation.topViewController as? NativeScreenController else { return }
        source.prepareForNavigation { [weak self] safe in
            guard let self, safe else { return }
            if let target = navigation.viewControllers.first(where: {
                guard let screen = $0 as? NativeScreenController else { return false }
                return screen.route == route || (draftKey != nil && screen.draftKey == draftKey)
            }) {
                guard let index = navigation.viewControllers.firstIndex(of: target),
                      navigation.viewControllers.dropFirst(index + 1).allSatisfy({ ($0 as? NativeScreenController)?.navigationBlocked != true }) else { return }
                navigation.viewControllers.dropFirst(index + 1).forEach { ($0 as? NativeScreenController)?.closeSession() }
                navigation.popToViewController(target, animated: true)
                return
            }
            source.setNavigationInFlight(true)
            self.host.open(route) { result in
                source.setNavigationInFlight(false)
                guard source.navigationStillSafe else {
                    if case .success(let reply) = result, let instance = reply.instance { self.host.command(["kind": "close", "instance": instance]) { _ in } }
                    return
                }
                guard case .success(let reply) = result, reply.error == nil, let model = reply.screen else { source.showFailure(MobileHost.failure(nil)); return }
                navigation.pushViewController(NativeScreenController(model, host: self.host, reply: reply), animated: true)
            }
        }
    }

    public func stop() {
        guard Thread.isMainThread else { DispatchQueue.main.async { self.stop() }; return }
        stopped = true
        host.active = false
        retainedScreens.forEach { $0.suspendReads() }
        expireCheckpoints()
        host.foreground.cancel()
        queuedRoutes.removeAll()
        lifecycleObservers.forEach(NotificationCenter.default.removeObserver)
        lifecycleObservers.removeAll()
        host.queue.async {
            try? self.host.backend.stop()
        }
    }

    deinit {
        lifecycleObservers.forEach(NotificationCenter.default.removeObserver)
        let task = backgroundTask
        if task != .invalid {
            if Thread.isMainThread { UIApplication.shared.endBackgroundTask(task) }
            else { DispatchQueue.main.async { UIApplication.shared.endBackgroundTask(task) } }
        }
    }
}

final class NativeScreenController: UIViewController, UITableViewDataSource, UITableViewDelegate, UISearchResultsUpdating, UITextFieldDelegate, UITextViewDelegate {
    private var screen: MobileScreen
    private let host: MobileHost
    private var table: UITableView?
    private var search: UISearchController?
    private var pendingSearch: DispatchWorkItem?
    private var revision = 0
    private var appeared = false
    private var busy = false
    private var inputs: [String: UIView] = [:]
    private var baseline: [String: String] = [:]
    private var instance: String?
    private var editor: MobileEditor?
    private var bufferRevision = 0
    private var pendingCheckpoint: DispatchWorkItem?
    private var checkpointFence = MobileCheckpointFence()
    private var checkpointDeferral = MobileCheckpointDeferral()
    private var observedEffects = Set<String>()
    private var observedServices = Set<String>()
    private let status = UILabel()
    private var contentScroll: UIScrollView?
    private var imageGallery: MobileImageGallery?
    private var attachmentList: MobileAttachmentList?
    private var fieldErrors: [String: UILabel] = [:]
    private var detailsGroup: MobileDisclosureGroup?
    private var listQuery: [String: String] = [:]
    private var selection: MobileListSelectionState
    private var selectionMode = false
    private var listLoading = false
    private var listNeedsRefresh = false
    private var liveVisible = false
    private var watchPaused = false
    private var watchGeneration = MobileReadGeneration()
    private var liveReadRecovery = MobileReadRecovery()
    private var watchOperation: Operation?
    private var liveVersion: String?
    private var readMessage: String?
    private var readFailed = false
    private var filterHeader: UIStackView?
    private var loadMore: UIButton?
    private var actionValues: [String: String] = [:]
    private var operationUnconfirmed = false
    private var renderedMutationRevision: Int
    private var navigationPermit = -1
    var skipRefreshOnAppear = false
    var route: String { screen.route }
    var draftKey: String? { screen.form?.draft?.key }
    var navigationBlocked: Bool { !host.active || busy || operationUnconfirmed || host.foreground.isActive || editor?.pending == true || editor?.unknown == true }
    var hasUncertainEditor: Bool { operationUnconfirmed || editor?.pending == true || editor?.unknown == true }
    var navigationStillSafe: Bool { !navigationBlocked && navigationPermit == bufferRevision }
    private var dirty: Bool { screen.form != nil && fieldValues != baseline }

    init(_ screen: MobileScreen, host: MobileHost, reply: MobileReply? = nil) {
        self.screen = screen
        self.host = host
        instance = reply?.instance
        editor = reply?.editor
        bufferRevision = reply?.revision ?? 0
        selection = MobileListSelectionState(screen.list?.selection)
        selectionMode = !selection.selected.isEmpty
        listQuery = screen.values ?? [:]
        renderedMutationRevision = host.mutationRevision
        liveVersion = reply?.dataVersion ?? screen.live?.version
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.tintColor = MobileAppearance.accent
        view.backgroundColor = screen.list == nil ? .systemBackground : .systemGroupedBackground
        applyChrome()
        if let list = screen.list { buildList(list) }
        else if let form = screen.form { buildForm(form) }
        else if let preview = screen.filePreview { buildFilePreview(preview) }
        else if let content = screen.text { buildText(content) }
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        liveReadRecovery.reset()
        if appeared && screen.list != nil && (!skipRefreshOnAppear || listNeedsRefresh || renderedMutationRevision != host.mutationRevision) { refresh() }
        else if appeared { refreshImages() }
        skipRefreshOnAppear = false
        appeared = true
        updateBack()
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        liveVisible = true
        startWatch()
        MobileNotificationRouter.shared.flush()
        host.navigationChanged?()
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        sizeListHeader()
        sizeListFooter()
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        liveVisible = false
        cancelWatch()
        attachmentList?.stop()
        pendingSearch?.cancel()
        pendingCheckpoint?.cancel()
        revision += 1
        listLoading = false
        loadMore?.isEnabled = !busy
        table?.refreshControl?.endRefreshing()
    }

    override func viewDidDisappear(_ animated: Bool) {
        super.viewDidDisappear(animated)
        if isMovingFromParent || navigationController?.isBeingDismissed == true || isBeingDismissed {
            closeSession()
        }
    }

    private func applyChrome() {
        title = screen.title
        navigationItem.prompt = screen.prompt
        navigationItem.largeTitleDisplayMode = screen.largeTitle == true ? .always : .never
        let plan = MobileToolbarPlan(screen.actions ?? [])
        func symbol(_ action: MobileAction) -> UIImage? {
            let symbols = ["compose": "square.and.pencil", "info": "info.circle", "share": "square.and.arrow.up"]
            return action.icon.flatMap { symbols[$0] }.flatMap { UIImage(systemName: $0) }
        }
        func item(_ action: MobileAction) -> UIBarButtonItem {
            let image = symbol(action)
            let button = UIBarButtonItem(title: image == nil ? action.label : nil,
                image: image,
                primaryAction: UIAction { [weak self] _ in self?.perform(action.id) })
            button.style = action.primary == true ? .done : .plain
            button.accessibilityLabel = action.label
            button.accessibilityIdentifier = "native.action.\(action.id)"
            return button
        }
        var trailing: [UIBarButtonItem] = []
        if let primary = plan.primary { trailing.append(item(primary)) }
        if !plan.overflow.isEmpty {
            let actions = plan.overflow.map { action in
                UIAction(title: action.label, image: symbol(action), identifier: UIAction.Identifier("native.action.\(action.id)")) { [weak self] _ in self?.perform(action.id) }
            }
            let more = UIBarButtonItem(image: UIImage(systemName: "ellipsis"), menu: UIMenu(children: actions))
            more.accessibilityLabel = "More actions"
            more.accessibilityIdentifier = "native.action.more"
            trailing.append(more)
        }
        navigationItem.leftBarButtonItems = plan.leading.map(item)
        if screen.list?.selection != nil {
            let select = UIBarButtonItem(title: selectionMode ? "Done" : "Select", primaryAction: UIAction { [weak self] _ in
                guard let self, !self.navigationBlocked else { return }
                self.selectionMode.toggle()
                self.applyChrome()
                self.showList()
            })
            select.accessibilityIdentifier = "native.selection-mode"
            navigationItem.leftBarButtonItems = (navigationItem.leftBarButtonItems ?? []) + [select]
        }
        navigationItem.rightBarButtonItems = trailing
    }

    private func buildList(_ list: MobileList) {
        let table = UITableView(frame: .zero, style: .insetGrouped)
        table.backgroundColor = .systemGroupedBackground
        table.translatesAutoresizingMaskIntoConstraints = false
        table.dataSource = self
        table.delegate = self
        table.rowHeight = UITableView.automaticDimension
        table.estimatedRowHeight = 84
        table.keyboardDismissMode = .onDrag
        table.register(UITableViewCell.self, forCellReuseIdentifier: "row")
        view.addSubview(table)
        NSLayoutConstraint.activate([table.topAnchor.constraint(equalTo: view.topAnchor), table.bottomAnchor.constraint(equalTo: view.bottomAnchor), table.leadingAnchor.constraint(equalTo: view.leadingAnchor), table.trailingAnchor.constraint(equalTo: view.trailingAnchor)])
        self.table = table
        if let filters = list.filters, !filters.isEmpty {
            let header = UIStackView()
            header.axis = .vertical
            header.spacing = 10
            header.isLayoutMarginsRelativeArrangement = true
            header.directionalLayoutMargins = NSDirectionalEdgeInsets(top: 12, leading: 20, bottom: 12, trailing: 20)
            let disclosure = MobileDisclosureGroup("Filters", identifier: "native.filters")
            disclosure.layoutChanged = { [weak self] in self?.sizeListHeader() }
            for field in filters { addField(field, to: disclosure.content, filter: true) }
            header.addArrangedSubview(disclosure)
            configureStatus()
            header.addArrangedSubview(status)
            filterHeader = header
            table.tableHeaderView = header
        } else {
            configureStatus()
        }
        if list.refreshable == true {
            table.refreshControl = UIRefreshControl()
            table.refreshControl?.addTarget(self, action: #selector(refresh), for: .valueChanged)
        }
        if let key = list.searchKey {
            let search = UISearchController(searchResultsController: nil)
            search.obscuresBackgroundDuringPresentation = false
            search.searchBar.placeholder = list.searchPlaceholder
            search.searchBar.text = screen.values?[key]
            search.searchBar.accessibilityIdentifier = "native.search"
            search.searchResultsUpdater = self
            navigationItem.searchController = search
            navigationItem.hidesSearchBarWhenScrolling = false
            definesPresentationContext = true
            self.search = search
        }
        showList()
    }

    private func showList() {
        let footer = UIStackView()
        footer.axis = .vertical
        footer.spacing = 8
        footer.isLayoutMarginsRelativeArrangement = true
        footer.directionalLayoutMargins = NSDirectionalEdgeInsets(top: 8, leading: 20, bottom: 16, trailing: 20)
        let activity = UIStackView()
        activity.axis = readFailed || traitCollection.preferredContentSizeCategory.isAccessibilityCategory ? .vertical : .horizontal
        activity.alignment = .center
        activity.spacing = 12
        if let readMessage {
            let label = UILabel()
            label.text = readMessage
            label.numberOfLines = 0
            label.font = .preferredFont(forTextStyle: .footnote)
            label.adjustsFontForContentSizeCategory = true
            label.textColor = readFailed ? .systemRed : .secondaryLabel
            label.accessibilityIdentifier = "native.list.status"
            activity.addArrangedSubview(label)
        }
        if listLoading || (screen.live != nil && !watchPaused && !readFailed) {
            let cancel = UIButton(type: .system)
            cancel.setTitle(listLoading ? "Cancel loading" : "Pause", for: .normal)
            cancel.accessibilityLabel = listLoading ? "Cancel loading" : "Pause live updates"
            cancel.titleLabel?.font = .preferredFont(forTextStyle: .footnote)
            cancel.titleLabel?.adjustsFontForContentSizeCategory = true
            cancel.setContentHuggingPriority(.required, for: .horizontal)
            cancel.accessibilityIdentifier = "native.list.cancel"
            cancel.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
            cancel.addAction(UIAction { [weak self] _ in self?.cancelListRead() }, for: .touchUpInside)
            activity.addArrangedSubview(cancel)
        } else if readFailed || watchPaused {
            let retry = UIButton(type: .system)
            retry.setTitle("Retry", for: .normal)
            retry.accessibilityIdentifier = "native.list.retry"
            retry.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
            retry.addAction(UIAction { [weak self] _ in self?.refresh() }, for: .touchUpInside)
            activity.addArrangedSubview(retry)
        }
        if !activity.arrangedSubviews.isEmpty { footer.addArrangedSubview(activity) }
        if let paging = screen.list?.paging, let next = paging.next, !next.isEmpty {
            let button = loadMore ?? UIButton(type: .system)
            button.setTitle(paging.label?.isEmpty == false ? paging.label : "Load more", for: .normal)
            button.titleLabel?.font = .preferredFont(forTextStyle: .body)
            button.titleLabel?.adjustsFontForContentSizeCategory = true
            button.titleLabel?.numberOfLines = 0
            button.accessibilityIdentifier = "native.load-more"
            if loadMore == nil {
                button.addTarget(self, action: #selector(nextPage), for: .touchUpInside)
                button.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
            }
            button.isEnabled = !busy && !listLoading
            loadMore = button
            footer.addArrangedSubview(button)
        }
        table?.backgroundView = nil
        table?.tableFooterView = footer.arrangedSubviews.isEmpty ? nil : footer
        table?.reloadData()
        sizeListHeader()
        sizeListFooter()
    }

    private func sizeListFooter() {
        guard let table, let footer = table.tableFooterView, table.bounds.width > 0 else { return }
        let size = footer.systemLayoutSizeFitting(CGSize(width: table.bounds.width, height: 0), withHorizontalFittingPriority: .required, verticalFittingPriority: .fittingSizeLevel)
        if footer.frame.width != table.bounds.width || abs(footer.frame.height - size.height) > 0.5 {
            footer.frame = CGRect(x: 0, y: 0, width: table.bounds.width, height: size.height)
            table.tableFooterView = footer
        }
    }

    private func sizeListHeader() {
        guard let table, let header = filterHeader, table.bounds.width > 0 else { return }
        let size = header.systemLayoutSizeFitting(CGSize(width: table.bounds.width, height: 0), withHorizontalFittingPriority: .required, verticalFittingPriority: .fittingSizeLevel)
        if header.frame.width != table.bounds.width || abs(header.frame.height - size.height) > 0.5 {
            header.frame = CGRect(x: 0, y: 0, width: table.bounds.width, height: size.height)
            table.tableHeaderView = header
        }
    }

    @objc private func nextPage() {
        guard !navigationBlocked, !listLoading, let paging = screen.list?.paging, let next = paging.next, !next.isEmpty else { return }
        listQuery[paging.key] = next
        refresh()
    }

    private func queryChanged() {
        guard !busy, !host.foreground.isActive else { return }
        if let paging = screen.list?.paging { listQuery[paging.key] = "" }
        selection.reset()
        listNeedsRefresh = true
        bufferRevision += 1
        updateSearchResults(for: search ?? UISearchController(searchResultsController: nil))
        showList()
    }

    @objc private func refresh() {
        liveReadRecovery.reset()
        reloadList()
    }

    private func reloadList() {
        guard host.active, UIApplication.shared.applicationState == .active else { listNeedsRefresh = true; return }
        guard !busy, !host.foreground.isActive else { table?.refreshControl?.endRefreshing(); return }
        cancelWatch()
        watchPaused = false
        readFailed = false
        readMessage = "Loading…"
        pendingSearch?.cancel()
        pendingSearch = nil
        revision += 1
        let current = revision
        listLoading = true
        listNeedsRefresh = true
        loadMore?.isEnabled = false
        let anchor = listScrollAnchor()
        showList()
        restoreListScroll(anchor)
        let queryValues = screen.list?.queryValues(values) ?? [:]
        if instance != nil {
            host.open(screen.route, values: queryValues) { [weak self] result in
                guard let self else { return }
                guard current == self.revision else {
                    if case .success(let reply) = result, let instance = reply.instance { self.host.command(["kind": "close", "instance": instance]) { _ in } }
                    return
                }
                self.listLoading = false
                self.loadMore?.isEnabled = !self.busy
                self.table?.refreshControl?.endRefreshing()
                if case .success(let reply) = result, let error = reply.error { self.listReadFailed(error.message); return }
                guard case .success(let reply) = result, reply.error == nil, let model = reply.screen, model.list != nil else {
                    self.listReadFailed("The list could not be refreshed. Retry this read; no action was repeated."); return
                }
                self.send("close") { _ in }
                self.instance = reply.instance
                self.screen = model
                self.liveVersion = reply.dataVersion ?? model.live?.version
                self.listNeedsRefresh = false
                self.renderedMutationRevision = self.host.mutationRevision
                self.listQuery = model.list?.refreshedValues(queryValues, authoritative: model.values) ?? [:]
                self.selection.reconcile(model.list?.rows ?? [])
                self.configureInputs()
                self.applyChrome()
                self.readMessage = model.live == nil ? nil : "Live updates"
                self.showList()
                self.restoreListScroll(anchor)
                self.startWatch()
            }
            return
        }
        host.render(screen.route, values: queryValues) { [weak self] result in
            guard let self, current == self.revision else { return }
            self.listLoading = false
            self.loadMore?.isEnabled = !self.busy
            self.table?.refreshControl?.endRefreshing()
            switch result {
            case .success(let response):
                guard let model = response.screen, model.list != nil else { self.listReadFailed("The list could not be loaded."); return }
                self.screen = model
                self.liveVersion = model.live?.version
                self.listNeedsRefresh = false
                self.renderedMutationRevision = self.host.mutationRevision
                self.listQuery = model.list?.refreshedValues(queryValues, authoritative: model.values) ?? [:]
                self.selection.reconcile(model.list?.rows ?? [])
                self.configureInputs()
                self.applyChrome()
                self.readMessage = nil
                self.showList()
                self.restoreListScroll(anchor)
            case .failure(let error): self.listReadFailed(error.localizedDescription)
            }
        }
    }

    private func listReadFailed(_ message: String) {
        listLoading = false
        readFailed = true
        readMessage = message
        table?.refreshControl?.endRefreshing()
        let anchor = listScrollAnchor()
        showList()
        restoreListScroll(anchor)
        UIAccessibility.post(notification: .announcement, argument: message)
    }

    private func cancelListRead() {
        guard screen.list != nil, !busy, !host.foreground.isActive else { return }
        pendingSearch?.cancel()
        pendingSearch = nil
        revision += 1
        listLoading = false
        watchPaused = true
        cancelWatch()
        readFailed = false
        readMessage = "Read cancelled. Existing items and selection are unchanged."
        table?.refreshControl?.endRefreshing()
        let anchor = listScrollAnchor()
        showList()
        restoreListScroll(anchor)
    }

    private func cancelWatch() {
        watchGeneration.cancel()
        guard let operation = watchOperation else { return }
        watchOperation = nil
        operation.cancel()
        if operation.isExecuting, let instance {
            host.command(["kind": "checkpoint", "instance": instance, "revision": bufferRevision,
                          "values": screen.list?.queryValues(values) ?? [:]]) { _ in }
        }
    }

    private func startWatch() {
        guard liveVisible, host.active, UIApplication.shared.applicationState == .active, !watchPaused, !readFailed,
              !busy, !listLoading, !hasUncertainEditor, !host.foreground.isActive, presentedViewController == nil,
              screen.form == nil, let list = screen.list, screen.live != nil,
              let instance, let version = liveVersion, !version.isEmpty, watchOperation == nil else { return }
        let token = watchGeneration.begin()
        let currentRevision = revision
        let query = list.queryValues(values)
        watchOperation = host.command(["kind": "watch", "instance": instance, "revision": bufferRevision,
                                       "values": query, "dataVersion": version]) { [weak self] result in
            guard let self, self.watchGeneration.accepts(token), self.instance == instance,
                  self.liveVisible, self.revision == currentRevision else { return }
            self.watchOperation = nil
            switch result {
            case .failure:
                self.listReadFailed("Live updates are unavailable. Retry to refresh the current query.")
            case .success(let reply):
                if let error = reply.error {
                    if self.liveReadRecovery.recover(error.code) {
                        self.reloadList()
                        return
                    }
                    if self.liveReadRecovery.retry(error.code) {
                        DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(150)) { [weak self] in
                            guard let self, self.watchGeneration.accepts(token) else { return }
                            self.startWatch()
                        }
                        return
                    }
                    self.listReadFailed(error.code == "stale" || error.code == "expired" ? "This list changed. Retry to load current items." : error.message)
                    return
                }
                self.liveReadRecovery.reset()
                if reply.changed == true { self.refresh() }
                else {
                    self.liveVersion = reply.dataVersion ?? version
                    DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(150)) { [weak self] in
                        guard let self, self.watchGeneration.accepts(token) else { return }
                        self.startWatch()
                    }
                }
            }
        }
    }

    private func listScrollAnchor() -> (String?, CGFloat, CGPoint) {
        guard let table else { return (nil, 0, .zero) }
        let index = table.indexPathsForVisibleRows?.first
        let identifier = index.flatMap { listRow(at: $0)?.id }
        let offset = index.map { table.contentOffset.y - table.rectForRow(at: $0).minY } ?? 0
        return (identifier, offset, table.contentOffset)
    }

    private func restoreListScroll(_ anchor: (String?, CGFloat, CGPoint)) {
        guard let table else { return }
        table.layoutIfNeeded()
        var position = anchor.2
        if let identifier = anchor.0, let index = screen.list?.rows.firstIndex(where: { $0.id == identifier }) {
            position.y = table.rectForRow(at: IndexPath(row: index, section: 0)).minY + anchor.1
        }
        position.y = max(-table.adjustedContentInset.top, min(position.y, max(-table.adjustedContentInset.top, table.contentSize.height - table.bounds.height + table.adjustedContentInset.bottom)))
        table.setContentOffset(position, animated: false)
    }

    func updateSearchResults(for searchController: UISearchController) {
        cancelWatch()
        if let key = screen.list?.searchKey, searchController === search {
            let query = searchController.searchBar.text ?? ""
            if (listQuery[key] ?? "") != query {
                listQuery[key] = query
                if let paging = screen.list?.paging { listQuery[paging.key] = "" }
                selection.reset()
                listNeedsRefresh = true
                bufferRevision += 1
            }
        }
        pendingSearch?.cancel()
        revision += 1
        let task = DispatchWorkItem { [weak self] in self?.refresh() }
        pendingSearch = task
        DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(180), execute: task)
    }

    func tableView(_ tableView: UITableView, numberOfRowsInSection section: Int) -> Int { screen.list.map { max(1, $0.rows.count) } ?? 0 }

    private func listRow(at indexPath: IndexPath) -> MobileRow? {
        guard let rows = screen.list?.rows, rows.indices.contains(indexPath.row) else { return nil }
        return rows[indexPath.row]
    }

    func tableView(_ tableView: UITableView, cellForRowAt indexPath: IndexPath) -> UITableViewCell {
        if screen.list?.rows.isEmpty == true {
            let cell = UITableViewCell(style: .default, reuseIdentifier: nil)
            var content = cell.defaultContentConfiguration()
            content.text = screen.list?.empty
            content.textProperties.font = .preferredFont(forTextStyle: .body)
            content.textProperties.color = .secondaryLabel
            content.textProperties.numberOfLines = 0
            content.textProperties.alignment = .center
            cell.contentConfiguration = content
            cell.selectionStyle = .none
            cell.accessibilityIdentifier = "native.list-empty"
            return cell
        }
        let cell = tableView.dequeueReusableCell(withIdentifier: "row", for: indexPath)
        guard let row = listRow(at: indexPath) else { return cell }
        var content = cell.defaultContentConfiguration()
        content.text = row.title
        content.secondaryText = row.subtitle
        if let metadata = row.metadata, !metadata.isEmpty {
            let detail = NSMutableAttributedString(string: metadata, attributes: [.font: UIFont.preferredFont(forTextStyle: .caption1), .foregroundColor: MobileAppearance.accent])
            if let subtitle = row.subtitle, !subtitle.isEmpty {
                detail.append(NSAttributedString(string: "\n" + subtitle, attributes: [.font: UIFont.preferredFont(forTextStyle: .subheadline), .foregroundColor: UIColor.secondaryLabel]))
            }
            content.secondaryAttributedText = detail
        }
        if let icon = row.icon {
            content.image = UIImage(systemName: MobileAppearance.fileSymbol(icon))
            content.imageProperties.tintColor = MobileAppearance.accent
            content.imageProperties.preferredSymbolConfiguration = UIImage.SymbolConfiguration(pointSize: 23, weight: .regular)
            content.imageToTextPadding = 16
        }
        content.textProperties.font = .preferredFont(forTextStyle: .headline)
        content.textProperties.numberOfLines = 0
        content.secondaryTextProperties.numberOfLines = row.metadata?.isEmpty == false ? 3 : 2
        content.secondaryTextProperties.color = .secondaryLabel
        content.directionalLayoutMargins = NSDirectionalEdgeInsets(top: 16, leading: 16, bottom: 16, trailing: 12)
        cell.backgroundColor = .secondarySystemGroupedBackground
        cell.contentConfiguration = content
        cell.accessoryType = row.destination.isEmpty ? .none : .disclosureIndicator
        cell.accessoryView = nil
        cell.isUserInteractionEnabled = row.disabled != true
        cell.contentView.alpha = row.disabled == true ? 0.45 : 1
        let accessories = UIStackView()
        accessories.spacing = 8
        if selectionMode, screen.list?.selection != nil {
            let select = UIButton(type: .system)
            select.setImage(UIImage(systemName: selection.selected.contains(row.id) ? "checkmark.circle.fill" : "circle"), for: .normal)
            select.accessibilityLabel = "Select \(row.title)"
            select.accessibilityValue = selection.selected.contains(row.id) ? "Selected" : "Not selected"
            select.accessibilityIdentifier = "native.select.\(row.id)"
            select.isEnabled = row.disabled != true && !busy
            select.addAction(UIAction { [weak self] _ in self?.toggleSelection(row.id) }, for: .touchUpInside)
            select.widthAnchor.constraint(equalToConstant: 44).isActive = true
            accessories.addArrangedSubview(select)
            cell.accessoryType = .none
        }
        if !row.actions.isEmpty {
            let actions = UIButton(type: .system)
            actions.setImage(UIImage(systemName: "ellipsis.circle"), for: .normal)
            actions.accessibilityLabel = "Actions for \(row.title)"
            actions.accessibilityIdentifier = "native.row-actions.\(row.id)"
            actions.isEnabled = row.disabled != true && !busy
            actions.addAction(UIAction { [weak self, weak actions] _ in self?.showRowActions(row.id, anchor: actions) }, for: .touchUpInside)
            actions.widthAnchor.constraint(equalToConstant: 44).isActive = true
            accessories.addArrangedSubview(actions)
        }
        if !accessories.arrangedSubviews.isEmpty {
            accessories.frame = CGRect(x: 0, y: 0, width: CGFloat(accessories.arrangedSubviews.count * 44 + max(0, accessories.arrangedSubviews.count - 1) * 8), height: 44)
            cell.accessoryView = accessories
            cell.isAccessibilityElement = false
            cell.contentView.isAccessibilityElement = true
            cell.contentView.accessibilityLabel = [row.title, row.subtitle].compactMap { $0 }.joined(separator: ", ")
        } else {
            cell.isAccessibilityElement = true
            cell.contentView.isAccessibilityElement = false
        }
        cell.accessibilityIdentifier = "native.row.\(row.id)"
        return cell
    }

    func tableView(_ tableView: UITableView, titleForHeaderInSection section: Int) -> String? { screen.list?.header }
    func tableView(_ tableView: UITableView, titleForFooterInSection section: Int) -> String? { screen.list?.footer }

    func tableView(_ tableView: UITableView, didSelectRowAt indexPath: IndexPath) {
        guard !navigationBlocked, !listLoading, let row = listRow(at: indexPath), row.disabled != true else { return }
        tableView.deselectRow(at: indexPath, animated: true)
        if selectionMode { toggleSelection(row.id); return }
        guard !row.destination.isEmpty else { return }
        openDestination(row.destination)
    }

    private func toggleSelection(_ identifier: String) {
        guard !navigationBlocked, !listLoading, let list = screen.list, let row = list.rows.first(where: { $0.id == identifier }), let definition = list.selection else { return }
        if !selection.toggle(row, maximum: definition.maximum ?? 0) {
            UIAccessibility.post(notification: .announcement, argument: "The selection limit has been reached.")
            return
        }
        bufferRevision += 1
        showList()
    }

    private func showRowActions(_ identifier: String, anchor: UIView?) {
        guard !navigationBlocked, !listLoading, presentedViewController == nil, let row = screen.list?.rows.first(where: { $0.id == identifier }), row.disabled != true else { return }
        let actions = MobileRowActionsController(row: row)
        actions.invoke = { [weak self] action in self?.performRowAction(identifier, action: action) }
        let menu = UINavigationController(rootViewController: actions)
        menu.modalPresentationStyle = .popover
        menu.popoverPresentationController?.sourceView = anchor ?? view
        menu.popoverPresentationController?.sourceRect = anchor?.bounds ?? .zero
        present(menu, animated: true)
    }

    private func performRowAction(_ identifier: String, action: String) {
        guard !navigationBlocked, !listLoading, let row = screen.list?.rows.first(where: { $0.id == identifier }), row.disabled != true,
              row.actions.contains(where: { $0.id == action }) else { return }
        perform(action, extraValues: ["row": identifier])
    }

    private func swipeActions(_ indexPath: IndexPath, leading: Bool) -> UISwipeActionsConfiguration? {
        guard !navigationBlocked, !listLoading, !selectionMode, let row = listRow(at: indexPath), row.disabled != true else { return nil }
        let declared = (leading ? row.leadingActions : row.trailingActions) ?? []
        guard !declared.isEmpty else { return nil }
        let actions = declared.map { action in
            UIContextualAction(style: action.role == "destructive" ? .destructive : .normal, title: action.label) { [weak self] _, _, finish in
                guard let self, !self.navigationBlocked, !self.listLoading,
                      let current = self.screen.list?.rows.first(where: { $0.id == row.id }), current.disabled != true,
                      current.actions.contains(where: { $0.id == action.id && $0.role == action.role && $0.fullSwipe == action.fullSwipe }) else { finish(false); return }
                finish(true)
                self.performRowAction(row.id, action: action.id)
            }
        }
        let configuration = UISwipeActionsConfiguration(actions: actions)
        configuration.performsFirstActionWithFullSwipe = declared.first?.allowsFullSwipe == true
        return configuration
    }

    func tableView(_ tableView: UITableView, leadingSwipeActionsConfigurationForRowAt indexPath: IndexPath) -> UISwipeActionsConfiguration? { swipeActions(indexPath, leading: true) }
    func tableView(_ tableView: UITableView, trailingSwipeActionsConfigurationForRowAt indexPath: IndexPath) -> UISwipeActionsConfiguration? { swipeActions(indexPath, leading: false) }

    private func openDestination(_ destination: String) {
        guard !navigationBlocked else { return }
        guard editor?.pending != true, editor?.unknown != true else {
            status.text = "Finish the pending or unconfirmed save before opening an attachment. Your text is still here."
            return
        }
        if dirty && instance != nil {
            pendingCheckpoint?.cancel()
            let current = bufferRevision
            setBusy(true)
            checkpointEditor { [weak self] stored in
                guard let self else { return }
                self.setBusy(false)
                guard stored, current == self.bufferRevision, !self.navigationBlocked else {
                    self.status.text = "Save your changes before opening this attachment; the draft could not be safely preserved."
                    return
                }
                self.pushDestination(destination)
            }
            return
        }
        pushDestination(destination)
    }

    func prepareForNavigation(_ completion: @escaping (Bool) -> Void) {
        loadViewIfNeeded()
        guard !navigationBlocked, presentedViewController == nil else { completion(false); return }
        guard dirty else { navigationPermit = bufferRevision; completion(true); return }
        guard instance != nil else {
            navigationPermit = bufferRevision
            completion(screen.form?.draft != nil)
            return
        }
        pendingCheckpoint?.cancel()
        let current = bufferRevision
        setBusy(true)
        checkpointEditor { [weak self] stored in
            guard let self else { completion(false); return }
            self.setBusy(false)
            guard stored, self.bufferRevision == current, !self.navigationBlocked else {
                self.status.text = "Save your changes before leaving; the draft could not be safely preserved."
                completion(false)
                return
            }
            self.navigationPermit = current
            completion(true)
        }
    }

    func closeSession() {
        guard !hasUncertainEditor, !busy else { return }
        cancelWatch()
        attachmentList?.stop()
        pendingCheckpoint?.cancel()
        pendingSearch?.cancel()
        send("close") { _ in }
        instance = nil
    }

    func setNavigationInFlight(_ value: Bool) { setBusy(value) }

    func suspendReads() {
        guard isViewLoaded else { return }
        cancelWatch()
        attachmentList?.stop()
        pendingSearch?.cancel()
        pendingSearch = nil
        revision += 1
        if listLoading { listNeedsRefresh = true }
        listLoading = false
        table?.refreshControl?.endRefreshing()
    }

    func resumeVisibleReads() {
        guard isViewLoaded, liveVisible, view.window != nil, navigationController?.topViewController === self else { return }
        liveReadRecovery.reset()
        if screen.list != nil && !watchPaused && (listNeedsRefresh || renderedMutationRevision != host.mutationRevision) { refresh() }
        else { startWatch() }
    }

    func checkpointForBackground(completion: @escaping (Bool) -> Void) {
        guard isViewLoaded, screen.form != nil else { completion(true); return }
        checkpointEditor(completion: completion)
    }

    private func checkpointEditor(completion: @escaping (Bool) -> Void) {
        pendingCheckpoint?.cancel()
        pendingCheckpoint = nil
        guard !checkpointDeferral.hold(completion) else { return }
        if let instance {
            let ticket = checkpointFence.begin(revision: bufferRevision)
            send("checkpoint") { [weak self] result in
                guard let self, self.instance == instance, case .success(let reply) = result, reply.error == nil,
                      self.checkpointFence.accepts(ticket, revision: self.bufferRevision, replyRevision: reply.revision),
                      let editor = reply.editor else { completion(false); return }
                self.editor = editor
                if !self.busy && self.dirty {
                    self.status.text = editor.draftStored ? self.screen.form?.draft?.changed : "Draft recovery is unavailable. Save before leaving."
                }
                self.updateBack()
                completion(!self.hasUncertainEditor && (!editor.dirty || editor.draftStored))
            }
        } else {
            if let draft = screen.form?.draft, dirty { UserDefaults.standard.set(fieldValues, forKey: draft.key) }
            completion(!dirty)
        }
    }

    private func finishCheckpointDeferral(discarded: Bool = false) {
        let callbacks = checkpointDeferral.finish()
        guard !callbacks.isEmpty else { return }
        if discarded { callbacks.forEach { $0(false) } }
        else { checkpointEditor { stored in callbacks.forEach { $0(stored) } } }
    }

    private func pushDestination(_ destination: String) {
        let current = bufferRevision
        setBusy(true)
        if instance != nil {
            host.open(destination) { [weak self] result in
                guard let self else { return }
                self.setBusy(false)
                guard current == self.bufferRevision, !self.hasUncertainEditor else {
                    if case .success(let reply) = result, let instance = reply.instance { self.host.command(["kind": "close", "instance": instance]) { _ in } }
                    return
                }
                switch result {
                case .success(let reply):
                    guard reply.error == nil, let model = reply.screen else { self.showFailure(MobileHost.failure(nil)); return }
                    self.navigationController?.pushViewController(NativeScreenController(model, host: self.host, reply: reply), animated: true)
                case .failure(let error): self.showFailure(error)
                }
            }
            return
        }
        host.render(destination, values: [:]) { [weak self] result in
            guard let self else { return }
            self.setBusy(false)
            switch result {
            case .success(let response):
                guard let model = response.screen else { self.showFailure(MobileHost.failure(nil)); return }
                UISelectionFeedbackGenerator().selectionChanged()
                self.navigationController?.pushViewController(NativeScreenController(model, host: self.host), animated: true)
            case .failure(let error): self.showFailure(error)
            }
        }
    }

    private func buildForm(_ form: MobileForm) {
        let stack = contentStack()
        let document = form.style == "document"
        for field in form.fields where !document || field.style != "detail" {
            baseline[field.key] = field.value
            addField(field, to: stack)
        }
        addImages(to: stack)
        let details = form.fields.filter { $0.style == "detail" }
        if document && !details.isEmpty {
            let group = MobileDisclosureGroup(form.detailsTitle ?? "Details", identifier: "native.document.details")
            for field in details {
                baseline[field.key] = field.value
                addField(field, to: group.content)
            }
            detailsGroup = group
            stack.addArrangedSubview(group)
        }
        configureStatus()
        status.text = form.status
        stack.addArrangedSubview(status)
        if let editor {
            baseline = editor.baseline
            for (key, value) in editor.values {
                setFieldValue(value, key: key)
            }
            if editor.restored { status.text = form.draft?.restored }
            if editor.unknown { status.text = "A previous save may have succeeded. Keep this draft and inspect saved items before trying again." }
        } else if let draft = form.draft, let restored = UserDefaults.standard.dictionary(forKey: draft.key) as? [String: String] {
            for (key, value) in restored {
                setFieldValue(value, key: key)
            }
            status.text = draft.restored
        }
        showFieldErrors(editor?.fields ?? [:])
        updateBack()
    }

    private var fields: [MobileField] { screen.form?.fields ?? screen.list?.filters ?? [] }

    private func addField(_ field: MobileField, to stack: UIStackView, filter: Bool = false) {
        let documentField = screen.form?.style == "document" && field.style != "detail"
        let compact = filter || field.style == "detail"
        let label = UILabel()
        label.text = field.label + (field.required == true ? " (required)" : "")
        label.font = .preferredFont(forTextStyle: .caption1)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = .secondaryLabel
        label.numberOfLines = 0
        label.setContentHuggingPriority(.required, for: .vertical)
        if compact { label.font = .preferredFont(forTextStyle: .body); label.textColor = .label }
        if !documentField && !compact { stack.addArrangedSubview(label) }
        let input: UIView
        if ["toggle", "select", "date", "time"].contains(field.kind) {
            let typed = MobileTypedControl(field)
            typed.changed = { [weak self] in
                if filter { self?.queryChanged() } else { self?.changed() }
            }
            input = typed
        } else if field.kind == "multiline" {
            let text = UITextView()
            text.text = field.value
            text.font = .preferredFont(forTextStyle: field.style == "title" ? .title1 : .body)
            text.backgroundColor = .clear
            text.adjustsFontForContentSizeCategory = true
            text.textContainerInset = .zero
            text.textContainer.lineFragmentPadding = 0
            text.isScrollEnabled = false
            text.isEditable = field.disabled != true
            text.delegate = self
            text.setContentHuggingPriority(.defaultLow, for: .vertical)
            text.heightAnchor.constraint(greaterThanOrEqualToConstant: documentField ? 160 : 120).isActive = true
            input = text
        } else {
            let text = UITextField()
            text.text = field.value
            text.placeholder = field.placeholder
            text.font = .preferredFont(forTextStyle: field.style == "title" ? .title1 : .body)
            if documentField && field.style == "title" {
                text.font = UIFont(descriptor: UIFont.preferredFont(forTextStyle: .title1).fontDescriptor.withSymbolicTraits(.traitBold) ?? UIFont.preferredFont(forTextStyle: .title1).fontDescriptor, size: 0)
            }
            text.adjustsFontForContentSizeCategory = true
            text.returnKeyType = .next
            text.isEnabled = field.disabled != true
            text.delegate = self
            text.addTarget(self, action: filter ? #selector(filterChanged) : #selector(changed), for: .editingChanged)
            text.setContentHuggingPriority(.required, for: .vertical)
            input = text
        }
        if !(input is MobileTypedControl) {
            input.accessibilityLabel = label.text
            input.accessibilityIdentifier = "native.field.\(field.key)"
        }
        input.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
        inputs[field.key] = input
        if compact {
            let row = UIStackView(arrangedSubviews: [label, input])
            row.axis = .horizontal
            row.alignment = .center
            row.spacing = 12
            label.setContentCompressionResistancePriority(.defaultHigh, for: .horizontal)
            input.setContentHuggingPriority(.required, for: .horizontal)
            if traitCollection.preferredContentSizeCategory.isAccessibilityCategory { row.axis = .vertical; row.alignment = .fill }
            stack.addArrangedSubview(row)
        } else { stack.addArrangedSubview(input) }
        let error = UILabel()
        error.font = .preferredFont(forTextStyle: .footnote)
        error.adjustsFontForContentSizeCategory = true
        error.textColor = .systemRed
        error.numberOfLines = 0
        error.isHidden = true
        error.accessibilityIdentifier = "native.field.\(field.key).error"
        fieldErrors[field.key] = error
        stack.addArrangedSubview(error)
        if filter, let value = listQuery[field.key] { setFieldValue(value, key: field.key) }
    }

    @objc private func filterChanged() { queryChanged() }

    private func configureInputs() {
        for field in fields {
            let enabled = field.disabled != true && (!busy || (instance != nil && screen.form != nil))
            (inputs[field.key] as? UITextField)?.isEnabled = enabled
            (inputs[field.key] as? UITextView)?.isEditable = enabled
            (inputs[field.key] as? MobileTypedControl)?.configure(field, enabled: enabled)
            if field.disabled == true { setFieldValue(field.value, key: field.key) }
        }
    }

    private func setFieldValue(_ value: String, key: String) {
        let definition = fields.first { $0.key == key }
        let effective = definition?.disabled == true ? definition?.value ?? value : value
        if let text = inputs[key] as? UITextField, text.text != effective, text.markedTextRange == nil { text.text = effective }
        if let text = inputs[key] as? UITextView, text.text != effective, text.markedTextRange == nil { text.text = effective }
        if let typed = inputs[key] as? MobileTypedControl, typed.value != effective { typed.value = effective }
    }

    private func showFieldErrors(_ errors: [String: String]) {
        if fields.contains(where: { $0.style == "detail" && errors[$0.key]?.isEmpty == false }) { detailsGroup?.setExpanded(true) }
        for (key, label) in fieldErrors {
            label.text = errors[key]
            label.isHidden = errors[key]?.isEmpty != false
        }
        sizeListHeader()
    }

    private func buildFilePreview(_ preview: MobileFilePreview) {
        let stack = contentStack()
        stack.addArrangedSubview(MobileFilePreviewHeader(preview))
        addImages(to: stack)
        if let text = preview.text, !text.isEmpty {
            let divider = UIView()
            divider.backgroundColor = .separator
            divider.heightAnchor.constraint(equalToConstant: 0.5).isActive = true
            stack.addArrangedSubview(divider)
            let heading = MobileAppearance.label(preview.previewTitle ?? "Text preview", style: .footnote, color: .secondaryLabel)
            heading.accessibilityTraits.insert(.header)
            stack.addArrangedSubview(heading)
            let body = MobileAppearance.label(text, style: .body)
            body.accessibilityIdentifier = "native.file.preview"
            stack.addArrangedSubview(body)
        }
        configureStatus()
        stack.addArrangedSubview(status)
    }

    private func buildText(_ content: String) {
        let stack = contentStack()
        addImages(to: stack)
        let text = UILabel()
        text.numberOfLines = 0
        text.font = .preferredFont(forTextStyle: .body)
        text.adjustsFontForContentSizeCategory = true
        text.text = content
        configureStatus()
        stack.addArrangedSubview(text)
        stack.addArrangedSubview(status)
    }

    private func contentStack() -> UIStackView {
        let scroll = UIScrollView()
        scroll.translatesAutoresizingMaskIntoConstraints = false
        scroll.alwaysBounceVertical = true
        scroll.keyboardDismissMode = .interactive
        view.addSubview(scroll)
        let stack = UIStackView()
        stack.axis = .vertical
        stack.spacing = 20
        stack.translatesAutoresizingMaskIntoConstraints = false
        scroll.addSubview(stack)
        NSLayoutConstraint.activate([
            scroll.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            scroll.leadingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: view.keyboardLayoutGuide.topAnchor),
            stack.topAnchor.constraint(equalTo: scroll.contentLayoutGuide.topAnchor, constant: 16),
            stack.bottomAnchor.constraint(equalTo: scroll.contentLayoutGuide.bottomAnchor, constant: -20),
            stack.leadingAnchor.constraint(equalTo: scroll.contentLayoutGuide.leadingAnchor, constant: 20),
            stack.trailingAnchor.constraint(equalTo: scroll.contentLayoutGuide.trailingAnchor, constant: -20),
            stack.widthAnchor.constraint(equalTo: scroll.frameLayoutGuide.widthAnchor, constant: -40)
        ])
        contentScroll = scroll
        return stack
    }

    private func addImages(to stack: UIStackView) {
        let gallery = MobileImageGallery()
        gallery.compact = screen.form?.style == "document"
        gallery.open = { [weak self] destination in self?.openDestination(destination) }
        gallery.update(screen.images ?? [])
        stack.addArrangedSubview(gallery)
        imageGallery = gallery
        let attachments = MobileAttachmentList()
        attachments.open = { [weak self] destination in self?.openDestination(destination) }
        attachments.update(screen.attachments ?? [])
        stack.addArrangedSubview(attachments)
        attachmentList = attachments
    }

    private func refreshImages() {
        guard imageGallery != nil else { return }
        let current = revision
        host.render(screen.route, values: values) { [weak self] result in
            guard let self, current == self.revision else { return }
            switch result {
            case .success(let response):
                guard let model = response.screen else { return }
                self.screen.images = model.images
                self.imageGallery?.update(model.images ?? [])
                self.screen.attachments = model.attachments
                self.attachmentList?.update(model.attachments ?? [])
            case .failure:
                self.status.text = "Attachments could not be refreshed. Your text is still here."
            }
        }
    }

    private func configureStatus() {
        status.font = .preferredFont(forTextStyle: .footnote)
        status.adjustsFontForContentSizeCategory = true
        status.numberOfLines = 0
        status.textColor = .secondaryLabel
        status.accessibilityIdentifier = "native.status"
        status.setContentHuggingPriority(.required, for: .vertical)
        status.setContentCompressionResistancePriority(.required, for: .vertical)
    }

    private var fieldValues: [String: String] {
        var result: [String: String] = [:]
        for field in fields {
            let input = inputs[field.key]
            if field.disabled == true { result[field.key] = field.value }
            else if let typed = input as? MobileTypedControl { result[field.key] = typed.value }
            else if let text = input as? UITextField { result[field.key] = text.text ?? "" }
            else { result[field.key] = (input as? UITextView)?.text ?? field.value }
        }
        return result
    }

    private var values: [String: String] {
        var values = screen.list == nil ? screen.values ?? [:] : listQuery
        for (key, value) in fieldValues { values[key] = value }
        if let key = screen.list?.searchKey { values[key] = search?.searchBar.text ?? "" }
        if let definition = screen.list?.selection { values[definition.key] = selection.wire }
        values.merge(actionValues) { _, new in new }
        return values
    }

    func textFieldShouldReturn(_ textField: UITextField) -> Bool {
        guard let index = fields.firstIndex(where: { inputs[$0.key] === textField }) else { return false }
        if let next = fields.dropFirst(index + 1).first(where: { $0.disabled != true && inputs[$0.key]?.canBecomeFirstResponder == true }) { inputs[next.key]?.becomeFirstResponder() }
        else { textField.resignFirstResponder() }
        return false
    }

    func textViewDidChange(_ textView: UITextView) {
        if screen.list != nil { queryChanged() } else { changed() }
        view.layoutIfNeeded()
        if let scroll = contentScroll, let selection = textView.selectedTextRange {
            let caret = textView.convert(textView.caretRect(for: selection.end), to: scroll)
            scroll.scrollRectToVisible(caret.insetBy(dx: 0, dy: -24), animated: false)
        }
    }

    @objc private func changed() {
        showFieldErrors([:])
        bufferRevision += 1
        if instance != nil {
            status.text = dirty ? "Unsaved changes · checkpointing draft…" : screen.form?.status
            pendingCheckpoint?.cancel()
            let task = DispatchWorkItem { [weak self] in
                guard let self else { return }
                self.checkpointEditor { _ in }
            }
            pendingCheckpoint = task
            DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(250), execute: task)
            updateBack()
            return
        }
        if let draft = screen.form?.draft {
            if dirty {
                UserDefaults.standard.set(fieldValues, forKey: draft.key)
                status.text = draft.changed
            } else {
                UserDefaults.standard.removeObject(forKey: draft.key)
                status.text = screen.form?.status
            }
        }
        updateBack()
    }

    private func updateBack() {
        let uncertain = editor?.unknown == true
        let blocked = busy || operationUnconfirmed || host.foreground.isActive || editor?.pending == true || editor?.unknown == true
        guard let draft = screen.form?.draft else {
            navigationItem.hidesBackButton = blocked
            navigationController?.interactivePopGestureRecognizer?.isEnabled = !blocked
            navigationController?.isModalInPresentation = blocked
            return
        }
        navigationItem.hidesBackButton = dirty || blocked || uncertain
        navigationItem.leftBarButtonItem = dirty || blocked || uncertain ? UIBarButtonItem(title: draft.backLabel, style: .plain, target: self, action: #selector(close)) : nil
        navigationItem.leftBarButtonItem?.isEnabled = !blocked
        navigationController?.interactivePopGestureRecognizer?.isEnabled = !dirty && !busy && !host.foreground.isActive && !uncertain && !operationUnconfirmed
        navigationController?.isModalInPresentation = dirty || blocked || uncertain
    }

    @objc private func close() {
        guard !navigationBlocked, let draft = screen.form?.draft else { return }
        let alert = UIAlertController(title: draft.title, message: draft.message, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: draft.keepEditing, style: .cancel))
        alert.addAction(UIAlertAction(title: draft.keepDraft, style: .default) { [weak self] _ in self?.leave("keep") })
        alert.addAction(UIAlertAction(title: draft.discard, style: .destructive) { [weak self] _ in
            self?.leave("discard")
        })
        present(alert, animated: true)
    }

    private func setBusy(_ value: Bool) {
        busy = value
        for button in (navigationItem.rightBarButtonItems ?? []) + (navigationItem.leftBarButtonItems ?? []) { button.isEnabled = !value }
        configureInputs()
        table?.isUserInteractionEnabled = !value
        search?.searchBar.isUserInteractionEnabled = !value
        loadMore?.isEnabled = !value && !listLoading
        updateBack()
        if !value { DispatchQueue.main.async { [weak self] in self?.startWatch() } }
    }

    private func perform(_ action: String, extraValues: [String: String] = [:]) {
        guard !navigationBlocked, !listLoading, presentedViewController == nil else { return }
        cancelWatch()
        actionValues = extraValues
        pendingSearch?.cancel()
        revision += 1
        setBusy(true)
        if instance != nil {
            pendingCheckpoint?.cancel()
            let request = UUID().uuidString
            send("action", extra: ["requestId": request, "action": action]) { [weak self] result in
                guard let self else { return }
                if case .success(let reply) = result, let error = reply.error, reply.response == nil {
                    self.editor = reply.editor ?? self.editor
                    self.setBusy(false)
                    self.actionValues = [:]
                    self.report(error, editor: reply.editor)
                    return
                }
                self.complete(request)
            }
            return
        }
        host.dispatch(screen.route, action: action, values: values) { [weak self] result in
            guard let self else { return }
            self.setBusy(false)
            self.actionValues = [:]
            switch result {
            case .success(let response):
                self.apply(response)
                if response.navigation == nil && response.presentation == nil && response.files == nil && response.service == nil {
                    self.host.mutationRevision += 1
                    if self.screen.list != nil { self.refresh() }
                }
            case .failure(let error):
                UINotificationFeedbackGenerator().notificationOccurred(.error)
                self.showFailure(error)
            }
        }
    }

    private func send(_ kind: String, extra: [String: Any] = [:], completion: @escaping (Result<MobileReply, Error>) -> Void) {
        guard let instance else { return }
        if kind != "checkpoint" { checkpointFence.invalidate() }
        var fields: [String: Any] = ["kind": kind, "instance": instance, "revision": bufferRevision, "values": values]
        fields.merge(extra) { _, new in new }
        host.command(fields, completion: completion)
    }

    private func leave(_ choice: String) {
        guard !navigationBlocked else { return }
        guard instance != nil else {
            if choice == "discard", let draft = screen.form?.draft { UserDefaults.standard.removeObject(forKey: draft.key) }
            navigationController?.popViewController(animated: true)
            return
        }
        bufferRevision += 1
        let current = bufferRevision
        setBusy(true)
        checkpointDeferral.begin()
        send("leave", extra: ["choice": choice]) { [weak self] result in
            guard let self else { return }
            self.setBusy(false)
            guard case .success(let reply) = result, reply.error == nil, reply.revision == current,
                  current == self.bufferRevision, !self.navigationBlocked,
                  let editor = reply.editor, !editor.pending, !editor.unknown, editor.canLeave else {
                self.status.text = "The draft is not safe to leave yet. Wait for the save or checkpoint to finish."
                self.finishCheckpointDeferral()
                return
            }
            self.editor = editor
            self.finishCheckpointDeferral(discarded: true)
            self.navigationController?.popViewController(animated: true)
        }
    }

    private func respond(_ presentationID: String, choice: String, selected: [String]) {
        pendingCheckpoint?.cancel()
        let request = UUID().uuidString
        setBusy(true)
        send("respond", extra: ["requestId": request, "presentationId": presentationID, "choice": choice, "selected": selected]) { [weak self] result in
            guard let self else { return }
            if case .success(let reply) = result, let error = reply.error, reply.response == nil {
                self.editor = reply.editor ?? self.editor
                if error.code == "validation" || error.code == "stale" {
                    self.host.foreground.reject(presentationID, message: self.failureMessage(error, editor: reply.editor))
                } else if error.code == "unknown" || error.code == "internal" || error.code == "pending" {
                    self.complete(request, foregroundID: presentationID)
                } else {
                    self.finishForeground(presentationID) {
                        self.setBusy(false)
                        self.report(error, editor: reply.editor)
                    }
                }
                return
            }
            self.complete(request, foregroundID: presentationID)
        }
    }

    private func fileResult(_ identifier: String, result: MobileFileResult) {
        pendingCheckpoint?.cancel()
        let request = UUID().uuidString
        setBusy(true)
        send("file-result", extra: ["requestId": request, "fileId": identifier, "fileResult": result.wire]) { [weak self] _ in
            self?.complete(request, foregroundID: identifier)
        }
    }

    private func serviceResult(_ identifier: String, result: MobileServiceResult) {
        pendingCheckpoint?.cancel()
        let request = UUID().uuidString
        setBusy(true)
        send("service-result", extra: ["requestId": request, "serviceId": identifier, "serviceResult": result.wire]) { [weak self] _ in
            self?.complete(request, foregroundID: identifier)
        }
    }

    private func finishForeground(_ identifier: String?, restoreFocus: Bool = true, preserveFocus: Bool = false, completion: @escaping () -> Void) {
        guard let identifier else { completion(); return }
        host.foreground.finish(identifier, restoreFocus: restoreFocus, preserveFocus: preserveFocus, completion: completion)
    }

    private func failureMessage(_ error: MobileFailure, editor: MobileEditor?) -> String {
        ([error.message] + ((editor?.fields ?? error.fields)?.sorted { $0.key < $1.key }.map { "\($0.key): \($0.value)" } ?? [])).joined(separator: "\n")
    }

    private func report(_ error: MobileFailure, editor: MobileEditor? = nil) {
        if error.code == "unknown" || error.code == "internal" || error.code == "pending" { operationUnconfirmed = true }
        let message = failureMessage(error, editor: editor)
        status.text = message
        showFieldErrors(editor?.fields ?? error.fields ?? [:])
        updateBack()
        UIAccessibility.post(notification: .announcement, argument: message)
        if screen.form == nil {
            showFailure(NSError(domain: "GoFastrMobile", code: 1, userInfo: [NSLocalizedDescriptionKey: message, "code": error.code, "fields": error.fields ?? [:]]))
        }
    }

    private func complete(_ request: String, foregroundID: String? = nil, remainingReads: Int = 1) {
        checkpointDeferral.begin()
        send("complete", extra: ["requestId": request]) { [weak self] result in
            guard let self else { return }
            guard case .success(let reply) = result else {
                self.finishForeground(foregroundID) {
                    defer { self.finishCheckpointDeferral() }
                    self.setBusy(false)
                    self.report(MobileFailure(code: "unknown", message: "The operation outcome is unconfirmed. It was not retried. Keep this draft and inspect saved items.", fields: nil))
                }
                return
            }
            if reply.error?.code == "stale" || (reply.error == nil && reply.revision != self.bufferRevision) {
                guard remainingReads > 0 else {
                    self.finishForeground(foregroundID) {
                        defer { self.finishCheckpointDeferral() }
                        self.setBusy(false)
                        self.report(MobileFailure(code: "unknown", message: "The operation outcome could not be confirmed for this buffer. It was not retried. Keep this draft and inspect saved items.", fields: nil))
                    }
                    return
                }
                self.complete(request, foregroundID: foregroundID, remainingReads: remainingReads - 1)
                return
            }
            self.editor = reply.editor
            if reply.error == nil { self.operationUnconfirmed = reply.editor?.unknown == true || reply.editor?.pending == true }
            let chained = reply.response?.presentation != nil || reply.response?.files != nil || reply.response?.service != nil
            let restoreFocus = !chained && reply.response?.navigation == nil && reply.response?.share == nil
            self.finishForeground(foregroundID, restoreFocus: restoreFocus, preserveFocus: chained) {
                defer { self.finishCheckpointDeferral() }
                self.setBusy(false)
                if !chained { self.actionValues = [:] }
                if let error = reply.error { self.report(error, editor: reply.editor); return }
                guard let response = reply.response else { return }
                if let saved = response.saved {
                    self.screen.route = saved.route
                    if let key = saved.draftKey { self.screen.form?.draft?.key = key }
                }
                if let editor = reply.editor {
                    self.baseline = editor.baseline
                    if !editor.dirty && response.presentation == nil && response.files == nil && response.service == nil {
                        for (key, value) in editor.values {
                            if let field = self.inputs[key] as? UITextField, field.text != value, field.markedTextRange == nil { field.text = value }
                            if let field = self.inputs[key] as? UITextView, field.text != value, field.markedTextRange == nil { field.text = value }
                            if let field = self.inputs[key] as? MobileTypedControl, field.value != value { field.value = value }
                        }
                    }
                    self.status.text = editor.dirty ? self.screen.form?.draft?.changed : response.announcement
                }
                self.updateBack()
                self.host.navigationChanged?()
                DispatchQueue.main.async { MobileNotificationRouter.shared.flush() }
                self.apply(response, request: request, effectIDs: reply.effectIds ?? [:], presentationID: reply.presentationId, fileID: reply.fileId, serviceID: reply.serviceId)
                if response.navigation == nil && response.presentation == nil && response.files == nil && response.service == nil {
                    self.host.mutationRevision += 1
                    if self.screen.list != nil { self.refresh() }
                }
            }
        }
    }

    private func receipt(_ name: String, request: String, ids: [String: String], outcome: String) {
        guard let identifier = ids[name], observedEffects.insert(identifier).inserted else { return }
        let capabilities = ["share": "share/v1", "haptic": "haptics/v1", "navigation": "navigation/v1", "announcement": "announcement/v1"]
        guard let capability = capabilities[name] else { return }
        send("ack", extra: ["requestId": request, "receipt": ["id": identifier, "capability": capability, "status": outcome]]) { _ in }
    }

    private func apply(_ response: MobileResponse, request: String = "", effectIDs: [String: String] = [:], presentationID: String? = nil, fileID: String? = nil, serviceID: String? = nil) {
        if response.presentation != nil && (instance == nil || host.backend.command == nil) {
            report(MobileFailure(code: "unsupported", message: "This backend does not support presentation/v1. A versioned mobile command backend is required.", fields: nil))
            return
        }
        if response.files != nil && (instance == nil || host.backend.command == nil) {
            report(MobileFailure(code: "unsupported", message: "This backend does not support files/v1. A versioned mobile command backend is required.", fields: nil))
            return
        }
        if response.service != nil && (instance == nil || host.backend.command == nil) {
            report(MobileFailure(code: "unsupported", message: "Device services require a versioned mobile command backend.", fields: nil))
            return
        }
        if [response.files != nil, response.presentation != nil, response.service != nil].filter({ $0 }).count > 1 {
            report(MobileFailure(code: "protocol", message: "Only one foreground task may be presented at a time.", fields: nil))
            return
        }
        if instance == nil, response.clearDraft == true, let draft = screen.form?.draft {
            UserDefaults.standard.removeObject(forKey: draft.key)
            baseline = fieldValues
            updateBack()
        }
        if response.haptic == "success" { UINotificationFeedbackGenerator().notificationOccurred(.success) }
        if response.haptic == "selection" { UISelectionFeedbackGenerator().selectionChanged() }
        if response.haptic != nil { receipt("haptic", request: request, ids: effectIDs, outcome: "completed") }
        if let announcement = response.announcement {
            if screen.form == nil { status.text = announcement }
            UIAccessibility.post(notification: .announcement, argument: announcement)
            receipt("announcement", request: request, ids: effectIDs, outcome: "completed")
        }
        if let presentation = response.presentation {
            guard let presentationID, !presentationID.isEmpty else {
                report(MobileFailure(code: "protocol", message: "The presentation instance identity is missing. No response was sent.", fields: nil))
                return
            }
            setBusy(true)
            if !host.foreground.show(presentation, identifier: presentationID, from: self, respond: { [weak self] choice, selected in
                self?.respond(presentationID, choice: choice, selected: selected)
            }) {
                setBusy(false)
                report(MobileFailure(code: "unsupported", message: "This presentation cannot be shown while another transient is active, or its kind is unsupported.", fields: nil))
            }
            return
        }
        if let files = response.files {
            guard let fileID, !fileID.isEmpty else {
                report(MobileFailure(code: "protocol", message: "The file task instance identity is missing. No result was sent.", fields: nil))
                return
            }
            setBusy(true)
            if !host.foreground.show(files, identifier: fileID, from: self, respond: { [weak self] result in
                self?.fileResult(fileID, result: result)
            }) {
                setBusy(false)
                report(MobileFailure(code: "unsupported", message: "This file task cannot be shown while another transient is active.", fields: nil))
            }
            return
        }
        if let service = response.service {
            guard let serviceID, !serviceID.isEmpty, !host.foreground.isActive else {
                report(MobileFailure(code: "protocol", message: "This service is no longer active. It was not repeated.", fields: nil))
                return
            }
            guard observedServices.insert(serviceID).inserted else { return }
            cancelWatch()
            setBusy(true)
            if !host.foreground.show(service, identifier: serviceID, from: self, openRoute: { [weak host] route in
                host?.openServiceRoute?(route) ?? false
            }, respond: { [weak self] result in self?.serviceResult(serviceID, result: result) }) {
                serviceResult(serviceID, result: MobileServiceResult(status: "failed", message: "Another foreground task is active."))
            }
            return
        }
        if let share = response.share {
            let sheet = UIActivityViewController(activityItems: [share], applicationActivities: nil)
            sheet.popoverPresentationController?.barButtonItem = navigationItem.rightBarButtonItems?.last
            sheet.completionWithItemsHandler = { [weak self] _, completed, _, error in
                self?.receipt("share", request: request, ids: effectIDs, outcome: error != nil ? "failed" : completed ? "completed" : "cancelled")
            }
            present(sheet, animated: true)
        }
        guard let navigation = response.navigation else { return }
        if instance != nil {
            switch navigation.kind {
            case "push", "sheet":
                guard let route = navigation.route else { return }
                let openingRevision = bufferRevision
                host.open(route) { [weak self] result in
                    guard let self else { return }
                    guard openingRevision == self.bufferRevision, !self.hasUncertainEditor else {
                        if case .success(let reply) = result, let instance = reply.instance { self.host.command(["kind": "close", "instance": instance]) { _ in } }
                        self.receipt("navigation", request: request, ids: effectIDs, outcome: "cancelled")
                        return
                    }
                    let reply: MobileReply
                    switch result {
                    case .failure(let error): self.showFailure(error); return
                    case .success(let value): reply = value
                    }
                    guard reply.error == nil, let model = reply.screen else {
                        self.showFailure(NSError(domain: "GoFastrMobile", code: 1, userInfo: [NSLocalizedDescriptionKey: reply.error?.message ?? "The destination could not be opened."]))
                        return
                    }
                    let controller = NativeScreenController(model, host: self.host, reply: reply)
                    if navigation.kind == "push" {
                        self.navigationController?.pushViewController(controller, animated: true)
                        self.navigationController?.transitionCoordinator?.animate(alongsideTransition: nil) { context in
                            self.receipt("navigation", request: request, ids: effectIDs, outcome: context.isCancelled ? "cancelled" : "completed")
                        }
                    } else {
                        let sheet = UINavigationController(rootViewController: controller)
                        sheet.sheetPresentationController?.detents = [.medium(), .large()]
                        sheet.sheetPresentationController?.prefersGrabberVisible = true
                        self.present(sheet, animated: true) { self.receipt("navigation", request: request, ids: effectIDs, outcome: "completed") }
                    }
                }
            case "pop":
                navigationController?.popViewController(animated: true)
                navigationController?.transitionCoordinator?.animate(alongsideTransition: nil) { context in
                    self.receipt("navigation", request: request, ids: effectIDs, outcome: context.isCancelled ? "cancelled" : "completed")
                }
            case "dismiss": dismiss(animated: true) { self.receipt("navigation", request: request, ids: effectIDs, outcome: "completed") }
            default: showFailure(MobileHost.failure(nil))
            }
            return
        }
        switch navigation.kind {
        case "push":
            if let model = response.screen { navigationController?.pushViewController(NativeScreenController(model, host: host), animated: true) }
        case "pop": navigationController?.popViewController(animated: true)
        case "sheet":
            if let model = response.screen {
                let sheet = UINavigationController(rootViewController: NativeScreenController(model, host: host))
                sheet.sheetPresentationController?.detents = [.medium(), .large()]
                sheet.sheetPresentationController?.prefersGrabberVisible = true
                present(sheet, animated: true)
            }
        case "dismiss": dismiss(animated: true)
        default: showFailure(MobileHost.failure(nil))
        }
    }
}

extension UIViewController {
    func showFailure(_ error: Error) {
        let alert = UIAlertController(title: "Couldn’t finish", message: error.localizedDescription, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "OK", style: .default))
        present(alert, animated: true)
    }
}
