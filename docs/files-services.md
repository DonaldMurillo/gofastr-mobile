# Typed mobile file services

The single Notes application now demonstrates **import, export, and private
attachments** using shared Go screen/action definitions. UIKit and the mobile
WebView renderer consume the same `nativeui.FileRequest`. Application authors
write no JavaScript, Swift, HTML, CSS, or screen templates. The separate
[photo capability](photos-services.md) also uses this lifecycle and private
storage; native Android remains deferred.

## Application API

An action returns a picker request and the route registers its typed Go
continuation, following the same model as presentation behaviors:

```go
nativeui.Route{
    Path: "/files",
    Screen: renderFiles,
    Actions: map[string]nativeui.Handle{
        "import": func(context.Context, nativeui.Request) (nativeui.Result, error) {
            return nativeui.Result{Files: &nativeui.FileRequest{
                ID: "import", Commit: true,
                Pick: &files.Pick{Title: "Import note", Extensions: []string{".txt", ".md"}, MaxBytes: files.MaxBytes},
            }}, nil
        },
    },
    Files: map[string]nativeui.FileRespond{
        "import": importDocument,
    },
}
```

`importDocument` receives `nativeui.FileResult` containing a validated
`*files.Document` (`Name`, `MediaType`, `Data`). It still validates actual content
and authorizes its business mutation; filename filters and MIME labels do not
prove content safety. Cancelled/denied/unsupported/failed tasks never invoke the
continuation. `Commit` serializes the mutating continuation, not the time the OS
picker is open. Unexpected commit errors are unknown outcomes, never automatic
retries.

Export returns `FileRequest{ID: "export", Export: &document}`. No continuation is
required: export is a renderer side effect, not a repository write. Exporting a
note uses the latest submitted buffer without saving or clearing its draft.

`device/files.Store` supplies `Put`, `List`, `Read`, and `Delete`. Stored `File`
metadata contains an opaque random handle, filename, media type, and byte count,
not a device path. Reads/deletes require the original application collection.
Notes verifies note visibility through its owner-scoped service **before each
attachment operation**, and groups attachments by saved note ID. Attachment
storage is separate from the Notes repository, not an alternate entity pipeline.

## File cards in a screen

`Screen.Attachments` displays metadata and a registered local detail destination:

```go
nativeui.Attachment{
    ID: file.Handle, Name: file.Name, MediaType: file.MediaType,
    Size: file.Size, Destination: detailRoute,
}
```

These accessible, full-row controls supplement a form or text screen; they are
not another list-screen model. Each screen accepts at most 64 cards, with unique
nonempty IDs of at most 128 bytes, validated leaf filenames/MIME types, and sizes
between zero and 1 MiB. They contain no file bytes or device paths. Destinations
must resolve to a registered route, whose handler still authorizes access.
Photos with valid previews use `Screen.Images`; other attachments use file
cards. Storage alone does not add either representation to a screen.

## Renderer and runtime boundary

- `files/v1` advertises compiled support, not a permission grant. UIKit supplies
  document pickers; WebView explicitly advertises a browser fallback.
- Runtime issues an opaque `fileId` during read-only `complete` settlement.
  `file-result` requires the issuing connection, screen, request identity, and
  that exact pending ID. There is one foreground slot per connection, shared
  with alerts/dialogs/sheets. Close/expiry releases it.
- Accepted results are deduplicated by request identity plus content fingerprint.
  Only `complete` may be retried to observe settlement. Changed input under an
  existing identity is rejected; an accepted continuation never executes twice
  within the retained session. This is not crash-proof exactly-once delivery.
- UIKit reads/copies selected files off the UI thread and releases temporary
  access. Only bounded bytes and metadata cross the Go boundary. No paths, URLs,
  or arbitrary filesystem permissions are handed to application screens.
- Browser selection/download needs an explicit user gesture after the Go
  response. The library supplies that dialog; a browser download means
  **download initiated**, not independently verified external persistence.

