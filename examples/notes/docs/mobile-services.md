# Notes mobile service and live-data contract

The application code is Go-only. UIKit and WebView libraries execute the typed
requests; native Android remains deferred. Opening a screen, restoring a draft,
or saving reminder fields never requests OS permission. A saved reminder date
and time are **not** evidence that a notification was scheduled.

## Actions and continuations

| Route | Action IDs | Service continuation IDs |
| --- | --- | --- |
| `/notes/new` | `save`, `share`, `copy`, `export` | `share`, `copy` |
| `/notes/{id}` | `save`, `share`, `copy`, `export`, `attach`, `attachments`, `record`, `reminder-schedule`, `reminder-cancel`, `reminder-status` | `share`, `copy`, `record`, `reminder-schedule`, `reminder-cancel`, `reminder-status` |
| `/notes/{id}/files` | `attach`, `photo`, `camera`, `record` | `record` |
| `/notes/{id}/files/{handle}` | `share`, `export`, `remove` | `share` |

Existing file continuations remain `attach`, `photo`, `camera`, `export`, and
`/files` → `import`; remove is the existing confirmation choice `remove`.
`ServiceRequest.ID` is the continuation ID above, **not** the opaque one-shot
`Reply.ServiceID` transported as `Command.ServiceID` on `service-result`.
Settle the observed result with a fresh request ID, then read `complete`.
Repeated read-only completion/result recovery cannot repeat the OS operation.

Editor form keys remain `title`, `body`, `pinned`, `tag`, `reminder_date`, and
`reminder_time`. Notification scheduling forwards canonical `YYYY-MM-DD` and
`HH:MM` strings unchanged for interpretation in the device's local calendar.
Schedule uses the current reminder controls but does not save those controls
or dirty text; choose Done to persist them separately. Cancel and Status use
only the stable identifier and do not request permission or clear metadata.
Notification titles come from the saved note; the destination is `/notes/{id}`.
`Service.ReminderID` authorizes the note and hashes the application namespace,
authenticated owner and note ID into a bounded `notes.reminder.<sha256>` ID.

Sharing uses `sharing.Request` with text and a Markdown `files.Document` for a
note, or the original typed document for a file/photo/audio attachment. Copy is
an explicit `sharing.Clipboard` write. Both use the current editor buffer and
do not clear/save a draft. Text above 64 KiB is refused with an Export hint,
not silently truncated. File sharing inherits the service's four-document /
1 MiB decoded-total bound; Notes currently sends one document. Completion of a
share sheet is not claimed to confirm recipient delivery. Denied, cancelled,
unsupported and failed outcomes are surfaced as such, never as success.

## Recording persistence

Only saved notes expose `record`. Its request is bounded to **60 seconds /
512 KiB**. The lower byte limit reserves room for a single atomically persisted
attachment containing both audio bytes and the reported duration; it does not
change the public `audio.Request`, `audio.Document`, or file-store contracts.

At rest, the existing private attachment store holds one `files.Document` with
media type `application/vnd.gofastr.notes.audio+json` and this versioned payload:

```json
{"version":1,"audio":{"file":{"name":"recording.wav","mediaType":"audio/wav","data":"<base64>"},"durationMillis":1}}
```

The envelope is internal Notes metadata, never the exported/shared document.
The maximum encoded payload stays below the store's 1 MiB per-file limit.
`durationMillis` is the bounded host-reported recording duration, not a filename
inference or a claim of server-side media decoding. The exact admitted
`audio.Request.Accept` verifies content/container signatures, media type, byte
count and duration again before storage and when reopening the envelope.
Only completed, valid results can write. Cancellation never creates an item.
The detail route unwraps it to `Attachment.Audio` for local play/pause/seek and
unwraps it for share/export. List/editor cards show audio metadata without
embedding all audio bytes. Dirty title/body and their durable draft are kept
even when recording completes; attachment navigation is not a note save.
The private store remains bounded to 64 files / 16 MiB per installation.

## Paging, authorization and versions

`Service.Collection` requires the repository paging interface; it never calls
legacy `List` to fetch 200 and slice. The scoped CRUD pipeline filters the
owner, active/archived/trash view, optional tag, and search before applying SQL
ordering and LIMIT. Ordering is pinned first, then title ascending or update
time descending, then ID ascending as a stable tie-break.

