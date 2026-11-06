# Go-authored rendering extensions

Rendering extensions let an imported Go library own a screen piece on UIKit and
mobile WebView. They also work as the entire content of a screen; core navigation,
toolbar actions, authorization and editor behavior remain shared. They are not a
new application template language, remotely loaded plugins, or universal widgets
for desktop, canvas or TUI.

## Application code

Notes registers the acquisition and rendering libraries once:

```go
screens, err := nativeui.NewWithExtensions("/notes", []extension.Definition{
    photos.Definition(),
    media.Definition(),
}, routes...)
```

A handwritten Go screen composes a typed media block:

```go
block, err := media.Block("attachments", media.Content{
    Images: previews,
    Attachments: privateFiles,
})
if err != nil {
    return nativeui.Screen{}, err
}
return nativeui.Screen{
    Title: "Note",
    Form: editorForm,
    Components: []extension.Component{block},
}, nil
```

Application code supplies no Swift, JavaScript, HTML, CSS, template, factory name,
or wire JSON. The library's `media.Block` freezes typed content into the internal
snapshot. Extension authors, not application authors, implement platform code.
`media.Content` is an alias of `nativeui.Media`; its `Validate` method checks
bounded image, attachment, audio and plain-text preview data. Local destinations
are additionally resolved and authorized by the application runtime, not by the
constructor alone.

## Ownership

```text
Go services and authorization
            |
handwritten Screen + imported typed component constructor
            |
per-app extension registry + bounded component policy + route checks
            |
revisioned screen snapshot (stable component ID, name, payload, event map)
            |
       +----+-------------------+
       |                        |
UIKit compiled factory    WebView bundled DOM adapter
       |                        |
platform views            semantic DOM + library styles
       +------------+-----------+
                    |
opaque navigation event key -> existing guarded Go screen navigation
```

The first rendering contract is deliberately read-only plus navigation. Components
do not invent a second command channel or bypass foreground coordination. Their
screen's ordinary Go actions can still perform existing typed services. Arbitrary
custom component write events and service result types are not silently implied.

## Library contract

- Set the embedded manifest's `kind` to `component`; omitted/`file` retains the
  acquisition contract. A component declares exactly its own namespaced name as
  its capability, for example `gofastr.media/v1`.
- Supply `Definition.ValidateComponent`, not file `Validate`/`Accept` callbacks.
  Validators must be bounded, concurrency-safe and free of OS/network effects.
- `Component` carries `ID`, `Name`, JSON-string `Payload` and a `Destinations`
  map. IDs and event keys are safe opaque tokens. Payloads are at most 2 MiB;
  each screen permits 16 components and at most 2 MiB of combined payload/event
  data. No asset paths or source code are in a component snapshot.
- Explicitly register the definition with `nativeui.NewWithExtensions`. Static
  import/discovery packages code; it does not grant runtime permission.
- Publish bundled WebView metadata at library package init with
  `webassets.Register(Definition())`, before GoFastr seals its host manifest.
- Compile ordinary Swift sources unchanged; the CLI generates only a factory
  catalog. File and component adapters have separate registries.

Go app rendering validates each component against its selected library and checks
every declared destination with the existing router. It automatically requires
that component's rendering capability. `fallback` or missing support cannot
silently turn a native component into a WebView or omit its content.

## Renderer lifetime

Components reconcile by ID and adapter name. A changed payload updates its retained
view rather than rebuilding a form or replacing text inputs. An unchanged payload
does not restart media playback. Replacement/removal releases resources; leaving
a screen stops active media. Input buffers, caret/IME, focus, navigation decisions
and Go session identity remain owned by the established renderer/runtime.

WebView descriptors register in `window.__gofastr_component_extension_definitions`
and implement `mount(container, component, {open})`, returning `update(component)`
and `dispose()`. The adapter passes a declared key to `open`, never a raw URL.
UIKit follows the same keyed-event rule through its compiled component registry.
The ordinary route guard still checkpoints dirty content before leaving.

The media adapter owns photos, attachment cards, audio and text/file detail
composition. The generic core has no `gofastr.media/v1` switch. Notes uses this
component rather than the legacy `Images`, `Attachments`, and `FilePreview` screen
slots. Those slots remain supported for existing applications.

## Verification and honest limits

Use `go test ./extension/... ./nativeui ./extensions/media` for schema/policy
and JSON command-boundary checks; `go test ./examples/notes/...` exercises actual
Notes persistence, owner scope and attachment navigation through the component
wire. Browser acceptance lives in the existing `sim` photo/file/audio workflows,
including dirty text and downloaded-byte checks. Renderer-specific tests cover
mount/update/dispose and compiled UIKit adapters.

`TestNotesVisualHierarchyWebView` captures both platform styles in light/dark,
including large text and attachment details. Photo-detail capture awaits decode
and compositor frames, then checks the fixture's painted pixels; a nonzero
`naturalWidth` alone is not visual acceptance.

### Local validation — November 5, 2026

- `go test ./...` and `go vet ./...` pass, including executable Swift lifecycle
  regressions, core-only/media-enabled optimized UIKit checks, and Chromium
  photo/file/device-service acceptance. All 143 WebView Node regressions pass.
- Race checks pass for extension/nativeui/media/Notes and the WebView renderer.
  Those Go application packages also pass against the public GoFastr pin with
  `GOWORK=off`; this is not a claim of physical-device testing.
- A fresh gomobile/framework/optimized UIKit product build of
  `dev.gofastr.designqa` installs over the existing iPhone 17 Pro simulator app
  and launches with both compiled catalogs. A startup screenshot confirms the
  retained Notes collection; no simulator data was erased.
- Interactive native workflow verification remains **NOT RUN** for this slice:
  computer-use failed with `Sky Computer Use native pipe startup failed`.
  The launch capture does not prove attachment navigation, audio, IME or provider
  interactions. Physical-iPhone readiness remains **BLOCKED**: no usable device
  or signed physical build was available.

Native Android remains deferred. Source-level factories are trusted compiled
library code, not a sandbox or a stable binary ABI. Browser-style Chromium checks
do not prove WKWebView or native Android behavior; UIKit compilation/launch does
not prove real camera, microphone, provider, VoiceOver or IME acceptance. See the
[device acceptance gate](ios-device-acceptance.md) and
[authoring tools](extension-tooling.md) for those separate checks.
