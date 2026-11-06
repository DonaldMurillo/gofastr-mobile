import Foundation

struct MobileImage: Decodable, Equatable {
    let id: String
    let alt: String
    let data: Data
    let width: Int
    let height: Int
    let destination: String?
}

#if canImport(UIKit)
import UIKit

final class MobileImageGallery: UIStackView {
    var open: ((String) -> Void)?
    var compact = false
    private let heading = UILabel()
    private let items = MobileKeyedResources<MobileImageControl>(dispose: { $0.removeFromSuperview() })

    init() {
        super.init(frame: .zero)
        axis = .vertical
        spacing = 12
        isHidden = true
        heading.font = .preferredFont(forTextStyle: .subheadline)
        heading.textColor = .secondaryLabel
        heading.adjustsFontForContentSizeCategory = true
        heading.numberOfLines = 0
        heading.accessibilityTraits = .header
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func update(_ images: [MobileImage]) {
        isHidden = images.isEmpty
        heading.text = images.count == 1 ? "Photo" : "Photos"
        items.reconcile(images.map(\.id)) { _ in MobileImageControl() }
        for preview in images {
            items[preview.id]?.update(preview, compact: compact, open: open)
        }
        let children: [UIView] = images.isEmpty ? [] : [heading] + items.resources
        if arrangedSubviews.count != children.count || zip(arrangedSubviews, children).contains(where: { $0 !== $1 }) {
            for child in arrangedSubviews {
                removeArrangedSubview(child)
                child.removeFromSuperview()
            }
            children.forEach(addArrangedSubview)
        }
    }
}

private final class MobileImageControl: UIControl {
    private let picture = UIImageView()
    private var preview: MobileImage?
    private var compact = false
    private var aspect: NSLayoutConstraint?
    private var action: UIAction?

    init() {
        super.init(frame: .zero)
        picture.translatesAutoresizingMaskIntoConstraints = false
        picture.layer.cornerRadius = 16
        picture.clipsToBounds = true
        addSubview(picture)
        NSLayoutConstraint.activate([
            picture.topAnchor.constraint(equalTo: topAnchor), picture.bottomAnchor.constraint(equalTo: bottomAnchor),
            picture.leadingAnchor.constraint(equalTo: leadingAnchor), picture.trailingAnchor.constraint(equalTo: trailingAnchor)
        ])
        isAccessibilityElement = true
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func update(_ next: MobileImage, compact: Bool, open: ((String) -> Void)?) {
        if let action { removeAction(action, for: .touchUpInside) }
        action = nil
        guard next.width > 0, next.height > 0, next.data.count <= 128 * 1024 else { isHidden = true; return }
        if preview?.data != next.data {
            guard let image = UIImage(data: next.data) else { isHidden = true; return }
            picture.image = image
        }
        guard picture.image != nil else { isHidden = true; return }
        isHidden = false
        if aspect == nil || preview?.width != next.width || preview?.height != next.height || self.compact != compact {
            aspect?.isActive = false
            aspect = compact ? picture.heightAnchor.constraint(equalToConstant: 200) :
                picture.heightAnchor.constraint(equalTo: picture.widthAnchor, multiplier: CGFloat(next.height) / CGFloat(next.width))
            aspect?.isActive = true
        }
        preview = next
        self.compact = compact
        picture.contentMode = compact ? .scaleAspectFill : .scaleAspectFit
        accessibilityLabel = next.alt
        accessibilityIdentifier = "native.image.\(next.id)"
        accessibilityTraits = .image
        accessibilityHint = nil
        if let destination = next.destination, !destination.isEmpty {
            accessibilityTraits.insert(.button)
            accessibilityHint = "Open photo attachment"
            let action = UIAction { _ in open?(destination) }
            self.action = action
            addAction(action, for: .touchUpInside)
        }
    }
}
#endif
