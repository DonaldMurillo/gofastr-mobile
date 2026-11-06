import Foundation
import UIKit
import Social
import UniformTypeIdentifiers

/// Compiled into the share extension, together with GofastrIncomingShares.swift.
@objc(GofastrShareViewController)
final class GofastrShareViewController: SLComposeServiceViewController {
    private var posting = false
    private let state = NSLock()
    private let fileQueue = DispatchQueue(label: "dev.gofastr.share-extension.files")
    private var cancelled = false
    private let savedIdentifier = UUID().uuidString.replacingOccurrences(of: "-", with: "").lowercased()
    private var isCancelled: Bool { state.lock(); defer { state.unlock() }; return cancelled }
    override func didSelectCancel() {
        state.lock(); cancelled = true; state.unlock()
        super.didSelectCancel()
    }
    override func isContentValid() -> Bool { !posting && contentText.utf8.count <= 65_536 && !contentText.contains("\0") }
    override func didSelectPost() {
        guard !posting else { return }
        posting = true
        validateContent()
        let providers = (extensionContext?.inputItems as? [NSExtensionItem] ?? []).flatMap { $0.attachments ?? [] }
        guard providers.count <= 8, let group = Bundle.main.object(forInfoDictionaryKey: "GofastrAppGroup") as? String, !group.isEmpty else { fail(); return }
        let item = MobileIncomingShareItem(identifier: savedIdentifier, title: nil, text: contentText.isEmpty ? nil : contentText, files: [])
        load(providers, index: 0, item: item) { result in
            switch result {
            case .failure: DispatchQueue.main.async { self.fail() }
            case .success(let completed):
                DispatchQueue.global(qos: .userInitiated).async {
                    do {
                        guard !self.isCancelled else { return }
                        let inbox = try MobileIncomingShareInbox(groupIdentifier: group)
                        try inbox.append(completed)
                        DispatchQueue.main.async { if !self.isCancelled { self.extensionContext?.completeRequest(returningItems: nil) } }
                    } catch { DispatchQueue.main.async { self.fail() } }
                }
            }
        }
    }
    private func fail() {
        guard !isCancelled else { return }
        posting = false
        validateContent()
        let alert = UIAlertController(title: "Could not save share", message: "Use up to four files totaling 1 MiB and 64 KiB of text. The inbox holds 16 shares.", preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "OK", style: .default))
        present(alert, animated: true)
    }
    private func load(_ providers: [NSItemProvider], index: Int, item: MobileIncomingShareItem, completion: @escaping (Result<MobileIncomingShareItem, Error>) -> Void) {
        guard !isCancelled else { completion(.failure(MobileIncomingShareError.invalid)); return }
        guard index < providers.count else {
            do { try item.validate(); completion(.success(item)) } catch { completion(.failure(error)) }
            return
        }
        let provider = providers[index]
        if provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
            provider.loadItem(forTypeIdentifier: UTType.fileURL.identifier, options: nil) { value, error in
                guard error == nil, let url = value as? URL, url.isFileURL else { completion(.failure(MobileIncomingShareError.invalid)); return }
                let scoped = url.startAccessingSecurityScopedResource()
                defer { if scoped { url.stopAccessingSecurityScopedResource() } }
                do {
                    let type = UTType(filenameExtension: url.pathExtension)?.identifier ?? UTType.data.identifier
                    let next = try self.fileQueue.sync { try self.appendingFile(url, type: type, provider: provider, item: item) }
                    self.load(providers, index: index + 1, item: next, completion: completion)
                } catch { completion(.failure(error)) }
            }
            return
        }
        // A URL is text. It is never fetched or opened by the extension.
        if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) && !provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
            provider.loadItem(forTypeIdentifier: UTType.url.identifier, options: nil) { value, error in
                guard error == nil, let url = value as? URL, !url.isFileURL else { completion(.failure(MobileIncomingShareError.invalid)); return }
                var next = item
                do { next.text = try self.mergedText(url.absoluteString, current: next.text); self.load(providers, index: index + 1, item: next, completion: completion) }
                catch { completion(.failure(error)) }
            }
            return
        }
        if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
            provider.loadItem(forTypeIdentifier: UTType.plainText.identifier, options: nil) { value, error in
                guard error == nil else { completion(.failure(MobileIncomingShareError.invalid)); return }
                let text: String?
                if let string = value as? String { text = string }
                else if let data = value as? Data, data.count <= 65_536 { text = String(data: data, encoding: .utf8) }
                else { text = nil }
                guard let text, text.utf8.count <= 65_536, !text.contains("\0") else { completion(.failure(MobileIncomingShareError.invalid)); return }
                var next = item
                do { next.text = try self.mergedText(text, current: next.text); self.load(providers, index: index + 1, item: next, completion: completion) }
                catch { completion(.failure(error)) }
            }
            return
        }
        guard (item.files ?? []).count < 4,
              let type = provider.registeredTypeIdentifiers.first(where: { UTType($0)?.conforms(to: .data) == true }) else { completion(.failure(MobileIncomingShareError.invalid)); return }
        // The provider's temporary URL is valid only inside this callback.
        provider.loadFileRepresentation(forTypeIdentifier: type) { url, error in
            guard error == nil, let url else { completion(.failure(MobileIncomingShareError.invalid)); return }
            do {
                let next = try self.fileQueue.sync { try self.appendingFile(url, type: type, provider: provider, item: item) }
                self.load(providers, index: index + 1, item: next, completion: completion)
            } catch { completion(.failure(error)) }
        }
    }
    private func mergedText(_ text: String, current: String?) throws -> String {
        let previous = current ?? ""
        let combined = previous.isEmpty ? text : previous.contains(text) ? previous : previous + "\n" + text
        guard combined.utf8.count <= 65_536, !combined.contains("\0") else { throw MobileIncomingShareError.invalid }
        return combined
    }
    private func appendingFile(_ url: URL, type: String, provider: NSItemProvider, item: MobileIncomingShareItem) throws -> MobileIncomingShareItem {
        guard (item.files ?? []).count < 4 else { throw MobileIncomingShareError.invalid }
        let stream = try FileHandle(forReadingFrom: url)
        defer { try? stream.close() }
        let used = try (item.files ?? []).reduce(0) { $0 + (try $1.bytes().count) }
        let data = try stream.read(upToCount: 1_048_576 - used + 1) ?? Data()
        guard data.count <= 1_048_576 - used else { throw MobileIncomingShareError.invalid }
        var next = item
        let rawName = provider.suggestedName ?? url.lastPathComponent
        let name = !rawName.isEmpty && rawName.utf8.count <= 200 && rawName != "." && rawName != ".." && !rawName.contains("/") && !rawName.contains("\\") && !rawName.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) ? rawName : "shared-file-\((item.files ?? []).count + 1)"
        let file = MobileIncomingShareFile(name: name, mediaType: UTType(type)?.preferredMIMEType ?? "application/octet-stream", data: data.base64EncodedString())
        _ = try file.bytes()
        next.files = (next.files ?? []) + [file]
        return next
    }
}
