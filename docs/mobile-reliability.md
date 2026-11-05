# Mobile reliability: drafts, lifecycle, and retained collections

This slice belongs to the reusable UIKit and shared-definition WebView renderers.
Applications continue to author Go screens and actions. Notes is the acceptance
fixture, not a special-case implementation. GoFastr core is unchanged.

## Retained screens versus live sessions

The runtime deliberately retires older live watches when another screen opens
on the same renderer connection. That prevents obsolete queries from applying
later. Retaining a controller or DOM subtree does not reactivate its retired
watch. Native tab restoration can open other screens before the first list is
visible; revisiting a retained tab can encounter the same condition.

Both collection adapters now recover a `stale` or `expired` watch by reopening
the current authorized query once. They adopt the new screen instance and resume
watching, preserving the existing collection controls, valid selected IDs, and
scroll position. The replaced instance is closed through the normal protocol.
Cancelled or superseded callbacks still cannot commit results into another
screen or a suspended view.

Recovery is bounded to one reopen until a successful watch, an explicit refresh,
or a fresh foreground/appearance cycle. A repeated obsolete-session error or
failed reopen becomes a visible read error with the existing Retry control.
Authorization, unknown-outcome, capacity, transport, and other errors are not
silently treated as retired sessions. An expired renderer connection is not
silently renegotiated; a failed reopen remains visible.

A just-cancelled watch can still occupy its server worker. Both adapters retry
only `pending`/`cancelled` watch responses up to three times, 150ms apart, before
exposing Retry. This does not reopen the screen, reset the obsolete-session
budget, or retry a business operation.

## Durable editor boundaries

The shared Go runtime acknowledges the pending editor snapshot before admitting
an action. If configured draft storage fails, it returns a structured `draft`
error without executing the action. This is a known unexecuted request, not an
unknown mutation outcome; explicit retry is safe after storage is repaired.

Keep Draft needs acknowledged storage and cannot release pending/unknown saves.
Discard preserves the live buffer until revision-conditional cleanup succeeds.
Completion checkpoints the canonical baseline before cleanup; clean residue is
not restored over fresh repository data. Create-to-saved draft-key migration
writes the canonical key first and checks both semantic owners. An obsolete
completion cannot overwrite a newer editor or emit navigation/device effects.

Restarted pending/unknown saves preserve newer text and remain unknown. They are
not retried or unlocked by Keep/Discard. Failed completion persistence suppresses
effects; only observation, never the write, may be retried. See the
[runtime fault-test notes](../nativeui/docs/agent-notes.md).

## WebView history and backgrounding

For app-tagged Back/Forward, the adapter composes with GoFastr's pre-navigation
history hook, restores the source entry, and awaits Go leave policy plus any
Keep Draft acknowledgement before replaying the destination. Cancel, storage
failure, newer edits, composition, or uncertain work keep the same editor.
Permission is revision-bound and single-use; explicit app Back does not prompt
twice. App history lineage survives reloads. There is no competing router or
extra normal-history entry.

Untagged/foreign history cannot be repaired reliably: the adapter stays on the
current screen, replaces that unknown entry with the current app URL/state,
and requests in-app navigation. Arbitrary external history is not preserved.
Hard reload, tab closure, and process termination are not asynchronous barriers.

Hidden/pagehide events immediately initiate a Go checkpoint instead of waiting
for the typing debounce. The local backup remains, watches stop, and Save/device
tasks are never invoked. The browser/OS may still suspend or terminate before
the checkpoint completes.

Checkpoint replies are fenced by request order, editor revision, and mutation
epoch. A late same-revision snapshot cannot relock a completed save. Completion,
leave, and discard briefly defer background snapshots until canonicalization or
buffer restoration finishes; typing checkpoints still run during long saves.
Repeated completion mismatch stops after two observations, marks the operation
uncertain, and never retries its write.

## UIKit lifecycle

The renderer centrally observes suspension/resumption and checkpoints loaded
editors under a bounded UIKit background-task lifetime. The additive
`checkpoint(completion:)` reports success only for acknowledged current safe
snapshots; expiration/stop settles outstanding batches false once. `suspend()`
and `resume()` support scene-based hosts without app-owned editor policy.

Short commands are sequenced per instance without putting long mutations in the
checkpoint dependency chain. Reply fences reject pre-mutation snapshots, even at
the same revision. Completion/leave defer checkpoints so old buffers cannot
recreate discarded drafts or undo canonicalization. Restoration never reopens
retained pending editors; only visible attached controllers resume reads.
See [UIKit details and limits](../render/ios/docs/reliability.md).

## What must never be retried

Automatic recovery issues only read/open/close operations. It does not replay
actions, presentation continuations, file/device results, saves, or OS effects.
It does not replace editor buffers, take semantic draft ownership, or unlock
pending or unknown writes. Editor recovery continues to use the existing
[draft and runtime contract](mobile-runtime.md).

This is not crash-consistent exactly-once business execution. Immediate
termination before a checkpoint, cross-process ownership, crash-time mutation
reconciliation, and comprehensive real-device IME/screen-reader coverage remain
open. Native Android remains deferred.

