import Foundation

struct MobileImage: Decodable {
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
    private var previews: [MobileImage] = []

    init() {
        super.init(frame: .zero)
        axis = .vertical
        spacing = 12
        isHidden = true
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func update(_ images: [MobileImage]) {
        guard images.count != previews.count || zip(images, previews).contains(where: { $0.id != $1.id || $0.data != $1.data || $0.alt != $1.alt || $0.destination != $1.destination }) else { return }
        previews = images
        for child in arrangedSubviews {
            removeArrangedSubview(child)
            child.removeFromSuperview()
        }
        isHidden = images.isEmpty
        guard !images.isEmpty else { return }
        let heading = UILabel()
        heading.text = images.count == 1 ? "Photo" : "Photos"
        heading.font = .preferredFont(forTextStyle: .subheadline)
        heading.textColor = .secondaryLabel
        heading.adjustsFontForContentSizeCategory = true
        heading.numberOfLines = 0
        heading.accessibilityTraits = .header
        addArrangedSubview(heading)
        for preview in images {
            guard preview.width > 0, preview.height > 0, preview.data.count <= 128 * 1024,
                  let image = UIImage(data: preview.data) else { continue }
            let container = UIControl()
            let picture = UIImageView(image: image)
            picture.translatesAutoresizingMaskIntoConstraints = false
            picture.contentMode = compact ? .scaleAspectFill : .scaleAspectFit
            picture.layer.cornerRadius = 16
            picture.clipsToBounds = true
            container.addSubview(picture)
            NSLayoutConstraint.activate([
                picture.topAnchor.constraint(equalTo: container.topAnchor),
                picture.bottomAnchor.constraint(equalTo: container.bottomAnchor),
                picture.leadingAnchor.constraint(equalTo: container.leadingAnchor),
                picture.trailingAnchor.constraint(equalTo: container.trailingAnchor)
            ])
            if compact {
                picture.heightAnchor.constraint(equalToConstant: 200).isActive = true
            } else {
                picture.heightAnchor.constraint(equalTo: picture.widthAnchor, multiplier: CGFloat(preview.height) / CGFloat(preview.width)).isActive = true
            }
            container.isAccessibilityElement = true
            container.accessibilityLabel = preview.alt
            container.accessibilityIdentifier = "native.image.\(preview.id)"
            container.accessibilityTraits = .image
            if let destination = preview.destination, !destination.isEmpty {
                container.accessibilityTraits.insert(.button)
                container.accessibilityHint = "Open photo attachment"
                container.addAction(UIAction { [weak self] _ in self?.open?(destination) }, for: .touchUpInside)
            }
            addArrangedSubview(container)
        }
    }
}
#endif
