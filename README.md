# gofastr-native

Go-authored application screens, shared behaviors, and typed device services,
with native UIKit, native Android, and mobile WebView renderers. Application screens and actions
are Go `nativeui` definitions. Apps write no Swift, HTML, CSS, or screen templates.
The Notes example exercises the same definitions through either renderer.
See [shared mobile screens](docs/shared-mobile-screens.md) and
[Native Notes](docs/native-presentation.md).

Imported libraries can own [rendering components](docs/rendering-extensions.md)
and [device acquisition adapters](docs/compiled-extensions.md), while applications
stay Go-only. Notes uses the compiled media component for its attachments.
See [extension authoring tools](docs/extension-tooling.md) and the separate
[physical-device acceptance gate](docs/ios-device-acceptance.md).

Existing GoFastr web applications can also run inside a phone app's process.
Their Go server answers WebView requests over an in-memory listener: no socket,
no port, no loopback origin. Native rendering does not remove WebView support.
This remains experimental, not a supported production framework yet.

Previously named `gofastr-mobile`; the module and CLI are now `gofastr-native`.
See [rename and migration](docs/project-rename.md) for the new paths and the
compatibility identifiers deliberately left unchanged.

The module builds on gofastr's public API only. Go's `internal/` rule goes by
import path, so this repository sees exactly what any other app sees. When it
needs something the framework does not expose, the fix goes into gofastr as a
public seam, not a copy here.

## Start a new app

```sh
go run ./cmd/gofastr-native doctor --platform android-emu
go run ./cmd/gofastr-native init --out ../hello
cd ../hello
go test ./...
go tool gofastr-native dev --platform android-emu --renderer native --pkg ./hello
```

`init` creates a module with Go screens, a private item store, durable
drafts, tests, and the CLI pinned as a `go tool`. It never writes into an
existing directory. Until gofastr-native publishes a version, the new
`go.mod` uses this checkout through a `replace`. `dev` rebuilds, reinstalls,
and restarts the app after each change; it is not hot reload. See
[getting started](docs/getting-started.md) for what survives a restart and
what `dev` does when a build or install fails. `examples/starter` is the
same scaffold, rendered inside this repository.

See [developer workflow and validation](docs/developer-workflow.md) for a
command overview, completed checks, and the remaining platform acceptance gates.

## The gofastr pin

