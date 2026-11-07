# iPhone readiness and manual acceptance

**Hardware acceptance: NOT RUN.** Simulator/browser results, source typechecking
and mocked command tests do not satisfy this gate. Native Android remains
deferred. This checklist supplements [extension tooling](extension-tooling.md),
[compiled acquisition adapters](compiled-extensions.md) and
[rendering extensions](rendering-extensions.md).

## Observed baseline — November 5, 2026

The local read-only readiness command found selected Xcode **26.6** (17F113), the
physical iPhoneOS **26.5** SDK, and Swift. CoreDevice knows an iPhone and an iPad,
but both report `tunnelState=unavailable`, `developerModeStatus=disabled`, and
`ddiServicesAvailable=false`. Zero devices qualify as usable; a cached paired
entry is not a connected test device. No agent device control is available.

No signed device `.app` was supplied to that run, so app signature/provisioning,
purpose strings and scheme checks were **NOT CHECKED**, not passed. No install,
launch, pairing, permission prompt, notification delivery, signing, provisioning,
uninstall, data reset or user-data operation was performed. Generated library
adapters are typechecked with the real renderer for physical arm64; this is not
a full Go-bound, linked or signed physical-device product.

## Read-only readiness gate

From this repository, substitute the path to an **existing owner-authorized signed
device-test build**. `--app` never builds, replaces or signs it.

```sh
export GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL
go run ./cmd/gofastr-native doctor --platform ios-device --dry-run
go run ./cmd/gofastr-native doctor --platform ios-device \
  --app /absolute/path/to/ExistingTest.app \
  --pkg ./examples/notes/notesnative \
  --require camera,microphone,notifications,extensions \
  --url-scheme your-test-app-scheme
```

Use the existing build's exact scheme. Run without `go run ./cmd/gofastr-native`
when using the installed `gofastr-native` binary. Normal readiness exits **1**
when a required check fails, **2** for invalid options, **0** when selected checks
pass. A successful dry run exits 0 but labels every readiness/acceptance step
**NOT RUN**. It does not execute commands or create temporary reports.

The actual gate only reads:

- Selected full Xcode/version, `iphoneos` SDK and Swift tool paths.
- `xcrun devicectl list devices`'s supported JSON report, with a 15-second timeout.
  The report is a private temporary file outside the checkout, removed afterward;
  no table-text scraping or `/dev/stdout` JSON workaround is used. Default
  selection requires a physical iPhone with paired/connected CoreDevice state,
  Developer Mode enabled and DDI services available. `--device` can select an
  exact CoreDevice identifier or UDID (including an authorized iPad), never a
  fuzzy name. Unknown/unavailable states fail closed.
- Bounded existing `Info.plist`, physical `iPhoneOS` platform and an arm64
  Mach-O executable, existing `codesign --verify --deep --strict`, and a
  non-ad-hoc team signature. Existing provisioning is decoded with `security
  cms -D -i`; expiration, app/team identity and the usable device allowlist must
  match. A simulator/ad-hoc signature is not device readiness. This gate targets
  development/ad-hoc device testing with an embedded profile, not App Store or
  enterprise distribution certification.
- Declared camera/microphone purpose strings. They become mandatory only when
  listed in `--require`; the command above opts in explicitly. These strings do
  not prove actual OS authorization or a working capture session.
- An exact expected `CFBundleURLSchemes` entry when `--url-scheme` is supplied.
  `--require notifications` requires that flag. iOS notifications do not use a
  fabricated notification-purpose Info.plist key; delivery, authorization and
  reminder tap routing remain manual **NOT RUN** gates.
- Static compiled manifest/asset/collision checks when `--pkg` is supplied.
  `--require extensions` requires a nonempty valid compiled graph. This cannot
  prove that the existing `.app` was built from that graph, selects all expected
  definitions, or exposes their runtime capabilities. Verify provenance and
  explicit per-app registration separately.

Doctor never changes signing/provisioning, connects/pairs/unlocks devices,
enables Developer Mode, mounts developer images, requests/resets permission,
schedules notifications or installs/uninstalls/launches an app. Any owner setup
must happen separately on a dedicated authorized test device. Readiness is not
an installation guarantee, app version/OS compatibility certification or hardware
acceptance; it always prints the outstanding manual gates.

Useful independent **read-only** commands:

```sh
xcode-select -p
xcodebuild -version
xcrun --sdk iphoneos --show-sdk-path
xcrun devicectl list devices
codesign --verify --deep --strict /absolute/path/to/ExistingTest.app
codesign -d --verbose=4 /absolute/path/to/ExistingTest.app
plutil -p /absolute/path/to/ExistingTest.app/Info.plist
go run ./cmd/gofastr-native extension check --pkg ./examples/notes/notesnative
```

