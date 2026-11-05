import UIKit

final class MobileFileTask: NSObject, MobileForegroundTask, UIDocumentPickerDelegate, UIAdaptivePresentationControllerDelegate {
    let identifier: String
    private let model: MobileFiles
    private weak var source: UIViewController?
    private let focus: MobileForegroundFocus
    private let queue: DispatchQueue
    private let submission: (MobileFileResult) -> Void
    private var controller: UIDocumentPickerViewController?
    private var exportDirectory: URL?
    private var state = MobileFileTaskState()

    init(_ model: MobileFiles, identifier: String, source: UIViewController, focus: MobileForegroundFocus,
         queue: DispatchQueue, respond: @escaping (MobileFileResult) -> Void) {
        self.model = model
        self.identifier = identifier
        self.source = source
        self.focus = focus
        self.queue = queue
        submission = respond
        super.init()
    }

    func show() {
        guard model.isValid else { report(MobileFileResult(status: .failed)); return }
        if let pick = model.pick {
            do {
                let picker = UIDocumentPickerViewController(forOpeningContentTypes: try MobileFileAccess.contentTypes(pick), asCopy: true)
                picker.title = pick.title
                picker.allowsMultipleSelection = false
                present(picker)
            } catch { report(MobileFileResult(status: MobileFileAccess.status(for: error))) }
        } else if let export = model.export {
            queue.async { [self] in
                let result = Result { try MobileFileAccess.prepareExport(export) }
                DispatchQueue.main.async {
                    switch result {
                    case .success(let url):
                        self.exportDirectory = url.deletingLastPathComponent()
                        guard self.state.phase != .finished else { self.cleanExport(); return }
                        self.present(UIDocumentPickerViewController(forExporting: [url], asCopy: true))
                    case .failure(let error): self.report(MobileFileResult(status: MobileFileAccess.status(for: error)))
                    }
                }
            }
        }
    }

    private func present(_ picker: UIDocumentPickerViewController) {
        guard let source, source.viewIfLoaded?.window != nil, source.presentedViewController == nil else {
            report(MobileFileResult(status: .failed)); return
        }
        guard state.present() else { return }
        controller = picker
        picker.delegate = self
        picker.view.accessibilityIdentifier = "native.files"
        picker.view.accessibilityViewIsModal = true
        source.present(picker, animated: true) { [weak self] in
            picker.presentationController?.delegate = self
        }
    }

    func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        guard self.controller === controller, state.receive() else { return }
        guard urls.count == 1, let url = urls.first else { report(MobileFileResult(status: .failed)); return }
        if let pick = model.pick {
            queue.async { [self] in
                let result = Result { try MobileFileAccess.read(url, pick: pick) }
                DispatchQueue.main.async {
                    switch result {
                    case .success(let file): self.report(MobileFileResult(status: .completed, file: file))
                    case .failure(let error): self.report(MobileFileResult(status: MobileFileAccess.status(for: error)))
                    }
                }
            }
        } else { report(MobileFileResult(status: .completed)) }
    }

    func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
        guard self.controller === controller, state.receive() else { return }
        report(MobileFileResult(status: .cancelled))
    }

    func presentationControllerShouldDismiss(_ presentationController: UIPresentationController) -> Bool {
        state.phase == .presented
    }

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        guard state.receive() else { return }
        report(MobileFileResult(status: .cancelled))
    }

    private func report(_ result: MobileFileResult) {
        guard state.report() else { return }
        controller?.isModalInPresentation = true
        submission(result)
    }

    func finish(restoreFocus: Bool, completion: @escaping () -> Void) {
        state.finish()
        MobileForegroundDismissal.dismiss(controller) { [self] in
            cleanExport()
            if restoreFocus { focus.restore(source) }
            completion()
        }
    }

    private func cleanExport() {
        guard let directory = exportDirectory else { return }
        exportDirectory = nil
        queue.async { try? FileManager.default.removeItem(at: directory) }
    }
}
