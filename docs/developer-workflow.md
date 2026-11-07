# Developer workflow

GoFastr Native now provides an app generator, toolchain diagnostics, an edit
loop, a component gallery, inspection and tracing, and release packaging.
The framework remains experimental. Build success does not establish device
or store readiness.

## Commands

Run these from this repository. Generated apps pin the CLI as a Go tool and
use `go tool gofastr-native` instead of `go run ./cmd/gofastr-native`.

| Task | Command or guide |
| --- | --- |
| Check Android prerequisites | `go run ./cmd/gofastr-native doctor --platform android` |
| Create an app in a new directory | `go run ./cmd/gofastr-native init --out ../hello` |
| Watch Go changes without a device | `go run ./cmd/gofastr-native dev --platform go --renderer native --pkg ./examples/starter/starter --test` |
| Rebuild and restart on Android | [Getting started](getting-started.md#check-the-toolchain) |
| Try controls in Chromium | `go run ./examples/gallery/cmd/gallery-sim --platform android` |
| Inspect synthetic renderer support | `go run ./cmd/gofastr-native inspect --pkg ./examples/gallery/gallerynative --profile all` |
| Enable native runtime traces | [Debugging](debugging.md#turning-it-on-in-an-app-build) |
| Sign and package releases | [Release builds](release.md) |

`init` refuses an existing output directory and rolls back incomplete file
writes. Until a published GoFastr Native version is available, its working
dependency source is a local checkout through `replace`. See
[getting started](getting-started.md) for module setup and recovery when
dependency resolution fails.

`doctor` reports missing prerequisites and prints fixes without installing
them or changing the app's module manifests. Listing Android devices is
opt-in through `--devices` because it starts the adb server.

`dev` polls local packages and embedded assets, combines rapid saves into
one rebuild, and leaves the installed app alone after a compile failure.
It rebuilds and restarts rather than hot reloading. Saved data persists;
background checkpointing is best effort, so edits newer than the last
completed checkpoint can be lost. Navigation is not preserved reliably.
Uncertain installs are reported without retrying, uninstalling or clearing data.

The [gallery](component-gallery.md) demonstrates supported screen contracts.
The browser version uses Chromium with intercepted requests and no app HTTP
listener. It does not establish native UIKit or Android behavior.

`inspect` executes app code against temporary data and is not a sandbox. Its
reports suppress app content and use opaque screen IDs. Registered route
patterns require an opt-in `InspectApp` export. Tracing is disabled by default;
apps must supply an observer hook for `--debug-trace` to produce events.

## Validation record

Checked on Windows on November 6, 2026, using Go 1.27.3, JDK 17 and Android
SDK 36. Hardware and macOS checks below remain separate.

| Check | Result |
| --- | --- |
| Full Go suite, `go test -p 2 -count=1 ./...` | PASS, 34 packages with tests |
| Final CLI package after integration guards | PASS |
| `go vet ./...` | PASS |
| WebView JavaScript tests | PASS, 144 tests |
| Trace-enabled runtime, gallery and Notes tests | PASS |
| Inspection content redaction, app-output suppression and module immutability | PASS |
| Fresh app creation, normal dependency resolution, tests, vet and pinned CLI | PASS |
| Generated starter Android APK | PASS, x86_64 debug build |
| Gallery Android APK with tracing | PASS, x86_64 debug build |
| Android signing fixtures and artifact verification with the real SDK | PASS |
| iOS profile, entitlement and IPA logic using synthetic fixtures | PASS, no Mac execution |

The first full run exposed two test synchronization issues. Startup timeout
can occur before a worker starts, and Chromium can replace a JavaScript
context during navigation. Tests now check actual worker startup and joined
shutdown, and retry only the read-only readiness observation after that
specific context replacement. Focused checks passed 30 and 3 repeated runs
respectively, followed by a passing full suite.

Local evidence is in `dist/dx-acceptance/report.json`, with logs and APK hashes.
`dist/` is ignored; those generated artifacts are not part of the repository.
Reproduce the automated checks with:

```sh
go test -p 2 -count=1 ./...
go vet ./...
node --test render/webview/*.test.cjs
go test -tags gofastr_debugtrace ./nativeui/... ./examples/gallery/... ./examples/notes/notesnative
```

## Remaining acceptance gates

- Android install and launch of the new starter, gallery and release builds;
  dev-loop lifecycle and draft recovery on an emulator; ARM64 phone launch.
- Full AAB build and validation with an owner-supplied bundletool, then Play
  delivery checks. The CLI does not download bundletool or publish builds.
- macOS iOS simulator and device builds, real provisioning and signing,
  devicectl installation, and IPA runtime acceptance.
- iPhone share extension, widget and private Keychain isolation acceptance.
- Race-detector runs. The Windows validation used `CGO_ENABLED=0`.

The iOS packaging path supports development and ad hoc profiles. App Store
and enterprise profiles are refused. See [release builds](release.md) and
[iOS device acceptance](ios-device-acceptance.md) before distributing a build.
