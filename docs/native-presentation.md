# Go-defined native presentation

Application authors write **GoFastr-style Go screens using gofastr-native definitions**.
They do not write Swift, Java, HTML, CSS, or wire JSON. GoFastr remains web-first;
this experiment is an external mobile presentation module, not a new core widget
system. There are no GoFastr source changes or new production dependencies.

The original handwritten UIKit proof is preserved at checkpoint **82e936f**.
Its application-owned Swift file has been removed from the current design.

The same Go mobile definitions now also render through the generic
[WebView adapter](shared-mobile-screens.md). This document describes the UIKit
path; shared headless behaviors and stronger async guarantees remain in the
[mobile presentation roadmap](mobile-roadmap.md).

## Ownership and structure

| Layer | Owns |
| --- | --- |
| `nativeui/` | Go screen, list, form, field, action, draft, and navigation definitions; local route/action dispatch; versioned binding envelope |
| `examples/notes/notesscreens/screens.go` | Notes list, create/edit, information sheet, labels, destinations, and Go action handlers |
| `examples/notes/notesnative/` | Installation identity, lifecycle, and thin gomobile entry points |
| `examples/notes/notes`, `notesstore/` | Shared typed behavior, validation, repository access, and owner scopes |
| `render/ios/GofastrNative.swift` | Ordinary, reusable UIKit renderer source: navigation, input state, local draft recovery, and native capabilities |
| `cmd/gofastr-native/templates/ios/NativeApp.swift.tmpl` | Minimal generated application entry and gomobile binding adaptation |
| `cmd/gofastr-native/` | Selects, binds, compiles, packages, and launches the renderer |

The renderer is maintained as ordinary Swift source, with no template expansion
or application binding imports. The CLI copies it unchanged and compiles it
alongside a tiny generated application entry. That entry injects the
`GofastrNativeBackend` closures for `Start`, `Stop`, `Root`, `Render`, and
`Dispatch`, plus the versioned `Command` service. Its template is build glue,
not screen composition or behavior.
Application authors never copy or customize either file; all supported screen
definitions use the same renderer.

`mobileui` remains the separate mobile-web presentation package. Desktop, canvas,
and TUI modules need not interpret this mobile schema or implement HTML APIs.
They can share application services and choose their own presentation definitions.

## Run

On a Mac with Xcode:

```sh
go run ./cmd/gofastr-native run --platform ios-sim \
  --renderer native --pkg ./examples/notes/notesnative \
  --id dev.gofastr.notesnative --name 'Native Notes' \
  --out dist/mobile/ios-native
```

No app-owned host template is required. The default renderer remains `webview`;
`--renderer native` selects the bundled UIKit implementation. `build` packages
without launching; `--dry-run` writes no output. Physical-device signing is not
wired. Native Android is **not implemented**: the CLI rejects that combination
rather than silently rendering a WebView or claiming native parity.

The experimental `--host-template` escape hatch remains for compatibility with
the checkpoint. It is not the screen-authoring model and cannot be combined with
`--renderer native`. It replaces only the host source, leaves the generated
plist/manifest unchanged, and requires trusted source matching its binding API.
The caller must supply a regular file no larger than 1 MiB. Nothing is downloaded
or interpreted as remote executable presentation code.

## Screen authoring

`notesscreens.New(service)` registers routes much like a web presentation:

```go
nativeui.New("/notes",
    nativeui.Route{
        Path: "/notes/new",
        Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
            return nativeui.Screen{
                Title: "New note",
                Actions: []nativeui.Action{{ID: "save", Label: "Done", Primary: true, Commit: true}},
                Form: &nativeui.Form{Fields: []nativeui.Field{
                    {Key: "title", Label: "Title", Kind: "text"},
                    {Key: "body", Label: "Note", Kind: "multiline"},
                }},
            }, nil
        },
        Actions: map[string]nativeui.Handle{
            "save": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
                note, err := service.Save(ctx, "", request.Values["title"], request.Values["body"])
                if err != nil {
                    return nativeui.Result{}, err
                }
                return nativeui.Result{
                    Navigation: &nativeui.Navigation{Kind: "pop"}, ClearDraft: true,
                    Saved: &nativeui.SavedState{
                        Route: "/notes/" + url.PathEscape(note.ID),
                        Values: map[string]string{"title": note.Title, "body": note.Body},
                    },
                }, nil
            },
        },
    },
    // The root list and existing-note routes are registered alongside this one.
)
```

This abbreviated sketch omits the root route; the complete runnable composition
is `examples/notes/notesscreens/screens.go`. That file also supplies draft policy,
search, refresh, sharing, and the information sheet. Icons use semantic mobile
names, mapped to native assets inside the renderer rather than SF Symbol names
in application code.

