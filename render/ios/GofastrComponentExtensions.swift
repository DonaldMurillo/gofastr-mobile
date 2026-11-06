import Foundation

struct MobileComponent: Decodable, Equatable {
    static let maximumPayloadBytes = 2 * 1024 * 1024
    let id: String
    let name: String
    let payload: String
    let destinations: [String: String]

    private enum CodingKeys: String, CodingKey { case id, name, payload, destinations }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        id = try values.decode(String.self, forKey: .id)
        name = try values.decode(String.self, forKey: .name)
        payload = try values.decode(String.self, forKey: .payload)
        destinations = try values.decodeIfPresent([String: String].self, forKey: .destinations) ?? [:]
        guard !id.isEmpty, !name.isEmpty, !payload.isEmpty,
              payload.utf8.count <= Self.maximumPayloadBytes, destinations.count <= 128,
              destinations.allSatisfy({ !$0.key.isEmpty && !$0.value.isEmpty && $0.value.utf8.count <= 4096 }),
              (try? JSONSerialization.jsonObject(with: Data(payload.utf8), options: [.fragmentsAllowed])) != nil else {
            throw MobileComponentFailure.invalidPayload
        }
    }

    func decodePayload<Payload: Decodable>(_ type: Payload.Type) throws -> Payload {
        guard !payload.isEmpty, payload.utf8.count <= Self.maximumPayloadBytes else { throw MobileComponentFailure.invalidPayload }
        return try JSONDecoder().decode(type, from: Data(payload.utf8))
    }
}

enum MobileComponentFailure: Error, LocalizedError {
    case invalidPayload
    case duplicateIdentity
    case unavailable(String)

    var errorDescription: String? {
        switch self {
        case .invalidPayload: return "This component has an invalid or oversized payload."
        case .duplicateIdentity: return "This screen contains duplicate component identities."
        case .unavailable(let name): return "The required rendering component \(name) is unavailable."
        }
    }
}

struct MobileComponentEvent {
    let component: MobileComponent
    let key: String
    fileprivate let generation: UUID
    var destination: String? { component.destinations[key] }
}

struct MobileComponentIssue {
    let id: String
    let error: Error
}

final class MobileKeyedResources<Resource> {
    private var entries: [String: Resource] = [:]
    private var order: [String] = []
    private let dispose: (Resource) -> Void

    init(dispose: @escaping (Resource) -> Void) { self.dispose = dispose }

    var resources: [Resource] { order.compactMap { entries[$0] } }
    subscript(_ identifier: String) -> Resource? { entries[identifier] }

    @discardableResult func reconcile(_ identifiers: [String], make: (String) -> Resource) -> [Resource] {
        var seen = Set<String>()
        let unique = identifiers.filter { seen.insert($0).inserted }
        let removed = entries.keys.filter { !seen.contains($0) }
        let retired = removed.compactMap { entries.removeValue(forKey: $0) }
        retired.forEach(dispose)
        order = unique
        for identifier in unique where entries[identifier] == nil { entries[identifier] = make(identifier) }
        return resources
    }

    deinit { resources.forEach(dispose) }
}

final class MobileRetainedResource<Value: Equatable, Resource> {
    private var value: Value?
    private(set) var resource: Resource?
    private let dispose: (Resource) -> Void

    init(dispose: @escaping (Resource) -> Void) { self.dispose = dispose }

    @discardableResult func update(_ next: Value?, make: (Value) -> Resource) -> Resource? {
        guard next != value else { return resource }
        if let resource { dispose(resource) }
        resource = nil
        value = next
        if let next { resource = make(next) }
        return resource
    }

    deinit { if let resource { dispose(resource) } }
}

final class MobileComponentReconciler<Resource> {
    private struct Entry {
        var component: MobileComponent
        var generation: UUID
        let resource: Resource
    }

    private var entries: [String: Entry] = [:]
    private var order: [String] = []
    private let make: (MobileComponent, @escaping (String) -> Void) throws -> Resource
    private let update: (Resource, MobileComponent, @escaping (String) -> Void) throws -> Void
    private let dispose: (Resource) -> Void
    private let emit: (MobileComponentEvent) -> Void

    init(make: @escaping (MobileComponent, @escaping (String) -> Void) throws -> Resource,
         update: @escaping (Resource, MobileComponent, @escaping (String) -> Void) throws -> Void,
         dispose: @escaping (Resource) -> Void, emit: @escaping (MobileComponentEvent) -> Void) {
        self.make = make
        self.update = update
        self.dispose = dispose
        self.emit = emit
    }

    var resources: [Resource] { order.compactMap { entries[$0]?.resource } }

    func contains(_ event: MobileComponentEvent) -> Bool {
        guard let entry = entries[event.component.id] else { return false }
        return entry.generation == event.generation && entry.component == event.component
    }

