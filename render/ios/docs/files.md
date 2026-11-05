# UIKit typed files adapter

`GofastrFiles.swift` contains the optional `files/v1` wire model, bounded file
access, filename sanitation, result encoding, and one-shot delivery state.
`GofastrFileController.swift` owns `UIDocumentPickerViewController` delegates.
`GofastrForeground.swift` coordinates one file task or presentation across every
controller sharing the host, preserving the originating editor and focus through
presentation/file chains. Source files remain ordinary embedded Swift library
code; no gomobile/bootstrap changes or dependencies are required.

## File cards and editor navigation

`GofastrAttachments.swift` decodes optional `screen.attachments` and supplies
Dynamic Type file cards with full-row controls and stable accessibility IDs
`native.attachment.<id>`. Cards contain metadata and a local destination, never
file bytes/paths. The note composes cards below its body; returning refreshes
cards and photos without replacing text controls, the baseline, or the session.
Plain-text detail previews are supplied by Go, not parsed by the renderer.

Direct card navigation blocks pending/unknown saves and requires a successful,
durably stored checkpoint before leaving a dirty editor. Go action-driven
forward navigation enforces the same draft requirement in the shared runtime.
Neither path saves the note or discards newer text as a side effect.

## Wire and lifecycle

- `MobileResponse.files` and `MobileReply.fileId` are optional. Old responses and
  replies still decode unchanged; a legacy backend gets an explicit unsupported
  error rather than attempting a local continuation.
- Exactly one `pick`, `export`, or optional `photo` is accepted. Photo acquisition
  shares the file result/lifecycle; see [photos.md](photos.md) for its bounded JPEG,
  camera permission, metadata, and device-acceptance contract. Pick uses
  `forOpeningContentTypes:asCopy:true`; export uses `forExporting:asCopy:true`.
  Extension filters are normalized and checked again after selection.
- Only the opaque `fileId` returned by read-only `complete` identifies a task.
  The app-authored `files.id` is not a path, action, or runtime identity.
- Each delegate/preparation outcome writes `file-result` once with a fresh
  request ID, that opaque `fileId`, `fileResult`, and current values/revision.
  Outcomes are `completed`, `cancelled`, `denied`, `unsupported`, or `failed`.
  Import completion contains only `{name,mediaType,data}`. Export completion
  returns no bytes, URLs, or destination paths.
- Writes are never retried, even after uncertain transport failure. The existing
  read-only `complete` settlement handles results and continuation effects.
  Picker dismissal/transition completion precedes continuation application and
  focus restoration. Foreground work blocks leaving and new actions, without
  rebuilding fields, replacing marked text, or clearing local editing buffers.
- Cancellation and adaptive dismissal are de-duplicated against selection and
  read completion. A permission error maps to `denied`; unsupported file kinds
  and extension mismatches map to `unsupported`; local I/O/size failures map to
  `failed`. UIKit exposes cancellation/selection delegates, not a general file
  provider failure callback: provider errors handled inside its UI cannot be
  independently classified by this adapter.

## Bounded prototype handoff

Imports and exports are capped at **1 MiB (1,048,576 bytes)**. This prototype
hands the entire bounded buffer to Go as base64, not chunks; base64 adds roughly
one third to the payload size. A pick's smaller `maxBytes` is also enforced
(zero allows only empty files). Invalid limits/requests fail locally.

Security-scoped access, coordinated reading, MIME metadata lookup, and base64
encoding run on a serial background file queue. Import reads at most
`maxBytes + 1` bytes, never relying on provider-reported file size. Directories
and symlinks are rejected. The selected name is a sanitized last path component;
MIME is best effort with an `application/octet-stream` fallback. No arbitrary
file URL/path is included in commands. Export validates bounded base64 before
writing into a unique private temporary directory, sanitizes the leaf name, and
removes the directory after picker settlement/dismissal or preparation failure.

## Verification and next time

Run `GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./cmd/gofastr-mobile ./render/ios`
and `GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./cmd/gofastr-mobile ./render/ios`.
The command package typechecks all bundled UIKit sources without Go bindings;
the renderer package executes actual Swift DTO, bounded I/O, filename, export,
permission mapping, and duplicate-callback state regressions on macOS. Source
contract checks guard shared foreground ownership, one-write/read-only-settle
ordering, optional DTOs, and the existing system-alert delegate fix.

On November 5, 2026, a fresh bound simulator build displayed the synthetic private
`files-qa.txt` card and opened its native detail/text preview via accessibility. The fixture
entered through the public Go file-result protocol, not a picker selection.
Fresh provider import/export GUI acceptance remains incomplete: coordinate
control returned `noWindowsAvailable`, while toolbar actions were missing from
the available accessibility tree. This does not replace the earlier acceptance
or prove that the current provider UI works.

Next time, exercise a fresh bind/link/launch on simulator or device before
claiming picker acceptance: test local/iCloud/provider import, export completion
and cancellation, oversize input, permission failure, presentation/file chains,
dirty-editor IME/caret retention, and navigation blocking. Automated typechecking
and Foundation checks do not establish those UIKit/provider behaviors. Never
assign an adaptive `presentationController.delegate` to `UIAlertController`;
that runtime assertion remains guarded in the presentation adapter.
