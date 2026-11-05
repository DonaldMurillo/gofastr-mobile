import UIKit
import PhotosUI
import AVFoundation
import ImageIO
import UniformTypeIdentifiers

enum GofastrPhotoExtension: MobileFileExtensionFactory {
    static let name = "gofastr.photos/v1"

    static func capabilities() -> [String: String] {
        ["photos/v1": "available", "camera/v1": MobilePhotoPlatform.cameraAvailable() ? "available" : "unavailable"]
    }

    static func valid(_ operation: MobileFileExtension) -> Bool {
        guard operation.name == name, let photo = try? JSONDecoder().decode(MobilePhoto.self, from: Data(operation.payload.utf8)) else { return false }
        return photo.isValid
    }

    static func make(_ operation: MobileFileExtension, identifier: String, source: UIViewController,
                     focus: MobileForegroundFocus, queue: DispatchQueue,
                     respond: @escaping (MobileFileResult) -> Void) -> MobileForegroundTask? {
        guard valid(operation), let photo = try? JSONDecoder().decode(MobilePhoto.self, from: Data(operation.payload.utf8)) else { return nil }
        return MobilePhotoTask(photo, identifier: identifier, source: source, focus: focus, queue: queue, respond: respond)
    }
}

enum MobilePhotoPlatform {
    static func cameraAvailable(in bundle: Bundle = .main) -> Bool {
        precondition(Thread.isMainThread)
        #if targetEnvironment(simulator)
        return false
        #else
        guard let purpose = bundle.object(forInfoDictionaryKey: "NSCameraUsageDescription") as? String,
              !purpose.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return false }
        return UIImagePickerController.isSourceTypeAvailable(.camera) &&
            (UIImagePickerController.availableMediaTypes(for: .camera)?.contains(UTType.image.identifier) ?? false)
        #endif
    }
}

