# Typed device services and bounded live reads

Implemented November 5, 2026. This is the Go request/transport contract, not
evidence of real microphone, notification-provider, share-provider or physical
device acceptance. Native Android remains deferred. No production dependency
was added. Existing files/photos and legacy `share/v1` remain compatible.

## Public API

- `device/notifications.Request{Operation, Identifier, Title, Body, Route, Date,
  Time string}` exposes `Validate() error`; operations are `Schedule`, `Cancel`,
  `Status` (`schedule`, `cancel`, `status`). Identifiers are application-scoped,
  1–128 ASCII token bytes (`A-Z`, `a-z`, `0-9`, `.`, `_`, `:`, `-`). Scheduling
  requires a title (maximum 200 UTF-8 bytes), body at most 64 KiB, a registered
  local route, canonical `YYYY-MM-DD` and `HH:MM`. Go validates the calendar
  shape; only the host can reject past/nonexistent **device-local** times.
  Cancel/status accept only an identifier, never request permission, and do not
  schedule. A schedule action is the only permission-requesting operation.
- `device/sharing.Request{Title, Text string; Files []files.Document}` exposes
  `Validate() error`: nonempty text or files, title at most 200 bytes, text at
  most 64 KiB, at most four documents, each at most 1 MiB, and **combined decoded
  bytes at most 1 MiB**. `sharing.Clipboard{Text string}.Validate()` checks a
  write-only, explicit UTF-8 clipboard write with the same text bound; empty
  text is an explicit clear. These values never accept arbitrary device paths.
- `device/audio.Request{Title string; MaxSeconds, MaxBytes int}` exposes
  `Validate() error` and `Accept(files.Document, durationMillis int) error`.
  Zero limits use `DefaultMaxSeconds` (60) / `DefaultMaxBytes` (1 MiB); positive
  limits cannot exceed those maxima. Title is required and at most 200 bytes.
  Successful recordings require nonempty content and positive reported duration
  within the admitted limits. Hosts enforce actual recording elapsed time and
  must stop on backgrounding/interruption; there is no background recording or
  speech-recognition service.
- `audio.Document{File files.Document; DurationMillis int}.Validate()` applies
  maximum recording bounds to private playback bytes. `nativeui.Attachment.Audio
  *audio.Document` optionally embeds playback. Its filename, media type and byte
  count must match attachment metadata. This is local play/pause/seek input,
  never a remote streaming URL. Players stop/release on departure/interruption.

Accepted audio MIME/container pairs are AAC ADTS (`audio/aac`), AAC M4A/MP4
(`audio/mp4`, `audio/x-m4a`), WebM/Opus (`audio/webm`), Ogg/Opus (`audio/ogg`,
`application/ogg`), and PCM/float WAV (`audio/wav`, `audio/wave`, `audio/x-wav`).
Content must match the container signatures; arbitrary filenames and MIME alone
are insufficient. ADTS frames, MP4 top-level boxes, Ogg page bounds and WAV
chunks are bounded/checked. WebM must identify EBML/WebM, an Opus track and a
segment. This is **not a full media decoder**, codec verifier, compressed-stream
duration calculator or malware scanner. Platform support may be narrower;
unsupported playback/recording must be disclosed, not transcoded silently.

## Service lifecycle and wire fields

`nativeui.ServiceRequest` has `ID string`, exactly one of `Notification
*notifications.Request`, `Share *sharing.Request`, `Clipboard
*sharing.Clipboard`, `Audio *audio.Request`, and `Commit bool`. Its exported
`Validate()` checks shape; application validation also requires an immutable
`Route.Services map[string]ServiceRespond` entry and registered reminder route.
`ServiceRespond` is `func(context.Context, Request, ServiceResult) (Result, error)`.

`ServiceResult` has `Status EffectStatus`, optional `Message`, `Permission`,
`Identifier` strings (each at most 512 UTF-8 bytes without control characters),
optional `File *files.Document`, and `DurationMillis int`. An identifier, when
present, must match the admitted reminder. Non-audio and unsuccessful results
must contain **neither file nor duration**. There is no clipboard-read result.

1. Explicit registered `action` returns `Result.Service`. The runtime validates
   and deep-copies the exact request. Do not execute its OS work yet.
2. `complete` with that action's request ID admits the service, reauthorizing the
   current source action and checking unchanged admission inputs. Its response
   includes `response.service` and opaque `serviceId`; `ServiceRequest.ID` names
   the continuation and is **not** the opaque identity.
3. The adapter records the opaque ID before performing the operation **once**.
   It sends a fresh request ID, `kind: "service-result"`, `serviceId`, observed
   `serviceResult`, current `values`, and current `revision`.
4. Go validates bytes/status/target against the admitted immutable request and
   reauthorizes the currently available source action before invoking the
   continuation once. Current editable fields reach it; row, selection, query
   and other noneditor targets cannot drift. All valid statuses, including
   `cancelled`, `denied`, `unsupported`, `failed`, reach Go for honest feedback.
   Application continuations must check `Status == EffectCompleted` before
   writing attachments. Unsuccessful results cannot clear/save editor state.
5. `complete` with the service-result request ID settles effects and releases
   the foreground owner. Replayed old completion cannot reissue a settled
   service. Foreign/closed/resolved opaque IDs never run the continuation.

Capabilities are `notifications/v1`, `sharing/v1`, `clipboard/v1`, `audio/v1`,
`live/v1`; `share/v1` remains the older string-sharing effect. A capability is a
renderer fact, not a permission grant. Service requests can be admitted even
when support is unavailable: the adapter returns `unsupported` through the
typed result so Go can render status. `Screen.Requires` still enforces hard
capability requirements before a screen opens.

