#if canImport(UIKit)
import UIKit
import AVFoundation

final class MobileAudioRecorderController: UIViewController, AVAudioRecorderDelegate, AVAudioPlayerDelegate {
    private let request: MobileAudioRequest
    private let queue: DispatchQueue
    private let respond: (MobileServiceResult) -> Void
    private let status = UILabel()
    private let recordButton = UIButton(type: .system)
    private let pauseButton = UIButton(type: .system)
    private let stopButton = UIButton(type: .system)
    private let previewButton = UIButton(type: .system)
    private let useButton = UIButton(type: .system)
    private var recorder: AVAudioRecorder?
    private var player: AVAudioPlayer?
    private var directory: URL?
    private var payload: MobileFileExport?
    private var durationMillis = 0
    private var timer: Timer?
    private var observers: [NSObjectProtocol] = []
    private var phase = "idle"
    private var generation = 0
    private var measuring = false
    private var ownsSession = false

    init(request: MobileAudioRequest, queue: DispatchQueue, respond: @escaping (MobileServiceResult) -> Void) {
        self.request = request
        self.queue = queue
        self.respond = respond
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    override func viewDidLoad() {
        super.viewDidLoad()
        title = request.title?.isEmpty == false ? request.title : "Voice recording"
        view.backgroundColor = .systemBackground
        navigationItem.leftBarButtonItem = UIBarButtonItem(title: "Cancel", primaryAction: UIAction { [weak self] _ in self?.cancelRecording() })
        navigationItem.leftBarButtonItem?.accessibilityIdentifier = "native.audio.cancel"
        status.font = .preferredFont(forTextStyle: .body)
        status.adjustsFontForContentSizeCategory = true
        status.numberOfLines = 0
        status.accessibilityIdentifier = "native.audio.status"
        let stack = UIStackView(arrangedSubviews: [status, recordButton, pauseButton, stopButton, previewButton, useButton])
        stack.axis = .vertical
        stack.spacing = 16
        stack.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 24),
            stack.leadingAnchor.constraint(equalTo: view.layoutMarginsGuide.leadingAnchor),
            stack.trailingAnchor.constraint(equalTo: view.layoutMarginsGuide.trailingAnchor)
        ])
        configure(recordButton, "Record", identifier: "record") { [weak self] in self?.beginRecording() }
        configure(pauseButton, "Pause", identifier: "pause") { [weak self] in self?.pauseRecording() }
        configure(stopButton, "Stop", identifier: "stop") { [weak self] in self?.stopRecording() }
        configure(previewButton, "Play preview", identifier: "preview") { [weak self] in self?.preview() }
        configure(useButton, "Use recording", identifier: "use") { [weak self] in self?.accept() }
        observers.append(NotificationCenter.default.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { [weak self] notification in
            guard let raw = notification.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt,
                  AVAudioSession.InterruptionType(rawValue: raw) == .began else { return }
            self?.cancelRecording(message: "Recording was interrupted. No audio was saved.")
        })
        observers.append(NotificationCenter.default.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { [weak self] _ in
            self?.cancelRecording(message: "Recording stopped when the app left the foreground. No audio was saved.")
        })
        update("Record up to \(request.seconds) seconds. Audio is saved only after Use recording.")
    }

    private func configure(_ button: UIButton, _ title: String, identifier: String, action: @escaping () -> Void) {
        button.setTitle(title, for: .normal)
        button.titleLabel?.font = .preferredFont(forTextStyle: .body)
        button.titleLabel?.adjustsFontForContentSizeCategory = true
        button.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
        button.accessibilityIdentifier = "native.audio.\(identifier)"
        button.addAction(UIAction { _ in action() }, for: .touchUpInside)
    }

    private func update(_ message: String) {
        status.text = message
        recordButton.isEnabled = phase == "idle"
        pauseButton.isEnabled = phase == "recording" || phase == "paused"
        pauseButton.setTitle(phase == "paused" ? "Resume" : "Pause", for: .normal)
        stopButton.isEnabled = phase == "recording" || phase == "paused"
        previewButton.isEnabled = phase == "ready"
        useButton.isEnabled = phase == "ready" && payload != nil
    }

    private func beginRecording() {
        guard phase == "idle" else { return }
        phase = "permission"
        let current = generation
        update("Requesting microphone permission…")
        AVAudioSession.sharedInstance().requestRecordPermission { granted in
            DispatchQueue.main.async {
                guard current == self.generation, self.phase == "permission" else { return }
                guard granted else {
                    self.respond(MobileServiceResult(status: "denied", message: "Microphone permission was denied.", permission: "denied"))
                    return
                }
                self.prepareRecorder(current)
            }
        }
    }

    private func prepareRecorder(_ current: Int) {
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker])
            try session.setActive(true)
            ownsSession = true
        } catch { fail(error); return }
        phase = "preparing"
        update("Preparing recording…")
        queue.async {
            let directory = FileManager.default.temporaryDirectory.appendingPathComponent("gofastr-audio-\(UUID().uuidString)", isDirectory: true)
            let prepared = Result { () throws -> AVAudioRecorder in
                try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
                let recorder = try AVAudioRecorder(url: directory.appendingPathComponent("recording.m4a"), settings: [
                    AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: 16_000,
                    AVNumberOfChannelsKey: 1, AVEncoderBitRateKey: 32_000
                ])
                guard recorder.prepareToRecord() else { throw MobileServiceFailure.unsupported }
                return recorder
            }
            DispatchQueue.main.async {
                guard current == self.generation, self.phase == "preparing" else {
                    if case .success(let recorder) = prepared { recorder.stop() }
                    self.queue.async { try? FileManager.default.removeItem(at: directory) }
                    return
                }
                self.directory = directory
                switch prepared {
                case .failure(let error): self.fail(error)
                case .success(let recorder):
                    self.recorder = recorder
                    recorder.delegate = self
                    guard recorder.record(forDuration: Double(self.request.seconds)) else { self.fail(MobileServiceFailure.unsupported); return }
                    self.phase = "recording"
                    self.update("Recording…")
                    self.timer = Timer.scheduledTimer(withTimeInterval: 0.1, repeats: true) { [weak self] _ in self?.tick() }
                }
            }
        }
    }

    private func tick() {
        guard phase == "recording" || phase == "paused", let recorder else { return }
        durationMillis = min(request.seconds * 1_000, max(durationMillis, Int(recorder.currentTime * 1_000)))
        status.text = "\(phase == "paused" ? "Paused" : "Recording") · \(durationMillis / 1_000) / \(request.seconds) seconds"
        if durationMillis >= request.seconds * 1_000 { stopRecording(); return }
        guard !measuring else { return }
        measuring = true
        let current = generation
        let url = recorder.url
        queue.async {
            let size = (try? url.resourceValues(forKeys: [.fileSizeKey]))?.fileSize ?? 0
            DispatchQueue.main.async {
                self.measuring = false
                guard current == self.generation, self.phase == "recording" || self.phase == "paused" else { return }
                if size >= self.request.bytes { self.fail(MobileServiceFailure.tooLarge) }
            }
        }
    }

    private func pauseRecording() {
        if phase == "recording" {
            recorder?.pause()
            phase = "paused"
            update("Recording paused.")
        } else if phase == "paused" {
            let remaining = Double(request.seconds) - Double(durationMillis) / 1_000
            guard remaining > 0, recorder?.record(forDuration: remaining) == true else { stopRecording(); return }
            phase = "recording"
            update("Recording…")
        }
    }

    private func stopRecording() {
        guard phase == "recording" || phase == "paused", let recorder else { return }
        durationMillis = min(request.seconds * 1_000, max(durationMillis, Int(recorder.currentTime * 1_000)))
        phase = "reading"
        timer?.invalidate()
        timer = nil
        recorder.stop()
        recorder.delegate = nil
        self.recorder = nil
        let url = recorder.url
        let current = generation
        let duration = durationMillis
        update("Preparing bounded audio preview…")
        queue.async {
            let result = Result { () throws -> (MobileFileExport, AVAudioPlayer) in
                let bytes = try MobileServiceBounds.read(url, maximum: self.request.bytes)
                let file = MobileFileExport(name: "recording.m4a", mediaType: "audio/mp4", data: bytes.base64EncodedString())
                try self.request.accept(file, durationMillis: duration)
                let player = try AVAudioPlayer(data: bytes)
                guard player.prepareToPlay(), player.duration.isFinite, player.duration > 0,
                      player.duration <= Double(self.request.seconds) + 0.5 else { throw MobileServiceFailure.invalid }
                return (file, player)
            }
            DispatchQueue.main.async {
                guard current == self.generation, self.phase == "reading" else { return }
                switch result {
                case .failure(let error): self.fail(error)
                case .success(let audio):
                    self.payload = audio.0
                    self.player = audio.1
                    self.player?.delegate = self
                    self.phase = "ready"
                    self.update("Ready · \(duration) ms. Preview or use this recording.")
                }
            }
        }
    }

    private func preview() {
        guard phase == "ready", let player else { return }
        if player.isPlaying { player.pause(); previewButton.setTitle("Play preview", for: .normal) }
        else if player.play() { previewButton.setTitle("Pause preview", for: .normal) }
        else { fail(MobileServiceFailure.unsupported) }
    }

    private func accept() {
        guard phase == "ready", let payload else { return }
        phase = "finished"
        respond(MobileServiceResult(status: "completed", permission: "granted", file: payload, durationMillis: durationMillis))
    }

    private func fail(_ error: Error) {
        guard phase != "finished" else { return }
        phase = "finished"
        respond(MobileServiceResult(status: error as? MobileServiceFailure == .unsupported ? "unsupported" : "failed", message: error.localizedDescription))
    }

    private func cancelRecording(message: String = "Recording cancelled. No audio was saved.") {
        guard phase != "finished" else { return }
        phase = "finished"
        respond(MobileServiceResult(status: "cancelled", message: message))
    }

    func shutdown() {
        generation += 1
        phase = "finished"
        timer?.invalidate()
        timer = nil
        recorder?.delegate = nil
        recorder?.stop()
        recorder = nil
        player?.stop()
        player = nil
        payload = nil
        observers.forEach(NotificationCenter.default.removeObserver)
        observers.removeAll()
        if ownsSession {
            try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
            ownsSession = false
        }
        if let directory {
            self.directory = nil
            queue.async { try? FileManager.default.removeItem(at: directory) }
        }
    }

    func audioRecorderDidFinishRecording(_ recorder: AVAudioRecorder, successfully flag: Bool) {
        guard phase == "recording" || phase == "paused" else { return }
        if flag { stopRecording() } else { fail(MobileServiceFailure.invalid) }
    }

    func audioRecorderEncodeErrorDidOccur(_ recorder: AVAudioRecorder, error: Error?) { fail(error ?? MobileServiceFailure.invalid) }
    func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) { previewButton.setTitle("Play preview", for: .normal) }

    deinit {
        timer?.invalidate()
        observers.forEach(NotificationCenter.default.removeObserver)
        recorder?.stop()
        player?.stop()
        if ownsSession { try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation]) }
        if let directory { queue.async { try? FileManager.default.removeItem(at: directory) } }
    }
}

