# Shared Go component contracts

`docs/mobile-components.md` remains the frozen cross-agent contract. This file
documents the shared Go implementation in `nativeui` and `behavior`; it does not
claim that a renderer implements a control, gesture, or accessibility behavior.

## Additive API and wire shape

- `nativeui.Field` adds `Required bool`, `Disabled bool`, and `Options []Option`.
  `Option` has `Value string`, `Label string`, and `Disabled bool`. Field kinds
  are `text`, `multiline`, `toggle`, `select`, `date`, and `time`. Any mixture and
  order is supported, including multiple multiline fields before other controls.
- `nativeui.List` adds `Filters []Field`, `Selection *Selection`, and
  `Paging *Paging`. `Paging` has `Key`, `Next`, and `Label` strings.
- `nativeui.Selection` already served presentations. Its existing
  `Options`/`Selected`/`Multiple`/`Min`/`Max` API remains intact. Additive
  collection-only fields are `Key string` and `Maximum int`. Use only
  `Key`/`Selected`/`Maximum` for a collection and the existing presentation fields
  for a presentation; mixing the two modes is rejected. No frozen name changes.
- `nativeui.Row` adds `Disabled bool`, `LeadingActions []RowAction`, and
  `TrailingActions []RowAction`. `RowAction` has `ID`, `Label`, `Role` strings and
  `FullSwipe bool`. Register its ID in that route's existing `Route.Actions` map.
- `nativeui.Screen` adds `Tabs []Tab` and `ActiveTab string`. `Tab` has
  `ID`, `Label`, `Route` strings and semantic `Icon` (`nativeui.Icon`). Tab routes
  must be distinct, literal registered roots, without queries or parameters.

JSON names are lower camel case, including `required`, `disabled`, `options`,
`filters`, `selection`, `maximum`, `paging`, `leadingActions`, `trailingActions`,
`fullSwipe`, `tabs`, and `activeTab`. Existing presentation selection wire fields
remain present; consumers should ignore fields they do not use.

## Definition and input validation

`App.Render`/`RenderJSON` validate definitions and local destinations. Enhanced
collection rows need distinct stable IDs; legacy navigation-only rows may still
omit IDs. Rows with selection/actions (or disabled rows) may omit destinations.
Action IDs must be registered and declared on that screen or target row. A row
may expose an action ID on multiple records, but not twice on the same record.
Roles are empty or `destructive`; destructive full-swipe is always rejected.

Keys, IDs, nonempty option values, page tokens, and tab icon tokens use the shared
`behavior.SafeToken` rule: 1–128 ASCII letters, digits, `_`, `-`, `.`, or `:`,
excluding standalone `.` and `..`. Labels remain plain text. Field keys cannot
be `row`; search/filter/selection/paging keys must not collide. Forms/filters
have at most 128 fields, selects at most 256 distinct options, rows at most 16
actions, and shells at most 16 tabs. The existing 128-value/8 MiB input limits
also apply after rendered defaults are supplied.

`App.Dispatch`/`DispatchJSON` and runtime action commands validate before invoking
handlers, including non-commit actions. Required controls reject empty/whitespace
values, except disabled controls retain their authorized baseline. Toggle values
are exactly `true` or `false`. Selects reject foreign and disabled options.
Dates require a valid, positive-year `YYYY-MM-DD` date; times require `HH:MM` in
24-hour notation. No timezone is inferred. Optional empty strings are accepted.
Required controls may initially render empty for record creation. Disabled
controls cannot change through render input, action input, or editor checkpoints.
Incomplete enabled inputs remain checkpointable as drafts; strict validation
happens at action admission. Rejected enabled input is still retained in the
editor/draft, without starting a save or invoking the handler.

Invalid submitted controls, selection IDs, and row targets return
`*nativeui.ActionError{Code: "validation", Fields: ...}`. Direct Dispatch errors
also wrap `ErrInvalidValues`; Runtime exposes safe message/field data, not causes.
Application errors preserve their existing wrapping chain through Dispatch.
Unknown undeclared actions do not run, even if registered on the route.

## Collection values and pure behavior