All screen definitions and adapter assets remain bundled. This file transfer is
local, through gomobile or the existing in-memory WebView transport; it does not
add remote executable presentation or upload attachments to a server.

## Notes workflows

- **Notes → Files → Import note:** choose a UTF-8 `.txt` or `.md` file. The first
  line becomes its title (an initial Markdown `# ` is removed); remaining text
  becomes the body. Empty, binary, oversized, or invalid notes do not save.
- **Note → Export:** export the current title/body as Markdown, including
  unsaved edits. Cancelling leaves the note and draft unchanged.
- **Saved note → More → Add file:** attach a file directly, then see its filename,
  type, and size below the note body. **More → Attachments** opens the full list.
  Opening a card shows its detail screen, export, and removal confirmation.
- Text attachments show a literal UTF-8 preview, limited to the first 16 KiB at
  a valid character boundary. Markdown/HTML is not executed. Binary files show
  metadata and export; export always uses the original stored bytes.
- Opening Attachments or a file card preserves unsaved note text. Forward
  navigation requires a durably stored draft and no pending/unknown save;
  failure reports a visible error instead of silently refusing navigation.
  Returning refreshes file cards/photos without replacing editor controls.

## Deliberate prototype limits

- Single-file operations, **1 MiB maximum** per transfer. This first slice uses
  bounded base64 handoffs, not streaming or a chunk protocol. Large media needs
  a separate handle/stream design rather than increasing this bound blindly.
- Attachment storage: **16 MiB of content / 64 files per installation**. Metadata
  and base64 storage overhead are additional. Atomic file replacement, private
  directory/file modes, root-confined opens, collection checks, and random
  handles are implemented. This is sandbox storage, **not encryption at rest**.
- One shared `Store` per installation process; quotas are process-coordinated,
  not a multi-process transaction/lock service. Disk access is synchronous Go
  work, never work for the native UI thread.
- No cloud sync, arbitrary filesystem browsing from Go, background transfer,
  persistent OS bookmarks, or cross-launch deduplication of import mutations.
- Extension/MIME labels are advisory. Notes performs UTF-8/content validation;
  future consumers must provide their own parsers and safety policy.
- Device-provider errors and real-device signing/permissions still need physical
  device testing. Consult renderer-specific notes under `render/*/docs/files.md`
  and the verification evidence in `docs/agent-notes.md`.

## Verification evidence

- `go test ./sim -run TestNotesFilesWebView -count=3` passes for both iOS and
  Android styling through the real in-memory bridge. The test imports a file,
  verifies actual downloaded unsaved Markdown bytes, exercises attachment
  add/export-cancel/remove, verifies visible cards and text previews, keeps dirty
  note text across navigation/direct attachment, and checks 200% text size.
- Earlier acceptance (November 4, 2026): a fresh gomobile bind/link/install/launch
  on the iPhone 17 Pro simulator (iOS 26.5) opens real UIKit document pickers.
  Cancellation returns to Notes;
  exporting an unsaved editor preserves its buffer/draft; importing that
  exported Markdown returns a saved editor with the same title/body.
- File-card acceptance (November 5, 2026): a native rebuild displays an existing
  private attachment and opens its detail/text preview through accessibility.
  This fixture was added through the public Go file-result boundary, not the OS
  picker. Fresh picker/export GUI acceptance is incomplete: Simulator coordinate
  control failed with `noWindowsAvailable`, and toolbar items were absent from
  the available accessibility tree. Do not count this as a provider-flow retest.
- `examples/notes/notesnative/files_test.go` covers the public binding boundary,
  task identities, duplicate results, attachment persistence across restart,
  invalid input, and shared file/presentation ownership. Renderer tests also
  exercise Swift DTOs/bounded I/O and browser settlement/recovery.
- These checks do not establish physical-device/provider, VoiceOver/IME, or
  actual WKWebView/Android WebView chooser/download acceptance. Chromium's
  Android styling is not native Android validation. Native Android remains
  deferred; physical-camera acceptance is tracked separately in
  [photo services](photos-services.md).