final class MobileAudioPreview: UIStackView {
    private static let queue = DispatchQueue(label: "gofastr.native.audio-playback", qos: .userInitiated)
    private let document: MobileAudioDocument
    private let button = UIButton(type: .system)
    private let seek = UISlider()
    private let status = UILabel()
    private var player: AVAudioPlayer?
    private var timer: Timer?
    private var observers: [NSObjectProtocol] = []
    private var generation = 0
    private var loading = false
    private var ownsSession = false

    init(_ document: MobileAudioDocument, identifier: String) {
        self.document = document
        super.init(frame: .zero)
        axis = .vertical
        spacing = 8
        button.setTitle("Play audio", for: .normal)
        button.accessibilityIdentifier = "native.audio.play.\(identifier)"
        button.heightAnchor.constraint(greaterThanOrEqualToConstant: 44).isActive = true
        button.addAction(UIAction { [weak self] _ in self?.toggle() }, for: .touchUpInside)
        seek.accessibilityLabel = "Audio position"
        seek.accessibilityIdentifier = "native.audio.seek.\(identifier)"
        seek.maximumValue = Float(document.durationMillis) / 1_000
        seek.isEnabled = false
        seek.addTarget(self, action: #selector(seekChanged), for: .valueChanged)
        status.font = .preferredFont(forTextStyle: .caption1)
        status.adjustsFontForContentSizeCategory = true
        status.numberOfLines = 0
        status.text = "\(document.durationMillis) ms"
        addArrangedSubview(button)
        addArrangedSubview(seek)
        addArrangedSubview(status)
        for name in [AVAudioSession.interruptionNotification, UIApplication.didEnterBackgroundNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in self?.stop() })
        }
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    private func toggle() {
        guard !loading else { return }
        if let player {
            if player.isPlaying {
                player.pause()
                timer?.invalidate()
                timer = nil
                button.setTitle("Play audio", for: .normal)
                releaseSession()
            } else { play(player) }
            return
        }
        loading = true
        button.isEnabled = false
        status.text = "Loading audio…"
        let current = generation
        Self.queue.async {
            let result = Result { () throws -> AVAudioPlayer in
                let player = try AVAudioPlayer(data: self.document.validatedBytes())
                guard player.prepareToPlay(), player.duration.isFinite, player.duration > 0, player.duration <= 60.5 else { throw MobileServiceFailure.unsupported }
                return player
            }
            DispatchQueue.main.async {
                guard current == self.generation else { return }
                self.loading = false
                self.button.isEnabled = true
                switch result {
                case .failure: self.status.text = "This audio cannot be played on this device."
                case .success(let player):
                    self.player = player
                    self.seek.maximumValue = Float(player.duration)
                    self.seek.isEnabled = true
                    self.play(player)
                }
            }
        }
    }