## Verification

`TestRetainedCollectionRecoversReadWithoutReplayingActions` uses the actual
mobile host, Go runtime, and bridge in both WebView platform modes. It retires
the live session by opening another screen on the same connection, then verifies
that the original DOM and selection survive the new instance, no write commands
are issued, and persisted note content is unchanged. It reproduced the visible
obsolete-screen Retry failure before the fix.

Renderer tests also cover the one-reopen budget, permanent errors, failed
reauthorization, suspended late reads, and Swift recovery decisions. The existing
shared-screen browser flow covers edits during a pending save, caret retention,
and durable Go draft recovery after browser backup storage is cleared.

`TestMobileHistoryWaitsForDurableDraft` holds the real bridge's Keep response,
checks that the editor remains, exercises Cancel and newer typing, and verifies
Forward recovery without browser backup or extra writes.
`TestMobileBackgroundCheckpointSurvivesReload` dispatches visibility before the
debounce, clears browser backup, reloads, and checks the Go draft without saving
the note. These use Chromium's iOS/Android presentation modes, not actual
WKWebView/Android engines or OS background tests.

Runtime wire tests inject failed storage/cleanup, lost acknowledgements,
superseded ownership, and canonical-key migration. Notes binding tests exercise
actual Stop/Start and an unavailable draft directory. Foundation tests execute
sequencing/fencing/expiration; UIKit sources separately compile and link.
`TestMobileBackgroundCheckpointWaitsForDiscard` holds the real discard request,
backgrounds the editor, and verifies that only the restored baseline is later
checkpointed and recovered after reload. Node tests additionally delay a pending
snapshot beyond completion and check canonicalization and bounded observation.

```sh
export GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL
node --test render/webview/*.test.cjs
go test ./render/ios ./cmd/gofastr-native ./nativeui ./behavior
go test -race ./nativeui ./behavior ./examples/notes/notesnative ./render/webview
go test -race ./sim -run 'TestMobileComponentsWebView|TestMobileHistoryWaitsForDurableDraft|TestMobileBackgroundCheckpointSurvivesReload|TestMobileBackgroundCheckpointWaitsForDiscard|TestRetainedCollectionRecoversReadWithoutReplayingActions|TestSharedMobileScreensWebView' -count=3
go test -p 1 ./...
GOWORK=off go test -p 1 ./...
```

The November 5, 2026 final validation passed all 127 Node tests, the scoped Go
race checks, three repetitions of the five recovery/shared-screen browser flows
under the race detector, and the complete workspace and public-pin Go suites.
JavaScript syntax, Go formatting, workspace/public-pin vet, and whitespace checks
also passed.

Earlier full runs, including the initial rename validation, hit intermittent
component-menu/touch timeouts at stages 8 and 11. A later 30-repetition run
captured the stage 11 failure: the open row menu was still moving during
snap-back. Pointerdown hit its summary, pointerup hit the row link instead, and
the next gesture began at an offscreen transformed coordinate. This was a
renderer hit-target bug, not evidence that CDP touch events needed retries.

The adapter now ends snap-back immediately when the row menu opens. A new
computed-transform assertion failed in both styles before the fix; all 30 runs
per style then passed, along with fresh complete workspace and public-pin Go
suites. The earlier stage 8 pin timeout did not recur in these runs; its specific
cause was not independently captured, so this is not a claim that every earlier
flake shared the same cause. Failure reports retain bounded pointer/action traces.
Fresh scoped runtime/behavior/device/WebView/Notes race checks also pass, as do
three race-enabled repetitions of all six browser workflows listed above.

The earlier full race suite also exposed a stale browser-node handle at
`sim/files_test.go:117`, which reproduced in an untouched snapshot of checkpoint
`16786d5`. It was not changed in this continuation. Fresh normal full suites
passed; no product action was retried to mask a failure.

UIKit compilation and Foundation execution are not native tab/foreground gesture
acceptance. Simulator checks and any remaining physical-device limitations must
be reported separately.

On November 5, 2026, the rebuilt `dev.gofastr.designqa` simulator app launched
with its existing fixture notes and showed live updates without Retry. A later
full gomobile rebuild/install and Settings-to-Notes background/resume smoke check
also retained those notes and returned to live updates. Screenshots were inspected
at `/tmp/gofastr-reliability-native-start.png` and
`/tmp/gofastr-reliability-native-resume.png`.

This does not establish native dirty-editor/IME, tab gestures, background-task
expiration, or device-provider acceptance. Earlier coordinate input returned
`noWindowsAvailable`, and Agent device access was disabled. No simulator reset
or app-data deletion was used as a workaround.

A subsequent full renamed-CLI rebuild/install retained the same fixture notes.
Accessibility-driven simulator checks opened and closed row actions, expanded
and collapsed filters, and paused/restarted live reads, returning to Live
updates without changing note contents or metadata. Coordinate-only tab/editor
checks still returned `noWindowsAvailable`; they are not reported as verified by
this continuation. Native Android remains deferred.
