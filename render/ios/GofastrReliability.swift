import Foundation

final class MobileCommandSequencer {
    private struct Pending {
        weak var operation: Operation?
    }
    private var pending: [String: Pending] = [:]

    func prepare(_ operation: Operation, kind: String, instance: String?) {
        guard kind != "watch", let instance, !instance.isEmpty else { return }
        pending = pending.filter { $0.value.operation?.isFinished == false }
        if let previous = pending[instance]?.operation { operation.addDependency(previous) }
        if !["action", "respond", "file-result", "service-result"].contains(kind) {
            pending[instance] = Pending(operation: operation)
        }
    }
}

struct MobileCheckpointFence {
    struct Ticket {
        let generation: Int
        let sequence: Int
        let revision: Int
    }
    private var generation = 0
    private var sequence = 0

    mutating func begin(revision: Int) -> Ticket {
        sequence += 1
        return Ticket(generation: generation, sequence: sequence, revision: revision)
    }

    mutating func invalidate() { generation += 1 }

    func accepts(_ ticket: Ticket, revision: Int, replyRevision: Int?) -> Bool {
        ticket.generation == generation && ticket.sequence == sequence && ticket.revision == revision && replyRevision == revision
    }
}

struct MobileCheckpointDeferral {
    private var held = false
    private var completions: [(Bool) -> Void] = []

    mutating func begin() { held = true }

    mutating func hold(_ completion: @escaping (Bool) -> Void) -> Bool {
        guard held else { return false }
        completions.append(completion)
        return true
    }

    mutating func finish() -> [(Bool) -> Void] {
        held = false
        let callbacks = completions
        completions.removeAll()
        return callbacks
    }
}

final class MobileCheckpointBatch {
    private var remaining: Int
    private var safe = true
    private var completion: ((Bool) -> Void)?

    init(count: Int, completion: @escaping (Bool) -> Void) {
        precondition(count > 0)
        remaining = count
        self.completion = completion
    }

    func complete(_ stored: Bool) {
        guard completion != nil else { return }
        safe = safe && stored
        remaining -= 1
        if remaining == 0 { finish(safe) }
    }

    func expire() { finish(false) }

    private func finish(_ stored: Bool) {
        guard let callback = completion else { return }
        completion = nil
        callback(stored)
    }
}

struct MobileRetainedRoute {
    let route: String
    let draftKey: String?
    let uncertain: Bool
}

enum MobileRestorationSafety {
    static func canOpen(route: String, draftKey: String?, editor: Bool, retained: [MobileRetainedRoute]) -> Bool {
        !retained.contains { current in
            current.route == route ||
            (draftKey?.isEmpty == false && current.draftKey == draftKey) ||
            (editor && current.uncertain)
        }
    }
}
