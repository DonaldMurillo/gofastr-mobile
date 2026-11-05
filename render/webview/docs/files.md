# WebView files adapter

Applications return the same Go `nativeui.Result.Files` definition used by the
UIKit renderer. The library embeds `files.js` as `gfm-webview-files` and requires
it from `gfm-webview`; `gfm-webview-files` requires the library-owned
`photos.js` normalizer (`gfm-webview-photos`). No application JavaScript, paths, URLs, file-system handles,
new dependencies, or second application router are involved. See
[presentations.md](presentations.md) for the companion modal adapter.

## File cards and forward navigation

The renderer consumes optional `Screen.Attachments` as escaped metadata cards
below note fields/text. Stable selectors are `[data-gfw-attachments]` for the
group and `[data-gfw-attachment]` for a card. A fresh Go `open` response
reconciles these cards after Back/cache restoration; cached HTML alone is not
the current attachment state. Go supplies bounded literal text previews on the
detail screen; neither Markdown nor HTML is executed.

Effects that navigate forward pass an asynchronous retry callback to
`_leaveGuard.ok`. A dirty-editor confirmation can therefore finish its durable
checkpoint and resume the intended navigation. Without the callback, the first
attempt can be blocked with no later navigation. Pending/unknown save policy
and draft ownership remain shared Go responsibilities, not browser overrides.
The files E2E covers dirty Attachments navigation, direct attachment, visible
cards, literal preview, removal, and actual downloaded export bytes.

## Contract and admission

`response.files` contains `id` and exactly one operation:

```json
{"id":"import","pick":{"title":"Attach a note","extensions":[".txt"],"maxBytes":1048576}}
```

```json
{"id":"export","export":{"name":"note.txt","mediaType":"text/plain","data":"aGVsbG8="}}
```

```json
{"id":"photo","photo":{"title":"Choose photo","source":"library","maxBytes":1048576,"maxDimension":2048}}
```

`photo` is optional and mutually exclusive with `pick` and `export`. Its title is
a nonempty string, source is exactly `library` or `camera`, `maxBytes` is an
integer in 1–1,048,576, and `maxDimension` is an integer in 1–2,048. Photo uses
the same opaque issued `fileId`, `file-result`, foreground modal slot, and
complete-only recovery as the document operations. There is no photo-specific
write protocol. The parent-owned Go runtime must independently admit only actual
JPEG data with a `.jpg`/`.jpeg` filename, `image/jpeg` MIME, and the requested byte
and decoded dimension limits; browser checks do not replace that trust boundary.

The adapter opens only after observing `complete` with both `response.files` and
an opaque `fileId` issued by the runtime. The definition's `id` is a Go continuation
key, not a valid reply identity. The hello advertises `files/v1: fallback`: a
standard browser input and download are not a native document-picker/export
guarantee. If a necessary browser primitive is missing, an explicit Continue
control reports `unsupported`; the adapter never invents success. Invalid
definitions report `failed` without accessing file content.

The real labelled `input[type=file]` requires a fresh user gesture after the
asynchronous Go request. Nothing calls `input.click()` or automatically opens a
system picker. It has no `multiple` flag. Cancel, Escape, an empty selection, and
the file input's supported `cancel` event report cancellation. Older inputs that
do not emit `cancel` still have the dialog's explicit Cancel control.

Picker `maxBytes` must be a positive integer no larger than 1,048,576. The adapter
checks file count, metadata size, a plain leaf filename, and the optional
case-insensitive extension allowlist before reading. It reads only a bounded
slice, rechecks the resulting size, and encodes at most 1 MiB of bytes as base64.
An empty file is valid. Missing or invalid picker MIME metadata becomes
`application/octet-stream`. The `accept` attribute is a chooser hint, never the
policy check; Go must also validate the reported document. Oversized, unsafe or
disallowed selections report `denied` with no bytes. WebView leaf filenames
reject control characters, path separators and colon/scheme/drive prefixes.

Exports do nothing until the explicit Download button is activated. The adapter
decodes bounded, canonical base64, constructs a `Blob` and a temporary generated
Blob URL, clicks a temporary download link, removes the link, and revokes the URL
on completion, disposal, failure, or its scheduled cleanup. The only accepted
download source is the supplied bounded byte value, never an application URL or
filesystem path. A successful invocation reports `completed` **without** a
`file` payload and announces **“Download initiated”**, not a confirmed external
disk save. A browser/host can still block or decline the resulting download;
physical-device host support remains an acceptance responsibility.

## Results and recovery

The renderer submits exactly one write per attempt:

```json
{"kind":"file-result","requestId":"fresh-uuid","fileId":"opaque-issued-id","revision":3,"values":{"title":"current buffer"},"fileResult":{"status":"completed","file":{"name":"note.txt","mediaType":"text/plain","data":"aGVsbG8="}}}
```

