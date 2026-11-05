import UIKit

final class MobileTabController: UITabBarController, UITabBarControllerDelegate, UINavigationControllerDelegate {
    private let host: MobileHost
    private let tabModels: [MobileTab]
    private let initial: NativeScreenController
    private let storageKey: String
    private let metadata: MobileNavigationMetadata?
    private var navigations: [UINavigationController] = []
    private var restoring = true
    private var switching = false
    private var restoredRoutes = Set<String>()
    var ready: (() -> Void)?

    init(screen: MobileScreen, host: MobileHost, reply: MobileReply) {
        self.host = host
        initial = NativeScreenController(screen, host: host, reply: reply)
        var identifiers = Set<String>()
        tabModels = Array((screen.tabs ?? []).filter {
            !$0.id.isEmpty && $0.id.utf8.count <= 128 && identifiers.insert($0.id).inserted && MobileLocalRoute.validate($0.route) != nil
        }.prefix(MobileNavigationMetadata.maximumTabs))
        storageKey = "gofastr.native.navigation.v1." + host.backend.root()
        if let data = UserDefaults.standard.data(forKey: storageKey), data.count <= 65536 {
            metadata = try? JSONDecoder().decode(MobileNavigationMetadata.self, from: data)
        } else { metadata = nil }
        super.init(nibName: nil, bundle: nil)
        delegate = self
        let symbols = ["notes": "note.text", "files": "folder", "file": "folder", "settings": "gearshape", "list": "list.bullet", "info": "info.circle", "compose": "square.and.pencil", "share": "square.and.arrow.up"]
        navigations = tabModels.map { tab in
            let placeholder = UIViewController()
            placeholder.view.backgroundColor = .systemBackground
            let navigation = UINavigationController(rootViewController: placeholder)
            navigation.navigationBar.prefersLargeTitles = true
            navigation.delegate = self
            let symbol = tab.icon ?? tab.id
            let image = UIImage(systemName: symbols[symbol] ?? symbol) ?? UIImage(systemName: "square.grid.2x2")
            navigation.tabBarItem = UITabBarItem(title: tab.label, image: image, selectedImage: nil)
            navigation.tabBarItem.accessibilityIdentifier = "native.tab.\(tab.id)"
            navigation.tabBarItem.accessibilityLabel = tab.label
            return navigation
        }
        viewControllers = navigations
        customizableViewControllers = []
        selectedIndex = tabModels.firstIndex { $0.id == screen.activeTab } ?? 0
        tabBar.isUserInteractionEnabled = false
        view.isUserInteractionEnabled = false
        host.navigationChanged = { [weak self] in self?.persist() }
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func restore() { restoreTab(0) }

    private func restoreTab(_ index: Int) {
        guard index < tabModels.count else {
            restoring = false
            tabBar.isUserInteractionEnabled = true
            view.isUserInteractionEnabled = true
            if !navigations.contains(where: { $0.tabBarItem.isEnabled }), let navigation = navigations.first {
                navigation.setViewControllers([initial], animated: false)
                navigation.tabBarItem.isEnabled = true
            }
            if metadata?.version == 1, let active = metadata?.activeTab, let index = tabModels.firstIndex(where: { $0.id == active }), navigations[index].tabBarItem.isEnabled {
                selectedIndex = index
            } else if !navigations[selectedIndex].tabBarItem.isEnabled {
                selectedIndex = navigations.firstIndex { $0.tabBarItem.isEnabled } ?? 0
            }
            if initial.navigationController == nil {
                if initial.hasUncertainEditor { navigations[selectedIndex].pushViewController(initial, animated: false) }
                else { initial.closeSession() }
            }
            if let uncertain = navigations.firstIndex(where: { navigation in
                navigation.viewControllers.contains { ($0 as? NativeScreenController)?.hasUncertainEditor == true }
            }) { selectedIndex = uncertain }
            persist()
            ready?()
            return
        }
        let tab = tabModels[index]
        if tab.route == initial.route {
            navigations[index].setViewControllers([initial], animated: false)
            restoredRoutes.insert(initial.route)
            restoreStack(index, routes: metadata?.routes(for: tab) ?? [tab.route], depth: 1)
            return
        }
        openRestorationRoute(tab.route) { [weak self] result in
            guard let self else { return }
            guard case .success(let reply) = result, reply.error == nil, let screen = reply.screen else {
                self.navigations[index].tabBarItem.isEnabled = false
                self.restoreTab(index + 1)
                return
            }
            let controller = NativeScreenController(screen, host: self.host, reply: reply)
            self.navigations[index].setViewControllers([controller], animated: false)
            self.restoredRoutes.insert(screen.route)
            self.restoreStack(index, routes: self.metadata?.routes(for: tab) ?? [tab.route], depth: 1)
        }
    }

    private func restoreStack(_ index: Int, routes: [String], depth: Int) {
        guard depth < routes.count, !restoredRoutes.contains(routes[depth]),
              (navigations[index].topViewController as? NativeScreenController)?.hasUncertainEditor != true else {
            restoreTab(index + 1)
            return
        }
        openRestorationRoute(routes[depth]) { [weak self] result in
            guard let self else { return }
            guard case .success(let reply) = result, reply.error == nil, let screen = reply.screen else {
                self.restoreTab(index + 1)
                return
            }
            let controller = NativeScreenController(screen, host: self.host, reply: reply)
            self.restoredRoutes.insert(screen.route)
            self.navigations[index].setViewControllers(self.navigations[index].viewControllers + [controller], animated: false)
            self.restoreStack(index, routes: routes, depth: depth + 1)
        }
    }

    private func openRestorationRoute(_ route: String, completion: @escaping (Result<MobileReply, Error>) -> Void) {
        host.render(route, values: [:]) { [weak self] result in
            guard let self else { return }
            guard case .success(let response) = result, let screen = response.screen else { completion(.failure(MobileHost.failure(nil))); return }
            let retained = self.retainedScreens + (self.initial.navigationController == nil ? [self.initial] : [])
            guard MobileRestorationSafety.canOpen(route: screen.route, draftKey: screen.form?.draft?.key, editor: screen.form != nil,
                retained: retained.map { MobileRetainedRoute(route: $0.route, draftKey: $0.draftKey, uncertain: $0.hasUncertainEditor) }) else {
                completion(.failure(MobileHost.failure(nil)))
                return
            }
            self.host.open(route, completion: completion)
        }
    }

    func tabBarController(_ tabBarController: UITabBarController, shouldSelect viewController: UIViewController) -> Bool {
        guard !restoring, !switching, !host.foreground.isActive, presentedViewController == nil,
              let current = selectedViewController as? UINavigationController,
              current.presentedViewController == nil else { return false }
        let target = viewControllers?.firstIndex(of: viewController)
        guard target != nil || viewController === moreNavigationController else { return false }
        if viewController === selectedViewController { return false }
        guard let screen = current.topViewController as? NativeScreenController else {
            (viewController as? UINavigationController)?.topViewController.flatMap { $0 as? NativeScreenController }?.skipRefreshOnAppear = true
            return true
        }
        switching = true
        screen.prepareForNavigation { [weak self] safe in
            guard let self else { return }
            self.switching = false
            guard safe else { return }
            (viewController as? UINavigationController)?.topViewController.flatMap { $0 as? NativeScreenController }?.skipRefreshOnAppear = true
            self.selectedViewController = viewController
            self.persist()
        }
        return false
    }

    func tabBarController(_ tabBarController: UITabBarController, didSelect viewController: UIViewController) { persist() }

    func navigationController(_ navigationController: UINavigationController, didShow viewController: UIViewController, animated: Bool) { persist() }

    var canOpenRoute: Bool {
        !restoring && !switching && !host.foreground.isActive && presentedViewController == nil &&
        activeNavigation?.presentedViewController == nil && (activeNavigation?.topViewController as? NativeScreenController)?.navigationBlocked == false
    }

    var activeNavigation: UINavigationController? { selectedViewController as? UINavigationController }

    func open(_ route: String, draftKey: String?) {
        guard canOpenRoute, let source = activeNavigation?.topViewController as? NativeScreenController else { return }
        switching = true
        source.prepareForNavigation { [weak self] safe in
            guard let self else { return }
            guard safe else { self.switching = false; return }
            for (index, navigation) in self.navigations.enumerated() {
                if let target = navigation.viewControllers.firstIndex(where: {
                    guard let screen = $0 as? NativeScreenController else { return false }
                    return screen.route == route || (draftKey != nil && screen.draftKey == draftKey)
                }) {
                    guard navigation.viewControllers.dropFirst(target + 1).allSatisfy({ ($0 as? NativeScreenController)?.navigationBlocked != true }) else {
                        self.switching = false
                        return
                    }
                    for removed in navigation.viewControllers.dropFirst(target + 1) { (removed as? NativeScreenController)?.closeSession() }
                    (navigation.viewControllers[target] as? NativeScreenController)?.skipRefreshOnAppear = true
                    navigation.popToViewController(navigation.viewControllers[target], animated: false)
                    self.selectedIndex = index
                    self.switching = false
                    self.persist()
                    return
                }
            }
            guard self.navigations.flatMap(\.viewControllers).allSatisfy({ ($0 as? NativeScreenController)?.hasUncertainEditor != true }) else {
                self.switching = false
                return
            }
            source.setNavigationInFlight(true)
            self.host.open(route) { [weak self] result in
                guard let self else { return }
                source.setNavigationInFlight(false)
                self.switching = false
                guard source.navigationStillSafe else {
                    if case .success(let reply) = result, let instance = reply.instance { self.host.command(["kind": "close", "instance": instance]) { _ in } }
                    return
                }
                guard case .success(let reply) = result, reply.error == nil, let screen = reply.screen else {
                    source.showFailure(MobileHost.failure(nil))
                    return
                }
                let path = route.split(separator: "?", maxSplits: 1).first.map(String.init) ?? route
                let target = self.tabModels.indices.filter {
                    let root = self.tabModels[$0].route.split(separator: "?", maxSplits: 1).first.map(String.init) ?? self.tabModels[$0].route
                    return self.navigations[$0].tabBarItem.isEnabled && (path == root || path.hasPrefix(root + "/"))
                }.max { self.tabModels[$0].route.count < self.tabModels[$1].route.count } ?? self.selectedIndex
                self.selectedIndex = target
                self.navigations[target].pushViewController(NativeScreenController(screen, host: self.host, reply: reply), animated: true)
                self.persist()
            }
        }
    }

    func persist() {
        guard !restoring, tabModels.indices.contains(selectedIndex) else { return }
        var stacks: [String: [String]] = [:]
        for (index, tab) in tabModels.enumerated() {
            let controllers = navigations[index].viewControllers.compactMap { $0 as? NativeScreenController }
            let routes = controllers.prefix(MobileNavigationMetadata.maximumDepth).map(\.route)
            if routes.first == tab.route, routes.allSatisfy({ MobileLocalRoute.validate($0) != nil }) { stacks[tab.id] = routes }
        }
        let metadata = MobileNavigationMetadata(version: 1, activeTab: tabModels[selectedIndex].id, stacks: stacks)
        if let data = try? JSONEncoder().encode(metadata), data.count <= 65536 { UserDefaults.standard.set(data, forKey: storageKey) }
    }

    var retainedScreens: [NativeScreenController] { navigations.flatMap(\.viewControllers).compactMap { $0 as? NativeScreenController } }
}
