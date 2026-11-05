# Reminders, sharing, audio, and live-data implementation contract

November 5, 2026. This is the coordination contract for four parallel slices,
not evidence that their acceptance gates have passed. Keep the single Notes
application and the existing files/photos/presentation/editor APIs compatible.
GoFastr core is read-only; native Android is deferred. No new production
dependencies. Applications author Go only; renderer/host library code owns the
platform implementations. Preserve the existing dirty work and do not commit.

## Ownership

- Go services worker: `device/notifications`, `device/sharing`, `device/audio`,
  `nativeui/**`, new `behavior/task*`, and `docs/device-services.md`.
- UIKit worker: `render/ios/**`, including the reusable native service runner.
- WebView worker: `render/webview/**`, using the same typed command boundary.
- Notes worker: `examples/notes/**`, including real repository paging, change
  observation, persistence, and Go screen/service composition.
- Parent: CLI/bootstrap/host glue, browser acceptance, integration, roadmap and
  cross-cutting documentation. Do not install Android tooling or add demos.

## Typed service requests

New packages expose ordinary Go structs with camel-case JSON tags:

- `notifications.Request{Operation, Identifier, Title, Body, Route, Date, Time string}`.
  Operations are `schedule`, `cancel`, and `status`. Schedule uses canonical
  `YYYY-MM-DD` and `HH:MM` in the device's local calendar/timezone, not Go's
  assumed timezone. The adapter rejects past/invalid local dates. Identifier is
  stable, bounded and scoped by the application. Route is a registered local
  route. Permission is requested only after an explicit schedule action.
- `sharing.Request{Title, Text string; Files []files.Document}`. At most four
  documents, each within existing file limits, combined decoded bytes <=1 MiB;
  text <=64 KiB. Native shares use temporary private files with cleanup.
- `sharing.Clipboard{Text string}` is write-only, explicit user action; <=64 KiB.
- `audio.Request{Title string; MaxSeconds, MaxBytes int}`. Recording is user
  initiated, maximum 60 seconds/1 MiB; zero means the documented defaults.
  `Validate()` and `Accept(files.Document, durationMillis int)` verify bounds,
  supported audio signatures/media types, and nonempty content. Accept bounded
  AAC/M4A, WebM/Opus, Ogg/Opus, and WAV where actually supported. Never trust a
  filename or MIME assertion alone. No speech recognition or background recording.

Each request has `Validate() error`; notifications validates policy/shape in Go
and actual local scheduling time in the host. Keep immutable request values and
avoid arbitrary OS paths, remote executable input, or reflection APIs.

`nativeui.ServiceRequest` has `ID string`, exactly one of
`Notification *notifications.Request`, `Share *sharing.Request`,
`Clipboard *sharing.Clipboard`, `Audio *audio.Request`, and `Commit bool`.
`Result.Service *ServiceRequest` carries it. `Route.Services` is
`map[string]ServiceRespond`; handler signature is
`func(context.Context, Request, ServiceResult) (Result, error)`.

`ServiceResult` has `Status EffectStatus`, optional `Message`, `Permission`,
`Identifier` strings, optional `File *files.Document`, and `DurationMillis int`.
Statuses retain existing completed/cancelled/denied/unsupported/failed meanings.
Only successful audio returns bytes. The runtime validates the result against
the exact admitted request and reauthorizes the source action/continuation.
Valid outcomes reach the declared continuation, including cancellation/denial,
so Go can render honest status. No cancellation path may commit an attachment.

## Transport and one-shot settlement

Add capabilities `notifications/v1`, `sharing/v1`, `clipboard/v1`, `audio/v1`,
and `live/v1`; preserve legacy `share/v1`. Add `Command.ServiceID`,
`Command.ServiceResult`, and `Reply.ServiceID` with camel-case tags. Runtime
issues opaque `serviceId` on the observed `complete` response. Adapters execute
once, then send `kind: service-result` with a fresh request ID, opaque service
ID, current values/revision, and the observed result; finish with `complete`.
Only read-only completion recovery may repeat after uncertain transport.
Never repeat an OS share, clipboard write, recording, or notification schedule
to recover a missing response. Share the connection foreground owner with
presentations/files. Preserve local inputs, caret, IME and dirty drafts.

