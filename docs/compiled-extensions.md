# Compiled extensions: photo acquisition pilot

Applications still author Go screens, actions, and typed services. Extension
**library authors**, not application authors, own platform adapter source. Nothing
is downloaded as executable presentation code, and GoFastr core is unchanged.

This first seam extends **bounded file acquisition**. It is not yet arbitrary
screen/component registration, arbitrary service results, desktop/TUI rendering,
a plugin marketplace, or a stable binary ABI. It proves package ownership and
registration before broadening those contracts.

## Application API

The single Notes showcase registers its extension when constructing its shared
screen registry, used by both UIKit and mobile WebView:

```go
import (
    "github.com/DonaldMurillo/gofastr-native/extension"
    "github.com/DonaldMurillo/gofastr-native/extensions/photos"
    "github.com/DonaldMurillo/gofastr-native/nativeui"
)

screens, err := nativeui.NewWithExtensions(
    "/notes", []extension.Definition{photos.Definition()}, routes...,
)
```

An action calls the library's typed Go API, not Swift, JavaScript, or a template:

```go
operation, err := photos.Acquire(photos.Request{
    Title: "Choose photo", Source: photos.Library,
    MaxBytes: files.MaxBytes, MaxDimension: photos.MaxDimension,
})
if err != nil {
    return nativeui.Result{}, err
}
return nativeui.Result{Files: &nativeui.FileRequest{
    ID: "attach", Commit: true, Extension: operation,
}}, nil
```

The route registers its existing `Files["attach"]` Go continuation, which stores
the validated document. Picker cancellation, denial, failure, and unsupported
outcomes do not invoke it. `Commit` identifies a mutating continuation, not a
permission grant or proof that a provider succeeded.

## Ownership and lifecycle

```text
Go screen/action -> registered typed operation
                 -> existing foreground/file task
                 -> selected compiled renderer adapter
                 -> bounded document + observed outcome
                 -> Go extension validation + source reauthorization
                 -> existing Files continuation -> complete
```

`extension.Registry` is per app, not a mutable process-wide plugin registry.
Definitions and asset bytes are copied at construction. Names, capability
ownership, manifest/embedded agreement, asset sizes, and duplicate registration
are checked. Request validators return only declared capabilities; completed
documents must pass both the base file policy and the extension validator.
Keep validators bounded and free of network/OS work; they may run while the
runtime owns a session lock. Device work belongs in the renderer task.

`FileOperation` uses a namespaced version (`gofastr.photos/v1`) and at most 8 KiB
of JSON payload, transported as a string for simple Swift/Go/JS decoding. Typed
library helpers hide that wire detail from applications. `extension.Decode`
rejects unknown fields and trailing JSON. A new extension doesn't add another
case to `FileRequest`, the runtime dispatch union, or the foreground coordinator.

The existing file pipeline remains the sole owner of opaque file identities,
one foreground operation, current-action reauthorization, draft/revision handling,
conditional commit serialization, outcome fingerprints, and completion recovery.
No accepted device work or uncertain mutation is automatically replayed. This
does not promise exactly-once side effects across process crashes.

## Extension package structure

The photo pilot lives in `extensions/photos/`:

```text
photos.go                         typed Go API + Definition + validators
gofastr-native.extension.json      compiled package manifest
GofastrPhotos.swift                bounded image conversion / cancellation
GofastrPhotoController.swift       UIKit picker + factory
photos.js                         WebView chooser adapter / normalization
```

A library may live in another Go module. Its Go package embeds the manifest and
all declared assets and returns an `extension.Definition`. Applications import
and explicitly register that definition. Importing a package is not runtime
authorization. Match registration to the adapters included by the dependency
graph; an advertised unregistered custom capability fails negotiation.

### iOS packaging

The CLI runs `go list -deps -json` for the iOS target and discovers manifests in
the compiled dependency graph. It validates package-relative paths, rejects
symlinks/traversal/duplicate factories and capability owners, and bounds reads.
Swift files are copied verbatim and compiled in the host module. Only a tiny
factory catalog is generated; the existing binding/bootstrap calls its installer.
No screen logic is generated into an app-owned template. Dry runs do not read
unavailable assets or write generated files.