The connection, screen instance and protocol version are the existing bridge
envelope. Values and revision are read when submitting the outcome, after any
asynchronous file read. Status is `completed`, `cancelled`, `denied`,
`unsupported`, or `failed`; unsuccessful results carry no file. Browser permission
exceptions map to `denied`, aborts to `cancelled`, unsupported primitives to
`unsupported`, and other read/download exceptions to `failed`.

The renderer observes the admitted result using the existing read-only `complete`
rules, including current-revision/stale reads. A lost write acknowledgement is
recovered with `complete`, never another `file-result`. When completion itself
cannot be observed, the dialog disables file access, Download and Cancel, and
offers Check result, which only rereads the same request ID. A `pending` result
also retains this read-only recovery. Known pre-admission `validation` or
`invalid_request` rejections without a response are correctable; post-admission
handler failures consume the file modal rather than offering a duplicate write.

The transfer is deliberately one bounded whole base64 value. There is no chunk,
stream, retry-write, upload URL, or native filesystem-path protocol.

## Modal and editor behavior

Files and presentations share a single modal slot. Neither can open over the
other; accepted chains close the consumed dialog before mounting the successor
and retain the original action opener. The labelled modal mounts inside the
same `[data-gfm-platform]` themed subtree as presentations, inherits design tokens,
contains Tab/Shift+Tab and escaped focus, and restores the opener or screen when
the flow ends. It never replaces the editor DOM or clears a draft by itself.
Only Go's ordinary observed response can commit or change fields.

The renderer suppresses competing actions, search submissions, discard, refresh
and app navigation while a modal is open. The capture-phase before-navigation
guard runs ahead of the normal leave guard so it cannot open a competing
confirmation. Before-unload requests are guarded, and the browser Navigation API,
when available and cancellable, blocks external history/navigation gestures too.
Only the current library-generated Blob download is exempt from that guard.
An uncancellable host departure or older WebView browser-history gesture is not a
new router: existing host/lifecycle handling still applies. If the screen is
removed, disposal closes the dialog and ignores pending file-read completions.

## Photo fallback and limits

`photos/v1` and `camera/v1` advertise `fallback` only when the file input, bounded
Blob reads, canvas 2D/JPEG encoder API, base64, and an image decoder are present.
Without those primitives they advertise `unavailable`, and a requested operation
offers an explicit Continue → `unsupported`. Older file-only adapters without
photo negotiation keep working. Function presence cannot prove that a particular
JPEG encoder, decoder, chooser or host will succeed; actual failures still report
`unsupported`, `denied`, `cancelled`, or `failed`, with no file content.

Both sources mount a real, single `input[type=file][accept="image/*"]` inside
the existing file dialog. Library uses the **Choose photo** label. Camera adds
`capture="environment"`, with **Open browser photo chooser (may offer camera)**
and an explicit explanation that this does not guarantee capture or permission.
Nothing opens automatically, requests `getUserMedia`, or starts a camera stream.
The application may label its Go actions **Choose photo** and **Take photo**;
this library never claims that the browser fallback took a photo. Physical
Android/iOS browser/WebView chooser and camera-permission behavior is host-owned.

Normalization is deliberately narrower than the input's chooser hint:

- Only JPEG and PNG MIME **and matching container headers** are supported;
  filenames/extensions alone grant no access. SVG, GIF, WebP, HEIC and unknown or
  empty MIME selections report `unsupported`. The browser must actually decode
  the selected JPEG/PNG before any completed result exists.
- Source file size is checked **before reading**, capped at **20 MiB**. A bounded
  256 KiB prefix supplies JPEG dimensions and EXIF orientation; JPEG headers
  exceeding that prefix are unsupported. PNG walks at most 256 chunk headers,
  skips compressed image data, and reads each eXIf block only up to 256 KiB,
  including orientation metadata after IDAT. Unsupported containers are rejected.
- Declared source dimensions are limited to **16,384 per side** and
  **67,108,864 pixels**. `createImageBitmap` receives explicit resize dimensions
  no larger than the requested bound, `imageOrientation: 'from-image'` and high
  resize quality. EXIF rotations/mirroring are delegated to that native decoder;
  swapped dimensions are accounted for when sizing. When bitmap decoding is
  absent, an object-URL `Image` uses the browser's auto-orientation and a stricter
  **16,777,216 source pixels** limit before decoding. Returned bitmap/image
  dimensions are also checked against that stricter decoded-allocation limit.