UIKit owns an ordinary reusable `GofastrServiceRunner` in `render/ios`, with:

```swift
public init(presenter: UIViewController, openRoute: @escaping (String) -> Bool)
public static func capabilities() -> [String: String]
public func perform(requestJSON: String, completion: @escaping (String) -> Void)
public func cancel()
```

JSON passed here is the admitted `ServiceRequest` value (no Go binding), and
completion JSON is `ServiceResult`. Calls and completion are on the main thread;
bounded file/encoding I/O runs off-main. Native renderer integration also uses
its existing foreground coordinator. Publish static capability facts, never a
permission grant. Missing microphone purpose text means audio is unavailable.

For hosted WebView, the parent installs trusted-main-frame
`window.webkit.messageHandlers.gofastrServices` as a reply-capable handler.
It accepts `{kind:"capabilities"}` or `{kind:"perform",request:ServiceRequest}`
and resolves a decoded object. The WebView adapter may also accept a
library-owned `window.__gofastrDeviceServices` object with async `capabilities()`
and `perform(request)` for other hosts/test shells. No app-authored JavaScript.
Plain browser fallbacks: rich Web Share only when available, explicit downloads
otherwise (do not claim actual sharing); clipboard only after explicit click;
MediaRecorder only when supported and secure; scheduled reminders unsupported,
never an in-page timer pretending to be a system notification.

## Audio playback

Saved audio uses the existing private attachment store. `audio.Document` has
`File files.Document` and `DurationMillis int` (JSON `file`, `durationMillis`).
Add optional `Attachment.Audio *audio.Document` with bounded bytes for
an explicit play/pause/seek preview; do not use an arbitrary remote URL. Keep
platform players local and stop/release them on departure/interruption.
The Notes attachment detail screen exposes playback and export/share actions.

## Live collections and task state

Add `Screen.Live *Live`, `Live{Version string}`, and `Route.Watch Watch`, where
`Watch` is `func(context.Context, Request, string) (string, error)`.
`Command.DataVersion`, `Reply.DataVersion`, and `Reply.Changed bool` support a
read-only `watch` command. It observes the current authorized route/query and
is cancellable/bounded (no longer than 10 seconds). No mutation or navigation
is triggered by a notification. Release the session lock while waiting, reject
obsolete/closed instances on completion, and never occupy the UI thread.

Adapters watch only the visible live list, cancel on retirement/navigation,
and refresh using current query controls when the token changes. Coalesce
replaceable updates, never writes. Preserve current valid selected IDs and
scroll; do not replace an editor buffer. Surface loading/error/retry/cancel
states for collection fetches, with cancellation applying to read work only.
New `behavior.Task` supplies deterministic generation/state transitions for
replaceable asynchronous reads; unknown mutation outcomes remain nonretryable.

Notes must page/filter/order in its owner-scoped repository, not fetch 200 and
slice. Keep bounded per-request/per-screen rows and explicit Load more; prove
records beyond 200 are reachable. Add a process-local version/change hub with
cancellable waiting; bump only after successful writes, including metadata and
attachments. Versions use an epoch so restart cannot reuse a previous token.
Do not claim remote sync, durable background execution, or atomic bulk writes.

## Acceptance

- One Notes app: schedule/cancel/status, note and attachment share/copy, record
  and persist/reopen/play audio, >200-record paging and live data refresh.
- Public command E2E: foreign/stale IDs, duplicate settlement, denied/cancelled/
  unsupported outcomes, bounds/invalid audio, stale reads, interrupted work,
  no automatic OS/write replay, dirty-input preservation.
- Real browser acceptance with explicit fake device adapter for deterministic
  OS outcomes; label these protocol tests, not real microphone/notification
  acceptance. Test plain-browser unsupported behavior separately.
- Fresh iOS bind/link/run and actual feasible simulator UI; physical microphone,
  notifications, provider/share completion, VoiceOver/IME remain unverified
  unless actually exercised. Android native remains deferred.
