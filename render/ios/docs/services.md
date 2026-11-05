# UIKit device services and live collections

Implementation notes, November 5, 2026. These are library/protocol verification
notes, not real microphone, notification, or share-provider acceptance results.
The frozen cross-worker contract is `../../../docs/mobile-services-plan.md`.
Android native remains deferred. All imports are Apple system frameworks.

## Reusable public runner

`GofastrServiceRunner` is ordinary Swift source, embedded unchanged by
`sources.go`, with no gomobile binding imports or application-specific code:

```swift
public init(presenter: UIViewController, openRoute: @escaping (String) -> Bool)
public static func capabilities() -> [String: String]
public func perform(requestJSON: String, completion: @escaping (String) -> Void)
public func cancel()
```

Pass the admitted `nativeui.ServiceRequest` JSON, not a transport command or the
application's declared continuation ID. Completion is `ServiceResult` JSON.
Calls are main-thread operations; off-main perform/cancel calls are marshalled
to main, and completions return on main after owned UI dismissal and temporary
share-file cleanup. Static capabilities are availability facts, never permission
grants. A nonempty `NSMicrophoneUsageDescription` is required for `audio/v1`.
Retain the runner until completion and cancel it when its host is retired.

The runner bounds JSON to 4 MiB of UTF-8 and serializes decoding, share-file
I/O, recorded-file reads, and encoding work off-main. Each invocation settles
once; cancelled preparation and late permission/provider callbacks cannot invoke
the continuation again. Concurrent perform calls fail instead of replacing an
active task. Only completed audio includes `file` and `durationMillis`.
Outcome messages are bounded to 512 UTF-8 bytes with control characters removed.

## Native transport ownership

The host negotiates notifications/sharing/clipboard/audio availability alongside
legacy `share/v1` and `live/v1`. `MobileForegroundCoordinator` reserves the same
slot for presentations, files/photos, and services, and retains input/VoiceOver
focus between chained foreground tasks. The native renderer consumes the opaque
`serviceId` issued by `complete`, performs the OS operation once, submits a fresh
`requestId` with `service-result`, and observes it through `complete`. Never use
`ServiceRequest.id` as the transport identity. Never repeat a service-result
write or OS operation to recover an uncertain result. Existing read-only
completion recovery remains the only retry path. Per-screen observed service
IDs prevent duplicate OS execution.

Editor controls are not reconstructed on service return. Current values and
revision accompany settlement; dirty buffers, marked text, caret and disabled
field values retain the existing renderer policy. Device work is not a reason
to clear editor dirty state or commit a cancelled attachment.

## Reminders and notification taps

Schedule parses exact Gregorian `YYYY-MM-DD` / `HH:MM` in `TimeZone.current`.
Round-tripping the calendar components rejects invalid dates and nonexistent
DST wall times. A time must be strictly future both before permission and after
the permission sheet returns. Repeated fall-back times use Calendar's selected
occurrence. Requests are one-shot `UNCalendarNotificationTrigger`s; permission
is requested only by schedule. Status and cancellation do not prompt. Status
returns permission plus `scheduled`/`not scheduled`; cancellation removes the
identifier's pending and delivered notifications. Identifiers use a private
`gofastr.notification.` prefix in this app's notification-center namespace.
If cancellation arrives while UN is adding a request, settlement waits for that
callback and removes the accepted request before reporting cancellation; a late
add cannot silently leave a reminder scheduled after a cancelled result.

The process-wide `MobileNotificationRouter` owns the UN delegate, forwards
unrelated notifications to the prior delegate, and routes only default taps
with a validated local `gofastr.route`. Foreground delivery shows a banner and
sound, never navigation. Warm and cold taps use the same `openRoute` callback;
when the callback returns false, the most recent tap is retained and retried
after activation, renderer appearance, or foreground settlement. Nothing is
opened merely because a reminder fires. Native renderer construction installs
the delegate and its existing `openURL(_:)` path handles queued startup routes
and dirty-editor navigation policy.

## Sharing and clipboard

Sharing accepts at most four files, each at most 1 MiB, combined decoded bytes
at most 1 MiB, and text at most 64 KiB. It rejects malformed base64 and unsafe
filenames. Share files live in an operation-private 0700 temporary directory
with 0600 files; case-insensitive duplicate names are uniquified. Completion,
failure, explicit cancellation, and late cancelled preparation all clean up
temporary files. `UIActivityViewController` completion distinguishes completed,
cancelled, and failed; presenting a sheet is not evidence of sharing. iPad
popover anchoring uses the presenter. Clipboard is write-only and is executed
only for an explicitly admitted clipboard request, including intentional clear.

## Recording and saved playback

The recording sheet has explicit Record, Pause/Resume, Stop, Play/Pause preview,
Use recording, and Cancel controls. Permission is requested on Record, not on
capability discovery. Recording is mono AAC in M4A (16 kHz / 32 kbps), capped by
the requested maximum (default 60 seconds / 1 MiB). Recorder duration and 100 ms
file-size monitoring bound acquisition; an oversized final file fails rather
than truncating or committing it. Final FileHandle reads are capped at max+1,
container signatures and duration are checked, and AVAudioPlayer must decode a
finite duration before Use recording is enabled. Only that explicit button
returns audio bytes. Pause does not commit or start another service.

