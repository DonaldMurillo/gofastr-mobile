import UIKit

enum MobileAppearance {
    static let accent = UIColor { traits in
        traits.userInterfaceStyle == .dark
            ? UIColor(red: 1, green: 0.8, blue: 0.408, alpha: 1)
            : UIColor(red: 0.659, green: 0.369, blue: 0, alpha: 1)
    }

    static func label(_ text: String, style: UIFont.TextStyle, color: UIColor = .label) -> UILabel {
        let label = UILabel()
        label.text = text
        label.font = .preferredFont(forTextStyle: style)
        label.adjustsFontForContentSizeCategory = true
        label.textColor = color
        label.numberOfLines = 0
        return label
    }

    static func fileSymbol(_ icon: String?) -> String {
        switch icon {
        case "photo": return "photo"
        case "audio": return "waveform"
        default: return "doc.text"
        }
    }
}

final class MobileDisclosureGroup: UIStackView {
    let content = UIStackView()
    private let button = UIButton(type: .system)
    private let heading: String
    private(set) var expanded = false
    var layoutChanged: (() -> Void)?

    init(_ title: String, identifier: String) {
        heading = title
        super.init(frame: .zero)
        axis = .vertical
        spacing = 12
        content.axis = .vertical
        content.spacing = 12
        content.isHidden = true
        content.isLayoutMarginsRelativeArrangement = true
        content.directionalLayoutMargins = NSDirectionalEdgeInsets(top: 0, leading: 16, bottom: 16, trailing: 16)
        button.accessibilityIdentifier = identifier
        button.contentHorizontalAlignment = .leading
        button.heightAnchor.constraint(greaterThanOrEqualToConstant: 48).isActive = true
        button.addAction(UIAction { [weak self] _ in
            guard let self else { return }
            self.setExpanded(!self.expanded)
        }, for: .touchUpInside)
        backgroundColor = .secondarySystemGroupedBackground
        layer.cornerRadius = 16
        addArrangedSubview(button)
        addArrangedSubview(content)
        update()
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func setExpanded(_ expanded: Bool) {
        self.expanded = expanded
        content.isHidden = !expanded
        update()
        layoutChanged?()
    }

    private func update() {
        var configuration = UIButton.Configuration.plain()
        configuration.title = heading
        configuration.image = UIImage(systemName: expanded ? "chevron.down" : "chevron.right")
        configuration.imagePlacement = .trailing
        configuration.imagePadding = 12
        configuration.contentInsets = NSDirectionalEdgeInsets(top: 12, leading: 16, bottom: 12, trailing: 16)
        configuration.baseForegroundColor = .secondaryLabel
        button.configuration = configuration
        button.titleLabel?.font = .preferredFont(forTextStyle: .subheadline)
        button.titleLabel?.adjustsFontForContentSizeCategory = true
        button.titleLabel?.numberOfLines = 0
        button.accessibilityLabel = heading
        button.accessibilityValue = expanded ? "Expanded" : "Collapsed"
        button.accessibilityHint = expanded ? "Hide details" : "Show details"
    }
}
