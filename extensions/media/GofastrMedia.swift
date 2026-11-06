import Foundation

struct GofastrMediaPayload: Decodable {
    let images: [MobileImage]?
    let attachments: [MobileAttachment]?
    let preview: MobileFilePreview?

    static func decode(_ payload: String) throws -> GofastrMediaPayload {
        guard !payload.isEmpty, payload.utf8.count <= MobileComponent.maximumPayloadBytes else {
            throw MobileComponentFailure.invalidPayload
        }
        return try JSONDecoder().decode(Self.self, from: Data(payload.utf8))
    }

    var eventImages: [MobileImage] {
        (images ?? []).enumerated().map { index, image in
            MobileImage(id: image.id, alt: image.alt, data: image.data, width: image.width, height: image.height,
                        destination: "image.\(index)")
        }
    }

    var eventAttachments: [MobileAttachment] {
        (attachments ?? []).enumerated().map { index, file in
            MobileAttachment(id: file.id, name: file.name, mediaType: file.mediaType, size: file.size,
                             destination: "file.\(index)", audio: file.audio, icon: file.icon, displayDetail: file.displayDetail)
        }
    }
}

#if canImport(UIKit)
import UIKit

enum GofastrMediaComponent: MobileComponentExtensionFactory {
    static let name = "gofastr.media/v1"

    static func capabilities() -> [String: String] { [name: "available"] }

    static func make(payload: String, compact: Bool, emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView {
        GofastrMediaView(try GofastrMediaPayload.decode(payload), compact: compact, emit: emit)
    }
}

final class GofastrMediaView: MobileComponentExtensionView {
    private let stack = UIStackView()
    private let gallery = MobileImageGallery()
    private let attachments = MobileAttachmentList()
    private var generation = UUID()
    var view: UIView { stack }

    init(_ media: GofastrMediaPayload, compact: Bool, emit: @escaping (String) -> Void) {
        stack.axis = .vertical
        stack.spacing = 20
        gallery.compact = compact
        apply(media, emit: emit)
    }

    func update(payload: String, emit: @escaping (String) -> Void) throws {
        apply(try GofastrMediaPayload.decode(payload), emit: emit)
    }

    private func apply(_ media: GofastrMediaPayload, emit: @escaping (String) -> Void) {
        generation = UUID()
        let current = generation
        let callback: (String) -> Void = { [weak self] event in
            guard self?.generation == current else { return }
            emit(event)
        }
        var children: [UIView] = []
        if let preview = media.preview {
            let control = UIControl()
            let header = MobileFilePreviewHeader(preview, open: { callback("preview") })
            header.isUserInteractionEnabled = false
            header.translatesAutoresizingMaskIntoConstraints = false
            control.addSubview(header)
            NSLayoutConstraint.activate([
                header.topAnchor.constraint(equalTo: control.topAnchor),
                header.bottomAnchor.constraint(equalTo: control.bottomAnchor),
                header.leadingAnchor.constraint(equalTo: control.leadingAnchor),
                header.trailingAnchor.constraint(equalTo: control.trailingAnchor)
            ])
            control.isAccessibilityElement = false
            control.accessibilityIdentifier = "native.file.open"
            control.addAction(UIAction { _ in callback("preview") }, for: .touchUpInside)
            children.append(control)
        }
        gallery.open = callback
        gallery.update(media.eventImages)
        children.append(gallery)
        attachments.open = callback
        attachments.update(media.eventAttachments)
        children.append(attachments)
        if let preview = media.preview, let text = preview.text, !text.isEmpty {
            let divider = UIView()
            divider.backgroundColor = .separator
            divider.heightAnchor.constraint(equalToConstant: 0.5).isActive = true
            children.append(divider)
            let heading = MobileAppearance.label(preview.previewTitle ?? "Text preview", style: .footnote, color: .secondaryLabel)
            heading.accessibilityTraits.insert(.header)
            children.append(heading)
            let body = MobileAppearance.label(text, style: .body)
            body.accessibilityIdentifier = "native.file.preview"
            children.append(body)
        }
        if stack.arrangedSubviews.count != children.count || zip(stack.arrangedSubviews, children).contains(where: { $0 !== $1 }) {
            for child in stack.arrangedSubviews {
                stack.removeArrangedSubview(child)
                child.removeFromSuperview()
            }
            children.forEach(stack.addArrangedSubview)
        }
    }

    func dispose() {
        generation = UUID()
        gallery.open = nil
        attachments.open = nil
        attachments.stop()
    }

    deinit { dispose() }
}
#endif
