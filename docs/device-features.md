# Shared native device features

Share-in, secure storage with biometric authentication, and home-screen widgets
use the same Go contracts on Android and iOS. The app authors requests and
handles typed outcomes in `nativeui.Route.Services`. The platform adapters own
OS prompts, storage, intents, and extension processes. All three are opt-in.

| Feature | Go request | Android adapter | iOS adapter |
| --- | --- | --- | --- |
| Receive shares | `ServiceRequest.IncomingShare` | SEND intents and a private inbox | Share extension and App Group inbox |
| Secrets and authentication | `ServiceRequest.SecureStorage` | Keystore AES-GCM and BiometricPrompt | Keychain and LocalAuthentication |
| Home-screen summary | `ServiceRequest.Widget` | AppWidgetProvider and RemoteViews | WidgetKit extension and App Group snapshot |

The same Go-only demo runs on both platforms:

```powershell
go run ./cmd/gofastr-native build --platform android-emu --renderer native --pkg ./examples/devicefeatures/featuresnative --receive-shares --secure-storage --biometric-usage 'Authenticate to use the demo secret.' --widgets --url-scheme gofastr-features --out dist/mobile/device-features
```

On a Mac, use `--platform ios-sim` and add
`--app-group group.dev.example.devicefeatures`. The CLI compiles and embeds the
share and WidgetKit executables separately under `PlugIns/*.appex`, with a shared
App Group entitlement. Neither extension links the Go runtime. The host's
Keychain group stays private to the host. App Group names must match the
registered group and provisioning profiles for a physical device. The current
CLI builds simulator apps with ad-hoc signing; physical signing is still a
separate release step.

The demo reviews or discards incoming shares. It does not import them into Notes.
For an app import, save using the stable incoming identifier for deduplication,
then acknowledge it in a separate service. A repeated read is intentionally safe.

The secure demo uses fixed test bytes and displays only the byte count.
Biometric authentication does not encrypt an existing database. Android
device-protected storage works from API 24; crypto-bound biometric operations
require API 28 or newer. No passcode fallback releases a biometric secret.

Widgets display only explicitly published public snapshots. Add the widget
through the system launcher/gallery after publishing. Updating the stored
snapshot requests a system reload; the completion does not prove that the OS
has displayed it. Clear the snapshot when signing out or removing its content.

See [share-in](share-in.md), [secure storage](secure-storage.md), and
[widgets](widgets.md) for bounds, platform behavior, and validation gates.

## Acceptance

`scripts/android_features_smoke.py` builds a small test sender and AppWidgetHost
and installs a debuggable copy of the demo on an explicitly selected emulator.
It never clears app data. Private-file inspection and deliberate corruption
apply only to the fixed demo secret in that test app. The delivered demo APK
does not enable debugging. The script checks encrypted storage, restart and
tamper handling, bounded incoming shares with real provider grants, explicit
acknowledgement, widget display, deep links, and clearing.

With a virtual fingerprint already enrolled as emulator ID 1, add
`--biometrics-only` to check crypto-bound reads and writes, cancellation, and
backgrounding. That mode does not change enrollment or lock settings. Physical
biometric hardware, enrollment invalidation, lockout, and iOS acceptance remain
separate gates.

Windows checks include the Go suite, vet, Android SDK compilation/resource
linking, actual Android 14 x86-64 behavior, and the existing WebView tests.
macOS tests compile and execute Foundation wire fixtures and compile/sign both
iOS extensions against the simulator SDK. These tests skip on Windows, so the
iOS implementation still requires a Mac build and runtime acceptance.
