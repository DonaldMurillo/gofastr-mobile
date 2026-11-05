# WebView device services, bounded audio and live collections

This is library-owned renderer behavior. Applications declare Go
`nativeui.ServiceRequest`, `Screen.Live` and `Attachment.Audio` values; they do
not supply scripts. The typed Go runtime remains responsible for authorization,
the admitted service union, audio signatures, and one-shot continuations.

## Hosted adapter

The parent bootstrap supplies a trusted-main-frame, reply-capable
`window.webkit.messageHandlers.gofastrServices` handler. The library calls:

```js
await handler.postMessage({ kind: 'capabilities' });
await handler.postMessage({ kind: 'perform', request });
```

Both resolve decoded objects, not JSON strings. Capabilities return a map from
`notifications/v1`, `sharing/v1`, `clipboard/v1` and `audio/v1` to
`available`, `fallback` or `unavailable`. `live/v1` is renderer-owned.
Discovery is permission-free and bounded to two seconds; unknown/failed host
facts are unavailable. A discovered host is not silently replaced with a browser
fallback after a device operation fails.

Other hosts and parent-owned deterministic browser tests may supply
`window.__gofastrDeviceServices` with async `capabilities()` and
`perform(request)` methods. This explicit adapter takes precedence over WebKit.
It is optional, not an application-authoring requirement or production proof of
OS permissions. Hosted calls must execute through the real UIKit runner. The
renderer does not implement fake native reminders or recording results.
Install either host before the first renderer hello; capability discovery is
cached for that connection and does not probe permission grants dynamically.

`foreground.js` owns the connection's single local foreground lease; services,
presentations and files all acquire/release it. Opening one cannot replace
another type's task. Service dialogs use semantic, labelled modal controls and
plain text, contain focus, and restore the original opener after completion.
Native UI owns cancellation while a host `perform` call is in progress. The
canonical bridge has no synthetic JavaScript `cancel` message.

## Settlement and recovery

The public sequence is `action → complete → service-result → complete`.
Only the observed `complete` supplies the opaque `serviceId`; the declared Go
continuation key is never used as that identity. The service-result command gets
a fresh request ID and the latest form values/revision. The renderer executes a
device operation once and never offers a second perform for an issued service.
Cancellation, denial, unsupported and failure results all reach Go; only accepted
successful audio carries a file and duration. IME/caret and newer editor buffers
are not replaced by older completion data.

After an uncertain result write/receipt, **Check completion** only repeats
`complete` for the original result request ID, with current values/revision. It
never retries `perform`, resends the service result, or dispatches the source
action. A definitive rejected settlement remains non-replayable and displays
the error; it is not converted into another mutation attempt. The existing
legacy `share/v1` path stays compatible, but no longer writes to the clipboard
implicitly when sharing is unavailable.

## Plain-browser behavior

- Reminders are unavailable. Continue settles `unsupported`; no page timer or
  Notification API pretends to schedule a system reminder.
- Rich sharing uses Web Share only from **Share…**, including files only when
  `canShare` accepts the actual files. Cancellation is distinct from failure.
  Otherwise each **Download filename** explicitly initiates a bounded Blob
  download. **Finish downloads** reports `unsupported`, not successful sharing;
  external disk persistence cannot be confirmed. Shared text can be explicitly
  downloaded as `shared-text.txt`. At most four input files / 1 MiB combined and
  64 KiB text are accepted; no remote file URLs or arbitrary paths are used.
- Clipboard writes occur only from **Copy text**, never load/discovery or share
  fallback. Unsupported access exposes a readonly selectable textarea. Denial
  is not labelled as successful copying; clipboard content is never read.
- Recording requires a secure context, `getUserMedia`, supported MediaRecorder
  audio MIME, bounded Blob conversion and object-URL support. **Record** alone
  requests permission. Pause/resume/stop lead to a native-controls preview;
  **Accept recording** is the only successful byte-bearing outcome. The hard
  wall-clock limit includes pauses and is at most 60 seconds; accumulated chunks
  must stay within the request's byte limit, at most 1 MiB. Exceeding bytes fails
  without attachment; the time limit stops for preview rather than auto-accepts.
  Track end/mute, recorder error, backgrounding, pagehide and retirement stop and
  clean up browser capture. Late permission grants are immediately released.
  Cancel discards preview/chunks and never commits an attachment. Go validates
  the accepted container/signature independently; browser checks are not a codec
  sandbox or a promise that every browser can encode every supported Go format.

Saved `Attachment.Audio` is hydrated from bounded private bytes, never a remote
URL. It uses an `<audio controls preload="none">` element for explicit
play/pause/seek, with an accessible attachment label and no autoplay. Competing
players pause. Background/departure revokes object URLs, pauses/releases players
and removes recording callbacks. A retained foreground page can recreate its
saved local source, but playback never restarts automatically.

