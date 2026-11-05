# UIKit controls, collections, tabs, and local URLs

All application screens and handlers remain Go-authored. The optional component
DTOs extend the frozen `docs/mobile-components.md` wire contract without changing
`GofastrNativeBackend.init`, `makeWindow()`, or `stop()`. Ordinary `*.swift` sources
are embedded unchanged by `sources.go`; no production dependencies are added.

## Controls and list state

`GofastrComponents.swift` contains binding-independent component DTOs, stable-ID
selection state, strict local-route normalization, date/time formatting, and
bounded navigation metadata. `GofastrFieldControl.swift` supplies `UISwitch`,
`UIMenu` selection with disabled options, and native compact `UIDatePicker`s.
Date/time use a POSIX locale, Gregorian calendar, and fixed GMT formatter solely
to encode calendar strings (`yyyy-MM-dd` / `HH:mm`), not timestamps, timezones,
or notifications. Empty dates/times have explicit Set and optional Clear buttons;
required fields cannot be cleared. An empty optional toggle remains empty until
the user changes its switch; changes emit exactly `true` or `false`.

Labels and error messages use Dynamic Type. Disabled controls retain Go-authored
values even when action buttons are reenabled. Full field snapshots include all
typed controls, text, and multiline values. Inline field errors come from the
same action/complete/checkpoint policy; UIKit does not replace Go validation.
Editor completion updates only a clean current buffer and never touches marked
text. Text views, caret/selection, IME, and sessions are not rebuilt by attachment
refresh or tab switching. Checkpoints remain debounced by 250ms off the UI thread.

Filters are persistent native controls in the table header. Changing search or
a filter clears the paging token and stable-ID selection, then performs the
existing debounced Go open. A selection-mode toolbar button adds accessible row
checkboxes; selected IDs are a sorted JSON array under the declared selection
key. Maximum is enforced locally for feedback and still validated by Go. Pending
actions freeze list interactions without clearing selection. Reordered snapshots
keep the same IDs; refreshed accumulated snapshots prune IDs no longer present.
Load more explicitly submits `Paging.Next` under `Paging.Key`. The returned Go
snapshot replaces rows; UIKit never appends stale query results or invents pages.

Leading/trailing `UIContextualAction`s and the labelled row Actions menu invoke
the same registered action/complete path with the latest values and stable
`row` ID. The alternative menu is a native popover/table (a dismissible sheet on
compact devices), with labelled, individually addressable actions. Every handler
rechecks the current row ID and disabled/pending state. Full swipe is disabled
unless the first action explicitly opts in and is non-destructive; destructive
actions can never full-swipe. Go owns confirmation and undo. Successful final
no-navigation mutations refresh the current list. Other retained lists refresh
when next visible if a mutation occurred; live editors remain intact.
Read refresh submits only declared filter/search/paging keys, never selection,
row context, or undo tokens. Refreshed `Screen.Values` replaces action metadata
so the next action receives the latest undo token (or none); retained local
controls and stable-ID selection are not recreated or reset.

## Tabs, restoration, and pending policy

`GofastrNavigation.swift` uses a real `UITabBarController` and one retained
`UINavigationController` per tab. Simple switching does not recreate screens,
editors, or tables; existing scroll and text selection belong to those controls.
A dirty departure requires Go to acknowledge a durable checkpoint. Pending or
unconfirmed writes, an active files/photos/presentation task, or a presented
sheet block switching and deep-link departure. All tabs share the same host and
single `MobileForegroundCoordinator`; existing Back/sheet settlement remains in
`NativeScreenController`. A newer local edit cancels delayed route application.

UserDefaults stores only version, active tab ID, and local route arrays under
`gofastr.native.navigation.v1.<backend root>`. Storage is bounded to 64KiB,
sixteen tabs (the Go contract limit), and six routes per stack. UIKit's native
More interface handles overflow tabs, with user reordering disabled so stable
tab-to-stack identity cannot change behind the renderer. No field values, action IDs, request IDs,
effect receipts, or OS tasks are persisted in this metadata. Startup disables
interaction until reconstruction completes. Each restored destination is
read-validated, then opened through the same Go host with a fresh instance/current
saved baseline and Go draft restoration. Duplicate semantic draft keys are not
opened again; uncertain editors are retained, selected for inspection, and stop
deeper restoration. While any retained editor is pending/unknown, restoration
may open read-only roots but not speculative new editor sessions. A duplicate
canonical route is rejected even without a draft key. An unavailable saved
destination preserves the successfully restored prefix of that stack; an
unavailable tab root is disabled, with the live startup root as final fallback.
Restoration never dispatches actions, replays effects, or replaces a live pending
editor. Normal future navigation is not limited to six live screens; only persisted
route metadata is bounded.

## Exact public hookup

```swift
@discardableResult
public func openURL(_ url: URL) -> Bool
public func checkpoint()
```

Keep the renderer strongly retained for the application lifetime. After the
existing `window = renderer.makeWindow()`, forward a custom-scheme cold launch
URL immediately with `renderer.openURL(url)`. For warm delivery, the parent's
`application(_:open:options:)` should first enforce its explicit configured
scheme allowlist, then return `renderer.openURL(url)`. Scene-based hosts should
forward their allowlisted `UIOpenURLContext.url` through the same method. The
parent owns `CFBundleURLTypes`/`--url-scheme` and delegate/scene glue; no bootstrap
or application-specific code is added here.

