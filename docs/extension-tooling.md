# Extension authoring tools

The CLI inspects and scaffolds **compiled library packages**, not application
templates or remotely loaded plugins. See [compiled acquisition adapters](compiled-extensions.md)
and [Go-authored rendering extensions](rendering-extensions.md) for the runtime
contracts, and [physical-device acceptance](ios-device-acceptance.md) for the
separate hardware gate. No production dependencies are added by these commands.

## Inspect a compiled import graph

From the consuming Go module:

```sh
go run ./cmd/gofastr-native extension check --pkg ./examples/notes/notesnative
go run ./cmd/gofastr-native extension check --pkg ./examples/notes/notesnative --dry-run
```

An installed CLI can use `gofastr-native extension check --pkg ./my/bind/package`.
Check targets the same `GOOS=ios GOARCH=arm64 CGO_ENABLED=1 go list -deps -json`
graph as iOS packaging. It does **not** execute package initialization or parse
arbitrary Swift/JavaScript source. Missing Go modules/toolchains must be resolved
through the module's normal dependency workflow; check does not install adapters.

Output is sorted by manifest identity, with kind, manifest path, static Swift
factory, sorted capability keys, ordered Swift source paths and WebView script.
The current Notes graph has:

| Identity | Effective kind | Swift factory | Assets |
| --- | --- | --- | --- |
| `gofastr.media/v1` | `component` | `GofastrMediaComponent` | `GofastrMedia.swift`, `media.js` |
| `gofastr.photos/v1` | `file` | `GofastrPhotoExtension` | `GofastrPhotos.swift`, `GofastrPhotoController.swift`, `photos.js` |

Omitted manifest `kind` preserves existing file acquisition. Explicit `file`
behaves identically. `component` requires exactly one capability equal to its
versioned `name`; its Go definition uses `ValidateComponent`, not file
`Validate`/`Accept`. Both factory kinds expose static `name` and `capabilities()`.

The check rejects unknown/invalid manifest fields, duplicate identities, duplicate
capability owners and duplicate Swift factory names **across both kinds**, with
the conflicting identity/path and an actionable fix. It checks all declared
assets, including JavaScript. Bounds stay aligned with the core schema: 16
compiled libraries, 8 capabilities per file manifest, 16 Swift files per manifest,
8 KiB manifest and 512 KiB per nonempty regular asset. Assets must remain
package-relative without traversal or symlinked asset/ancestor paths. Regular-file
checks happen before opening assets, so directories and other nonregular assets
do not become adapter source.

The sole generated Swift catalog installs the kinds separately:

```swift
MobileFileExtensions.install([GofastrPhotoExtension.self])
MobileComponentExtensions.install([GofastrMediaComponent.self])
```

It checks factory identity/capability keys against the manifests, copies ordinary
library-owned Swift verbatim and preserves the existing host-template behavior.
Core-only catalogs install empty registries; no photo/media implementation is
silently added. A check PASS is **not** evidence that callbacks exist, Swift/JS
compiles, the app selects the definition, a capability is available, or hardware
permission flows work. Build/test the consuming application too.

## Scaffold a working library

Choose a **new** directory whose parent already exists inside an existing Go
module. The module must already require a matching gofastr-native version.

```sh
gofastr-native extension init --kind file --name example.documents/v1 \
  --out extensions/documents --dry-run
gofastr-native extension init --kind file --name example.documents/v1 \
  --out extensions/documents
gofastr-native extension init --kind component --name example.text/v1 \
  --out extensions/text
```

Each package contains `adapter.go`, `adapter_test.go`, an embedded
`gofastr-native.extension.json`, ordinary `Adapter.swift`/`adapter.js`, and a
focused README showing the **actual** per-app registration API. There is no app,
screen, host, Xcode project, module file or dependency installation.

- **File:** `Acquire(files.Pick)` constructs a bounded file operation;
  `Definition()` supplies authoritative `Validate` and `Accept` callbacks.
  UIKit delegates to the existing cancellable document foreground task. WebView
  supplies a browser chooser, bounded normalization and cancellation, reporting
  `fallback` honestly. This is not a camera or image-normalization scaffold.
- **Component:** `Block(id, Content{Text: ...})` constructs a stable keyed,
  read-only text snapshot. `Definition()` supplies `ValidateComponent`. UIKit
  implements the actual `MobileComponentExtensionFactory.make(payload:compact:emit:)`
  protocol and returns `MobileComponentExtensionView` (`view`,
  `update(payload:emit:)`, `dispose()`).
  WebView implements `mount(container, component, host)` returning `update(next)`
  and idempotent `dispose()`. Text is literal; no HTML execution or write channel.

Both call `extension/webassets.Register(Definition())` at **package init** to
publish static asset metadata before GoFastr seals its behavior manifest.
Import-time publication is not per-app enablement. Each consuming app explicitly
selects definitions with `nativeui.NewWithExtensions(root, definitions, routes...)`.
Do not move publication into a later app constructor, auto-register every imported
library for every app, or bypass the existing authorization/navigation runtime.

Names must match the core's bounded lowercase `namespace/vN` syntax. Names are
data literals, never directory fragments or executable source substitutions.
The Go package and filenames are fixed; factory names use a deterministic digest
of the validated identity, avoiding arbitrary type-name interpolation. Existing
directories/files/symlinks are refused even if empty. Files use exclusive creation;
a write failure rolls back files created by this attempt, leaving unrelated
concurrent files alone and reporting cleanup failures. Dry runs execute no
external command and create no directories/files. They may read destination/module
metadata to reject an unsafe or unusable plan.

## Verification and durable notes

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./cmd/gofastr-native -count=1
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./cmd/gofastr-native
go test ./extensions/documents ./extensions/text
gofastr-native extension check --pkg ./my/consuming/bind/package
```

CLI regressions exercise command boundaries, deterministic collision reporting,
asset bounds/symlinks, no-write dry runs, exclusive creation and rollback. Generated
libraries run their real Go policy/static-publication tests outside the checkout;
ordinary JavaScript adapters exercise acquisition/cancel/update/dispose in Node.
On macOS with Xcode, generated file and component Swift adapters plus the real
bundled renderer/catalog are typechecked for physical `arm64-apple-ios16.0`.
That is compilation evidence only: no full bound device app, signing, installation
or hardware acceptance is implied. Parent/application workflows own full builds,
renderer integration and simulator/browser acceptance.

### November 5, 2026 — static publication and factory split

- Scope: extension CLI/scaffold.
- Trigger: imported packages can contain acquisition and rendering adapters.
- Approach: discover statically, split the generated registries, and retain
  `webassets.Register` at package init plus explicit per-app definition selection.
- Evidence: CLI catalog/scaffold tests cover both kinds and the empty catalog;
  generated Swift is checked against the actual generic component protocol.
- Next time: inspect the current renderer protocol before changing scaffolds;
  never infer hardware availability or app registration from static discovery.
- Status: active.
