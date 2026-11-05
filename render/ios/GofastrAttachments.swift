import Foundation

struct MobileFilePreview: Decodable {
    let file: MobileAttachment
    let caption: String?
    let previewTitle: String?
    let text: String?
}

struct MobileAttachment: Decodable, Equatable {
    let id: String
    let name: String
    let mediaType: String
    let size: Int
    let destination: String
    let audio: MobileAudioDocument?
    let icon: String?
    let displayDetail: String?

    enum CodingKeys: String, CodingKey {
        case id, name, mediaType, size, destination, audio, icon
        case displayDetail = "detail"
    }

    var detail: String { displayDetail ?? "File · \(size) B" }
}

#if canImport(UIKit)
import UIKit

final class MobileAttachmentList: UIStackView {
    var open: ((String) -> Void)?
    private var files: [MobileAttachment] = []
    private var players: [MobileAudioPreview] = []

    init() {
        super.init(frame: .zero)
        axis = .vertical
        spacing = 8
        isHidden = true
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func update(_ attachments: [MobileAttachment]) {
        guard attachments != files else { return }
        stop()
        players.removeAll()
        files = attachments
        for child in arrangedSubviews {
            removeArrangedSubview(child)
            child.removeFromSuperview()
        }
        isHidden = files.isEmpty
        guard !files.isEmpty else { return }
        let heading = label("Attachments", style: .subheadline)
        heading.textColor = .secondaryLabel
        heading.accessibilityTraits = .header
        addArrangedSubview(heading)
        for file in files {
            let row = UIControl()
            row.backgroundColor = .secondarySystemBackground
            row.layer.cornerRadius = 16
            let icon = UIImageView(image: UIImage(systemName: MobileAppearance.fileSymbol(file.icon)))
            icon.tintColor = MobileAppearance.accent
            icon.backgroundColor = MobileAppearance.accent.withAlphaComponent(0.10)
            icon.layer.cornerRadius = 10
            icon.contentMode = .scaleAspectFit
            icon.preferredSymbolConfiguration = UIImage.SymbolConfiguration(pointSize: 22, weight: .regular)
            icon.widthAnchor.constraint(equalToConstant: 44).isActive = true
            icon.heightAnchor.constraint(equalToConstant: 44).isActive = true
            let labels = UIStackView(arrangedSubviews: [label(file.name, style: .subheadline), label(file.detail, style: .caption1)])
            labels.axis = .vertical
            labels.spacing = 4
            let chevron = UIImageView(image: UIImage(systemName: "chevron.forward"))
            chevron.tintColor = .tertiaryLabel
            chevron.setContentHuggingPriority(.required, for: .horizontal)
            let content = UIStackView(arrangedSubviews: [icon, labels, chevron])
            content.axis = .horizontal
            content.alignment = .center
            content.spacing = 12
            content.isUserInteractionEnabled = false
            content.translatesAutoresizingMaskIntoConstraints = false
            row.addSubview(content)
            NSLayoutConstraint.activate([
                row.heightAnchor.constraint(greaterThanOrEqualToConstant: 60),
                content.topAnchor.constraint(equalTo: row.topAnchor, constant: 16),
                content.bottomAnchor.constraint(equalTo: row.bottomAnchor, constant: -16),
                content.leadingAnchor.constraint(equalTo: row.leadingAnchor, constant: 16),
                content.trailingAnchor.constraint(equalTo: row.trailingAnchor, constant: -16)
            ])
            row.isAccessibilityElement = true
            row.accessibilityTraits = .button
            row.accessibilityLabel = file.name
            row.accessibilityValue = file.detail
            row.accessibilityHint = "Open file attachment"
            row.accessibilityIdentifier = "native.attachment.\(file.id)"
            row.addAction(UIAction { [weak self] _ in self?.open?(file.destination) }, for: .touchUpInside)
            addArrangedSubview(row)
            if let audio = file.audio {
                let player = MobileAudioPreview(audio, identifier: file.id)
                players.append(player)
                addArrangedSubview(player)
            }
        }
    }

    func stop() { players.forEach { $0.stop() } }

    private func label(_ value: String, style: UIFont.TextStyle) -> UILabel {
        let label = UILabel()
        label.text = value
        label.font = .preferredFont(forTextStyle: style)
        label.adjustsFontForContentSizeCategory = true
        label.numberOfLines = 0
        if style == .caption1 { label.textColor = .secondaryLabel }
        return label
    }
}
#endif