## Live collection read lifecycle

`live.js` watches only the current connected, visible, live list and never an
editor (even a mixed form/list definition). Watch commands send
`dataVersion: Screen.Live.Version` and current query values without selection
operands. A changed reply causes a replaceable read-only `open` of the current
query, not an action or automatic navigation. The new screen instance authorizes
the newly rendered rows; the old instance is closed. Query changes, refresh and
paging also use this cancellable read path for connected non-editor collections.
The initial protocol open also hydrates rows from its authoritative snapshot,
so cached navigation HTML cannot claim the current watch version with old rows.

The renderer checks generation, instance/query identity, current state and
cancellation both before admission display and before committing read results.
Late unused open instances are closed. Live updates reconcile valid selected IDs
and preserve current query controls, DOM form/editor buffers, focus where the row
survives, and scroll. Rows retain list/heading/link/disabled/menu semantics, and
actions continue using stable row IDs. Only replaceable reads are coalesced.
Hidden query/page values are updated from the admitted read and prior row-action
operands are cleared, so later commands cannot target the preceding page/row.
New opaque hidden values are inserted, removed values are retired, and selection
toolbar actions are replaced from that same snapshot. Archive/Undo therefore
adopts the current receipt and visible action together rather than refreshing
only row text.

AbortSignals reach `fetch` or the parent-owned `__gofastr_transport`. The parent
transport should honor them. Suspension/retirement never awaits a long poll;
leave/action/close proceed independently. The Go runtime also cancels obsolete
watches and releases its session lock while waiting (bounded to ten seconds).
Hidden pages suspend; visible retained pages resume. **Retry read** and **Cancel
read** affect reads only. Errors do not become mutation retries. A retry reads
the current query; after failed paging, Load more remains available explicitly.

## Stable parent acceptance selectors

| Selector | Meaning |
| --- | --- |
| `[data-gfw-service="notification\|share\|clipboard\|audio"]` | Singleton service dialog; choose one literal kind, not the pipe string. |
| `[data-gfw-service-id]` | Runtime-issued opaque identity. |
| `[data-gfw-service-status]` | Accessible polite/assertive feedback. |
| `[data-gfw-service-perform]` | Explicit native perform, browser share/copy, or unsupported Continue/Finish downloads. |
| `[data-gfw-service-cancel]` | Cancel before settlement; disabled after one-shot consumption. |
| `[data-gfw-service-complete]` | Normally hidden, read-only Check completion. |
| `[data-gfw-service-text]` | Readonly clipboard fallback text. |
| `[data-gfw-service-download]` | Explicit per-file/text download. |
| `[data-gfw-service-download-link]` | Ephemeral Blob download anchor, exempt from app navigation. |
| `[data-gfw-audio-record]`, `[data-gfw-audio-pause]`, `[data-gfw-audio-resume]`, `[data-gfw-audio-stop]`, `[data-gfw-audio-accept]` | Browser recorder actions. |
| `[data-gfw-audio-preview]` | Uncommitted native audio preview controls. |
| `[data-gfw-audio-playback="attachment-id"]` | Saved attachment native controls. |
| `[data-gfw-audio-pending]` | SSR hint before private playback hydration. |
| `[data-gfw-screen][data-gfw-live-state]` | `idle`, `watching`, `loading`, `error` or `cancelled`. |
| `[data-gfw-live-controls]`, `[data-gfw-live-status]` | Read-only state controls and accessible feedback. |
| `[data-gfw-live-retry]`, `[data-gfw-live-cancel]` | Retry/cancel a read, never a mutation. |
| `[data-gfw-collection-content]` | Replaceable rows/paging/footer, excluding query controls and form. |

`data-gfw-contract` exposes negotiated facts; `data-gfw-capabilities` includes
them after opening. Existing row, selection, filter, query, tab and file markers
remain unchanged.

## Checks and limits

```sh
for source in render/webview/*.js; do node --check "$source"; done
node --test render/webview/*.test.cjs
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test -race ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./render/webview
```

Node tests use dependency-free DOM/device primitives and transport fakes. Go
tests exercise the public HTTP command boundary, typed service outcomes, opaque
identity/current values, asset delivery, private audio markup and concurrent
watch/leave cancellation. These are not real microphone, audio decoding,
notification, share-provider, physical device, VoiceOver or IME acceptance.
The parent owns real-browser E2E and the hosted bootstrap. Native Android,
background recording, speech recognition, remote sync and durable background
work are deferred/not implemented here.
