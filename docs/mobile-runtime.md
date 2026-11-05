# Local mobile runtime and built-in services

`nativeui.NewRuntime(screens, RuntimeOptions{Drafts: store})` coordinates the
Go-authored mobile screen registry. It does not own a navigation stack, replace
GoFastr's router, or require other presentation modules to use this vocabulary.
The Notes UIKit binding and shared-definition WebView use it; legacy whole-web
hosting remains separate. See [editor policy](editor-behavior.md),
[startup ownership](mobile-lifecycle.md), and [screen setup](shared-mobile-screens.md).

## Application setup

Applications still compose Go screens and actions. Mark repository writes with
`nativeui.Action.Commit: true`; return `Result.Saved` with the canonical route,
field values and draft key after a successful save. This lets a newly created
editor adopt its saved identity without rebuilding its controls or creating a
second entity on its next save. Validation returns `ActionError` with safe
messages and optional field errors. An unclassified write error means an
**unknown outcome**, not permission to retry.

Create `behavior.NewFileDraftStore` in the installation's private directory and
pass it through `RuntimeOptions.Drafts`. For WebView, the optional third
argument to `webview.New(screens, platform, options)` supplies these options.
Nil storage means memory-only editor sessions. The negotiated draft capability
is unavailable in that case; sessionStorage is only a renderer backup.
Notes uses `mobile-drafts/` in each app's existing data directory.

## Compiled renderer protocol

`Runtime.CommandJSON(ctx, payload)` is the local gomobile boundary. Its typed
equivalent is `Runtime.Handle(ctx, Command)`. The command protocol version and
older RenderJSON/DispatchJSON envelope both currently equal 1 but are separate
contracts. Native applications export `Command(payload)` alongside the older
binding methods. WebView sends the same commands to `/_mobile/actions` with
`X-GoFastr-Mobile-Protocol: 1`; structured command outcomes use HTTP 200, while
malformed envelopes use HTTP 400. Legacy action requests retain their status
mapping. Authentication, CSRF and local-origin checks still apply.

| Command | Responsibility |
| --- | --- |
| `hello` | Negotiate version and explicit available/fallback/unavailable capabilities; issue a connection ID |
| `open` | Resolve a local route, check required capabilities, issue a screen-instance ID, restore a semantic draft |
| `checkpoint` | Observe a complete local editing buffer and monotonic renderer revision; acknowledge durable storage |
| `action` | Admit a request ID once per retained screen, run its Go action off the UI thread |
| `respond` | Validate a presentation choice/selection, admit its Go continuation once per retained request, and settle with `complete` |
| `complete` | Observe the result against the newest buffer; settle save policy without executing the action again |
| `leave` | Return allow/confirm/block policy; `keep` needs a durable checkpoint; `discard` cannot bypass a pending/unknown save |
| `ack` | Record completed/cancelled/unsupported/denied/failed presentation outcomes against issued effect IDs |
| `close` | Release a detached screen after its action has finished |

The same route can have distinct instances. Reusing a request ID with different
input is rejected. Stale revisions and foreign connection/instance IDs are
rejected. A newer editor takes ownership of its semantic draft key within one
Runtime; obsolete checkpoints cannot overwrite it. Full snapshots are used;
there is no general tree-diff, streaming patch protocol, or remote UI loading.

## Threading and storage guarantees

Typing, IME, selection, focus and scrolling remain renderer-local. Both adapters
debounce Go checkpoints by 250ms and send full buffers at save/explicit-leave
boundaries, not synchronously per keystroke. UIKit uses up to four background
operations so a save does not block checkpoints; Go serializes commit actions
and never holds the screen mutex while executing application business code.

A successful save establishes a canonical baseline. Newer edits remain dirty,
stay on the same editor and suppress that save's automatic navigation. Non-save
actions may push/present another screen while dirty only after durable draft
storage, with no pending/unknown save; they never clear the draft. Unsafe
forward navigation returns `unsaved_changes` rather than silently succeeding.
Dirty pop/replace still follows the existing leave policy. Unchanged buffers
can normalize to server-confirmed values and leave normally. File drafts use
private modes, atomic synced writes, and token/revision-conditional cleanup.
They are not encrypted, cloud-synced, or committed repository records.

With configured draft storage, failed admission checkpoints return `draft`
without executing the action. Failed completion persistence blocks navigation
and effects; read-only completion retry never reruns the mutation. Discard waits
for conditional cleanup, clean residue does not override fresh repository data,
and canonical draft-key migration stores the new key first. See
[reliability guarantees and fault coverage](mobile-reliability.md).

The default runtime retains at most 128 connections/screens, 64 action records
per screen, and expires idle sessions after 30 minutes. Limits are configurable
within documented constructor bounds. JSON commands are capped at 8 MiB.
Request deduplication is in-memory and bounded: it is **not exactly-once writes
across crashes**. A recovered pending save remains uncertain. Inspect saved
data rather than automatically resubmitting it. There is no built-in conflict
resolver or mutation ledger yet. Storage ownership is process/runtime-local,
not cross-process coordination for a shared draft directory.

## Honest adapter limits

Retained live collections use [bounded read-only recovery](mobile-reliability.md)
when their watch instance is retired or expired. This does not retry mutations
or silently renegotiate an expired renderer connection.

- UIKit has real navigation, sheets, keyboard controls and share cancellation
  callbacks. Existing controllers/inputs are retained during saves; collection
  refresh still reloads its table rather than using keyed row reconciliation.
- WebView keeps GoFastr navigation/history. App links and app-tagged browser/OS
  Back/Forward wait for Go leave/checkpoint acknowledgement. Untagged history
  fails closed with an in-app navigation fallback. Background events initiate
  immediate checkpoints; hard reload/termination can still outrun that work.
- Older route-navigation WebView sheets are full pages; shared Go presentations
  now use actual overlays. Sharing may use copy fallback; haptics may be
  unavailable. Fallbacks and observed receipts are explicit. Issuing a haptic
  or announcement does not prove a user physically perceived it.
- UIKit sequences checkpoints, fences obsolete replies, and offers bounded
  background batches; see its [lifecycle contract](../render/ios/docs/reliability.md).
- Receipts are best-effort observations, not transactional OS effects. Actual OS
  suspension/termination, full IME/VoiceOver/TalkBack and physical-device
  deep-link/restoration/tab acceptance remain work. Native Android is deferred.

## Verification

`go test ./...` includes binding lifecycle/draft restart, pure editor fault tests,
protocol flags, and real-transport browser flows. The shared-screen E2E delays
a create request, types while it is pending, checks caret/input retention,
adopts the created route, saves again, and verifies only one record exists.
It clears sessionStorage and reloads to verify Go draft recovery independently
of the browser backup. Native compilation/typechecking and actual simulator
interaction are separate evidence, not substitutes for physical-device testing.