Do not use `run --platform ios-sim` as a physical-device install workflow.
`build --platform ios-device` now builds, signs and packages a device app with
the owner's identity and profiles, and `run --platform ios-device --device <id>`
installs and launches it. See [release builds](release.md#iphone---platform-ios-device).
Doctor stays read-only, and no command resets, uninstalls or pairs a device.
None of these paths has run on a Mac yet:

| Device build gate | Status |
| --- | --- |
| `build --platform ios-device` on a Mac with a real development profile | NOT RUN |
| `codesign --verify --deep --strict` and entitlement equality on the signed app | NOT RUN |
| Ad hoc `--profile release` IPA installed on a registered iPhone | NOT RUN |
| `run --platform ios-device` install, launch, and UNKNOWN-outcome handling | NOT RUN |
| Share extension and widget through the app-group container on device | NOT RUN |
| Keychain isolation: an extension cannot read the app's private item | NOT RUN |

A dry run with synthetic profiles passes in the unit tests. It shows the
planned commands and the profile checks, not a working device build.

## Repeatable owner-operated acceptance matrix

Before a run, record date, tester, dedicated device/model/OS, build revision,
bundle ID, renderer, selected extension identities, initial OS permission states,
and the exact readiness command/exit status. Use disposable test notes and an
explicitly authorized separate test bundle. Never uninstall, erase drafts,
reset permissions or replace a user's existing Notes app as test preparation.
If a required initial permission state is unavailable, mark that case BLOCKED
rather than resetting user state. Do not claim that a provider offered by the
browser is native camera acceptance.

For each case record PASS/FAIL/BLOCKED, screenshots or a tester observation,
expected/actual result, and follow-up issue. These initial statuses apply to
physical iPhone UIKit **and** physical iPhone WebView; neither has been run.

| Case | Owner-operated steps and required outcome | UIKit | WebView |
| --- | --- | --- | --- |
| Camera cancel | From a disposable editor, open capture/chooser and cancel without saving; no attachment or mutation, no stuck foreground, focus/draft preserved. Cancel the capture UI, not an imaginary cancellable OS consent prompt. | NOT RUN | NOT RUN |
| Camera deny | On an authorized test install starting undetermined, choose Don't Allow (or observe existing denial). Show honest denied/unavailable feedback; no success attachment, replay, repeated automatic prompts or lost draft. | NOT RUN | NOT RUN |
| Camera success | On an authorized allowed camera, capture one image and attach once. Confirm visible preview, restart persistence, bounded JPEG output without source EXIF/GPS, export, and no duplicate submission. WebView camera offering is provider/browser-dependent; record its actual capability/fallback. | NOT RUN | NOT RUN |
| Microphone cancel | Open recorder, cancel/discard before committing (and after recording where supported). No saved recording, dangling indicator, duplicate completion or lost editor/focus state. | NOT RUN | NOT RUN |
| Microphone deny | Observe authorized first-request denial or existing denial. Explain denied/unavailable status; do not save fake audio or request permission during readiness/capability negotiation. | NOT RUN | NOT RUN |
| Microphone success | Record/stop/attach/play actual bounded audio once; interrupt/background safely, export, restart and replay. Confirm audio-session cleanup and no duplicate attachment. | NOT RUN | NOT RUN |
| Reminder permission/cancel | User initiates reminder permission/scheduling on the dedicated test app; cancel or deny safely. Do not schedule anything from doctor. Record authorization state and unsupported paths honestly. | NOT RUN | NOT RUN |
| Reminder tap | Schedule one authorized reminder on disposable content; background then tap it, repeat from terminated app. Open the exact allowed Go route/scheme and correct note once; no unsafe destination, replay or draft loss. | NOT RUN | NOT RUN |
| Share provider cancel/success | Open the actual system share/provider UI for a disposable file; cancel, then complete with an available authorized provider. Confirm accurate outcome, one-shot completion, temp-resource cleanup, no unwanted navigation and restored focus/draft. | NOT RUN | NOT RUN |
| VoiceOver | Enable through the tester's normal device workflow; traverse list/editor/media/recording/dialog controls. Verify names, order, selected state, error announcements, grouped content, focus entry/return and no inaccessible gestures. | NOT RUN | NOT RUN |
| Dynamic Type | Exercise normal through largest accessibility text sizes, both orientations and light/dark. Confirm usable actions, readable attachment names, media controls and validation with no clipped/overlapping critical UI. | NOT RUN | NOT RUN |
| Keyboard/IME | Type multiline text with software/hardware keyboards where available, marked CJK composition, emoji, selection, undo/redo, keyboard dismissal and sheet return. No duplicate/lost text, stuck inset or interrupted composition. | NOT RUN | NOT RUN |
| Restart/drafts | Create and edit disposable draft, navigate, background/foreground and relaunch normally after termination. Verify intended save/unsaved recovery, cursor/focus policy and attachment persistence; never erase user data to test recovery. | NOT RUN | NOT RUN |
| Provider interruption | Switch app/lock device or cancel during chooser/read/recording as authorized. Verify bounded cancellation, accurate unavailable/error state, no stale callbacks updating a new screen and safe retry without mutation replay. | NOT RUN | NOT RUN |

**Release acceptance remains BLOCKED** until a usable authorized physical iPhone,
an appropriate existing signed/provisioned build, and recorded manual results
exist. Automated fixture successes must never replace the NOT RUN statuses.

## Maintainer notes — November 5, 2026

- Scope: physical iOS readiness.
- Trigger: CoreDevice lists cached devices that are not currently usable.
- Approach: require connected/paired/enabled/DDI state and an allowlisted signed
  device-test build; keep hardware behavior explicitly separate from readiness.
- Evidence: the local doctor exits 1 with zero usable devices and no supplied app;
  command-boundary tests reject ad-hoc signatures and unavailable devices.
- Next time: rerun the read-only gate before planning physical work, retain
  NOT RUN/BLOCKED rows until actually observed, and leave user data untouched.
- Status: active.
