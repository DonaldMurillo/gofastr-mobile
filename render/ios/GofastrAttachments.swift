import Foundation

struct MobileFilePreview: Decodable, Equatable {
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
    private let heading = MobileAppearance.label("Attachments", style: .subheadline, color: .secondaryLabel)
    private let items = MobileKeyedResources<MobileAttachmentItem>(dispose: { $0.stop(); $0.removeFromSuperview() })
    private var players: [MobileAudioPreview] { items.resources.compactMap(\.player) }

    init() {
        super.init(frame: .zero)
        axis = .vertical
        spacing = 8
        isHidden = true
        heading.accessibilityTraits = .header
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func update(_ attachments: [MobileAttachment]) {
        isHidden = attachments.isEmpty
        items.reconcile(attachments.map(\.id)) { _ in MobileAttachmentItem() }
        for file in attachments { items[file.id]?.update(file, open: open) }
        let children: [UIView] = attachments.isEmpty ? [] : [heading] + items.resources
        if arrangedSubviews.count != children.count || zip(arrangedSubviews, children).contains(where: { $0 !== $1 }) {
            for child in arrangedSubviews {
                removeArrangedSubview(child)
                child.removeFromSuperview()
            }
            children.forEach(addArrangedSubview)
        }
    }

    func stop() { players.forEach { $0.stop() } }

}

private final class MobileAttachmentItem: UIStackView {
    private let row = UIControl()
    private let icon = UIImageView()
    private let nameLabel = MobileAppearance.label("", style: .subheadline)
    private let detailLabel = MobileAppearance.label("", style: .caption1, color: .secondaryLabel)
    private let playback = MobileRetainedResource<MobileAudioDocument, MobileAudioPreview>(dispose: { $0.stop() })
    private var action: UIAction?
    var player: MobileAudioPreview? { playback.resource }

    init() {
        super.init(frame: .zero)
        axis = .vertical
        spacing = 8
        row.backgroundColor = .secondarySystemBackground
        row.layer.cornerRadius = 16
        icon.tintColor = MobileAppearance.accent
        icon.backgroundColor = MobileAppearance.accent.withAlphaComponent(0.10)
        icon.layer.cornerRadius = 10
        icon.contentMode = .scaleAspectFit
        icon.preferredSymbolConfiguration = UIImage.SymbolConfiguration(pointSize: 22, weight: .regular)
        icon.widthAnchor.constraint(equalToConstant: 44).isActive = true
        icon.heightAnchor.constraint(equalToConstant: 44).isActive = true
        let labels = UIStackView(arrangedSubviews: [nameLabel, detailLabel])
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
        row.accessibilityHint = "Open file attachment"
        addArrangedSubview(row)
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func update(_ file: MobileAttachment, open: ((String) -> Void)?) {
        icon.image = UIImage(systemName: MobileAppearance.fileSymbol(file.icon))
        nameLabel.text = file.name
        detailLabel.text = file.detail
        row.accessibilityLabel = file.name
        row.accessibilityValue = file.detail
        row.accessibilityIdentifier = "native.attachment.\(file.id)"
        if let action { row.removeAction(action, for: .touchUpInside) }
        let action = UIAction { _ in open?(file.destination) }
        self.action = action
        row.addAction(action, for: .touchUpInside)
        playback.update(file.audio) { audio in MobileAudioPreview(audio, identifier: file.id) }
        let children: [UIView] = [row] + (player.map { [$0] } ?? [])
        if arrangedSubviews.count != children.count || zip(arrangedSubviews, children).contains(where: { $0 !== $1 }) {
            for child in arrangedSubviews {
                removeArrangedSubview(child)
                child.removeFromSuperview()
            }
            children.forEach(addArrangedSubview)
        }
    }

    func stop() { player?.stop() }
}
#endif