Rendered `Screen.Values` includes search/filter values, the current paging value,
and selection JSON under `Selection.Key` (including `[]` for empty selection).
Filters' declared `Field.Value` is the canonical query value; screen authors
should derive it from the request and application state. Selection arrays contain
record IDs, never indices. Duplicate, foreign, or disabled IDs are rejected.
`Maximum == 0` means 200; explicit limits must be 1–200. Rendered collection
selection communicates the effective maximum to both adapters.

Shared headless utilities, also used by definition/submission validation:

- `behavior.CollectionState{Query Values, Page string, Selected []string}` and
  `.WithQuery(query)` clone state; a changed query clears page and selection.
- `.LoadMore(next)` validates a new safe token and preserves accumulated query
  context/selection. It does not fetch records or invent a backend cursor.
- `behavior.SelectionMaximum`, `ValidateSelection`, and `DecodeSelection`
  implement the same bounded identity rules for definitions and submitted JSON.
- `behavior.MaximumSelection` is 200; `SafeToken` is deterministic and shared.

Actions must submit the query and paging context of the currently rendered list.
Changing them during a mutation is rejected: reset selection/paging and reopen
the list first. For direct `App.Dispatch`, put the rendered query/page in the
destination URL; Runtime retains its opened screen's query context. Load-more
opens the route with `Paging.Next`; the application's screen function returns
accumulated rows. Stable IDs remain selectable across those accumulated rows.
Adapters/application query transitions use `WithQuery` semantics; Go rejects
mutations that attempt to bypass that transition with a stale query/selection.

## Authorization and retained actions

Runtime accepts row actions through the existing `Command{Kind: "action",
Action: rowActionID, Values: map[string]string{"row": recordID}}` boundary.
`Values["row"]` is reserved for an enabled row declaring that action. It cannot
be a field, query value, or arbitrary parameter for ordinary screen actions.
The registered Go handler receives the row ID in `Request.Values["row"]`.
Runtime conservatively serializes row actions through its mutation lane.

Dispatch rerenders the current authorized definition with query context only,
not submitted form fields or row IDs, before admitting the handler. Runtime
also validates its retained definition before beginning editor/save bookkeeping.
Presentation/file continuations retain their originating action's input, bind
row/collection targets to that input, and revalidate against current screen
authorization before invoking a continuation. Cancellation/denial does not run
the continuation or require otherwise-valid form data. Existing pending,
identity/replay, unknown-outcome, effect-receipt, and draft ownership rules remain.

Partial form submissions/checkpoints retain unsupplied fields. Partial canonical
`SavedState.Values` merge over the submitted form baseline rather than dropping
added metadata. Save completion retains newer editor inputs; restored older
drafts inherit newly declared controls and current disabled values. Extra
non-control request values still reach handlers, except the reserved `row` key.

Screen functions should be read-only: authorization now intentionally rerenders
them during actions/continuations. Handlers remain authoritative and must
reauthorize records and writes transactionally in their service/repository;
membership in a rendered list is not a substitute for application authorization.
Confirmation and undo policy remain application-owned. Tabs are metadata only;
stacks, focus, scroll, gestures, and labelled non-gesture action menus remain
renderer responsibilities. No navigation replay, cursor service, JS, or Swift
application-authoring requirement is introduced.

## Verification and durable implementation note

Focused public-boundary coverage is in `nativeui/components_test.go`; deterministic
query/paging/selection coverage is in `behavior/collection_test.go`. Existing
runtime draft, asynchronous-save, identity, presentation, files, and photos tests
remain part of the same packages. There is no new integration lane.

## 2026-11-05 - Reauthorize component inputs without losing editor baselines
- Scope: nativeui, behavior
- Trigger: typed metadata and collection targets share the existing string map.
- Approach: validate current definitions at Dispatch/continuation boundaries; merge partial form buffers and canonical saves before editor settlement.
- Evidence: `nativeui/components_test.go`, `behavior/collection_test.go`; `GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test -race ./nativeui ./behavior`.
- Next time: extend these shared validators and public command tests instead of adding renderer-specific validation or replacing partial baselines.
- Status: active