The binding exports `Start(dataDir)`, `Stop()`, `Root()`, `Render(route, values)`,
`Dispatch(route, action, values)`, and `Command(payload)`. JSON exists only at this **local in-process
gomobile boundary** because it binds strings reliably. Applications construct Go
structs and handlers, not JSON. It is neither a remote screen protocol nor an
obligatory representation for other renderer modules. The earlier typed
`List/Get/Save` exports remain compatible, but the generic renderer never calls
them or knows their names.

Literal and `{parameter}` path segments are supported; register literals before
overlapping parameterized routes. Query values and submitted fields appear in
`Request.Values`; route parameters are separate in `Request.Params`. Remote
URLs, missing routes, unregistered actions, malformed/oversized values, and
unsupported screen/field/icon definitions return errors. Submitted values never
supply identity or bypass service authorization. Register routes at startup; the
registry is immutable afterwards.

## Runtime and navigation

1. UIKit starts the binding off the UI thread, negotiates services, and opens
   `Root()` through the local command protocol.
2. Go resolves the route, reads the shared service, and builds mobile definitions.
3. The library decodes one versioned response and mounts real UIKit controls.
4. Typing, selection, focus, scrolling, keyboard geometry, and gesture state stay
   native; there is no synchronous Go round-trip per keystroke. Debounced Go
   checkpoints provide installation-local draft recovery.
5. A named action sends the current fields to its registered Go handler. Go
   validates and saves through the shared service before returning success effects.
6. The native renderer applies push/pop, a medium/large sheet, dismissal, sharing,
   haptics, announcements, or draft cleanup. Failed saves retain the fields/draft.

**UIKit owns the sole presentation stack.** Go routes identify destinations and
Go actions request navigation; there is no parallel browser history or hidden Go
screen stack. Clean Back uses native navigation. Dirty editors disable edge-pop
and show the Go-defined Keep Editing / Keep Draft / Discard prompt. Returning to
a list refreshes its data while preserving that controller's search state.

Async search ignores stale responses. Actions disable duplicate submission but
leave editing enabled. The shared Go policy preserves newer text and adopts the
created identity before another save. Business execution does not hold lifecycle
or screen locks; commands have a 30-second context. Stop closes admission and
drains operations before its 10-second host shutdown budget. A context-ignoring
operation can still delay draining. See [runtime](mobile-runtime.md) and
[lifecycle guarantees](mobile-lifecycle.md).

## Native surface and limits

Supported now: large-title navigation, inset-grouped lists, native search and
pull refresh, a text form with single-line inputs followed by at most one
multiline input, plain-text reading screens, navigation-bar actions, medium/large
sheets, the system share UI, success/selection feedback, accessibility announcements,
semantic colors, native text styles, and keyboard-layout-guide avoidance.

Notes uses the shared Go file draft store; the optional older backend without
`Command` retains its UserDefaults behavior. Drafts survive checkpoints/restarts
but are not encrypted secrets, committed notes, cloud sync, autosave, or conflict
handling. The shared Notes list still caps at 200 entries.

The later [component slice](mobile-components.md) adds tabs, typed mixed form
controls, filters/selection/paging, swipes, and bounded route restoration; the
[device service slice](mobile-services.md) adds typed platform tasks. Photo
acquisition now lives in a [compiled extension](compiled-extensions.md).

Missing: Android native renderer, arbitrary component trees/custom native widgets,
general scene restoration, physical-device release tooling, and a public GoFastr
renderer-host mounting seam. No remote UI/code loading is added.

**Bootstrap boundary:** startup composes `notesapp/application.Open` without a
web presentation, then uses `mobilehost.ServiceOnly` for the complete in-memory
Serve lifecycle. M3 Gate A is complete; listener-free Gate B is not. Neither
GoFastr checkout is modified.

## Verification

`go test ./nativeui ./examples/notes/notesnative ./cmd/gofastr-native` checks
route/action boundaries, definitions, create/search/edit/share/sheet effects,
shared persistence/validation, no platform screen source in the Notes app, and
renderer selection/rejection. Tests do not establish visual or platform parity.

The Go-defined app is compiled, linked, installed, and exercised on the iPhone
17 Pro / iOS 26.5 simulator. Actual UI checks cover invalid saves, create/save/
reopen, search, Keep Draft across process restart, and opening/dismissing native
sharing without sending content. Light/dark, keyboard, and large-text spot checks
are recorded in the implementation notes. Physical haptics, VoiceOver, physical
iPhone signing, and native Android are not verified.
