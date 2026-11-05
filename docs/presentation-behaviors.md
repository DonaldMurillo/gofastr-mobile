# Go presentation and selection behaviors

The shared mobile interaction layer is Go, not a JavaScript behavior framework.
Applications use Go definitions and typed continuations. Library-owned UIKit
uses no JavaScript; WebView retains its internal browser interaction glue.

## One example application

Notes is the sole example application. Open **Behaviors** from its main screen
to exercise alerts, confirmations, menus, dialogs, bottom sheets, single
selection, and multiple selection. Its editor exercises validation, drafts,
saving, sharing, and platform feedback. Add new examples here, not in another app.

`examples/notes/notesscreens` owns all shared definitions, including `showcase.go`.
`notesnative` and `notesmobile` are thin binding/launch adapters, not independent
products. `notesweb` and terminal adapters remain compatibility checks against
the same Notes service. Applications own no platform screen source or templates.

Single selection changes Notes ordering through a local route. Multiple
selection previews any nonempty subset of the available tags, including all
three; Shared remains disabled. It does not add a tagging data model. The reset
confirmation intentionally does not delete saved notes or drafts.

## Authoring

An ordinary Go action returns `nativeui.Result{Presentation: ...}`. Its ID names
a typed continuation in that route's `Presentations` map:

```go
nativeui.Route{
    Path: "/example",
    Screen: renderExample,
    Actions: map[string]nativeui.Handle{
        "ask": func(context.Context, nativeui.Request) (nativeui.Result, error) {
            return nativeui.Result{Presentation: &nativeui.Presentation{
                ID: "confirm", Kind: nativeui.PresentationConfirmation,
                Title: "Continue?", Dismissible: true,
                Choices: []nativeui.PresentationChoice{
                    {ID: "continue", Label: "Continue"},
                    {ID: "cancel", Label: "Cancel", Role: "cancel"},
                },
            }}, nil
        },
    },
    Presentations: map[string]nativeui.Respond{
        "confirm": func(ctx context.Context, request nativeui.Request, result nativeui.PresentationResult) (nativeui.Result, error) {
            return nativeui.Result{Announcement: "Confirmed"}, nil
        },
    },
}
```

Kinds are alert, confirmation, menu, dialog, and sheet. Selection is an optional
dialog/sheet body, not a catch-all dropdown. Menus choose actions; selection
chooses stable option IDs. `Selection.Min`/`Max` constrain confirmation. Zero Max
means one for single selection or the option count for multiple selection.

`behavior.Presentation` validates declared choices, disabled states, selection
identity, duplicates, cardinality, and cancellation. Results use declared option
order. Invalid input leaves the task open. Cancel choices and allowed external
dismissal bypass selection requirements and **never run the app continuation**.
Non-dismissible tasks require an explicit choice; they can still offer Cancel.

Set `Presentation.Commit` when its continuation may mutate application data.
This serializes it with marked writes. Unknown write outcomes block unsafe
retry, but deduplication is not durable across process crashes.

## Runtime and renderer boundary

The additive `presentation/v1` capability is required. Screens can declare it
in `Requires`; unsupported renderers fail explicitly rather than changing
renderer. Older route `Navigation{Kind:"sheet"}` retains its compatibility
behavior and is distinct from these transient overlays.

1. `action` runs Go; read-only `complete` issues an opaque `presentationId`
   alongside `response.presentation`.
2. The adapter owns controls, tentative selection, focus, gestures, animation,
   and accessibility mapping. It never selects an application handler itself.
3. `respond` submits that ID, choice, selected IDs, latest screen values/revision,
   and a new request ID. Go validates before admitting the continuation.
4. `complete` settles without rerunning it and releases the presenter. A
   continuation can return another presentation; dismiss the old one first.

One shared presentation is allowed per renderer connection. IDs belong to a
retained screen and connection. Closed/stale callbacks cannot run. Admitted
request IDs deduplicate within the bounded runtime lifetime; closing/expiring a
screen releases its reservation. Native sharing and older navigation sheets
also need adapter-level foreground ownership; this is not an OS window manager.

## Scope and validation

This slice does not implement camera, document/photo picking, arbitrary device
filesystem access, external capability registration, or remote UI loading.
Future file/media services should return typed outcomes and opaque resource
handles, not arbitrary device paths or large JSON media payloads.

Android native remains deferred. Chromium tests of Android appearance are not
device/TalkBack evidence. Full browser/OS Back durability, broader IME/VoiceOver
verification, and keyed list updates remain existing roadmap gaps.

Validation on November 4, 2026 includes the Notes native command binding and
real-transport Chromium UI flows for all seven examples in iOS/Android styles,
including minimum-selection recovery, applying all enabled tags, disabled options, chaining,
200% text resizing, scoped touch targets, cancellation, and focus restoration.
The iOS 26.5 simulator also exercised UIKit alerts, chained menu/confirmation,
dialog/sheet dismissal policy, single selection, and multiple-selection
validation with visible result feedback. A separately built WKWebView Notes
launcher exercised menu/confirmation chaining and cancellation/focus restoration.
These are two renderer launchers of the same Go-authored example, not two apps
with separately implemented screens. Physical-device, VoiceOver/TalkBack,
interactive swipe cancellation, and broader keyboard checks remain separate.
See [runtime](mobile-runtime.md), [screen setup](shared-mobile-screens.md), and
[roadmap](mobile-roadmap.md).
