# External presentation prototype

This is an implementation experiment, not a new cross-platform rendering API.
GoFastr remains web-first. The mobile presentation lives **outside GoFastr** in
the importable `github.com/DonaldMurillo/gofastr-mobile/mobileui` package. No
changes to either GoFastr checkout are needed, and no dependencies were added.
An additional [Go-defined native presenter](native-presentation.md) exercises
the boundary with real UIKit controls. Apps compose mobile screens/actions in
Go; the library owns the platform renderer, not application Swift templates.

The newer [shared mobile screen adapter](shared-mobile-screens.md) also renders
`notesscreens` through WebView. The `notesweb` presentation described here remains
an independent compatibility example, not the UI used by `notesmobile.Start`.

## The boundary being tested

| Package | Owns | Does not own |
| --- | --- | --- |
| `examples/notes/notes` | Typed `Note`, `Repository`, shared validation and commands | HTML, HTTP, widgets, routes, navigation, platform detection |
| `examples/notes/notesstore` | Temporary adapter to GoFastr's scoped CRUD pipeline | Rendering or a new repository generator |
| `mobileui` | Mobile-web markup, tokens, components, CSS, browser behavior | Notes data, business validation, native OS integration |
| `examples/notes/notesweb` | Handwritten Notes screen composition and HTTP command adaptation | CSS or overrides of another design system's internals |
| `examples/notes/notesterminal` | A line-oriented terminal presentation of the same service | HTML, browser runtime, GoFastr imports |
| `examples/notes/notesnative` | Gomobile screen/action entry points, installation identity, lifecycle adaptation | Platform screen source or duplicated validation |
| `examples/notes/notesscreens` | Go mobile screen composition and actions using `nativeui` | Swift, Java, HTML, CSS, or wire JSON |
| `nativeui`, `render/ios`, `render/webview` | Shared mobile definitions, local dispatch, platform rendering and capabilities | A mandatory representation for existing web apps, canvas, desktop, or TUI |
| `examples/notes/notesapp` | Application composition, entity declaration, adapter selection | Styles or hand-rolled screen markup |
| `mobilehost` | Existing in-process lifecycle and transport | Screen presentation |

The independent web and terminal presentations share application behavior,
**not a widget tree**. UIKit and the new mobile WebView adapter additionally
share the Go mobile screen definitions; desktop/canvas/TUI need not implement
them. The terminal creates notes that the web presentation can open, and edits
made by the web presentation are visible in the terminal when they use the same
data directory. They use the same entity, validation, identity, and owner scopes.

`notesapp.Service(app)` supplies typed behavior for an initialized application.
Direct callers must provide authenticated context, just as other in-process
repository callers do. HTTP requests receive the existing host's identity.
An ownerless service call is refused; renderers do not grant themselves access.

The storage adapter is deliberately narrow. Replace it with an adapter to the
generated typed entity repository when that work is available; do not copy
repository generation or introduce a second Local/Remote abstraction here.

## Mobile-web presentation

GoFastr's public `registry.RegisterStyle`, `registry.RegisterBehavior`, themes,
components, screen registration, and `UIHost.RenderScreen` are sufficient for
this first external web presentation. Its registered CSS and JS are embedded
in the binary and served by the local host. Apps and hosts contain no CSS.
It neither overrides `framework/ui` selectors nor adds `data-cui-*` contracts.

`notesweb.New(service, platform)` creates the web host. `notesapp.BuildWithPlatform`
selects `mobileui.IOS` or `mobileui.Android` **before initial SSR**. Unsupported
modes return an error. The WebView Notes binding selects the shared-screen
adapter instead, using the same platform from its compiled OS; the legacy
`Build` helper defaults to iOS. Width is not a platform
detector. Tests intentionally use an iPhone user agent for both modes.

The presentation provides:

- iOS large heading, inset grouped rows, warm tint, inline compose action, and
  back affordance; Android typography, tonal surfaces, and a labeled floating
  primary action. Neither is an exact implementation of an OS widget toolkit.
- Package-owned adaptive light/dark palettes, native system-font stacks,
  44px iOS / 48px Android control floors, and text sizes expressed in `rem`.
  Key geometry is independent of text scaling so large text does not also
  consume the entire screen in padding.
- Full-row links, labeled search, empty/no-result states, and a document editor
  using real input/textarea elements with native selection and keyboard behavior.
- Explicit Done-to-save, truthful saving/error status, server-side validation,
  browser-native validation, and an ordinary POST/303 fallback.
- Component-owned enhancement that sends saves over the existing transport,
  invalidates Notes' cached screens, and uses GoFastr's existing client router.
  No parallel router or custom push/pop stack is installed.
- Per-note drafts in `sessionStorage`, restored when a screen is revisited.
  These are **session drafts, not durable autosave**. Storage failure is shown
  explicitly; committed note data remains in SQLite. Text changed while a save
  is in flight stays in the editor, and subsequent saves use the created ID.
- Visible focus, semantic headings/labels/lists, live save/error announcements,
  no decorative animation under reduced motion, and no gesture-only actions.