`notesapp://notes/new?tag=work` normalizes to `/notes/new?tag=work`; local URLs
such as `URL(string: "/notes")` are also supported. HTTP(S), file and other
external-service schemes, userinfo, ports, fragments, nonlocal paths, traversal,
encoded slashes/control characters, and malformed/bounded-out input return false.
No bootstrap domain validator is required: a synchronous, read-only backend
render preflight checks the registered route/current screen and supported required
capabilities before acceptance. Only this infrequent URL admission can synchronously
enter Go; typing/filter updates never do. New destinations then use `host.open`
and the existing authoritative draft/leave policy. A retained route/semantic draft
reuses its controller rather than opening a second editor for the same draft.

The Bool means **validated URL admitted**, not a promise that an asynchronous
checkpoint, fresh Go open, or OS transition finished. Later failures are reported
without retrying writes. Malformed, unregistered, unsupported, stopped, or currently
blocked targets return false, rather than launching another app or faking a screen.
One validated cold-start target is queued until host negotiation/tab restoration
finish; another URL while that slot is occupied returns false. URL handling requires
the versioned command backend; legacy render/dispatch applications return false.

The renderer observes will-resign-active/did-enter-background to call `suspend()`,
did-become-active to call `resume()`, and will-terminate for a best-effort checkpoint.
The existing `checkpoint()` remains asynchronous and source compatible. Its new
`checkpoint(completion:)` overload holds one shared UIKit background task until
all submitted editor snapshots reply or the OS expires the task. Superseded,
failed, pending/unknown, and expired/denied checkpoints return false; a dirty legacy
UserDefaults backup is not claimed as a confirmed durable write. No hook waits
synchronously for Go or guarantees storage before immediate process termination.
`stop()` retains the existing shutdown API. See the exact lifecycle glue, sequencing
contract, and limits in [native reliability](reliability.md).

## Accessibility identifiers

- `native.field.<key>`: actual text view/text field/switch/select/date/time input.
- `native.field.<key>.error`: inline field validation label.
- `native.field.<key>.set` / `.clear`: explicit date/time value controls.
- `native.tab.<id>`: tab bar item.
- `native.row.<row>`: stable record cell; `native.select.<row>`: selection checkbox.
- `native.selection-mode`: Select/Done toolbar button.
- `native.row-actions.<row>`: labelled non-gesture Actions menu opener.
- `native.row-action.<row>.<action>`: individual action cell in that menu.
- `native.load-more`: explicit paging button.
- `native.list-empty`: accessible non-action cell for an empty collection.
- Existing `native.search`, `native.status`, `native.action.<id>`, and attachment/
  image identifiers retain their meanings. Native swipe labels use the declared
  action label; use the menu action cell IDs for deterministic accessibility tests.

## Verification and next-time notes

Executable optimized Foundation checks cover real old/new DTO decoding, disabled
option flags, date/time round-trips, stable-ID/max selection, snapshot reorder and
deletion, full-swipe prohibition, route rejection, and metadata bounds/no-input
storage. Query projection checks reject stale selection/row/undo read state and
verify replacement/removal of authoritative undo metadata. The optimized
simulator dylib compile/link catches Swift definite-
initialization and UIKit API errors that `-typecheck` can miss. Run:

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./cmd/gofastr-native ./render/ios
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./cmd/gofastr-native ./render/ios
```

November 5 parent acceptance also built/bound/linked/installed/launched the app
on iOS 26.5 and exercised filters, simultaneous selection, row-menu pin,
switch/save, retained tabs and dirty editor text. Empty filters remain in the AX
tree because the empty message is a real cell; background/zero-row footer labels
did not suffice. Public compose/share tab symbols now map to actual SF Symbols.
See [measured acceptance and limits](../../../docs/mobile-components.md#verification).
Actual native swipes, cold/warm URL delivery past the OS confirmation, VoiceOver,
physical devices, IME, and rotation remain unverified. Native Android is deferred.

### 2026-11-05 — component and restoration maintenance
- Scope: UIKit components/navigation.
- Trigger: optional typed controls and tab stacks extend an existing editor host.
- Approach: preserve controls, correlate by stable IDs, and persist routes only.
- Evidence: `components_test.go` executes Foundation checks and compiles optimized UIKit.
- Next time: avoid naming a subclass property `tabs`; iOS 18 added `UITabBarController.tabs`.
- Status: active; actual touch/VoiceOver/IME acceptance remains unverified.

### 2026-11-05 — mutation refresh values
- Scope: UIKit collection queries/actions.
- Trigger: a no-navigation mutation removes selected rows and replaces an undo token.
- Approach: read with query keys only; replace authoritative action metadata, then reconcile IDs.
- Evidence: executable `components_test.go` query/undo regression checks.
- Next time: never merge old action values over refreshed `Screen.Values`.
- Status: active; native bulk/undo touch acceptance remains parent-owned.