    private func play(_ player: AVAudioPlayer) {
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playback, mode: .default)
            try session.setActive(true)
            ownsSession = true
            if player.currentTime >= player.duration { player.currentTime = 0 }
            guard player.play() else { throw MobileServiceFailure.unsupported }
            button.setTitle("Pause audio", for: .normal)
            timer?.invalidate()
            timer = Timer.scheduledTimer(withTimeInterval: 0.2, repeats: true) { [weak self] _ in
                guard let self, let player = self.player else { return }
                if !self.seek.isTracking { self.seek.value = Float(player.currentTime) }
                self.status.text = "\(Int(player.currentTime)) / \(Int(player.duration)) seconds"
                if !player.isPlaying {
                    self.button.setTitle("Play audio", for: .normal)
                    self.timer?.invalidate()
                    self.timer = nil
                    self.releaseSession()
                }
            }
        } catch { status.text = "Audio playback is unavailable."; stop() }
    }

    @objc private func seekChanged() { player?.currentTime = Double(seek.value) }

    func stop() {
        generation += 1
        loading = false
        timer?.invalidate()
        timer = nil
        player?.stop()
        player = nil
        button.isEnabled = true
        button.setTitle("Play audio", for: .normal)
        seek.value = 0
        seek.isEnabled = false
        releaseSession()
    }

    private func releaseSession() {
        guard ownsSession else { return }
        try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
        ownsSession = false
    }

    deinit {
        timer?.invalidate()
        player?.stop()
        observers.forEach(NotificationCenter.default.removeObserver)
        if ownsSession { try? AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation]) }
    }
}
#endif