The per-connection foreground owner is shared with presentations and files.
The owner is held until service-result **completion**, not just receipt. Closing
releases the owner, but late results stay invalid. `Commit` continuations share
the mutation mutex. An internal failure is an **unknown mutation**: the retained
session locks out new actions even after completion, preserves dirty drafts,
and requires inspecting/reconciling data rather than blind retries.

Recovery may repeat read-only `complete`. A repeated service-result with the
same request identity must match the original service ID, observed result,
revision and values; it only returns the recorded outcome. Neither path repeats
OS work or a Go write. Hosts must not rerun a share, clipboard write, recording,
notification schedule or continuation to recover uncertain transport. The
one-shot ledger is process-local, capped at 64 actions per retained instance,
with a result slot required before admitting OS work; it is not durable
exactly-once OS delivery. An 8 MiB command-envelope bound also
applies. A crash/restart does not authorize replaying unobserved work.

Adapters own caret/IME/dirty-buffer preservation. Clipboard is explicit-action
only; Web Share fallback downloads must be labeled downloads, not actual
sharing. A plain browser does not implement system reminder scheduling through
an in-page timer. Secure MediaRecorder support is optional. Native providers
use temporary private files with cleanup.

## Live reads

`Screen.Live *Live`, `Live{Version string}`, and `Route.Watch Watch` declare
observation. `Watch` is `func(context.Context, Request, string) (string, error)`.
Tokens are nonempty, at most 256 UTF-8 bytes, without control characters;
applications should include an epoch to avoid token reuse after restart.

`kind: "watch"` sends `dataVersion` and the **already rendered authorized query**
(search/filter/page keys only), not a changed editor buffer or selected IDs.
`Reply.DataVersion` and explicit `changed` report an observation only: no
navigation, screen replacement, editor checkpoint or mutation is performed.
Open responses also include the screen's data version. A different query needs
an explicit new `open` first. The runtime re-renders for authorization both
before waiting and before publishing a result.

Waiting releases the session lock, has a context deadline no later than ten
seconds, and returns when cancelled even if an application callback ignores its
context. A timeout is unchanged, not a fabricated update. Closing, opening a
replacement screen, superseding a semantic draft, or any subsequent nonwatch
command invalidates/cancels the pending generation. Stale completions cannot
publish a change. One callback per session and at most `MaxSessions` callbacks
per runtime may remain outstanding; callbacks **must honor context cancellation**
because Go cannot forcibly stop application goroutines.

Only adapters' visible live collection should watch. They cancel on departure,
coalesce replaceable read refreshes, use current query controls, retain still
valid selected IDs/scroll and never replace an editor buffer. This is
process-local observation, not remote sync or durable background execution.

## Deterministic task behavior

Zero-value `behavior.Task` is idle. `Begin(TaskRead|TaskMutation) (uint64, bool)`
returns an admitted generation. `Complete(generation, TaskState, message) bool`
settles that generation once; `Cancel(generation) bool` applies only to reads.
`Snapshot() TaskSnapshot` returns generation, kind, state, message and `CanRetry`.
States are `TaskIdle`, `TaskLoading`, `TaskSucceeded`, `TaskFailed`,
`TaskCancelled`, `TaskUnknown` (`idle`, `loading`, `success`, `error`,
`cancelled`, `unknown`). A newer read supersedes older reads; stale completions
do nothing. Pending mutations cannot be replaced/cancelled. Unknown mutations
stay nonretryable. Failed/cancelled reads expose explicit retry; this object
does not schedule, cancel OS work, or perform automatic retries. Messages are
bounded to 512 bytes, generations are monotonic, and transitions use no random
IDs or clocks. The gate is safe for concurrent completion/snapshot calls.

## Verification and implementation notes

Tests of the typed API use `Runtime.CommandJSON`, not internal handler calls.
Codec and Task tests are pure Go. The tests cover all four services, reminder
operations, all five outcomes, stale/foreign IDs, one-shot settlement and
recovery, current-draft preservation, exact result fingerprints, revoked source
actions, immutable input snapshots, unknown-write locks, cross-service
foreground contention, live cancellation/query authorization and obsolescence.

Run with `GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL`:

```sh
go test ./nativeui ./behavior ./device/notifications ./device/sharing ./device/audio
go test -race ./nativeui ./behavior ./device/notifications ./device/sharing ./device/audio
go vet ./nativeui ./behavior ./device/notifications ./device/sharing ./device/audio
GOWORK=off go test ./nativeui ./behavior ./device/notifications ./device/sharing ./device/audio
```

The owned-package test/race/vet matrix also passes with `GOWORK=off`. Existing
`device/files` and `device/photos` tests/race checks pass as well. The complete
`examples/notes/notesnative` consumer suite passes after the parallel Notes
worker's paging changes; no Notes source was edited by this slice.

### 2026-11-05 — One-shot services and cancellable watch
- Scope: device-services, nativeui, behavior/task.
- Trigger: cloning the files pattern can miss input drift, source reauthorization
  and cancellation bytes, or leave a live read holding the session mutex.
- Approach: freeze admitted requests, fingerprint complete observed results,
  reauthorize without dispatching the source action, lock unknown mutations,
  and invalidate watch generations before releasing/retiring sessions.
- Evidence: public-command regression tests in `nativeui/runtime_services_test.go`
  and `nativeui/runtime_live_test.go`, plus the command matrix above.
- Next time: keep OS execution outside recovery; test delayed completion after
  close/replacement and do not extend legacy file defects into service flows.
- Status: active; real-provider/browser acceptance belongs to the host workers.
