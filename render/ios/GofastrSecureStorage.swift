import Foundation

struct MobileSecureStorageRequest: Codable {
    let operation: String
    var key: String?
    var value: Data?
    var protection: String?
    var reason: String?

    func validate() throws {
        let key = key ?? "", protection = protection ?? "", reason = reason ?? ""
        guard (value?.count ?? 0) <= 16_384, reason.utf8.count <= 200,
              !reason.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) else { throw MobileSecureStorageFailure.invalid }
        if operation == "status" {
            guard key.isEmpty, protection.isEmpty, reason.isEmpty, value == nil else { throw MobileSecureStorageFailure.invalid }
            return
        }
        if operation == "authenticate" {
            guard key.isEmpty, value == nil, protection == "biometric", !reason.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw MobileSecureStorageFailure.invalid }
            return
        }
        guard ["put", "get", "delete"].contains(operation), ["device", "biometric"].contains(protection),
              key.range(of: #"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$"#, options: .regularExpression) != nil else { throw MobileSecureStorageFailure.invalid }
        guard (operation == "put") == ((value?.count ?? 0) > 0), operation == "put" || value == nil else { throw MobileSecureStorageFailure.invalid }
        let prompt = protection == "biometric" && operation != "delete"
        guard prompt ? !reason.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty : reason.isEmpty else { throw MobileSecureStorageFailure.invalid }
    }
}

struct MobileSecureStorageResult: Codable {
    var value: Data?
    var found = false
    var available: Bool?
    var biometricAvailable: Bool?

    var wire: [String: Any] {
        var result: [String: Any] = ["found": found]
        if let value { result["value"] = value.base64EncodedString() }
        if let available { result["available"] = available }
        if let biometricAvailable { result["biometricAvailable"] = biometricAvailable }
        return result
    }
}

enum MobileSecureStorageFailure: Error { case invalid }

#if canImport(UIKit)
import UIKit
import Security
import LocalAuthentication

/// Keychain operations stay off the main queue. Each protected access uses a new
/// LAContext and a biometryCurrentSet ACL; no application boolean releases bytes.
final class GofastrSecureStorage {
    private let queue = DispatchQueue(label: "gofastr.native.secure-storage", qos: .userInitiated)
    private var completion: ((MobileServiceResult) -> Void)?
    private var context: LAContext?
    private var generation: UInt64 = 0
    private var cancelRequested = false
    private var backgroundObserver: NSObjectProtocol?
    private let service = (Bundle.main.bundleIdentifier ?? "dev.gofastr.native") + ".gofastr.secure.v1"

    init() {
        // LocalAuthentication may temporarily make the app inactive while its
        // own prompt is visible. Cancel only actual background transitions.
        backgroundObserver = NotificationCenter.default.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { [weak self] _ in self?.cancel() }
    }
    deinit {
        if let backgroundObserver { NotificationCenter.default.removeObserver(backgroundObserver) }
        context?.invalidate()
    }

