# Native retained-screen reliability

This extends the shared [mobile reliability contract](../../../docs/mobile-reliability.md)
inside the reusable UIKit renderer only. It does not add Notes-specific behavior,
new dependencies, new GoFastr core behavior, or native Android support.

## Lifecycle and parent glue

`GofastrNativeRenderer.makeWindow()` installs UIApplication notification observers:

- will-resign-active and did-enter-background call `suspend()`;
- did-become-active calls `resume()`;
- will-terminate calls the existing asynchronous `checkpoint()` as best effort.

The existing generated AppDelegate remains compatible without a required template
change. Parent-owned entry glue can explicitly call `renderer.suspend()` from
`applicationDidEnterBackground` and `renderer.resume()` from
`applicationDidBecomeActive`; a scene-based host can use its corresponding scene
callbacks for the renderer it owns. Keep this glue-only: no storage, retry, or
editor policy belongs in the template. Existing `checkpoint()`, `openURL(_:)`,
and `stop()` call sites still compile. No asynchronous callback makes termination
a synchronous durability barrier.

`checkpoint(completion:)` is additive. Its Bool is true only when every submitted
loaded editor checkpoint was acknowledged for its current revision with no
pending/unknown outcome and, when dirty, a stored Go draft. Loaded noneditors
need no editor checkpoint. A superseded snapshot, missing/failed reply, changed
buffer, uncertain editor, expired/denied background execution, or dirty legacy
UserDefaults-only backup cannot produce a confirmed-success claim. Completion
runs on the main queue; work itself remains off the UI thread.

One renderer-owned UIKit background task covers overlapping checkpoint batches.
The task ends after all batches settle, on expiration, on stop, or on renderer
release. Expiration settles outstanding batches false once; a late reply does
not finish them twice, retry a mutation, or restart an OS task. Already submitted
Go calls may still finish after expiration. Route metadata remains bounded,
route-only UserDefaults data, not a synchronously fsynced restoration journal.

## Snapshot ordering and editor safety

`GofastrReliability.swift` is Foundation-only. Its command sequencer orders short
snapshot/complete/leave/close calls per instance. A mutation waits for previously
submitted snapshots, but subsequent typing checkpoints do not wait behind the
long-running save/respond/file-result/service-result operation. Independent
editors remain independent; watches stay on the separate watch queue.

The checkpoint fence requires the same instance, current local revision,
matching reply revision, latest checkpoint ticket, and unchanged mutation epoch.
A pre-action clean reply cannot unlock a newer pending/unknown operation. A
pre-completion pending reply cannot relock a completed save. Error replies never
replace the editor snapshot, and checkpoints never clear `operationUnconfirmed`.

There is a short checkpoint deferral while applying `complete` or `leave`:
otherwise a raw pre-canonicalization buffer queued behind complete could recreate
a dirty draft after a clean save, or queued old input could resurrect a discarded
draft. Deferred requests coalesce into one fresh snapshot after response handling.
A successful explicit departure settles them false instead of persisting discarded
input. Actual pending business saves still accept typing checkpoints. Departure
also checks its captured revision and current safety before popping; a newer edit
or background transition keeps the controller in place. A stale complete can be
read once more; repeated mismatch becomes unconfirmed without replaying the action.

## Retained navigation and reads

Restoration preflights current canonical routes and semantic draft keys against
retained controllers, including the startup editor. It does not reopen a duplicate
editor or speculate into another editor while a retained outcome is uncertain.
The uncertain controller is selected for inspection. Failed deeper restoration
keeps the successfully restored stack prefix rather than closing it wholesale.

Suspension invalidates executing watches and pending list/image read callbacks,
cancels debounced list searches, stops attachment playback, and marks interrupted
list loads for a later read. It does not close retained editors, clear input,
restart playback, repeat an action, or settle a device effect a second time.
Resume only activates the visible, attached, topmost controller. Hidden tabs and
covered stack entries do not reopen lists or start watches. An interrupted visible
list load refreshes its current authorized query; other visible live lists use
the preexisting one-reopen stale/expired recovery budget. Explicit read failures
keep their Retry affordance. Input controls, caret, and selection are not rebuilt.
Transient `pending`/`cancelled` watches retry at most three times, 150ms apart,
without reopening the screen; repeated failure becomes visible. Success or
explicit reactivation resets this separate read-only budget.

## Verification and limits

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./render/ios ./cmd/gofastr-native
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./render/ios ./cmd/gofastr-native
```

`TestReliabilitySwiftCheckpointOrderingAndRestoration` executes the actual
Foundation sequencing, fencing, deferral, one-shot expiration, and restoration
guards with gated OperationQueue work. It proves a newer checkpoint completes
while a save is still deliberately blocked, complete/close cannot overtake older
snapshots, callbacks settle once, and pending/unknown or duplicate editors are
not reopened. Existing services checks retain the bounded live-recovery and
stale-generation coverage. `TestComponentsUIKitOptimizedCompiles` compiles and
links all UIKit sources for an iOS 16+ simulator target and separately typechecks
an external client of the old and new public renderer APIs.

These are deterministic Foundation regressions and UIKit compile checks, not
executed native tab gestures, real background suspension/expiration, OS URL
delivery, IME, VoiceOver, permission dialogs, provider completion, or physical
device acceptance. The parent rebuilt/bound/installed the Notes QA app and
observed its existing notes and live list after a real simulator background/resume
cycle; see the [shared validation record](../../../docs/mobile-reliability.md).
No simulator reset or app-data deletion was used. Abrupt termination before a checkpoint runs, UserDefaults metadata
flush timing, cross-process mutation reconciliation, and crash exactly-once
effects remain outside this bounded change.

### 2026-11-05 — checkpoint ordering and retained ownership
- Scope: `ios/reliability`.
- Trigger: worker-pool replies can arrive after a same-revision mutation or canonical completion.
- Approach: sequence snapshots per instance, fence reply adoption, defer only completion/leave snapshots.
- Evidence: executable `reliability_test.go` plus optimized UIKit compilation and public client checks.
- Next time: keep long-running mutations out of the snapshot dependency chain; never recover by reopening an editor.
- Status: active; physical lifecycle/IME/VoiceOver acceptance is still unverified.
