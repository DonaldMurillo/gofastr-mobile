import UIKit

final class MobilePresentationTask: NSObject, MobileForegroundTask, UIAdaptivePresentationControllerDelegate, UIPopoverPresentationControllerDelegate {
    let identifier: String
    private let model: MobilePresentation
    private weak var source: UIViewController?
    private let submission: (String, [String]) -> Void
    private let focus: MobileForegroundFocus
    private var controller: UIViewController?
    private var selectionController: MobileSelectionController?
    private var pending = false
    private var finished = false
    private var message: String?

    init(_ model: MobilePresentation, identifier: String, source: UIViewController, focus: MobileForegroundFocus,
         respond: @escaping (String, [String]) -> Void) {
        self.model = model
        self.identifier = identifier
        self.source = source
        submission = respond
        self.focus = focus
        super.init()
    }

    func show() {
        guard !finished, let source else { return }
        let controller: UIViewController
        if model.selection != nil || model.kind == "dialog" || model.kind == "sheet" {
            let content = MobileSelectionController(model) { [weak self] choice, selected in
                self?.respond(choice, selected: selected)
            }
            content.setPending(pending, message: message)
            selectionController = content
            controller = UINavigationController(rootViewController: content)
            controller.modalPresentationStyle = model.kind == "dialog" ? .formSheet : .pageSheet
            controller.sheetPresentationController?.detents = model.kind == "sheet" ? [.medium(), .large()] : [.large()]
            controller.sheetPresentationController?.prefersGrabberVisible = model.dismissible
            controller.sheetPresentationController?.prefersScrollingExpandsWhenScrolledToEdge = true
        } else {
            let style: UIAlertController.Style = model.kind == "menu" ? .actionSheet : .alert
            let text = [model.message, message].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n\n")
            let alert = UIAlertController(title: model.title, message: text.isEmpty ? nil : text, preferredStyle: style)
            var cancelAdded = false
            for choice in model.choices {
                let style: UIAlertAction.Style
                if choice.role == "cancel" && !cancelAdded { style = .cancel; cancelAdded = true }
                else { style = choice.role == "destructive" ? .destructive : .default }
                let action = UIAlertAction(title: choice.label, style: style) { [weak self] _ in
                    self?.respond(choice.id, selected: [])
                }
                action.isEnabled = !choice.disabled && !pending
                alert.addAction(action)
                if alert.preferredAction == nil && style == .default && !choice.disabled && model.kind != "menu" {
                    alert.preferredAction = action
                }
            }
            if style == .actionSheet && model.dismissible && !cancelAdded {
                alert.addAction(UIAlertAction(title: NSLocalizedString("Cancel", comment: "Dismiss a presentation"), style: .cancel) { [weak self] _ in
                    self?.respond("", selected: [])
                })
            }
            if let popover = alert.popoverPresentationController {
                popover.delegate = self
                popover.barButtonItem = source.navigationItem.rightBarButtonItems?.last
                popover.sourceView = source.view
                popover.sourceRect = CGRect(x: source.view.bounds.midX, y: source.view.safeAreaInsets.top, width: 1, height: 1)
            }
            controller = alert
        }
        self.controller = controller
        controller.isModalInPresentation = !model.dismissible || pending
        controller.view.accessibilityIdentifier = "native.presentation"
        controller.view.accessibilityViewIsModal = true
        if !(controller is UIAlertController) { controller.presentationController?.delegate = self }
        source.present(controller, animated: true) {
            if !(controller is UIAlertController) { controller.presentationController?.delegate = self }
            if controller is UIAlertController { UIAccessibility.post(notification: .screenChanged, argument: controller.view) }
        }
    }

    private func respond(_ choice: String, selected: [String]) {
        guard !pending, !finished else { return }
        pending = true
        controller?.isModalInPresentation = true
        (controller as? UIAlertController)?.actions.forEach { $0.isEnabled = false }
        selectionController?.setPending(true)
        submission(choice, selected)
    }

