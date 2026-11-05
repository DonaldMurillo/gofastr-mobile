# Headless editor behavior

`github.com/DonaldMurillo/gofastr-native/behavior` is an application-independent
Go editor policy and an optional file-backed draft store. It does not import
GoFastr, `nativeui`, HTML, UIKit, or a particular application's model. All
dependencies are in the Go standard library.

The shared runtime now integrates this policy with UIKit and WebView; see
[runtime](mobile-runtime.md) and [reliability](mobile-reliability.md) for actual
acknowledgement, restart, and history coverage. This package's unit tests alone
are not renderer/device acceptance. Native Android and complete real-device
lifecycle/IME/accessibility verification remain deferred.

## Exact integration API

```go
func NewEditor(instanceID, route string, baseline Values) (*Editor, error)

func (*Editor) Snapshot() Snapshot
func (*Editor) Checkpoint(current Values) Snapshot
func (*Editor) Begin(current Values) BeginDecision
func (*Editor) Complete(result Result, current Values) CompletionDecision
func (*Editor) Leave(current Values) LeaveDecision
func (*Editor) Restore(snapshot Snapshot) error

type DraftStore interface {
    Load(context.Context, string) (Snapshot, bool, error)
    Put(context.Context, string, Snapshot) error
    ClearIfRevision(context.Context, string, DraftRevision) (bool, error)
}

func NewFileDraftStore(root string) (*FileDraftStore, error)
func (Snapshot) DraftRevision() DraftRevision
func (DraftRevision) Matches(Snapshot) bool
```

`Values` and `FieldErrors` are `map[string]string` types. Every boundary value is
a **complete** field snapshot, not a partial update: absent fields are removed.
Nil current values mean an empty form, not “leave the buffer unchanged.” The
editor copies input/output maps, including submitted values and field errors.
Snapshots, submissions, results, and decisions have exported JSON-tagged fields.
An `Editor` itself is not a wire object; use its constructor and do not copy it.

Each public editor operation is mutex-guarded. Observing a buffer and admitting
a save, applying its completion, or deciding leave happens in one critical
section. The runtime still owns chronological delivery and must drop stale
renderer checkpoints using its separate wire revisions. A mutex cannot identify
an old buffer delivered after a newer one. Callers must not concurrently mutate
maps while passing them into an operation.

### Renderer and action loop

1. At open, read the current saved entity and create an editor with a fresh,
   opaque **screen-instance** ID, the effective save route, and the entity's
   complete saved values. Do not use a route or entity ID as screen identity.
2. Select a trusted semantic draft key separately. Scope it to the installation,
   account, entity/create slot, and desired draft-sharing identity. The same key
   may be loaded by a new screen instance. `Snapshot.InstanceID` is a lifetime
   identity, not the store key.
3. `Load(ctx, key)` returns the persisted snapshot. If recovery is appropriate,
   call `editor.Restore(loaded)` **before** using the editor. Restore keeps the
   freshly loaded entity baseline, route, and new instance identity; it restores
   current values/errors, not the old baseline, action token, or submission.
4. Keep the editable buffer, caret, composition, selection, undo, and immediate
   typing feedback in the renderer. At bounded checkpoints, call
   `Checkpoint(fullBuffer)` and persist the returned snapshot with `Put`.
   Do not send every keystroke to Go.
5. On Save, call `Begin(fullBuffer)`. Dispatch business work only if `Admitted`
   is true, using `Submission.Route` and its frozen `Values`. Persist the
   returned **pending** snapshot before launching the mutation if crash-time
   unknown-outcome protection is required. A failed pending checkpoint must
   not launch that mutation. Keep the one admission and retry only the draft
   checkpoint, or explicitly complete it as a known failure before resubmitting.
6. Run service work outside the editor lock and UI thread. At finish, collect
   the latest complete renderer buffer, then call `Complete(result, fullBuffer)`.
   Never overwrite local text with the old submitted values merely because a
   service call finished. Persist the returned completion snapshot.
7. Apply save/navigation effects only when `Applied` is true. `MayLeave` is a
   permission **at that supplied checkpoint**, not a standing authorization:
   recheck any newer local typing/composition before applying a delayed effect.