Each read executes at most two scoped queries: one projected anchor lookup
when using a cursor, then at most **101 projected rows**, including one
lookahead. Body text and attachments are not fetched for collection rows.
There is no full count or deep OFFSET. Database search/sort cost can still
depend on stored data; this is a bound on queries/materialized rows, not a
claim of constant-cost SQL or a transactionally frozen collection snapshot.

Load more expands 10 → 100 rows within a window; at 100 it starts the next
keyset window at 10, so all records, including those beyond 200, remain
reachable without unbounded screen growth. Pass `List.Paging.Next` unchanged
as the `page` form value. It is an opaque, bounded safe token (numeric only in
the first window), bound to the query and a scoped anchor ID. Cursors use
GoFastr's ordinary bounded IDs (at most 64 bytes here). An unavailable or
filtered-out anchor requires restarting at page 1. Changes between reads are
not a database snapshot; refresh/reset paging when appropriate.

Collection keys are `search`, `view`, `tag`, `order`, `page`, `selected` (JSON ID
array), `row`, plus application-maintained `page_scope`, `collection_scope`,
and `undo`. Preserve the returned values when using current list actions.
Query changes reset paging/selection; Load more preserves still-visible
selections, and a new bounded window prunes IDs that are no longer displayed.
Every mutation rechecks current query membership and owner authorization.
Selection is at most 20; bulk operations/undo remain individually committed,
not atomic transactions.

`Screen.Live.Version` and `/notes`' `Route.Watch` use the same owner-scoped,
process-local hub. Tokens contain a random epoch and monotonic revision;
restart/eviction cannot reset a counter into an old epoch. At most 128 owners
are retained, with no retained write history. Waits use cancellation and a
10-second ceiling; the Runtime also retires obsolete watches. Versions bump
only after confirmed note, metadata/undo or attachment writes, not OS service
outcomes, failed writes, or duplicate settlements. Capture the version before
the list read so concurrent writes cannot be missed. This does not observe raw
out-of-band SQL, imply remote sync, or provide durable background execution.
Legacy web/terminal/native `List` callers retain their bounded 200-note API;
mobile screen collections use the repository cursor path instead.

## Parent acceptance fixtures

The shared-screen WebView composition explicitly allows only same-origin
microphone access in `Permissions-Policy` and private `blob:` media playback in
CSP. Camera/geolocation remain denied, and scripts/forms/frames retain the
strict policy. This does not grant microphone permission: recording requests it
only after the user presses Record. Legacy web and service-only/native builders
retain their defaults. `application.Open` accepts optional framework options so
the selected presentation can configure its host without importing UI there.

The live hub belongs to the shared `notes.Service` instance, not to SQLite or
an automatic database subscription. Writes through another freshly constructed
service or direct SQL do not automatically notify this composition. Acceptance
therefore mutates through an independent connection to the same command runtime;
remote/cross-process synchronization remains out of scope.

- Seed 215+ equal-title notes; drive each observed Paging.Next until exhaustion
  for both title and recent ordering. Assert ≤100 displayed rows, stable ID
  ties, foreign-note exclusion, and an actionable note beyond record 200.
- Explicit fake device results should cover completed/denied/cancelled/
  unsupported reminder outcomes, stable reminder IDs across restart, copy,
  rich note share, and ordinary/photo/audio document sharing. Plain browsers
  should honestly report unsupported notifications rather than prompt/claim
  scheduling. These are protocol tests, not actual notification acceptance.
- For deterministic recording settlement, use a valid bounded WAV fixture and
  a positive bounded reported duration. Reject forged service IDs, bad MIME/
  signatures, >60-second or >512-KiB results, and cancellation carrying bytes.
  Duplicate settlement must leave exactly one stored attachment.
- Type a dirty title/body before recording and again while the service is
  pending. Complete, restart, reopen both note and attachment, then verify the
  draft, original persisted note text, playback document and duration.
- Watch only the visible list; mutate from another connection and refresh
  without changing the editor buffer. Close/navigation must retire a pending
  read; a watch with changed unobserved controls must fail as stale.

Physical microphone capture, OS notification delivery, provider/share delivery,
VoiceOver, IME and actual platform playback remain unverified unless separately
exercised by the parent/platform workers.