- Canvas draws the decoded image on white (transparent input becomes white),
  resizes to fit the requested dimension without upscaling, and reencodes fresh
  JPEG pixels. No original EXIF/GPS metadata is copied. It tries JPEG qualities
  0.9, 0.65, 0.4, then halves dimensions and repeats down to 1×1; there are at
  most 36 encodes at a 2,048-side limit. An impossible byte budget reports `denied`.
- Encoded candidates larger than `maxBytes` are never read/base64-encoded.
  Accepted output is rechecked for byte count, JPEG header and dimensions and
  returned as `photo.jpg`, `image/jpeg`, and at most the requested base64 bytes.
  The ordinary export path downloads these same bytes unchanged.

These guards are **not a browser decoder memory sandbox**: browsers can allocate
full-resolution internal buffers before honoring bitmap resize options, and
container declarations cannot reveal every malformed stream or codec defect.
Native orientation, PNG eXIf support, codec behavior, canvas encoding and physical
chooser/camera access require real-browser/platform acceptance. The Image route
does not promise compatibility with legacy engines that ignore EXIF orientation.
No application can reliably abort an already-started native decode/toBlob call;
cancellation/disposal releases owned image URLs, closes acquired bitmaps and
zeroes the canvas immediately, checks cancellation at each async boundary, and
ignores/closes late results. Duplicate change events do not start another decode.
Cancel/Escape/input cancellation remains usable while photo normalization runs;
after a result write becomes uncertain, only **Check result** remains enabled.

## Stable acceptance selectors

- `[data-gfw-file="pick"]` / `[data-gfw-file="export"]` /
  `[data-gfw-file="photo"]`: the same single file dialog.
- `[data-gfw-photo-source="library"]` / `[data-gfw-photo-source="camera"]`:
  the photo dialog and its real input (the dialog retains this when unsupported).
- `[data-gfw-file-id]`: the opaque runtime-issued file identity, not the Go key.
- `[data-gfw-file-input]`: the real single-file input; set selected files here.
- `[data-gfw-file-download]`: the explicit Download (or unsupported Continue) button.
- `[data-gfw-file-cancel]`: explicit cancellation.
- `[data-gfw-file-complete]`: normally hidden Check result; read-only recovery.
- `[data-gfw-file-status]`: polite or assertive accessible operation feedback.
- `[data-gfw-file-download-link]`: ephemeral, library-created Blob download link.

The parent-owned `sim/` chromedp coverage should verify `action → complete →
file-result → complete`, opaque identities, draft preservation, focus restoration,
cancel and denied reads, both platform styles/dark mode/text scaling, and file ↔
presentation chains. Use CDP file input selection rather than simulating an
automatic input click. To verify export, observe the real browser download event
and bytes after clicking Download; the renderer's completed status alone is not
proof of an external save. Test lost write acknowledgements and lost completion
responses independently, ensuring only `complete` is repeated. The Node fixture
tests DOM/wire glue, not OS chooser activation or actual Chromium/WKWebView saves.
Photo coverage in that suite uses mocked image decoding/encoding, not pixel or
EXIF/GPS acceptance evidence. The parent-owned browser E2E should upload a real
PNG through `[data-gfw-file-input]`, observe exactly one bounded JPEG file result,
then explicitly Download the same bytes. Include rotated/mirrored EXIF JPEGs,
metadata stripping, cancellation/disposal during normalization, denied reads,
lost writes, and physical chooser/camera behavior in browser/platform acceptance.

## Scoped checks

```sh
node --check render/webview/files.js
node --check render/webview/photos.js
node --check render/webview/presentation.js
node --check render/webview/webview.js
node --test render/webview/files.test.cjs render/webview/presentation.test.cjs
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test -race ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./render/webview
```

## 2026-11-04 - Activation and uncertain file writes

- Scope: `render/webview` file/presentation modal glue.
- Trigger: Go requests arrive after user activation has expired; write responses can be lost.
- Approach: Require real input/Download activation and keep uncertain outcomes read-only.
- Evidence: `files.test.cjs` covers 1 MiB boundaries, modal chains, and one-write recovery.
- Next time: Preserve opaque IDs, bound before reading, and never replay an accepted file result.
- Guardrail: Exempt the generated Blob download from navigation blocking, and defer URL revocation until its activation task can run.
- Status: active.

## 2026-11-04 - Photos reuse file admission and cleanup

- Scope: `render/webview` bounded photo fallback.
- Trigger: Library/camera chooser requests must preserve file-result one-shot recovery and editor drafts.
- Approach: Normalize JPEG/PNG in `photos.js`; keep modal identity, cancellation and transport in `files.js`.
- Evidence: `files.test.cjs` covers MIME/header/size limits, EXIF sizing, cleanup, byte-identical export and real command mocks.
- Next time: Keep fallback claims explicit, validate pixels again in Go, and exercise actual image codecs in parent-owned browser E2E.
- Status: active.