Factories implement the source-level `MobileFileExtensionFactory` contract in
`render/ios/GofastrExtensions.swift`: identity, availability facts, request
validation, and task creation. They return `MobileForegroundTask` using the
supplied focus owner, worker queue, and one-shot file result callback. Availability
queries run on main and must never request permission. `show` and UIKit work stay
on main; decoding/I/O stays on the provided background queue. `finish` cancels
outstanding work and restores focus only as directed by the coordinator.

The generated catalog checks identity and declared capability keys. Adapter
source is trusted compiled code, not sandboxed third-party code. Source-level
interfaces can still change with the prototype; pin matching library versions.

### Mobile WebView packaging

Library packages call `extension/webassets.Register(Definition())` in package
`init`, before GoFastr seals its behavior manifest. This publishes compiled
asset metadata, not runtime permission or an active UI host. The renderer resolves
those registrations and loads only the app's selected definitions.
Content-addressed, same-origin modules load before hello. No remote URL, eval,
application-authored JS, or new router is used. Missing asset registration returns
a renderer setup error; attempting late registration remains GoFastr's startup
error rather than mutating an already sealed host manifest.
An adapter registers exactly one entry in
`window.__gofastr_file_extension_definitions` with its manifest name,
`capabilities()`, and `prepare(typedPayload)`. Registration and capability keys
are checked against the manifest.

`prepare` validates the request and returns bounded chooser presentation metadata:
`kind`, `title`, `hint`, `label`, input `attributes` (`accept`/`capture`),
`supported()`, `job()`, `normalize(file, job)`, and `progress`. Invalid requests
return null. A job must expose `cancel()`; normalization must honor cancellation
and release temporary resources. The adapter never submits Go commands, owns
navigation, or closes/restores the foreground itself. The core dialog handles
those responsibilities and the existing `file-result -> complete` settlement.

## Photo support and compatibility

| Target | Observed behavior |
| --- | --- |
| UIKit photo library | PHPicker selection, background normalization, private note attachment |
| UIKit camera | Permission-gated capture when hardware and purpose text exist; unavailable in simulator |
| Mobile WebView | Explicit file chooser; browser may offer camera, never guarantees it |
| Native Android | Deferred; no native support claimed |

Output remains one real JPEG, maximum 1 MiB and 2048 pixels per dimension, without
source EXIF/GPS metadata. Browser source decoding supports bounded JPEG/PNG
inputs up to 20 MiB; provider/decoder allocations remain platform limitations.
No paths, asset identifiers, or source metadata cross into application data.

`FileRequest.Photo` and `device/photos` retain their Go policy/wire compatibility
for existing callers. Renderer implementations have moved out of core; register
the photo extension explicitly to provide that legacy operation. The legacy
photo DTO/alias remains a compatibility shim, not a model for new extensions.
Ordinary file pick/export and existing service requests are unchanged.

## Verification

- `go test ./extension ./nativeui`: registration/schema/immutability and an
  independently named document operation through the actual JSON command API,
  including invalid results, completion recovery, unknown outcomes, and no replay.
- `node --test render/webview/*.test.cjs`: existing adapter regressions plus an
  independently registered non-photo acquisition through the shared dialog.
- `go test ./sim -run TestNotesPhotosWebView -count=1`: real Chromium chooser,
  normalization, persistence, inline preview, export, cancellation and errors in
  both iOS/Android **WebView styles**, not Android native execution.
- `go test ./cmd/gofastr-native ./render/ios`: real import-graph discovery,
  unchanged source packaging, Foundation image checks, core-only and
  photo-enabled UIKit compilation. The Notes simulator app also builds/links.

Physical camera/provider permission behavior, VoiceOver/IME acceptance, and
device performance remain separate gates. Compilation and simulator launch are
not evidence of physical camera capture. Keep Android native deferred.