    static var enabled: Bool { Bundle.main.object(forInfoDictionaryKey: "GofastrSecureStorage") as? Bool == true }
    static var biometricAvailable: Bool {
        guard let purpose = Bundle.main.object(forInfoDictionaryKey: "NSFaceIDUsageDescription") as? String,
              !purpose.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return false }
        var error: NSError?
        return LAContext().canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: &error)
    }

    func perform(_ request: MobileSecureStorageRequest, completion: @escaping (MobileServiceResult) -> Void) {
        precondition(Thread.isMainThread)
        guard self.completion == nil else { completion(MobileServiceResult(status: "failed")); return }
        self.completion = completion
        cancelRequested = false
        generation &+= 1
        let token = generation
        guard Self.enabled else { finish(token, MobileServiceResult(status: "unsupported")); return }
        guard UIApplication.shared.applicationState != .background else { finish(token, MobileServiceResult(status: "cancelled")); return }
        do { try request.validate() } catch { finish(token, MobileServiceResult(status: "failed")); return }
        if request.operation == "status" {
            finish(token, MobileServiceResult(status: "completed", secureStorage: MobileSecureStorageResult(available: true, biometricAvailable: Self.biometricAvailable)))
            return
        }
        if request.protection == "biometric" && request.operation != "delete" {
            guard Self.biometricAvailable else { finish(token, MobileServiceResult(status: "unsupported")); return }
            let context = LAContext()
            context.localizedFallbackTitle = ""
            self.context = context
            context.evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, localizedReason: request.reason ?? "") { success, error in
                DispatchQueue.main.async {
                    guard self.generation == token, self.completion != nil else { return }
                    guard !self.cancelRequested else { self.finish(token, MobileServiceResult(status: "cancelled")); return }
                    guard success else {
                        let code = (error as? LAError)?.code
                        self.finish(token, MobileServiceResult(status: code == .userCancel || code == .appCancel || code == .systemCancel ? "cancelled" : "denied"))
                        return
                    }
                    if request.operation == "authenticate" { self.finish(token, MobileServiceResult(status: "completed", secureStorage: MobileSecureStorageResult())); return }
                    self.execute(request, token: token, context: context)
                }
            }
        } else { execute(request, token: token, context: nil) }
    }

    /// Invalidate a prompt. Started Keychain writes settle with their observed
    /// result rather than returning a cancellation before the write finishes.
    func cancel() { cancelRequested = true; context?.invalidate() }

    private func execute(_ request: MobileSecureStorageRequest, token: UInt64, context: LAContext?) {
        queue.async {
            let result: MobileServiceResult
            do { result = try self.access(request, context: context) }
            catch { result = MobileServiceResult(status: "failed") }
            DispatchQueue.main.async { self.finish(token, result) }
        }
    }

    private func access(_ request: MobileSecureStorageRequest, context: LAContext?) throws -> MobileServiceResult {
        var query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                    kSecAttrService as String: service,
                                    kSecAttrAccount as String: (request.protection ?? "") + ":" + (request.key ?? ""),
                                    kSecAttrSynchronizable as String: false]
        if let context { query[kSecUseAuthenticationContext as String] = context }
        if request.operation == "get" {
            query[kSecReturnData as String] = true
            query[kSecMatchLimit as String] = kSecMatchLimitOne
            var item: CFTypeRef?
            let status = SecItemCopyMatching(query as CFDictionary, &item)
            if status == errSecItemNotFound { return completed() }
            guard status == errSecSuccess else { return failure(status) }
            guard let data = item as? Data, !data.isEmpty, data.count <= 16_384 else { throw MobileSecureStorageFailure.invalid }
            return MobileServiceResult(status: "completed", secureStorage: MobileSecureStorageResult(value: data, found: true))
        }
        if request.operation == "delete" {
            query[kSecUseAuthenticationUI as String] = kSecUseAuthenticationUIFail
            let status = SecItemDelete(query as CFDictionary)
            return status == errSecSuccess || status == errSecItemNotFound ? completed() : failure(status)
        }
        guard let value = request.value else { throw MobileSecureStorageFailure.invalid }
        let updated = SecItemUpdate(query as CFDictionary, [kSecValueData as String: value] as CFDictionary)
        if updated == errSecSuccess { return completed() }
        guard updated == errSecItemNotFound else { return failure(updated) }
        // Bound the app's record count without returning any stored secret.
        let countQuery: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                        kSecAttrService as String: service,
                                        kSecReturnAttributes as String: true,
                                        kSecMatchLimit as String: kSecMatchLimitAll,
                                        kSecUseAuthenticationUI as String: kSecUseAuthenticationUIFail]
        var records: CFTypeRef?
        let counted = SecItemCopyMatching(countQuery as CFDictionary, &records)
        guard counted == errSecItemNotFound || counted == errSecSuccess,
              ((records as? [[String: Any]])?.count ?? 0) < 128 else { return MobileServiceResult(status: "failed") }
        query[kSecValueData as String] = value
        if request.protection == "biometric" {
            var error: Unmanaged<CFError>?
            guard let control = SecAccessControlCreateWithFlags(nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, .biometryCurrentSet, &error) else { throw MobileSecureStorageFailure.invalid }
            query[kSecAttrAccessControl as String] = control
        } else { query[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly }
        let status = SecItemAdd(query as CFDictionary, nil)
        return status == errSecSuccess ? completed() : failure(status)
    }

    private func completed() -> MobileServiceResult { MobileServiceResult(status: "completed", secureStorage: MobileSecureStorageResult()) }
    private func failure(_ status: OSStatus) -> MobileServiceResult {
        MobileServiceResult(status: status == errSecUserCanceled ? "cancelled" : status == errSecAuthFailed || status == errSecInteractionNotAllowed ? "denied" : "failed")
    }
    private func finish(_ token: UInt64, _ result: MobileServiceResult) {
        guard generation == token, let completion else { return }
        self.completion = nil
        context?.invalidate()
        context = nil
        completion(result)
    }
}
#endif
