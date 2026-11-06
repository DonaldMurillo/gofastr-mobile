# Windows and native Android

`--platform android-emu --renderer native` builds the shared Go screens with
Android controls. `notesnative` is the native binding on both platforms.
Applications supply Go definitions, actions, and typed services. They do not
supply Java screens or a Gradle project.

```powershell
go run ./cmd/gofastr-native doctor --platform android-emu
go run ./cmd/gofastr-native build --platform android-emu --renderer native --pkg ./examples/notes/notesnative --microphone-usage 'Record private voice notes.' --url-scheme notes-demo --out dist/mobile/android-native
go run ./cmd/gofastr-native run --platform android-emu --renderer native --pkg ./examples/notes/notesnative --device Pixel_5_API_34 --microphone-usage 'Record private voice notes.' --url-scheme notes-demo --out dist/mobile/android-native
```

Use an installed AVD name or an attached adb serial for `--device`. The SDK can
be selected with `ANDROID_HOME` or `ANDROID_SDK_ROOT`. Windows discovery also
checks `%LOCALAPPDATA%\Android\Sdk`. The JDK comes from `JAVA_HOME` or `PATH`.
Compile with SDK API 33 or newer; the app's minimum runtime is Android API 24.
No global tools, environment variables, SDK packages, drivers, or virtual-device
settings are changed by a build. Generated tools and compatibility sources stay
under the selected output directory. Run installs only the selected app package.

## Renderer and services

- Native lists, search, collapsed filters, refresh, accumulated paging,
  stable-ID selection, row action menus, and declared swipe actions.
- Native editors keep their controls while saves, presentations, and media
  updates complete. Text, IME, and focus stay local. Go owns validation, drafts,
  save outcomes, and departure policy. Background snapshots persist through Go.
- Each tab retains its stack and scroll position. Media navigation checkpoints
  dirty buffers. Back and tab departure use Go's keep/discard policy. Android 13+
  Back callbacks call the same guard. Restarts reopen the current saved-state
  route when Android supplies it; durable drafts remain independent of that route.
- Alerts, menus, confirmations, dialogs, and task sheets use native dialogs.
  Sheets are bottom-aligned tasks. Selection bounds and continuations are
  validated in Go. A rejected result stays open for correction. Responses settle
  once, followed by read-only completion. Uncertain writes are not replayed.
- Files use Android's Storage Access Framework. Only the selected URI is read.
  Imports and exports are bounded to 1 MiB; exports write to the user's selected
  destination. No broad storage permission or OS path crosses into Go.
- The photo library owns its Java acquisition adapter. Library selection uses a
  system chooser. Camera capture uses the system camera and a temporary private
  content URI. A worker bounds source bytes, samples decoding, applies orientation,
  and reencodes a JPEG without source metadata. Camera access can be disabled with
  `--camera-usage ''`.
- The media library owns its compiled factory. Keyed images and attachment cards
  update without replacing editors. Local audio has play/pause/seek controls and
  releases playback on backgrounding, navigation, or component removal.
- Clipboard writes and rich sharing require an admitted Go action. Sharing uses
  private, randomly named content URIs with temporary grants. Transfer storage is
  capped and old transfers expire after 24 hours. Closing the share sheet does not
  prove recipient delivery.
- Recording requires `--microphone-usage`, then Android permission at the user's
  explicit action. AAC/M4A recording is foreground-only, bounded by the request
  and the framework's 60 second / 1 MiB ceiling. Notes requests 512 KiB. Cancelled
  or backgrounded recordings do not become attachments.
- Reminders use app-scoped notification channels and inexact alarms. Scheduling
  validates future local time and requests notification permission when needed.
  Status/cancel do not request permission. Stored alarms are restored after a
  device restart or app update; notification taps open a Go route through the
  navigation guard. Android can delay inexact alarms. Force-stop prevents OS
  delivery until the app runs again. No exact-alarm permission is requested.
- Live reads run separately from commands. They watch only the authorized query
  while the collection is visible. Retired reads can reopen once without replaying
  a write. Editors are never replaced by live-read replies.

