# One Go mobile screen set, two renderers

Application screens and actions live in Go. `nativeui` is the experimental
mobile vocabulary; its name does not mean it requires UIKit. It is not a schema
that GoFastr web applications, desktop, canvas, or TUI modules must implement.

Notes is the single example app, with `examples/notes/notesscreens` shared by
two thin renderer launch adapters:

| Entry point | Renderer | Application authoring |
| --- | --- | --- |
| `examples/notes/notesnative` | Actual UIKit controls and navigation | `notesscreens.New(service)` |
| `examples/notes/notesmobile` | Mobile HTML controls in WebView | The same `notesscreens.New(service)` |
| `notesapp.BuildWithPlatform` | Existing handwritten mobile-web presentation | `notesweb` Go screens, preserved for compatibility |

The ordinary WebView host contract is unchanged. Existing GoFastr web apps do
not have to migrate to mobile definitions. Renderer selection is a build/boot
choice; mixed native/web screen stacks and automatic fallbacks are not added.
GoFastr core and both framework checkouts are unchanged.

## Authoring and mounting

Compose a registry with `nativeui.New(root, routes...)`. A route has a Go screen
function and named Go action handlers. Screens currently support lists, forms,
or plain text; actions can request navigation, share text, haptics, an
announcement, and draft cleanup. The complete runnable example is
`examples/notes/notesscreens/screens.go`.

Actions also return typed Go presentations with Go continuations. Notes'
**Behaviors** screen demonstrates every implemented presentation/selection family
alongside the editor. See [presentation behaviors](presentation-behaviors.md).

For WebView, mount the library adapter once during application setup:

```go
screens, err := notesscreens.New(service)
if err != nil {
    return err
}
renderer, err := webview.New(screens, mobileui.IOS)
if err != nil {
    return err
}
app.Router().Post(webview.ActionPath, renderer.Actions())
app.Mount(renderer.UIHost())
```

`webview.ActionPath` is `/_mobile/actions`. `UIHost()` is a normal GoFastr web
host; `Actions()` is an `http.Handler`, not a second router. The application
still supplies its usual authentication and CSRF middleware. The mobile host
supplies installation identity centrally, never through submitted form fields.
`notesapp.BuildScreensWithPlatform` demonstrates the complete composition.

`/` renders the registered root definition. Registered routes and literal-first
`{parameter}` routes use GoFastr SSR and client-side navigation. The library
owns every HTML, CSS, and JavaScript asset. Apps and hosts add no bespoke CSS,
HTML screens, or platform screen source.

## Typed API and errors

`nativeui.App.Render(ctx, destination, values)` returns a validated `Screen`.
`Dispatch(ctx, destination, action, values)` invokes only registered actions and
returns a `Response`. `Routes()` returns copied public route/action metadata;
`Resolve(destination)` validates and identifies a registered local destination.
None of these typed Go calls serialize through JSON.

`RenderJSON` and `DispatchJSON` preserve the existing version-1 local gomobile
envelope. JSON is a binding/transport detail, not application screen authoring
or a remotely downloaded presentation format. Registry metadata exposes no
executable handlers. Routes, field counts, request sizes, and action names are
validated at the boundary; remote destinations are rejected.

Expected service failures can be wrapped in `nativeui.ActionError`:

```go
return nativeui.Result{}, &nativeui.ActionError{
    Code: "validation", Message: "Give your note a title.", Err: cause,
}
```

Use safe plain text in `Message` and optional per-field `Fields`. The adapter
maps `validation`, `not_found`, `forbidden`, and `conflict` to HTTP 422, 404,
403, and 409. Unexpected errors receive a generic 500 message. `Err` is never
serialized, and `Unwrap` preserves `errors.Is` / `errors.As`. Notes classifies
its service errors at the screen boundary, not inside its domain service.

## Honest behavior differences

