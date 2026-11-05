# WebView controls and collection adapter

The frozen [shared contract](../../../docs/mobile-components.md) is authored in
Go. `webview.New` renders it through the existing GoFastr UIHost/router. The
library embeds `components.js`, `webview.js`, and CSS; applications supply no
JavaScript, HTML, CSS, gesture handlers, or alternative router.

## Fields

`nativeui.Field` supports `Kind: "text"`, `"multiline"`, `"toggle"`, `"select"`,
`"date"`, and `"time"`, with `Required`, `Disabled`, and
`Options []nativeui.Option{Value, Label, Disabled}`. Controls have semantic
labels, associated error descriptions, and `aria-invalid` on field errors.
Checkboxes, selects, date inputs, and time inputs use browser controls/pickers.

All values remain strings: toggle `"true"`/`"false"`, date `YYYY-MM-DD`, time
`HH:MM`, and `""` for optional empty values. An initially empty optional toggle
retains `""` until interacted with; thereafter unchecked means `"false"`.
Required toggles accept both boolean strings, so HTML `required` is deliberately
not applied to the checkbox. Required selects do not silently choose the first
option. Disabled options/controls remain disabled, and Go rejects edits to their
protected baseline. `readonly` is not interpreted as `disabled`.

Commands read current control properties, including `checked`, not the original
HTML attributes. Input checkpoints are debounced by 250 ms and deferred during
IME composition. Pending commands do not replace the editor buffer. Go draft
storage is authoritative; the existing session backup and leave policy remain.

## Collections

- `List.Filters []Field` uses the same labelled controls. Changes apply the
  current query and clear paging/selection. Existing search still submits
  explicitly; search, refresh, and Load more use GoFastr navigation.
- `List.Selection *Selection{Key, Selected, Maximum}` uses stable enabled row
  IDs. Commands send one JSON-array **string** under `Key`, plus current filters
  and search values. Maximum zero means no renderer-imposed selection limit.
  Disabled/removed IDs are reconciled out of a new snapshot; selection survives
  cancellation and same-query refresh/paging. At most 32 in-memory collection
  contexts are retained. Selection is not saved in localStorage or an auth grant.
- `List.Paging *Paging{Key, Next, Label}` renders explicit Load more when `Next`
  is nonempty. Go returns the accumulated snapshot; JavaScript does not invent
  remote paging or append independently fetched row fragments.
- On a selectable collection, non-primary, non-leading `Action.Commit` actions
  appear **only** in the collection toolbar, including undo when supplied by Go.
  Primary and first leading actions remain compact header/FAB controls; remaining
  actions appear once in the labelled native More disclosure. The disclosure is
  relocated into the header when JS starts, but remains usable in SSR fallback.
  The contract has no separate selection-only action flag; this partition uses
  existing semantic flags rather than matching application IDs or labels.
- Successful settled actions/responses without navigation or a new modal task
  force a same-route refresh. This also refreshes `Screen.Values`, such as a Go
  undo token, instead of retaining the previous hidden values. A cancelled
  presentation refreshes without replaying the mutation or losing selection.

Go continues validating/authorizing each submitted selected or row ID and owning
confirmation/undo. No selected identifier is trusted because it came from the UI.
GoFastr's navigation epochs reject old HTTP screen responses; adapter query
epochs also reject an older delayed leave result before requesting navigation.

GoFastr can **morph and retain the screen root and form**, even for paging or a
same-route refresh. The adapter retires the previous instance on a committed
`gofastr:navigate`, or changed config/form, then opens the fresh Go definition.
Old timers, busy/query state, gestures, and asynchronous replies cannot mutate
the new generation. `[data-gfw-instance]` is cleared until the new open succeeds.
Do not wait for root disconnection as a navigation-readiness signal.

## Tabs and local entry

`Screen.Tabs []Tab{ID, Label, Route, Icon}` and `ActiveTab` render labelled bottom
navigation. Routes must be local registered routes. Optional icons are semantic
library assets, not app-supplied markup. Dirty switches go through the existing
Go checkpoint/leave policy and GoFastr leave guard. Pending/unknown mutations,
composition, and open foreground file/presentation tasks block switching.

**The current GoFastr seam exposes one browser history stack, not independent
per-tab stacks.** This adapter preserves each tab's last route, scroll, and named
control focus; Back/Forward remain GoFastr's shared history. It does not claim
full per-tab push/pop histories or install a competing stack. The reported
capability is `tabs: "last-route-scroll-focus-with-shared-history"`.

