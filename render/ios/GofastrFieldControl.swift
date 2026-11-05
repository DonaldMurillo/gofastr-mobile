import UIKit

final class MobileTypedControl: UIStackView {
    private(set) var field: MobileField
    private var storedValue: String
    private var enabled = true
    private let toggle = UISwitch()
    private let select = UIButton(type: .system)
    private let picker = UIDatePicker()
    private let clear = UIButton(type: .system)
    private let set = UIButton(type: .system)
    private let empty = UILabel()
    var changed: (() -> Void)?

    init(_ field: MobileField) {
        self.field = field
        storedValue = field.value
        super.init(frame: .zero)
        axis = .horizontal
        alignment = .leading
        spacing = 8
        if field.kind == "toggle" {
            addArrangedSubview(toggle)
            toggle.addTarget(self, action: #selector(toggled), for: .valueChanged)
            toggle.accessibilityIdentifier = "native.field.\(field.key)"
            toggle.accessibilityLabel = field.label
        } else if field.kind == "select" {
            select.titleLabel?.font = .preferredFont(forTextStyle: .body)
            select.titleLabel?.adjustsFontForContentSizeCategory = true
            select.titleLabel?.numberOfLines = 0
            select.contentHorizontalAlignment = .leading
            select.showsMenuAsPrimaryAction = true
            select.accessibilityIdentifier = "native.field.\(field.key)"
            select.accessibilityLabel = field.label
            select.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
            addArrangedSubview(select)
        } else {
            picker.datePickerMode = field.kind == "date" ? .date : .time
            picker.preferredDatePickerStyle = .compact
            picker.calendar = Calendar(identifier: .gregorian)
            picker.timeZone = TimeZone(secondsFromGMT: 0)
            picker.accessibilityIdentifier = "native.field.\(field.key)"
            picker.accessibilityLabel = field.label
            picker.addTarget(self, action: #selector(dateChanged), for: .valueChanged)
            addArrangedSubview(picker)
            empty.font = .preferredFont(forTextStyle: .footnote)
            empty.adjustsFontForContentSizeCategory = true
            empty.numberOfLines = 0
            empty.isHidden = true
            set.setTitle("Set \(field.label)", for: .normal)
            set.titleLabel?.font = .preferredFont(forTextStyle: .body)
            set.titleLabel?.adjustsFontForContentSizeCategory = true
            set.accessibilityIdentifier = "native.field.\(field.key).set"
            set.addTarget(self, action: #selector(dateChanged), for: .touchUpInside)
            set.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
            addArrangedSubview(set)
            clear.setTitle("Clear", for: .normal)
            clear.accessibilityLabel = "Clear \(field.label)"
            clear.titleLabel?.font = .preferredFont(forTextStyle: .body)
            clear.titleLabel?.adjustsFontForContentSizeCategory = true
            clear.accessibilityIdentifier = "native.field.\(field.key).clear"
            clear.addTarget(self, action: #selector(cleared), for: .touchUpInside)
            clear.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
            addArrangedSubview(clear)
        }
        update()
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    var value: String {
        get { field.disabled == true ? field.value : storedValue }
        set { storedValue = field.disabled == true ? field.value : newValue; update() }
    }

    func configure(_ field: MobileField, enabled: Bool) {
        self.field = field
        self.enabled = enabled
        if field.disabled == true { storedValue = field.value }
        update()
    }

    private func update() {
        let available = enabled && field.disabled != true
        toggle.isEnabled = available
        select.isEnabled = available
        picker.isEnabled = available
        clear.isEnabled = available
        set.isEnabled = available
        toggle.isOn = storedValue == "true"
        if field.kind == "select" {
            let selected = field.options?.first { $0.value == storedValue }
            select.setTitle(selected?.label ?? (storedValue.isEmpty ? field.placeholder ?? "Choose…" : storedValue), for: .normal)
            var options = (field.options ?? []).map { option in
                UIAction(title: option.label, attributes: option.disabled == true ? [.disabled] : [], state: option.value == storedValue ? .on : .off) { [weak self] _ in
                    guard let self, self.enabled, self.field.disabled != true, option.disabled != true else { return }
                    self.value = option.value
                    self.changed?()
                }
            }
            if field.required != true && !(field.options ?? []).contains(where: { $0.value.isEmpty }) {
                options.insert(UIAction(title: "None", state: storedValue.isEmpty ? .on : .off) { [weak self] _ in self?.cleared() }, at: 0)
            }
            select.menu = UIMenu(children: options)
            select.accessibilityValue = selected?.label ?? storedValue
        }
        if let date = MobileFieldDate.parse(storedValue, kind: field.kind) { picker.setDate(date, animated: false) }
        empty.text = storedValue.isEmpty ? "Not set. Use the picker to choose \(field.label.lowercased())." : storedValue
        picker.accessibilityValue = storedValue.isEmpty ? "Not set" : storedValue
        picker.isHidden = storedValue.isEmpty
        clear.isHidden = field.required == true || storedValue.isEmpty
        set.isHidden = !storedValue.isEmpty
    }

    @objc private func toggled() {
        guard enabled, field.disabled != true else { update(); return }
        storedValue = toggle.isOn ? "true" : "false"
        changed?()
    }

    @objc private func dateChanged() {
        guard enabled, field.disabled != true else { update(); return }
        value = MobileFieldDate.formatter(field.kind).string(from: picker.date)
        changed?()
    }

    @objc private func cleared() {
        guard enabled, field.disabled != true, field.required != true else { return }
        value = ""
        changed?()
    }
}

final class MobileRowActionsController: UITableViewController {
    let row: MobileRow
    var invoke: ((String) -> Void)?

    init(row: MobileRow) {
        self.row = row
        super.init(style: .insetGrouped)
        title = "Actions for \(row.title)"
        preferredContentSize = CGSize(width: 340, height: min(500, CGFloat(row.actions.count * 60 + 60)))
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    override func viewDidLoad() {
        super.viewDidLoad()
        navigationItem.rightBarButtonItem = UIBarButtonItem(systemItem: .close, primaryAction: UIAction { [weak self] _ in self?.dismiss(animated: true) })
    }

    override func tableView(_ tableView: UITableView, numberOfRowsInSection section: Int) -> Int { row.actions.count }

    override func tableView(_ tableView: UITableView, cellForRowAt indexPath: IndexPath) -> UITableViewCell {
        let action = row.actions[indexPath.row]
        let cell = UITableViewCell(style: .default, reuseIdentifier: nil)
        var content = cell.defaultContentConfiguration()
        content.text = action.label
        content.textProperties.font = .preferredFont(forTextStyle: .body)
        content.textProperties.numberOfLines = 0
        content.textProperties.color = action.role == "destructive" ? .systemRed : .label
        cell.contentConfiguration = content
        cell.accessibilityIdentifier = "native.row-action.\(row.id).\(action.id)"
        cell.accessibilityTraits.insert(.button)
        return cell
    }

    override func tableView(_ tableView: UITableView, didSelectRowAt indexPath: IndexPath) {
        let action = row.actions[indexPath.row]
        dismiss(animated: true) { self.invoke?(action.id) }
    }
}