final class MobilePhotoTask: NSObject, MobileForegroundTask, PHPickerViewControllerDelegate,
                             UIImagePickerControllerDelegate, UINavigationControllerDelegate, UIAdaptivePresentationControllerDelegate {
    let identifier: String
    private let photo: MobilePhoto
    private weak var source: UIViewController?
    private let focus: MobileForegroundFocus
    private let queue: DispatchQueue
    private let submission: (MobileFileResult) -> Void
    private var controller: UIViewController?
    private var provider: NSItemProvider?
    private var progress: Progress?
    private let work = MobilePhotoWork()
    private var state = MobileFileTaskState()

    init(_ photo: MobilePhoto, identifier: String, source: UIViewController, focus: MobileForegroundFocus,
         queue: DispatchQueue, respond: @escaping (MobileFileResult) -> Void) {
        self.photo = photo
        self.identifier = identifier
        self.source = source
        self.focus = focus
        self.queue = queue
        submission = respond
        super.init()
    }

    func show() {
        precondition(Thread.isMainThread)
        guard photo.isValid else { report(MobileFileResult(status: .failed)); return }
        if photo.source == .library {
            var configuration = PHPickerConfiguration()
            configuration.filter = .images
            configuration.selectionLimit = 1
            configuration.preferredAssetRepresentationMode = .current
            let picker = PHPickerViewController(configuration: configuration)
            picker.delegate = self
            present(picker)
        } else {
            guard MobilePhotoPlatform.cameraAvailable() else { report(MobileFileResult(status: .unsupported)); return }
            switch AVCaptureDevice.authorizationStatus(for: .video) {
            case .authorized: presentCamera()
            case .denied, .restricted: report(MobileFileResult(status: .denied))
            case .notDetermined:
                AVCaptureDevice.requestAccess(for: .video) { [weak self] granted in
                    DispatchQueue.main.async {
                        guard let self, self.state.phase == .preparing else { return }
                        if granted { self.presentCamera() }
                        else { self.report(MobileFileResult(status: .denied)) }
                    }
                }
            @unknown default: report(MobileFileResult(status: .unsupported))
            }
        }
    }

    private func presentCamera() {
        guard MobilePhotoPlatform.cameraAvailable() else { report(MobileFileResult(status: .unsupported)); return }
        guard AVCaptureDevice.authorizationStatus(for: .video) == .authorized else { report(MobileFileResult(status: .denied)); return }
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.mediaTypes = [UTType.image.identifier]
        picker.cameraCaptureMode = .photo
        picker.allowsEditing = false
        picker.delegate = self
        picker.modalPresentationStyle = .fullScreen
        present(picker)
    }

    private func present(_ picker: UIViewController) {
        guard let source, source.viewIfLoaded?.window != nil, source.presentedViewController == nil else {
            report(MobileFileResult(status: .failed)); return
        }
        guard state.present() else { return }
        controller = picker
        picker.title = photo.title
        picker.view.accessibilityIdentifier = "native.photos"
        picker.view.accessibilityViewIsModal = true
        source.present(picker, animated: true) { [weak self] in
            picker.presentationController?.delegate = self
        }
    }

    func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
        guard controller === picker, state.receive() else { return }
        guard !results.isEmpty else { report(MobileFileResult(status: .cancelled)); return }
        guard results.count == 1, let provider = results.first?.itemProvider,
              provider.hasItemConformingToTypeIdentifier(UTType.image.identifier) else {
            report(MobileFileResult(status: .failed)); return
        }
        controller?.isModalInPresentation = true
        self.provider = provider
        let photo = self.photo
        let work = self.work
        let queue = self.queue
        progress = provider.loadFileRepresentation(forTypeIdentifier: UTType.image.identifier) { [weak self] url, error in
            let original = Result { () throws -> Data in
                try work.check()
                if let error { throw error }
                guard let url, !Thread.isMainThread else { throw MobileFileAccess.Failure.invalid }
                return try MobilePhotoAccess.readOriginal(url, work: work)
            }
            queue.async {
                let result = original.flatMap { bytes in Result {
                    try autoreleasepool { try MobilePhotoAccess.jpeg(bytes, photo: photo, work: work) }
                } }
                DispatchQueue.main.async { self?.received(result) }
            }
        }
    }

    func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
        guard controller === picker, state.receive() else { return }
        guard info[.mediaType] as? String == UTType.image.identifier,
              let image = info[.originalImage] as? UIImage else { report(MobileFileResult(status: .failed)); return }
        controller?.isModalInPresentation = true
        let photo = self.photo
        let work = self.work
        queue.async { [weak self] in
            let result = Result { () throws -> MobileFilePayload in
                try work.check()
                guard let raster = image.cgImage else { throw MobileFileAccess.Failure.invalid }
                let orientation: CGImagePropertyOrientation
                switch image.imageOrientation {
                case .up: orientation = .up
                case .upMirrored: orientation = .upMirrored
                case .down: orientation = .down
                case .downMirrored: orientation = .downMirrored
                case .left: orientation = .left
                case .leftMirrored: orientation = .leftMirrored
                case .right: orientation = .right
                case .rightMirrored: orientation = .rightMirrored
                @unknown default: throw MobileFileAccess.Failure.invalid
                }
                return try autoreleasepool { try MobilePhotoAccess.jpeg(raster, orientation: orientation, photo: photo, work: work) }
            }
            DispatchQueue.main.async { self?.received(result) }
        }
    }

    func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
        guard controller === picker, state.receive() else { return }
        report(MobileFileResult(status: .cancelled))
    }

    func presentationControllerShouldDismiss(_ presentationController: UIPresentationController) -> Bool {
        state.phase == .presented
    }

    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) {
        guard controller === presentationController.presentedViewController else { return }
        report(MobileFileResult(status: .cancelled))
    }

    private func received(_ result: Result<MobileFilePayload, Error>) {
        guard state.phase == .reading else { return }
        switch result {
        case .success(let file): report(MobileFileResult(status: .completed, file: file))
        case .failure(let error): report(MobileFileResult(status: MobilePhotoAccess.status(for: error)))
        }
    }

    private func report(_ result: MobileFileResult) {
        precondition(Thread.isMainThread)
        guard state.report() else { return }
        controller?.isModalInPresentation = true
        cancelWork()
        submission(result)
    }

    func finish(restoreFocus: Bool, completion: @escaping () -> Void) {
        precondition(Thread.isMainThread)
        state.finish()
        cancelWork()
        (controller as? PHPickerViewController)?.delegate = nil
        (controller as? UIImagePickerController)?.delegate = nil
        controller?.presentationController?.delegate = nil
        MobileForegroundDismissal.dismiss(controller) { [self] in
            controller = nil
            if restoreFocus { focus.restore(source) }
            completion()
        }
    }

    private func cancelWork() {
        work.cancel()
        progress?.cancel()
        progress = nil
        provider = nil
    }
}