    func reject(_ message: String) {
        guard !finished else { return }
        self.message = message
        if let selectionController {
            pending = false
            controller?.isModalInPresentation = !model.dismissible
            selectionController.setPending(false, message: message)
            if controller?.presentingViewController != nil {
                UIAccessibility.post(notification: .announcement, argument: message)
            } else if let controller {
                source?.present(controller, animated: true) { UIAccessibility.post(notification: .announcement, argument: message) }
            }
            return
        }
        dismissController { [weak self] in
            guard let self, !self.finished else { return }
            self.pending = false
            self.show()
        }
    }

    func finish(restoreFocus: Bool, completion: @escaping () -> Void) {
        finished = true
        dismissController { [self] in
            if restoreFocus { focus.restore(source) }
            completion()
        }
    }

    private func dismissController(completion: @escaping () -> Void) {
        MobileForegroundDismissal.dismiss(controller, completion: completion)
    }

    func presentationControllerShouldDismiss(_ presentationController: UIPresentationController) -> Bool {
        model.dismissible && !pending && !finished
    }

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) { cancelGesture() }

    func popoverPresentationControllerShouldDismissPopover(_ popoverPresentationController: UIPopoverPresentationController) -> Bool {
        model.dismissible && !pending && !finished
    }

    func popoverPresentationControllerDidDismissPopover(_ popoverPresentationController: UIPopoverPresentationController) { cancelGesture() }

    private func cancelGesture() {
        guard model.dismissible else { return }
        respond("", selected: selectionController?.selected ?? [])
    }

}

private final class MobileSelectionController: UITableViewController {
    private let model: MobilePresentation
    private let submission: (String, [String]) -> Void
    private var state: MobilePresentationSelectionState
    private var pending = false
    private var message: String?
    private var heading: UILabel?

    var selected: [String] { state.selected }

    init(_ model: MobilePresentation, respond: @escaping (String, [String]) -> Void) {
        self.model = model
        submission = respond
        state = MobilePresentationSelectionState(model.selection)
        super.init(style: .insetGrouped)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = model.title
        view.backgroundColor = .systemGroupedBackground
        tableView.rowHeight = UITableView.automaticDimension
        tableView.estimatedRowHeight = 56
        tableView.accessibilityIdentifier = "native.presentation.selection"
        if model.dismissible {
            let close = UIBarButtonItem(systemItem: .close, primaryAction: UIAction { [weak self] _ in self?.cancel() })
            close.accessibilityLabel = "Dismiss"
            close.accessibilityIdentifier = "native.presentation.dismiss"
            navigationItem.leftBarButtonItem = close
        }
        let label = UILabel()
        label.text = model.title
        label.font = .preferredFont(forTextStyle: .title2)
        label.adjustsFontForContentSizeCategory = true
        label.numberOfLines = 0
        label.accessibilityTraits.insert(.header)
        heading = label
        let stack = UIStackView(arrangedSubviews: [label])
        stack.axis = .vertical
        stack.spacing = 12
        stack.isLayoutMarginsRelativeArrangement = true
        stack.directionalLayoutMargins = NSDirectionalEdgeInsets(top: 16, leading: 20, bottom: 20, trailing: 20)
        if let message = model.message, !message.isEmpty {
            let detail = UILabel()
            detail.text = message
            detail.font = .preferredFont(forTextStyle: .body)
            detail.adjustsFontForContentSizeCategory = true
            detail.numberOfLines = 0
            stack.addArrangedSubview(detail)
        }
        tableView.tableHeaderView = stack
        setPending(pending, message: message)
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        guard let header = tableView.tableHeaderView else { return }
        let size = header.systemLayoutSizeFitting(CGSize(width: tableView.bounds.width, height: 0),
            withHorizontalFittingPriority: .required, verticalFittingPriority: .fittingSizeLevel)
        if header.frame.size != size { header.frame.size = size; tableView.tableHeaderView = header }
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        UIAccessibility.post(notification: .screenChanged, argument: heading)
    }