Cancellation, audio interruption, and background departure stop recording and
preview, release the session, invalidate callbacks, and remove temporary files.
No background recording or speech recognition is configured. Files never pass
an OS path into Go. Request/attachment signature checks understand AAC/M4A,
PCM/float WAV, Ogg/Opus and WebM/Opus; decoding support is device-dependent, and
unsupported playback fails honestly instead of treating MIME as proof.

`Attachment.audio` uses the typed `{file, durationMillis}` document. Its card
has independent play/pause and seek controls; bounded bytes, not remote URLs,
initialize the player off-main. Departure, background and interruption release
players. Updating image/attachment metadata does not replace an editor session.

## Visible live lists

Only the visible, active application's live list can issue `watch`; forms and
hidden navigation/tab controllers do not watch. Watches have a separate serial
operation queue so the Go maximum-ten-second read cannot starve the existing
four-operation action/checkpoint pool. Cancellation invalidates a generation,
cancels queued work, and sends a read-only list checkpoint to cancel an executing
Go watch. No new command kind or arbitrary backend cancellation API is required.
Navigation, queries, actions, foreground tasks, retirement, and backgrounding
cancel observation. A cancelled/obsolete completion cannot refresh a new query.
Pending or uncertain editor/mutation outcomes also suspend observation; live
read retry must never become a path around an unknown mutation outcome.

Changed versions refresh using current query controls; only matching-generation
responses replace list data. The prior instance is closed after successful
replacement. Selection is intersected with valid returned row IDs, and scroll is
restored by first-visible row ID/offset (clamped old offset if the row vanished).
Loading, failed, cancelled, retry, and pause-live controls are exposed as
`native.list.status`, `native.list.retry`, and `native.list.cancel`. Cancelling a
fetch discards/ closes any late opened instance; it does not cancel a write or
erase existing items/selection. Errors never auto-replay an action. The initial
synchronous gomobile render/open has no transport cancellation primitive: its
late result is invalidated and closed, not falsely claimed to be interrupted.

## Parent bootstrap requirements

- Package `NSMicrophoneUsageDescription` through the parent's
  `--microphone-usage` option. Missing/empty text intentionally reports audio
  unavailable instead of requesting microphone permission or crashing.
- Hosted WebView bootstrap must retain one runner initialized early in launch
  with its actual presenter and validated local-route callback, install the
  trusted-main-frame reply-capable `gofastrServices` handler, and call the runner
  only for admitted user actions. Reply to `capabilities` without prompting, and
  decode completion JSON into the bridge response object. Cancel on retirement.
- Initialize the runner/native renderer during launch before notification
  response delivery; preserve its UN delegate (or forward responses to it).
  Return false from hosted `openRoute` while bootstrap/navigation is unsafe so
  the tap remains cached. Native startup and appearance flush that cache.
- Keep UIKit, AVFoundation, UserNotifications, and existing framework sources
  in the parent's Swift bind/link inputs. No production dependency is needed.

## Regression commands and limits

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./cmd/gofastr-mobile ./render/ios
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./cmd/gofastr-mobile ./render/ios
```

`TestServicesSwiftWireBoundsAndOneShotLifecycle` compiles and executes optimized
Swift Foundation code against actual Go-generated DTOs, checks local/DST/past
time validation, malformed/oversized transfers, private duplicate share files,
audio containers/duration/read bounds, failed-result byte stripping, one-shot
settlement, stale generations, and preserved valid selection. The Swift result
is decoded and accepted by the actual Go audio validator. Existing full-source
UIKit typechecking and optimized simulator-target library linking cover all new
source without Go bindings; a separate client module typechecks the public
runner interface. Offline AVAudioFile AAC/M4A encoding and AVAudioPlayer decoding
also execute and round-trip through Go's validator without recording a microphone.
These tests do not grant OS permission, record a real
microphone, tap a real notification, or finish a share provider. Parent E2E/GUI
acceptance must verify those, plus cold launch, VoiceOver, IME and real playback.

### Next-time lessons

- Scope: `ios/services`. Go omits false `ServiceRequest.commit`; decode it as
  optional while leaving required legacy wire flags strict. Next time, generate
  Swift fixtures from Go instead of assuming optional JSON flags are serialized.
- Scope: `ios/live`. A watch must not occupy the action/checkpoint worker pool;
  cancellation needs both generation invalidation and a Go checkpoint to release
  the long-poll wait. Next time, test stale callbacks before adding automatic retry.
- Scope: `ios/audio`. AVAudioRecorder writes may buffer until Stop; size polling
  alone cannot validate the final transfer. Next time, bound the final read and
  decode the actual container before exposing a successful recording result.
- Scope: `ios/services`. Swift JSON encoding can escape base64 slashes and
  control text, expanding an admitted 1 MiB file plus 64 KiB text beyond 1.6 MiB.
  Next time, bound the wire envelope separately from decoded content limits.
