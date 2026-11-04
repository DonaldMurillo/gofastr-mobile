# gofastr-mobile

A GoFastr app running inside a phone app's process. The Go server runs
in-process and answers the WebView's requests over an in-memory listener:
no socket, no port, no loopback origin. This is early work, not a supported
feature yet.

The module builds on gofastr's public API only. Go's `internal/` rule goes by
import path, so this repository sees exactly what any other app sees. When it
needs something the framework does not expose, the fix goes into gofastr as a
public seam, not a copy here.

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
| `examples/notes/notesapp/` | A notes app: an owner-scoped entity and entityui screens, zero CSS, adaptive theme. |
| `examples/notes/notesmobile/` | The surface `gomobile bind` exports: `Start`, `Stop`, `Handle`, `Open`, `Bridge`, with strings, bytes and ints only. A scaffold would generate this file. |
| `cmd/gofastr-mobile/` | The CLI that builds the phone app and runs it on the iOS simulator or an Android emulator. No Xcode project, no Gradle. |
| `cmd/gofastr-mobile/templates/` | The generated native shells: `App.swift` + `Info.plist` (WKURLSchemeHandler on `gofastr://app`, a `gofastr` script message handler) and `MainActivity.java` + `AndroidManifest.xml` (`shouldInterceptRequest` on `https://appassets.androidplatform.net`, a WebMessageChannel). |
| `sim/` | A Chromium set up as a phone WebView (390x844 @3x, touch, phone UA). Every request is intercepted through the DevTools Fetch domain and answered by `mobilehost`; a MessagePort stands in for the native channel. |

## Run the tests

```bash
go test ./...
SIM_SHOTS=/tmp/shots go test ./sim/ -run TestPhoneWebView -v
```

`TestPhoneWebView` runs twice: once as iOS and once with Android's limits
(the interception layer drops every request body). The generated Java is
compiled against gobind's Java bindings and signature-exact Android stubs
when a working `javac` (JDK 17+) is on `PATH`; the test skips otherwise.

## Run it on a simulator or emulator

`gofastr-mobile` does for phones what `gofastr desktop build` does for the
Mac. It generates a small native shell, compiles it with the platform's
command-line tools, assembles the `.app` or `.apk` itself, then installs and
launches it. Xcode and the Android SDK must be installed; neither is opened.

Check the toolchain first. It is read-only and prints the fix for anything
missing:

```bash
go run ./cmd/gofastr-mobile doctor
```

iOS simulator (a Mac with Xcode):

```bash
go run ./cmd/gofastr-mobile run --platform ios-sim --pkg ./examples/notes/notesmobile --screenshot dist/ios.png
```

Android emulator (an Android SDK with build-tools, a platform, the NDK, the
emulator and one AVD, plus a JDK 17+):

```bash
go run ./cmd/gofastr-mobile run --platform android-emu --pkg ./examples/notes/notesmobile --screenshot dist/android.png
```

`build` instead of `run` stops after the `.app` or `.apk`. `--dry-run`
prints every step without running any. `--device` picks a simulator (name or
UDID), an adb serial or an AVD. `--id` and `--name` set the bundle id and the
display name. Output lands in `dist/mobile/<platform>/`.

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

- **iOS simulator (iOS 26.5, Xcode on macOS).** The CLI builds and launches
  the notes app. Driven through the eval hook in the real `WKWebView`: the
  transport is installed and finds the `gofastr` handler, client navigation
  to the create form keeps window state, a note save goes over the bridge and
  the list shows it, and a native multipart form posts over the bridge and
  its answer becomes the next document.
- **Chromium phone sim, both modes.** The list, client navigation, a save
  (`POST /api/notes -> 201`) over the bridge, the toast, light and dark
  themes, and a native multipart form whose `303` is followed in Go. In the
  Android mode the sim strips every intercepted body, and the save and the
  form still work, because their bodies come over the bridge.
- **Android shell compiles.** `MainActivity.java` compiles against gobind's
  generated bindings and the stubs.
- **Single-user identity.** Rows carry a per-install owner id that survives a
  restart on the same data directory.
- **Streaming.** `Host.Open` returns headers before the body ends and hands
  each flushed chunk over as it is written (Go test).

## Not verified yet

- The Android app on an emulator or device. No Android SDK is set up on the
  development machine.
- A physical iPhone. It needs a development team and a provisioning profile,
  which the CLI does not wire yet.
- On device: whether `gofastr://app` is a secure context (Web Crypto,
  service workers) and whether `localStorage` persists.

## Known gaps

- **The desktop contract does not fit a phone.** `desktop.Shell` assumes a
  process-owned event loop, windows, a tray and menus. What mobile wants is
  the OS-neutral half (the capability registry, grants, the bridge chokepoint,
  app state), and that lives inside `battery/desktop`.
- **Two internal helpers are copied.** `cmd/gofastr-mobile` copies
  `internal/fileperm.WriteOwnerOnly`, and `sim/` spells out the Chrome start
  timeouts that `internal/chromedptest` holds.
- **The adaptive theme is opt-in** (`site.WithTheme(theme.Default())`). The
  notes example sets it; a scaffold should too, because phone users expect
  OS dark mode.
- **The framework's 404 page has no styles.** On the phone it renders as bare
  HTML.
- **The entityui list's empty state is clipped at 390px**, over plain TCP as
  well.

## Next steps

1. Run the Android build on an emulator.
2. A `gofastr mobile` scaffold that generates the bind package.
3. Device signing for iOS.
4. Re-pin `go.mod` to the first gofastr release that carries the seams.
