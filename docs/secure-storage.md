# Secure storage and biometric prompts

`device/securestore` defines one Go contract for Android Keystore and iOS
Keychain adapters. Enable the native host with `--secure-storage`. Supply
`--biometric-usage 'Unlock private notes.'` to enable biometric operations.
Apps declare an admitted `nativeui.ServiceRequest` with a `SecureStorage`
request. The normal foreground-service coordinator owns its opaque task ID and
settles its result once. WebView hosts do not store secrets through this API.

```go
request := securestore.Request{
    Operation: securestore.Put,
    Key: "session-token",
    Protection: securestore.Biometric,
    Reason: "Save your session token",
    Value: []byte(token),
}
```

Operations are `put`, `get`, `delete`, `status`, and `authenticate`. Secrets
must contain 1 to 16,384 bytes. Keys contain at most 128 ASCII letters, digits,
periods, underscores, colons, or hyphens and start with a letter or digit.
Each app may store at most 128 records. `get` returns a typed result with
`found: false` for a missing record, or `found: true` and base64 `value` bytes.
No other operation returns secret bytes. `status` reports storage and enrolled
strong-biometric availability without asking for authentication.

`device` and `biometric` are separate compartments. Requesting a device record
with the same key as a biometric record cannot read or replace the biometric
record. A caller must choose the intended protection on every read and write.
There is no automatic downgrade when authentication fails or is unavailable.

## Protection on each platform

Android stores AES-256-GCM ciphertext in the app's no-backup directory. A
separate, nonexportable Android Keystore key protects each record. Authenticated
additional data binds ciphertext to its record and protection. A changed nonce,
ciphertext, or tag fails the read. The framework never falls back to plaintext
or regenerates a missing key while reading. Keystore hardware backing depends
on the device; the API does not claim every key is hardware-backed.

`device` storage runs on Android API 24 and later. It requires no fresh prompt.
The app sandbox and Keystore protect its bytes at rest. `biometric` storage
requires API 28 or later and an enrolled strong biometric. Each encrypt or
decrypt operation passes its Keystore cipher to the OS `BiometricPrompt` as a
`CryptoObject`. No remembered application boolean can authorize that cipher.
Enrollment changes invalidate the key. API 28 and 29 report enrolled fingerprint
availability conservatively because their framework APIs cannot reliably query
all strong face sensors. An actual crypto operation remains the final check.

iOS stores generic-password records in the app's default Keychain group with
`kSecAttrAccessibleWhenUnlockedThisDeviceOnly`. Records are not synchronizable.
Biometric records add a `biometryCurrentSet` access control. A fresh
`LAContext` prompts for Face ID or Touch ID before each protected read or write,
and the same context is used for the Keychain operation. No device-passcode
fallback releases a biometric record. Keychain groups remain separate from
share-extension and widget app groups. Simulator signing uses a per-app group;
a physical build must use its provisioned application identifier and access
groups.

Deletion returns no bytes and does not request authentication. Deleting a
missing record succeeds. An invalidated biometric key can be removed explicitly
and replaced with a new record. The old ciphertext cannot be recovered through
this API. The app must decide how users recover or sign in again.

`authenticate` performs a fresh strong-biometric presence check. It does not
read a secret, unlock a database, or encrypt existing notes. To protect data
cryptographically, keep the relevant key in a biometric record and use the
successful `get` result for the app's encryption implementation.

## Cancellation and data handling

Only one foreground device service runs at a time. Backgrounding cancels a
biometric prompt. A storage operation that has already started settles through
its actual OS callback; cancellation must not report that a completed write
was undone. The host must not replay uncertain writes.

Native errors contain no keys, values, ciphertext, or OS exception text. Java
clears working plaintext arrays after crypto and cancelled prompts. The JSON
bridge, Go values, Swift `Data`, and OS libraries may retain memory copies; this
API makes no promise of complete memory erasure. Application callbacks must
keep secrets out of logs, list models, draft snapshots, and widget payloads.
Android ciphertext is excluded from backup. iOS Keychain lifetime can extend
past uninstall, so deletion on uninstall is not guaranteed.

## Verification

Go tests validate operation/protection combinations, byte bounds, base64 wire
encoding, missing reads, and rejection of secret bytes in unrelated results.
The Android library compiles against the SDK without AndroidX or Gradle. An
executable Swift Foundation test checks the same Go-generated fixture on macOS.
The Swift test skips on Windows. Physical-device biometric enrollment changes,
lockout, hardware-backed key behavior, and iOS Keychain acceptance remain
separate runtime checks.

The Android 14 x86-64 emulator acceptance test writes and reads encrypted demo
bytes, restarts the process, rejects a damaged ciphertext record, and checks
deletion and missing reads. An enrolled virtual strong fingerprint authorizes
encryption and decryption; cancelling or backgrounding the prompt releases no
bytes. This test caught and fixed a Keystore ordering error: `updateAAD` must run
after biometric authorization, just before `doFinal`. A separate test APK enables
debugging for private-file inspection. The normal built APK remains non-debuggable.

The adapters follow the platform contracts for [Android crypto-bound biometric
operations](https://developer.android.com/reference/android/hardware/biometrics/BiometricPrompt.CryptoObject),
[Apple biometric access control](https://developer.apple.com/documentation/security/secaccesscontrolcreateflags/biometrycurrentset),
and [Keychain access groups](https://developer.apple.com/documentation/security/sharing-access-to-keychain-items-among-a-collection-of-apps).