The renderer is under `render/android/`. The activity template only supplies Go
bindings, configuration, and the compiled factory catalog. Accessibility IDs use
the same `native.field.*`, `native.action.*`, `native.row.*`, and `native.tab.*`
names as the UIKit renderer. Normal font scaling and light/dark configuration
apply to the controls.

Android native is a negotiated `android` renderer. A required extension without
an Android adapter fails packaging. Optional manifest `android` metadata supplies
`factory` and package-relative Java `sources`; classes use the
`dev.gofastr.nativeui` package. The media and photo packages own their adapters.
`extension init` creates Android scaffolds alongside Swift and WebView assets.

## Build compatibility

The pinned dependencies exposed two failures inside a real Android app sandbox:

1. The translated musl code in `modernc.org/libc` used legacy x86-64 Linux path
   syscalls. Android killed startup with `SIGSYS`, syscall 6. The build copies the
   pinned module and maps those calls to equivalent `*at` calls for Android/amd64.
   Other architectures keep their original calls.
2. GoFastr's installation identity publisher used a hard link, which Android
   denied. Its Android build copy uses `renameat2(RENAME_NOREPLACE)` on the synced
   temporary file. This preserves atomic publication and refuses to overwrite an
   existing secret or installation identity. There is no overwrite fallback.

The CLI creates a separate module from the consumer's resolved dependency graph,
preserving pins and local replacements. These copies live under
`work/android-libc/`. The original repository, `go.mod`, `go.sum`, and Go module
cache stay unchanged. Compatibility generation rejects an unexpected source shape.
These are temporary build adaptations until the upstream dependencies support
Android directly. Review them when changing the dependency pins.

Native libraries link with a 16 KiB maximum page size. The build checks every ELF
load segment before packaging. Libraries remain compressed in the APK and Android
extracts them at installation. See Android's [page-size requirements](https://developer.android.com/guide/practices/page-sizes).
Windows invokes SDK Java entry points directly instead of passing arguments
through batch-file shell parsing. Generated Go tools use `.exe` names.

Windows draft storage uses a protected per-user ACL and synced file replacement.
It does not treat Unix mode bits as a Windows permission check. Unix and Android
retain private file modes and directory sync. Windows tests also caught two
non-Android assumptions: timestamp ties in undo eviction and teardown running
after temporary-directory removal. Undo now uses insertion order; lifecycle test
teardown runs before directory cleanup.

## Checks

```powershell
go test ./...
go vet ./...
node --test render/webview/*.test.cjs
python scripts/android_smoke.py --serial emulator-5580 --package dev.gofastr.androidqa --services
```

The Java checks compile the renderer and both extension adapters against the real
Android SDK and actual generated Go bindings when a full SDK/JDK is present.
The smoke test requires an emulator, creates its own notes, and never clears app
data. Results, screenshots, and UI XML go under `dist/android-acceptance/`.
Only one UI automation process should drive that emulator at a time.

November 6, 2026 validation used Windows, JDK 17, SDK/build-tools 36, NDK 27.1,
and an isolated Android 14 x86-64 emulator. Both renderer options build as APKs.
Native checks cover creation/save/list, backgrounding and process-death recovery,
Back prompts, correction of an invalid modal selection, clipboard, and a SAF file
import/export with exact UTF-8 byte comparison and dirty-editor return.
Photo acquisition/export strips a source metadata marker and produces validated
JPEG bytes. Foreground AAC recording passes Go validation, playback stops on
background, share cancellation retains the editor, and reminder status,
future scheduling with permission, and cancellation pass in the emulator.
The real Android WebView shell also launches and saves through the Go bridge.
See the generated acceptance report for the device workflows actually run.

ARM64 libraries compile and pass ELF alignment checks; this does not prove an
ARM64 device launch. Android 7-13 and Android 15-16 runtime acceptance, physical
camera/microphone, cloud document providers, notification delivery/reboot/taps,
TalkBack, hardware IME/composition, and recipient delivery require separate device
testing. Native Android does not implement PDF/Office rendering or cloud sync.
The WebView keeps its existing disclosed browser fallbacks. Its Android shell
now opens system file choosers; it does not advertise native Android services.