Bounded localStorage metadata has a version and a root/registered-route/tab
signature. It accepts at most 16 tabs, 4096-character local routes, 256-character
focus keys, and 32 KiB per metadata value. Invalid JSON, old definitions, unknown
tabs, traversal, remote URLs, and malformed routes are discarded. It persists
only active-tab/route/scroll/focus metadata: no field buffers, selected IDs,
actions, effects, or pending tasks. Visible field parameters and reserved
action/row/CSRF/selection parameters are removed before route persistence. Go
drafts supply editing recovery. An explicit non-root entry URL takes precedence
over restart metadata; unavailable storage degrades to normal local navigation.

Library hosts can call
`await window.__gofastr_webview.openRoute("/registered/local/path")`. The boolean
means navigation was admitted/requested, not that a screen load has completed.
This uses the same local validation, leave policy, and GoFastr open lifecycle as
ordinary links. It never calls an action or loads a remote URL. Native URL-scheme
registration/normalization belongs to parent packaging, not this adapter.

## Row actions and gestures

`Row.Disabled` blocks row activation, selection, and action dispatch.
`Row.LeadingActions`/`TrailingActions` contain
`RowAction{ID, Label, Role, FullSwipe}`; commands send stable `values.row` plus
current filters/selection through the same registered Go handler. Every action
also appears as a visible labelled button in the row's native Actions disclosure,
with no custom keyboard/ARIA menu engine. Destructive roles retain confirmations.

Pointer tracking/animation is local: no Go requests per frame. The first 28 CSS
pixels at the viewport's left edge are reserved for native Back. Horizontal
tracking starts at 12 px with directional bias; vertical scrolling, pointer
cancellation/lost capture, pending tasks, disabled rows, or navigation cancel it.
At 56 px a swipe reveals the accessible action menu. Full swipe requires at least
`max(168 px, 72% of row width)`, and only the first side action with explicit
`FullSwipe: true` can dispatch. Destructive full swipe is always forbidden. A
gesture-generated click is suppressed so the row cannot also open or write twice.
Transferring capture from a starting link to its row ignores the link's bubbled
loss event. A new pointerdown clears suppression from the previous gesture so
the next deliberate tap is not swallowed.

Opening row actions ends the content's snap-back transition immediately. A
visible menu must not move its disclosure or action buttons between pointerdown
and pointerup. The regular return animation remains for gestures that do not
open a menu; reduced motion continues to disable it entirely.

## Stable selectors

| Element | Selector |
| --- | --- |
| Existing editor control | `[data-gfw-field="<key>"]` |
| Filter control | `[data-gfw-filter="<key>"]` |
| Row selection | `[data-gfw-select="<row-id>"]` |
| Load more | `[data-gfw-load-more]` |
| Tab | `[data-gfw-tab="<id>"]` |
| Row action | `[data-gfw-row-action="<id>"][data-gfw-row-id="<row-id>"]` |
| Row Actions disclosure | `[data-gfw-row-menu="<row-id>"]` |
| Header More disclosure | `[data-gfw-header-menu]` |
| Current successful Go open | `[data-gfw-screen][data-gfw-instance]` |

## Verification and platform limits

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test -race ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./render/webview
for source in render/webview/*.js; do node --check "$source"; done
node --test render/webview/*.test.cjs
```

Go HTTP tests cover semantic markup, single action placement, form fallback,
typed validation, disabled controls, selection operands, and unregistered/unsafe
definitions. Dependency-free Node tests cover serialization, restoration,
thresholds/cancellation, wire effects, stale query leaves, and retained-root
paging/Cancel→Apply/hidden-undo lifecycle. They are not a real browser or OS.
Parent-owned `sim/components_test.go` also drives real CDP touch sequences for
full-swipe unpin, trailing action reveal, stationary open-menu targets, immediate
follow-up taps, and vertical scroll cancellation. The stationary-target assertion
failed in both styles before the renderer fix. These flows,
multi-selection/confirmation/undo, typed-field persistence, tab draft recovery,
and 200% text sizing pass 30 consecutive runs per style on November 5, 2026.
Browser E2E remains distinct from device acceptance.

Browser date/time pickers, pointer capture, edge Back, IME, focus restoration,
localStorage lifetime, VoiceOver, haptics, downloads, and share behavior depend on
the embedded WebView/native host. Styled Chromium tests do not establish physical
device or actual WKWebView/Android WebView behavior. This work does not change the
existing [presentation](presentations.md), [files/photos](files.md), draft, or
effect-settlement protocols. See [implementation notes](agent-notes.md) before
changing renderer lifecycle glue.