`Result` contains `InstanceID`, `Token`, `Revision`, `Outcome`, optional
`Values`, `FieldErrors`, `Message`, and `SavedRoute`. Copy the first three from
the admitted `Submission`, never from the current renderer revision.

The parent can map its `SavedState.Route/Values` into `Result.SavedRoute/Values`.
A successful nonempty saved route is retained in the editor even when newer
edits prohibit leaving. The next admitted save uses that route, avoiding a
second create after an initial create succeeded. The parent owns
`SavedState.DraftKey` adoption: retain the original key for conditional cleanup;
checkpoint newer edits under the adopted semantic key according to application
policy. Route adoption is not a second navigation stack or an entity migration.

## Transition decisions

| Event | Policy |
| --- | --- |
| Unchanged checkpoint | No field-revision increment. |
| Changed checkpoint | Increment local field revision; keep submitted values frozen. Clear errors only on fields whose value/presence changed. |
| Begin while pending | Deny with `ReasonPending`; the current buffer is still observed, but no second submission is returned. |
| Begin after unknown | Deny with `ReasonUnknown`, even after further edits. |
| Successful save | Advance baseline to `Result.Values`, or submitted values when `Values == nil`. Adopt `SavedRoute` when supplied. |
| Successful save, no intervening edit | Apply server-confirmed/canonical values to current values; propose conditional cleanup; allow leave when clean. |
| Successful save, intervening edit | Retain the latest full current buffer. If it differs from the committed baseline, `MayLeave` is false. Cleanup never covers the newer field revision. |
| Known failure | Retain baseline/current values; attach field errors only where current value/presence still equals the submitted value. Permit another explicit submission. |
| Unknown outcome | Retain edits and submission metadata; prohibit submission and automatic leave. No draft cleanup is proposed. |
| Wrong instance/token/submitted revision, completed action replay, invalid outcome | Reject without observing even the supplied buffer or changing state. No navigation/cleanup effects. |

`Result.Values == nil` means “the submitted values were committed”; a nonnil
empty map means “the server confirmed an empty complete snapshot.” JSON preserves
that distinction. Only `SaveSucceeded`, `SaveFailed`, and `SaveUnknown` are
accepted outcomes. A failure must mean **known not committed**; a timeout,
cancellation, transport error, or lost response after a possible commit is not
proof of failure.

An unknown action in the same live editor can be resolved with an authoritative
success/failure `Complete` for that exact submission. There is no automatic
retry, timeout-based unlock, or force-reset option. `Restore` turns persisted
pending/unknown state into `SaveUnknown` without reviving an old action token.
That recovered unknown cannot accept the previous screen's completion; a parent
must perform domain-supported reconciliation before deliberately replacing that
blocked session. Do not treat reopening or restoring the baseline as proof that
an uncertain create did not commit. This package does not supply repository
idempotency or reconciliation.

### Leave, Keep Draft, and Discard

`Leave(fullBuffer)` returns `LeaveAllow` for clean known state, `LeaveConfirm`
for unsaved edits, or `LeaveBlock` for pending/unknown work. It does not display
a dialog, pop a screen, cancel a mutation, or delete a draft.

- **Keep Editing:** retain the live editor and buffer.
- **Keep Draft:** persist the requested full snapshot and leave only after
  `Put` acknowledges success. Check that the live buffer has not advanced during
  that write. On any checkpoint failure, remain open and show truthful status.
- **Discard:** for known, nonpending state, the parent can checkpoint the current
  entity baseline and conditionally clear the previously loaded/stored draft
  revision. Pending/unknown state is not silently reset by changing field values.
  An explicit unknown-outcome abandon/navigation policy belongs to the parent;
  it must not authorize an automatic retry.

## Conditional draft cleanup

The store is keyed by the explicit semantic key passed to its methods, **not**
by route or screen instance. A `DraftRevision` contains the originating
`InstanceID`, local field `Revision`, and opaque `Token`. Obtain it using
`snapshot.DraftRevision()`; **do not construct it from an integer revision alone**.
The token contains a random per-editor-session identity plus the save-attempt
counter. This separates reopened screens even if their integer revisions match,
and separates a newer save attempt at an unchanged field revision.