    func setPending(_ pending: Bool, message: String? = nil) {
        self.pending = pending
        self.message = message
        guard isViewLoaded else { return }
        navigationItem.leftBarButtonItem?.isEnabled = !pending
        tableView.reloadData()
    }

    override func numberOfSections(in tableView: UITableView) -> Int { 2 }

    override func tableView(_ tableView: UITableView, numberOfRowsInSection section: Int) -> Int {
        section == 0 ? model.selection?.options.count ?? 0 : model.choices.count
    }

    override func tableView(_ tableView: UITableView, cellForRowAt indexPath: IndexPath) -> UITableViewCell {
        let cell = UITableViewCell(style: .subtitle, reuseIdentifier: nil)
        var content = cell.defaultContentConfiguration()
        content.textProperties.font = .preferredFont(forTextStyle: .body)
        content.textProperties.numberOfLines = 0
        content.secondaryTextProperties.numberOfLines = 0
        cell.accessibilityTraits = .button
        let disabled: Bool
        if indexPath.section == 0, let option = model.selection?.options[indexPath.row] {
            content.text = option.label
            content.secondaryText = option.detail
            disabled = option.disabled || pending
            if selected.contains(option.id) { cell.accessoryType = .checkmark; cell.accessibilityTraits.insert(.selected) }
            cell.accessibilityLabel = [option.label, option.detail].compactMap { $0 }.joined(separator: ", ")
            cell.accessibilityIdentifier = "native.presentation.option.\(option.id)"
        } else {
            let choice = model.choices[indexPath.row]
            content.text = choice.label
            content.textProperties.alignment = .center
            content.textProperties.color = choice.role == "destructive" ? .systemRed : view.tintColor
            content.textProperties.font = .preferredFont(forTextStyle: choice.role == "cancel" ? .body : .headline)
            disabled = choice.disabled || pending
            cell.accessibilityLabel = choice.label
            cell.accessibilityHint = choice.role == "destructive" ? "Destructive action" : nil
            cell.accessibilityIdentifier = "native.presentation.choice.\(choice.id)"
        }
        if disabled {
            content.textProperties.color = .tertiaryLabel
            cell.accessibilityTraits.insert(.notEnabled)
            cell.selectionStyle = .none
        }
        cell.contentConfiguration = content
        return cell
    }

    override func tableView(_ tableView: UITableView, willSelectRowAt indexPath: IndexPath) -> IndexPath? {
        guard !pending else { return nil }
        if indexPath.section == 0 { return model.selection?.options[indexPath.row].disabled == false ? indexPath : nil }
        return model.choices[indexPath.row].disabled ? nil : indexPath
    }

    override func tableView(_ tableView: UITableView, didSelectRowAt indexPath: IndexPath) {
        tableView.deselectRow(at: indexPath, animated: true)
        guard !pending else { return }
        if indexPath.section == 0, let option = model.selection?.options[indexPath.row] {
            guard !option.disabled else { return }
            state.toggle(option)
            for visible in tableView.indexPathsForVisibleRows ?? [] where visible.section == 0 {
                guard let identifier = model.selection?.options[visible.row].id, let cell = tableView.cellForRow(at: visible) else { continue }
                let checked = selected.contains(identifier)
                cell.accessoryType = checked ? .checkmark : .none
                if checked { cell.accessibilityTraits.insert(.selected) }
                else { cell.accessibilityTraits.remove(.selected) }
            }
            UIAccessibility.post(notification: .layoutChanged, argument: tableView.cellForRow(at: indexPath))
        } else {
            let choice = model.choices[indexPath.row]
            guard !choice.disabled else { return }
            submission(choice.id, selected)
        }
    }

    override func tableView(_ tableView: UITableView, titleForFooterInSection section: Int) -> String? {
        section == 1 ? (pending ? "Working…" : message) : nil
    }

    override func accessibilityPerformEscape() -> Bool {
        guard model.dismissible, !pending else { return false }
        cancel()
        return true
    }

    private func cancel() {
        guard model.dismissible, !pending else { return }
        submission("", selected)
    }
}