    @discardableResult func reconcile(_ components: [MobileComponent]) throws -> [MobileComponentIssue] {
        guard Set(components.map(\.id)).count == components.count else { throw MobileComponentFailure.duplicateIdentity }
        let incoming = Dictionary(uniqueKeysWithValues: components.map { ($0.id, $0) })
        let removed = entries.keys.filter { identifier in
            guard let next = incoming[identifier], let prior = entries[identifier]?.component else { return true }
            return next.name != prior.name
        }
        let retired = removed.compactMap { entries.removeValue(forKey: $0)?.resource }
        retired.forEach(dispose)
        order = components.map(\.id)
        var issues: [MobileComponentIssue] = []
        for component in components {
            if var entry = entries[component.id] {
                guard entry.component != component else { continue }
                entries.removeValue(forKey: component.id)
                entry.component = component
                entry.generation = UUID()
                do {
                    try update(entry.resource, component, emitter(for: component.id, generation: entry.generation))
                    entries[component.id] = entry
                } catch {
                    dispose(entry.resource)
                    issues.append(MobileComponentIssue(id: component.id, error: error))
                }
                continue
            }
            let generation = UUID()
            do {
                let resource = try make(component, emitter(for: component.id, generation: generation))
                entries[component.id] = Entry(component: component, generation: generation, resource: resource)
            } catch {
                issues.append(MobileComponentIssue(id: component.id, error: error))
            }
        }
        return issues
    }

    private func emitter(for identifier: String, generation: UUID) -> (String) -> Void {
        { [weak self] event in
            guard let self, let entry = self.entries[identifier], entry.generation == generation,
                  entry.component.destinations[event] != nil else { return }
            self.emit(MobileComponentEvent(component: entry.component, key: event, generation: generation))
        }
    }

    func disposeAll() {
        let retired = resources
        entries.removeAll()
        order.removeAll()
        retired.forEach(dispose)
    }

    deinit { resources.forEach(dispose) }
}

#if canImport(UIKit)
import UIKit

protocol MobileComponentExtensionView: AnyObject {
    var view: UIView { get }
    func update(payload: String, emit: @escaping (String) -> Void) throws
    func dispose()
}

protocol MobileComponentExtensionFactory {
    static var name: String { get }
    static func capabilities() -> [String: String]
    static func make(payload: String, compact: Bool, emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView
}

enum MobileComponentExtensions {
    private(set) static var factories: [String: MobileComponentExtensionFactory.Type] = [:]

    static func install(_ definitions: [MobileComponentExtensionFactory.Type]) {
        precondition(Thread.isMainThread)
        precondition(Set(definitions.map { $0.name }).count == definitions.count)
        precondition(definitions.allSatisfy { !$0.name.isEmpty && Set($0.capabilities().keys) == Set([$0.name]) })
        factories = Dictionary(uniqueKeysWithValues: definitions.map { ($0.name, $0) })
    }

    static func capabilities() -> [String: String] {
        precondition(Thread.isMainThread)
        return factories.values.reduce(into: [:]) { result, factory in
            result.merge(factory.capabilities()) { first, _ in first }
        }
    }

    static func make(_ component: MobileComponent, compact: Bool, emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView {
        precondition(Thread.isMainThread)
        guard let factory = factories[component.name], factory.capabilities()[component.name] == "available" else {
            throw MobileComponentFailure.unavailable(component.name)
        }
        return try factory.make(payload: component.payload, compact: compact, emit: emit)
    }
}

final class MobileComponentViews: UIStackView {
    private let compact: Bool
    private var active = true
    var open: ((MobileComponentEvent) -> Void)?
    private lazy var reconciler = MobileComponentReconciler<MobileComponentExtensionView>(
        make: { [weak self] component, emit in
            try MobileComponentExtensions.make(component, compact: self?.compact ?? false, emit: emit)
        }, update: { component, model, emit in
            try component.update(payload: model.payload, emit: emit)
        }, dispose: { component in
            component.dispose()
            component.view.removeFromSuperview()
        }, emit: { [weak self] event in self?.open?(event) })

    init(compact: Bool) {
        self.compact = compact
        super.init(frame: .zero)
        axis = .vertical
        spacing = 20
        isHidden = true
    }

    required init(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    func contains(_ event: MobileComponentEvent) -> Bool { active && reconciler.contains(event) }

    func activate(_ components: [MobileComponent]) {
        active = true
        reconcile(components)
    }

    func reconcile(_ components: [MobileComponent]) {
        guard active else { return }
        do {
            let issues = try reconciler.reconcile(components)
            let views = reconciler.resources.map(\.view) + issues.map { issue in
                let message = MobileAppearance.label(issue.error.localizedDescription, style: .footnote, color: .systemRed)
                message.accessibilityIdentifier = "native.component.error.\(issue.id)"
                return message
            }
            if arrangedSubviews.count != views.count || zip(arrangedSubviews, views).contains(where: { $0 !== $1 }) {
                for child in arrangedSubviews {
                    removeArrangedSubview(child)
                    child.removeFromSuperview()
                }
                views.forEach(addArrangedSubview)
            }
            isHidden = views.isEmpty
        } catch {
            clear()
            let message = MobileAppearance.label(error.localizedDescription, style: .footnote, color: .systemRed)
            message.accessibilityIdentifier = "native.component.error"
            addArrangedSubview(message)
            isHidden = false
        }
    }

    func dispose() {
        active = false
        clear()
    }

    private func clear() {
        reconciler.disposeAll()
        for child in arrangedSubviews {
            removeArrangedSubview(child)
            child.removeFromSuperview()
        }
        isHidden = true
    }
}
#endif