No gofastr release carries the seams this module uses yet, so `go.mod` pins
the gofastr main commit that merged them (#663, `6905b8f07`) as a
pseudo-version. The seams:

| Seam | What it does here |
|---|---|
| `framework.App.Serve(net.Listener)` | Runs the app's full lifecycle on the in-memory listener. |
| `window.__gofastr_transport` (core-ui runtime) | Every runtime request goes through it, so `bridge.js` can carry requests over the native message channel. |
| `framework/localapp` | Opens the data directory: SQLite DB, session secret, the installation's owner id. Shared with the desktop and service batteries. |
| `App.NoteAuthenticator` and the banner | The startup banner prints paths for a listener with no `host:port`, and the host quiets the "No auth is wired" warning. |

Once a release ships them, replace the pseudo-version with that tag. To work
against a local gofastr checkout, copy `go.work.example` to `go.work` and
point it at the checkout. `go.work` is gitignored.

## Layout

| Path | What it is |
|---|---|
| `mobilehost/` | The host. `Options(data)` turns a `localapp.Data` into app options. `New(app, cfg)` registers the `mobile` battery (single-user identity), and `Start` runs the app on an in-memory listener. `Do` and `Open` serve one request buffered or streamed. `Bridge` answers one message from `bridge.js`. |
| `mobilehost/bridge.js` | The page half of the native bridge: installs `window.__gofastr_transport` and sends native POST forms over the same channel. |
| `mobileui/` | An experimental external mobile-web presentation package: iOS/Android components, styles, behavior, and adaptive themes, using GoFastr's public registry. |
| `examples/notes/notesapp/` | Composes an owner-scoped entity, rendering-independent Notes service, and handwritten mobile screens. No CSS or structural screen markup. |
| `examples/notes/notesweb/` | Handwritten task screens composed from `mobileui`, plus the HTTP save adapter. |
| `examples/notes/notes/`, `notesstore/` | Typed application behavior and a temporary scoped-CRUD repository adapter. No presentation code. |
| `examples/notes/notesterminal/` | A second, line-oriented presentation of the same service, without HTML or browser dependencies. Run with `go run ./examples/notes/cmd/notes-terminal --data /absolute/directory`. |
| `examples/notes/notesmobile/` | Thin WebView binding for shared Go mobile screens: `Start`, `Stop`, `Handle`, `Open`, `Bridge`, with strings, bytes and ints only. |
| `nativeui/` | External mobile screen/action definitions in Go, local routing, and the gomobile wire envelope. |
| `examples/notes/notesscreens/` | One Notes screen/action package, written only in Go using `nativeui`, consumed by UIKit and WebView. |
| `examples/notes/notesnative/` | Thin native binding: lifecycle, screen rendering/action dispatch, compatibility data commands, and centrally supplied installation identity. |
| `cmd/gofastr-native/` | The CLI that builds the phone app and runs it on the iOS simulator or an Android emulator. No Xcode project, no Gradle. |
| `render/ios/` | Ordinary, binding-independent UIKit renderer source, copied verbatim and compiled alongside the generated app entry. |
| `render/android/` | Binding-independent Java renderer and device adapters, compiled against the Android SDK without Gradle. |
| `render/webview/` | Generic adapter from the same Go mobile definitions to `mobileui` components, actions, and GoFastr navigation. |
| `cmd/gofastr-native/templates/` | Library-owned WebView shells and minimal native binding/bootstrap glue, plist, and manifest. Apps only supply Go screens. |
| `sim/` | A Chromium set up as a phone WebView (390x844 @3x, touch, phone UA). Every request is intercepted through the DevTools Fetch domain and answered by `mobilehost`; a MessagePort stands in for the native channel. |

## External presentation prototype

The mobile Notes bindings now consume one Go screen/action package,
`notesscreens`, through either UIKit or the generic WebView renderer. The latter
uses the external `mobileui` design system with iOS/Android modes, not desktop
entityui screens at phone width. Both renderers support list/editor/About,
search, create/edit, explicit saves, validation, and draft recovery, with
documented platform differences.

Notes is the main application example. Its **Behaviors** screen adds
Go-authored alerts, confirmations, menus, dialogs, sheets, and single/multiple
selection through both renderers. Native/WebView packages are thin launch
adapters, not separate example products. See [Go presentation behaviors](docs/presentation-behaviors.md).

The same application also has **Files**: import text/Markdown notes, export a
note's current buffer, and add/export/remove private attachments on saved notes.
These use [typed Go file services](docs/files-services.md), with a 1 MiB transfer
limit, native iOS document pickers, and an explicit mobile-WebView fallback.
Saved-note attachments also offer **Choose photo** and **Take photo** through
[typed photo services](docs/photos-services.md), normalizing to bounded JPEGs
without original EXIF/GPS metadata. Native camera acceptance requires a real
device; the WebView path is an explicitly disclosed chooser fallback.
Photo acquisition is the first [compiled extension](docs/compiled-extensions.md):
Notes registers it in Go; its UIKit/WebView adapters belong to the library package.
Photos display inside the note and open a larger attachment preview. Both
renderers share the same Go [image preview definitions](docs/image-previews.md).

Notes also exposes typed reminder scheduling/status/cancellation, rich note and
attachment sharing, explicit clipboard writes, and private voice recordings.
Collections now page in the owner-scoped repository beyond 200 records and
watch confirmed changes from the shared service. See [device services and
acceptance limits](docs/mobile-services.md). Native Android uses the same Go
protocol and screens. See [Windows and Android builds and acceptance](docs/android.md).

[Incoming shares, secure storage with biometrics, and home-screen widgets](docs/device-features.md)
also use shared Go requests with Android and iOS adapters. The Go-only
`examples/devicefeatures/featuresnative` demo exercises their opt-in APIs.

The [Notes integrations](docs/notes-device-features.md) add reviewed shared
imports, explicitly selected note widgets, and biometric-protected text notes.

[Portable Notes workflows](docs/notes-portable-workflows.md) add reviewed backup
and restore, matching search passages, bulk selection, and guarded editor saves.

The same Notes app now exercises [mobile components](docs/mobile-components.md):
typed toggles/selects/date/time, filters and accumulated Load more, stable-ID
multi-selection and bulk actions with undo, Notes/Files/Settings tabs, and row
swipes with labelled menu alternatives. UIKit retains a navigation stack per tab;
WebView retains each tab's last route/scroll/focus within GoFastr's shared history.
The [mobile document design](docs/mobile-design.md) keeps writing first, puts
photos/files inside the note, and gives the Files tab a real attachment library
with readable metadata and reusable previews. Styling stays in renderer libraries;
the example app still authors only Go screen definitions.
Reminder fields are metadata; scheduling is an explicit device-service action.
Notes paging is repository-backed; the Files library currently covers the 200
most recent active notes. Optional native iOS custom links use `--url-scheme notes-demo`.

Existing whole-GoFastr-web-app hosting remains independent and supported.
`notesweb` preserves that handwritten web presentation; the terminal adapter
reuses the same typed service without implementing the mobile screen contract.

The native prototype uses Go-authored `nativeui` screens and a reusable, bundled
UIKit or Android renderer selected by `--renderer native`. Apps do not write platform templates
or wire JSON. No GoFastr code or production dependencies changed.
UIKit now composes services/installation data without mounting unused web
screens. It still uses in-memory `App.Serve` for the complete framework lifecycle;
listener-free startup is not claimed. Shared editor policy, durable draft storage,
capabilities and observed effect receipts live in [mobile runtime services](docs/mobile-runtime.md).
See [the architecture, public APIs, validation, and limitations](docs/presentation-prototype.md)
and [the native host contract](docs/native-presentation.md), plus
[implementation notes](docs/agent-notes.md).

The [shared-screen implementation](docs/shared-mobile-screens.md) documents the
working APIs and limits. The [mobile presentation roadmap](docs/mobile-roadmap.md)
tracks remaining shared headless task behaviors, async correctness, and explicit
platform capabilities. Sharing definitions is not yet behavior parity.

The [shared reliability layer](docs/mobile-reliability.md) gates actions/departure
on durable drafts, guards app history Back/Forward, checkpoints on backgrounding,
and recovers retired live reads without replaying writes or replacing inputs.

## Run the tests

Validation is local-only; this repository intentionally has no CI workflows.

```bash
go vet ./...
go test ./...
node --test render/webview/*.test.cjs
SIM_SHOTS=/tmp/shots go test ./sim/ -run TestPhoneWebView -v
SIM_SHOTS=/tmp/shared-shots go test ./sim/ -run TestSharedMobileScreensWebView -v
```

`TestPhoneWebView` runs twice: once as iOS and once with Android's limits
(the interception layer drops every request body). The generated Java is
compiled against gobind's Java bindings and signature-exact Android stubs
when a working `javac` (JDK 17+) is on `PATH`; the test skips otherwise.

## Run it on a simulator or emulator

`gofastr-native` does for phones what `gofastr desktop build` does for the
Mac. It generates a small native shell, compiles it with the platform's
command-line tools, assembles the `.app` or `.apk` itself, then installs and
launches it. Xcode and the Android SDK must be installed; neither is opened.

Check the toolchain first. It is read-only and prints the fix for anything
missing:

```bash
go run ./cmd/gofastr-native doctor
```

iOS simulator (a Mac with Xcode):

```bash
go run ./cmd/gofastr-native run --platform ios-sim --pkg ./examples/notes/notesmobile --screenshot dist/ios.png
```

Real UIKit Notes (separate app and data, no WebView screens):

```bash
go run ./cmd/gofastr-native run --platform ios-sim \
  --pkg ./examples/notes/notesnative \
  --renderer native \
  --id dev.gofastr.notesnative --name 'Native Notes' --out dist/mobile/ios-native
```

Android emulator (an Android SDK with build-tools, a platform, the NDK, the
emulator and one AVD, plus a JDK 17+):

Native Notes on Windows, from PowerShell:

```powershell
go run ./cmd/gofastr-native doctor --platform android-emu
go run ./cmd/gofastr-native run --platform android-emu --renderer native --pkg ./examples/notes/notesnative --device Pixel_5_API_34 --microphone-usage 'Record private voice notes.' --url-scheme notes-demo --out dist/mobile/android-native
```

Use an AVD name listed by `doctor`, or an attached device's adb serial. Camera
capture is enabled by default; recording requires the explicit microphone flag.
Build-only uses `build` in place of `run`. The APK contains ARM64 and x86-64
libraries and uses a debug signing key. See [Android implementation and tests](docs/android.md).

The WebView host remains available:

```bash
go run ./cmd/gofastr-native run --platform android-emu --pkg ./examples/notes/notesmobile --screenshot dist/android.png
```

`build` instead of `run` stops after the `.app` or `.apk`. `--dry-run`
prints every step without running any. `--device` picks a simulator (name or
UDID), an adb serial or an AVD. `--id` and `--name` set the bundle id and the
display name. Output lands in `dist/mobile/<platform>/`.
`--renderer native` selects the bundled Go-definition-driven UIKit or Android
renderer. Without it the WebView hosts remain the default. `--host-template` remains an experimental custom-host escape hatch,
not the application screen-authoring model; it cannot be combined with native
renderer selection. See [the contract and limits](docs/native-presentation.md).
`--microphone-usage 'Record a voice note to attach privately.'` explicitly adds
the iOS microphone purpose string; recording is unavailable when it is omitted
or empty. The default iOS WebView host also bundles the reusable native-service
adapter; custom host templates must opt into that library integration themselves.

| iOS simulator | Android emulator |
|---|---|
| build the pinned `gomobile` + `gobind` (go.mod `tool` directive) | same |
| `gomobile bind -target=iossimulator` -> `Notesmobile.xcframework` | `gomobile bind -target=android/arm64,android/amd64` -> `notesmobile.aar` |
| render `App.swift`, `Info.plist` | render `MainActivity.java`, `AndroidManifest.xml` |
| `xcrun swiftc` links the shell to the framework | `javac` -> `d8` -> `aapt2 link`, then dex + `libgojni.so` added in Go |
| `codesign -s -` (ad hoc; enough for the simulator) | `zipalign`, `apksigner` (debug key) |
| `simctl boot/install/launch`, `simctl io screenshot` | `emulator -avd`, `adb install/am start`, `screencap` |

Safari > Develop > Simulator attaches the Web Inspector to the iOS WebView;
`chrome://inspect` attaches DevTools to the Android one.

Signed release builds, Android App Bundles, and `--platform ios-device` are
described in [release builds](docs/release.md). That page lists which release
gates still need hardware or a store account and have not been run.

Run the [component gallery](docs/component-gallery.md) to explore supported
controls. Use [inspection and tracing](docs/debugging.md) to diagnose screen
definitions and runtime commands without logging app content.

A simulator build runs one JavaScript check on request: the body of an async
function in `GOFASTR_MOBILE_EVAL` runs once in the first page, and its result
is logged. `simctl launch` passes it through as
`SIMCTL_CHILD_GOFASTR_MOBILE_EVAL`; read the log with `--console-pty`. Device
builds compile the hook out.

## How a request reaches Go

- **Boot-time GETs** (the first document, stylesheets, scripts, images) go
  through the platform's interception API: `WKURLSchemeHandler` on iOS,
  `shouldInterceptRequest` on Android. The shell hands each one to `Handle`
  or `Open`.
- **Runtime requests** (client navigation, RPCs, form saves, islands) go
  through `window.__gofastr_transport`, which `bridge.js` installs. It sends
  the method, URL, headers and base64 body as JSON over the native channel to
  `Host.Bridge`:
  - iOS: a `WKScriptMessageHandlerWithReply` named `gofastr`, main frame and
    app scheme only.
  - Android: a `WebMessageChannel` port posted to the app origin after each
    page load, instead of `addJavascriptInterface`.
- **Native POST forms** the runtime leaves alone are sent the same way with
  `navigate: true`. The shell shows the answer as the next document:
  `loadSimulatedRequest` on iOS, `loadDataWithBaseURL` on Android.
- **Redirects** are followed in Go (the Fetch Standard's limit of 20), and
  the reply carries the final URL and a `redirected` flag, which the runtime
  reads.
- **Cookies** stay in a Go cookie jar. `Set-Cookie` never reaches the
  WebView.

The bridge buffers: a bridged request returns its whole body at once, capped
at 64 MiB. A long-lived stream (SSE) or a large download goes through the
interception path and `Open`, which hands chunks over as they arrive.

## What has been verified

- **Shared Go definitions in real WKWebView (iOS 26.5 simulator).** Fresh bind,
  build, install, and launch; New, invalid/corrected save, retained validation
  fields, reopen, search, and About dismissal use `notesscreens` over the bridge.
  A window marker survives the flow, proving same-document client navigation.
- **Shared-screen browser acceptance, both modes.** Real in-memory host/bridge:
  validation and persistence, editable pending saves/caret retention, canonical
  create identity without duplicate records, durable Go draft reload, unsaved
  share fallback, keep/discard drafts, search, About, light/dark, and 320px/200% text. The independent
  legacy web presentation still has its existing regression coverage.
- **Real UIKit Notes (iOS 26.5 simulator).** Native list/search, validation,
  create/save/reopen, durable local draft restoration, dirty Back, system
  keyboard avoidance, user-initiated share sheet, and medium/large native sheet.
  Go command tests also open native-created data in web and terminal presenters.
- **iOS simulator (iOS 26.5, Xcode on macOS).** The CLI builds and launches
  the notes app. Driven through the eval hook in the real `WKWebView`: the
  transport is installed and finds the `gofastr` handler, client navigation
  to the create form keeps window state, a note save goes over the bridge and
  the list shows it, and a native multipart form posts over the bridge and
  its answer becomes the next document.
- **Chromium phone sim, both modes.** Handwritten mobile screens, client-side
  navigation and saves (`POST /notes/save`), editing, validation, session-draft
  restoration, search, light/dark, and 200% text at 320px. A native multipart
  form's `303` is followed in Go. In Android mode every intercepted body is
  stripped; saves and forms still work through the bridge. This is not an
  Android-device or screen-reader test.
- **Android shell compiles.** `MainActivity.java` compiles against gobind's
  generated bindings and the stubs.
- **Single-user identity.** Rows carry a per-install owner id that survives a
  restart on the same data directory.
- **Streaming.** `Host.Open` returns headers before the body ends and hands
  each flushed chunk over as it is written (Go test).

## Not verified yet

- Android beyond one Android 14 x86-64 emulator: ARM64 devices, other API
  levels, TalkBack, and physical camera and microphone. The emulator runs
  that did happen are in [Android builds and acceptance](docs/android.md).
- A physical iPhone. `--platform ios-device` builds a signed app (see
  [release builds](docs/release.md)), but install and launch on a registered
  iPhone has not been run.
- On device: whether `gofastr://app` is a secure context (Web Crypto,
  service workers) and whether `localStorage` persists.

## Known gaps

- **The desktop contract does not fit a phone.** `desktop.Shell` assumes a
  process-owned event loop, windows, a tray and menus. What mobile wants is
  the OS-neutral half (the capability registry, grants, the bridge chokepoint,
  app state), and that lives inside `battery/desktop`.
- **Two internal helpers are copied.** `cmd/gofastr-native` copies
  `internal/fileperm.WriteOwnerOnly`, and `sim/` spells out the Chrome start
  timeouts that `internal/chromedptest` holds.
- **The adaptive theme is opt-in.** The external Notes presentation selects
  `mobileui.Theme(platform)`; a scaffold must also choose its presentation.
- **The framework's 404 page has no styles.** On the phone it renders as bare
  HTML.
- **Listener-free lifecycle is not extracted.** UIKit no longer mounts web
  screens, but its complete lifecycle still runs through in-memory Serve. See
  [Gate A versus Gate B](docs/mobile-lifecycle.md).
- **Some async acceptance gaps remain.** App-tagged browser history and explicit
  app departure now await Go acknowledgement. Untagged history fails closed;
  immediate termination can still outrun checkpoints. Keyed list updates,
  real-device lifecycle/IME/accessibility runs and crash-safe mutation
  reconciliation remain work.

## Next steps

Follow the [milestones and acceptance gates](docs/mobile-roadmap.md#build-order-and-acceptance-gates):

1. Finish the remaining M2 Back/IME/accessibility gates over the shared editor/runtime.
2. Propose a complete public listener-free lifecycle seam; do not copy framework startup.
3. Run the Android gates [Android builds and acceptance](docs/android.md) leaves open; preserve WebView support.
4. Expand task-shaped components/capabilities, then extensions, scaffolding, device
   signing, and measured optimization. Re-pin to framework releases as required
   public seams become available.

The [component slice](docs/mobile-components.md) implements the first controls,
collection, tab, and swipe contracts; uncapped repository paging, richer sheets,
and complete device/accessibility acceptance remain roadmap work.