| Behavior | UIKit | Shared-definition WebView |
| --- | --- | --- |
| Navigation | One native navigation/presentation stack | Existing GoFastr router/history with small entry annotations |
| Sheet | Native medium/large presentation | Full page, explicitly announced as a fallback |
| Back/dismiss | Native stack and dirty-editor prompt | History with root fallback and GoFastr's leave guard |
| Input | Native text controls, local IME/caret/focus | Semantic HTML controls, local editing buffer |
| Search/refresh | Native search and pull refresh | Search form and visible Refresh action |
| Draft storage | Shared Go file store in Notes; legacy backend retains UserDefaults | Same Go file store in Notes; session storage is an immediate backup |
| Sharing | System share sheet | Browser system sharing when available, then clipboard or visible copy text |
| Haptics | UIKit feedback | Vibration when available; otherwise announces unavailability |
| Accessibility | Native traits and Dynamic Type | HTML labels, focus, live regions, text resizing; not a Dynamic Type bridge |

Fields stay local and editable during actions; duplicate action buttons disable.
Both adapters checkpoint through the shared Go editor policy. Save completion
preserves newer edits and canonical entity identity, so a second save updates
instead of duplicating a newly created note. Validation retains the buffer;
unknown outcomes block unsafe resubmission. See [the runtime contract and its
limits](mobile-runtime.md), including the remaining browser-Back checkpoint gap.

Drafts are neither encrypted storage nor repository saves. `behavior.Editor`
supplies shared save/draft/leave policy; `nativeui.Runtime` supplies instance and
request coordination, capabilities and effect receipts. Native startup no longer
mounts unused web screens, but still uses in-memory Serve for full lifecycle
ownership. Keyed collection reconciliation, Android native, deep links,
restoration, tabs and listener-free startup remain roadmap work.

## Native source, not screen templates

`render/ios/GofastrNative.swift` is ordinary reusable Swift source and contains no
application binding imports or Notes screen definitions. The CLI embeds and
copies it verbatim, then compiles it with a small generated app entry that
injects `GofastrNativeBackend` closures. The remaining
`NativeApp.swift.tmpl` is launch/binding glue only; screen rendering and behavior
do not live in a template. Apps write neither file.

## Run and verify

```sh
go run ./cmd/gofastr-native run --platform ios-sim --renderer native \
  --pkg ./examples/notes/notesnative --id dev.gofastr.notesnative \
  --name 'Native Notes' --out dist/mobile/ios-native

go run ./cmd/gofastr-native run --platform ios-sim --renderer webview \
  --pkg ./examples/notes/notesmobile --id dev.gofastr.notesshared \
  --name 'Shared Notes' --out dist/mobile/ios-shared-webview

SIM_SHOTS=/tmp/gofastr-shared-screens go test ./sim \
  -run TestSharedMobileScreensWebView -count=1 -v
go test ./...
GOWORK=off go test ./...
```

The separate example app IDs keep their device databases separate. The same
screen package and services do not imply automatic data sharing between app
sandboxes.

The browser flow uses the real in-memory host and message bridge in both iOS
and Android presentation modes, including Android's missing intercepted request
bodies. It exercises create, invalid/corrected saves, editing during a delayed
create without duplicate records, reopen, durable draft reload, unsaved sharing
fallback, keep/discard drafts, search, About,
client navigation, light/dark, and a narrow large-text editor. Read its rendered
screenshots before making visual-quality claims. Chromium phone emulation is
not Android-device or WKWebView verification. See the implementation notes for
separate real simulator evidence and remaining device/accessibility limits.

On November 4, 2026, fresh UIKit and shared-definition WebView simulator builds
were bound, compiled, installed, and launched on iPhone 17 Pro / iOS 26.5. The
extracted ordinary UIKit source compiled and showed the existing native notes.
The real WKWebView eval flow created a note, retained an invalid draft, corrected
and saved it, reopened it, searched, and opened/dismissed About. A window marker
survived throughout, demonstrating client navigation rather than document
reloads. These checks do not establish physical-device, Android-device,
VoiceOver/TalkBack, or cross-renderer accessibility parity.
