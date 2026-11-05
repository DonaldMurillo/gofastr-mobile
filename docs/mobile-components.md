# Mobile controls, collections, navigation, and gestures

This expansion keeps application screens and handlers in Go. UIKit and WebView
implement the same contracts in library source; GoFastr core is unchanged and
native Android remains deferred. Existing files, photos, and presentation APIs
must remain compatible. The contracts below coordinate the parallel work;
acceptance evidence is recorded after integration, not implied by this design.

## Shared contracts

- `Field` keeps string values and adds `Required`, `Disabled`, and
  `Options []Option`. `Option` has `Value`, `Label`, and `Disabled`. Supported
  kinds add `toggle`, `select`, `date`, and `time` to `text`/`multiline`.
  Toggle values are `true`/`false`; date/time use `YYYY-MM-DD`/`HH:MM`, not an
  implicit timezone or a scheduled notification. Empty optional values are
  allowed. Disabled controls cannot be changed by a submitted command.
- `List.Filters []Field` carries query controls. Filter changes reset paging
  and selection. `List.Selection *Selection` has `Key`, `Selected []string`,
  and `Maximum int`. Selected IDs cross the existing string-value boundary as
  one JSON array under `Key`; they are stable record IDs, never row indices.
- `List.Paging *Paging` has `Key`, `Next`, and `Label`. Its explicit Load more
  control changes the query to `Next`; the example returns accumulated rows.
  No renderer silently invents remote paging or treats old query results as new.
- `Row.Disabled` blocks interaction. `Row.LeadingActions` and
  `Row.TrailingActions` contain `RowAction{ID, Label, Role, FullSwipe}`.
  Registered Go handlers receive the row ID in `Request.Values["row"]`.
  `Role` is empty or `destructive`; destructive full-swipe is prohibited.
  Every swipe action also has a labelled, non-gesture menu alternative.
- `Screen.Tabs []Tab` and `Screen.ActiveTab` describe the shell.
  `Tab{ID, Label, Route, Icon}` uses stable IDs and local registered roots.
  Each renderer owns its real stacks, scroll/focus, and transition lifecycle;
  only navigation metadata is restored, never replayed writes or OS effects.

Go validates definitions and submitted values, authorizes mutations, and owns
confirmation/undo semantics. Rendering adapters own fingers, IME, scrolling,
animation, gesture cancellation, and native back behavior. There are no
per-frame Go calls and no app-authored JavaScript, Swift, or templates.

## Native deep-link packaging

`--url-scheme notes-demo` opts the native iOS build into a custom scheme.
`notes-demo://notes/<id>` opens the registered `/notes/<id>` route; registration
is absent by default. Schemes are bounded lowercase URI identifiers; web/file
and the internal WebView scheme are rejected. Other renderer/platform modes
report that this packaging is unsupported rather than silently accepting it.
The tiny generated bootstrap only checks the configured scheme and forwards
cold/warm URL events to the library navigator. It contains no screen definitions.

Custom schemes are not verified Universal Links and do not prove caller
identity. Links select local screens, never invoke mutations or permissions;
route handlers must still enforce authorization. Do not put secrets in links.

## Notes acceptance slices

1. Common form controls edit real pinned/tag/reminder metadata on notes, with
   persistent typed storage and shared validation rather than a second demo.
2. The notes list supports selection, filtering, accumulated paging, bulk
   tagging/archive/trash, and bounded undo. Trash is reversible soft deletion;
   changing query resets selection and each mutation reauthorizes its IDs.
3. Notes, Files, and Settings share one tab shell, preserve independent context,
   and restore safe routes/drafts. Native deep-link entry reuses route opening.
4. Leading pin and trailing archive/trash gestures invoke the same Go actions
   as accessible menus. Destructive actions confirm; pending/disabled controls
   cannot dispatch again. WebView gestures must yield to vertical scrolling and
   native edge-back. Platform tracking/animation stays local.

## Verification

The four slices are implemented in `nativeui/`, `behavior/`, `render/ios/`,
`render/webview/`, and the single `examples/notes/notesscreens` application.
The exact validation rules live in [component contracts](component-contracts.md);
adapter details and selectors are in
[UIKit components](../render/ios/docs/components.md) and
[WebView components](../render/webview/docs/components.md).

November 5 native evidence: fresh optimized gomobile bind/Swift link/install/run
on iPhone 17 Pro, iOS 26.5 simulator; real UI checks exercised active/archived
filters, simultaneous selection, row-menu pin, switch/save, retained editor
context and dirty text across Files/Notes tabs. Large text/dark-mode inspection
caught missing tab symbols and an empty-message overlap; both were corrected.
The empty state is an actual accessible row so filters remain in the AX tree.

November 5 browser evidence: the full workspace and public-pin (`GOWORK=off`)
Go test suites pass. Component, shared-screen, presentation-showcase, file, and
photo browser workflows pass three consecutive runs in both iOS and Android
styles. Component acceptance exercises actual CDP touch events for leading
full-swipe unpin, trailing action reveal without mutation, immediate menu-close
taps, and vertical-scroll cancellation. It also verifies paging, multi-select
confirmation/cancellation/undo, persisted typed fields, tab draft recovery, and
200% text sizing. All 69 dependency-free renderer Node tests and JavaScript
syntax checks pass. Android styling here is Chromium coverage, not native
Android or Android WebView device acceptance.

The custom-scheme packaging builds and its forwarding is tested. The OS showed
its first-open confirmation during GUI acceptance; it was cancelled, so warm/
cold route delivery is not claimed as end-to-end verified. Native swipe code
compiles, but the GUI drag attempt opened a row rather than proving a swipe.
VoiceOver reading order, IME, rotation, physical-device gestures, and native
Android remain acceptance work, not implied by compilation or Chromium.

Relevant repeatable commands:

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./...
GOWORK=off GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./...
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test -race ./behavior ./nativeui ./render/webview ./examples/notes/...
SIM_SHOTS=/tmp/gofastr-component-shots GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./sim -run TestMobileComponentsWebView -count=3
SIM_SHOTS=/tmp/gofastr-component-shots GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./sim -run 'TestMobileComponentsWebView|TestSharedMobileScreensWebView|TestNotesBehaviorShowcaseWebView|TestNotesFilesWebView|TestNotesPhotosWebView' -count=3
node --test render/webview/*.test.cjs
```

## Deliberate limits

- Load more now uses owner-scoped repository cursors beyond 200 records. It
  expands ten-row batches to a bounded 100-row window, then starts the next
  window. Forward `Paging.Next` unchanged; filter changes clear selection/paging.
- Notes selection caps at 20. Bulk writes are sequential with honest partial
  results, not a transaction. Undo receipts last 30 seconds, cap at 32, and do
  not survive process restart. Text and attachments are preserved by metadata
  changes; every mutation rechecks current ownership and collection membership.
- Native tabs have independent retained stacks. WebView tabs have independent
  last-route/scroll/focus contexts, not separate Back/Forward stacks. Restoration
  metadata contains safe routes only; Go draft storage owns unsaved values.
- Reminder fields still save metadata; separate explicit device-service actions
  schedule/cancel/check OS reminders where supported. Plain-browser scheduling
  is unsupported. See [device services](mobile-services.md). Custom schemes are
  not Universal Links; immediate termination can outrun async checkpoints.
