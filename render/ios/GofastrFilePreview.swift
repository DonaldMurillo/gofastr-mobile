import UIKit

final class MobileFilePreviewHeader: UIStackView {
    init(_ preview: MobileFilePreview) {
        super.init(frame: .zero)
        axis = .vertical
        alignment = .center
        spacing = 12
        isLayoutMarginsRelativeArrangement = true
        directionalLayoutMargins = NSDirectionalEdgeInsets(top: 12, leading: 0, bottom: 12, trailing: 0)
        let tile = UIView()
        tile.backgroundColor = .secondarySystemGroupedBackground
        tile.layer.cornerRadius = 18
        tile.widthAnchor.constraint(equalToConstant: 76).isActive = true
        tile.heightAnchor.constraint(equalToConstant: 76).isActive = true
        let icon = UIImageView(image: UIImage(systemName: MobileAppearance.fileSymbol(preview.file.icon)))
        icon.translatesAutoresizingMaskIntoConstraints = false
        icon.tintColor = MobileAppearance.accent
        icon.contentMode = .scaleAspectFit
        tile.addSubview(icon)
        NSLayoutConstraint.activate([
            icon.centerXAnchor.constraint(equalTo: tile.centerXAnchor),
            icon.centerYAnchor.constraint(equalTo: tile.centerYAnchor),
            icon.widthAnchor.constraint(equalToConstant: 40),
            icon.heightAnchor.constraint(equalToConstant: 44)
        ])
        addArrangedSubview(tile)
        let title = MobileAppearance.label(preview.file.name, style: .title2)
        title.textAlignment = .center
        title.font = UIFont(descriptor: title.font.fontDescriptor.withSymbolicTraits(.traitBold) ?? title.font.fontDescriptor, size: 0)
        title.accessibilityTraits.insert(.header)
        title.accessibilityIdentifier = "native.file.name"
        addArrangedSubview(title)
        let detail = MobileAppearance.label(preview.file.detail, style: .subheadline, color: .secondaryLabel)
        detail.textAlignment = .center
        addArrangedSubview(detail)
        if let caption = preview.caption, !caption.isEmpty {
            let label = MobileAppearance.label(caption, style: .caption1, color: .secondaryLabel)
            label.textAlignment = .center
            addArrangedSubview(label)
        }
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }
}