On success, `CompletionDecision.ClearDraft` describes only a covered version.
For unchanged/canonicalized success it also matches the clean completion
snapshot. With newer edits it identifies the submitted version, not the newer
completion snapshot. `ClearIfRevision` compares all revision fields while
holding the same directory lock as writes; a mismatch returns `(false, nil)`.
Thus a late old-screen clear cannot remove a new screen's draft at the same key.
Repeating a clear after deletion is harmless. Cleanup failure cannot turn a
committed save into failure.

The store rejects older field revisions and older save-attempt snapshots within
one instance with `ErrDraftConflict`. With this intentionally small `Put`
interface, writes from **different** instances are last-writer-wins. The parent
must serialize ownership of a semantic key and reject obsolete checkpoint jobs;
there is no compare-and-swap write receipt or automatic cross-session merge.
Likewise, order pending/completion writes for one action. The file store does not
infer causality between two status snapshots sharing a field revision/attempt.

## File store guarantees and limits

`NewFileDraftStore(root)` takes a trusted app-owned directory from Go bootstrap
configuration. Never pass a bridge/JSON-supplied filesystem directory. Store keys
are opaque valid UTF-8 strings, not paths: filenames are SHA-256 hashes of keys
inside that directory. Persisted JSON has no path field, and reads reject unknown
fields, trailing JSON, unsupported formats, symlinks, and nonregular files.

The store creates/tightens its directory to `0700`, writes `0600` temporary files
in that directory, syncs and closes them, atomically renames them into place,
and syncs the directory before acknowledging success. Clears also sync the
directory. The named target is never exposed partially written. Bounds are:

- `MaxDraftKeyBytes`: 4,096 bytes per key, nonempty.
- `MaxDraftFields`: 1,024 entries per field/error map.
- `MaxDraftBytes`: 1 MiB for aggregate raw string input **and** the final encoded
  file, including baseline/current/submission and JSON escaping/metadata.
- Reads check file size and use a bounded reader; malformed/oversized inputs
  fail without replacing a previous valid checkpoint. Field strings must be
  valid UTF-8 rather than being silently replaced during JSON encoding.

Directory-level locks are shared across store handles for the canonical root
**within this Go process**. There is no cross-process advisory lock, hostile
same-user filesystem defense, backup encryption, cloud sync, account
authorization, retention policy, or orphan-temp-file cleanup. The parent must
enforce account/install isolation and single-process ownership of the directory.
Hashed names hide key text from filenames, not field contents from disk readers.
The implementation is tested on the current macOS filesystem; this is not a
claim of device, Windows ACL, or every-filesystem durability support.

If rename/removal succeeds but directory sync fails, the operation returns an
error although the namespace may already have changed. Do not acknowledge Keep
Draft or repeat an older write blindly; inspect with `Load` and reconcile the
latest runtime checkpoint. A nonnil error still means durability was not
acknowledged. A killed process can leave an owner-only temporary file; only the
hashed final filename is loaded. A draft can protect only text actually sent to
and acknowledged by the store, not unsent renderer buffers.

No clock is injected because this policy has no debounce, expiry, or timer-based
transition. Storage I/O is explicit through `DraftStore`, outside editor locks.
Normal application logs should not contain these snapshots or field contents.

## Verification and implementation notes

Run the isolated package checks:

```sh
go test ./behavior/...
go test -race ./behavior/...
go vet ./behavior/...
```

Pure transition tests cover duplicate admission, typing during saves,
server-confirmed baselines, route adoption, stale completions, field-specific
validation, unknown outcomes, restoration, detached JSON snapshots, and
concurrent entry points. Real temporary-directory store tests cover reload,
permissions, hashed keys, bounded/malformed input, unknown-action metadata,
old-screen and old-attempt cleanup, and concurrent writes/conditional clears
through separate store handles. No renderer/platform acceptance is claimed.

**Next time:** flush the latest complete renderer buffer at finish, use the
entire `DraftRevision()` rather than only a revision number, and persist pending
state before executing an action whose response could be lost. Keep old and
adopted semantic draft keys distinct until their cleanup/checkpoints are
acknowledged. These are correctness boundaries, not presentation conveniences.
