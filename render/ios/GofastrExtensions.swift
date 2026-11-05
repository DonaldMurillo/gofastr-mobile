import UIKit

protocol MobileFileExtensionFactory {
    static var name: String { get }
    static func capabilities() -> [String: String]
    static func valid(_ operation: MobileFileExtension) -> Bool
    static func make(_ operation: MobileFileExtension, identifier: String, source: UIViewController,
                     focus: MobileForegroundFocus, queue: DispatchQueue,
                     respond: @escaping (MobileFileResult) -> Void) -> MobileForegroundTask?
}

enum MobileFileExtensions {
    private(set) static var factories: [String: MobileFileExtensionFactory.Type] = [:]

    static func install(_ definitions: [MobileFileExtensionFactory.Type]) {
        precondition(Thread.isMainThread)
        precondition(Set(definitions.map { $0.name }).count == definitions.count)
        factories = Dictionary(uniqueKeysWithValues: definitions.map { ($0.name, $0) })
    }

    static func capabilities() -> [String: String] {
        precondition(Thread.isMainThread)
        return factories.values.reduce(into: [:]) { result, factory in
            result.merge(factory.capabilities()) { first, _ in first }
        }
    }

    static func valid(_ operation: MobileFileExtension) -> Bool {
        operation.isValid && (factories[operation.name]?.valid(operation) ?? false)
    }

    static func make(_ operation: MobileFileExtension, identifier: String, source: UIViewController,
                     focus: MobileForegroundFocus, queue: DispatchQueue,
                     respond: @escaping (MobileFileResult) -> Void) -> MobileForegroundTask? {
        guard valid(operation) else { return nil }
        return factories[operation.name]?.make(operation, identifier: identifier, source: source, focus: focus, queue: queue, respond: respond)
    }
}