The list currently shows the newest 200 matching notes. Pagination, multi-user
draft isolation, conflict handling, and background refresh are not prototype
features. Save cache invalidation is local to this presentation; external
mutations require a refresh. Drafts are not merged with concurrent writes.

## Running it

The existing native shell build command uses the new presentation automatically:

```sh
go run ./cmd/gofastr-mobile run --platform ios-sim \
  --pkg ./examples/notes/notesmobile --id dev.gofastr.notesprototype \
  --name 'Notes Prototype'
```

The separate bundle ID keeps prototype data apart from an existing Notes app.
For Android, use `--platform android-emu` with the required SDK/NDK/JDK installed.

The terminal proof is runnable without new dependencies:

```sh
go run ./examples/notes/cmd/notes-terminal --data /tmp/gofastr-notes-terminal
```

Commands are `list [search]`, `open ID`, `new`, `edit ID`, and `quit`. New/edit
read a title followed by body lines ending with a single period. This is a
line-oriented terminal UI, not a full-screen terminal toolkit.

**Bootstrap limitation:** the command currently reuses `notesapp.Build` and the
in-memory app lifecycle, including an unused mounted web presentation. It does
not open a WebView or render HTML to the terminal, and the terminal presentation
package has no web dependencies. This proves the application/presentation
boundary, **not a fully headless GoFastr host**. `mobilehost` still requires a
`UIHost`; a host-neutral lifecycle is a candidate future upstream seam, not
something this prototype pretends to implement.

## Verification

```sh
go test ./...
GOWORK=off go test ./mobileui ./examples/notes/...
go vet ./...
SIM_SHOTS=/tmp/gofastr-mobile-prototype go test ./sim/ -run TestPhoneWebView -count=1
```

The browser acceptance flow covers both explicit presentation modes with
Android's request-body interception limits: create, edit, validation without
losing text, session-draft restoration, search/no results, native multipart
fallback, client-side navigation/save, light/dark, and a 320px viewport at 200%
text. Screenshots must be inspected, not inferred from the DOM assertions.
The dependency test guards that Notes behavior and the terminal presentation
do not import GoFastr or mobile UI code. Persisted cross-presentation tests
also exercise owner-scoped read/update refusals and shared validation errors.

Real iOS simulator validation uses the existing simulator-only eval hook in
WKWebView. Set `SIMCTL_CHILD_GOFASTR_MOBILE_EVAL` when invoking `simctl launch`;
the hook reads `GOFASTR_MOBILE_EVAL` in the child. It is not available in a
physical-device build. Screenshots under `/tmp` are local validation artifacts,
not checked-in mockups or golden images.

The real iOS simulator run opened a saved note, changed its body, and submitted
the presentation's Done action through the native message channel. The updated
note rendered with saved status and the same window marker, confirming that
navigation and save did not reload the WebView. This does not verify physical
keyboard behavior, screen readers, or Android's WebView implementation.

Local verification on November 4, 2026 used the linked framework worktree
`fd6c39e12` and separately passed the pinned-module checks with `GOWORK=off`.
The standalone `/Users/dom/programming/gofastr` checkout is not the one selected
by this repository's local `go.work`; neither checkout was modified.

## Deliberately not implemented in the mobile-web presentation

- A universal screen schema, widget tree, renderer interface, or plugin loader.
  These adapters are compiled into the application, not downloaded code.
- Native controls. A separate [UIKit presenter](native-presentation.md) now
  implements the native Notes slice; desktop/Three.js renderers remain future work.
- Native-quality push/pop animations, interactive or predictive Back,
  per-tab stacks, collapsing titles, tabs, sheets, swipe actions, or pull refresh.
- Native Dynamic Type/font-scale preference bridging, screen-reader validation,
  Android device/emulator validation, or physical-iPhone validation.
- A new edge-to-edge/IME policy. The frame consumes safe-area environment values,
  but the existing native host's adjustment policy remains in place. The core
  document still does not set `viewport-fit=cover`; adding it or adding host
  padding independently would risk double insets. Keyboard avoidance remains
  the platform's normal input scrolling, not a promised normalized geometry API.
- Remote presentation, remote scripts/fonts, or App Store approval guarantees.

## What this suggests for GoFastr

Keep the current web renderer and design system as the default. Do not add iOS
controls or terminal widgets to core. External web packages can already own
components and screens through public APIs.

The native host now demonstrates Go-owned mobile screens/actions without HTML
and composes its services without mounting a web UI. It still uses in-memory
Serve for full lifecycle ownership, not a listener-free native startup. See
[Gate A and the remaining Gate B seam](mobile-lifecycle.md). Extract only the demonstrated gaps:
host-neutral app lifecycle, destination/action identity independent of HTTP,
explicit host capabilities, and screen activation/disposal/navigation delivery.
A native renderer should mount native controls, a canvas renderer should own its
scene, and neither should implement an HTTP response or return HTML merely to
satisfy the web host. Share application services, not an obligatory widget IR.

The ownership rule remains one styling surface **per presentation package**:
apps compose it, the package owns its internals, and no package patches another
design system's private selectors. Any upstream contract change to formally
support alternate presentation hosts should document that distinction explicitly.
