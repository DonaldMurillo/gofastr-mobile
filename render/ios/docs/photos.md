# UIKit photo acquisition adapter

Sources now live in `extensions/photos/`, discovered through its compiled package
manifest and registered from Go. See [the extension contract](../../../docs/compiled-extensions.md).

`GofastrPhotos.swift` implements binding-independent Foundation/CoreGraphics/ImageIO
helpers; `GofastrPhotoController.swift` owns UIKit picker delegates and video-only
camera permission. No dependencies or app-authored native code are required.
The extension manifest supplies CLI packaging; the application owns Go admission,
screens, purpose-string packaging, and actual GUI/device acceptance.

## Contract and capability negotiation

`MobileFiles` accepts exactly one `pick`, `export`, registered `extension`, or
legacy `photo`. Existing file requests decode unchanged. The legacy wire shape is:

```json
{"id":"app-continuation","photo":{"title":"Choose photo","source":"library","maxBytes":1048576,"maxDimension":2048}}
```

`source` is `library` or `camera`; byte limits are 1–1,048,576 and the maximum
dimension is 1–2,048 pixels. The adapter returns a real reencoded JPEG named
`photo.jpg` or `capture.jpg`, MIME `image/jpeg`, and bounded base64 `data` using
the existing `MobileFilePayload`. It never returns asset IDs, URLs, original
names, paths, original metadata, or a simulated photo. Limits too small to hold
even a one-pixel JPEG produce `failed`, never oversized or empty success.

When the compiled photo factory is installed, hello advertises `photos/v1` as
available. A core-only renderer does not advertise photo support. Simulator builds explicitly
disable `camera/v1` before consulting permission APIs; simulator camera claims
are not physical capture evidence. On devices, `camera/v1` is available only
when UIKit reports a camera capable of still-image acquisition **and** the main
bundle has a nonblank `NSCameraUsageDescription`. `MobileHost.connect` dispatches
to main before those platform checks; it does not prompt or request access.
Permission denial does not erase the hardware capability. Missing purpose text
or absent hardware is `unsupported` before any permission API or camera-source
assignment, avoiding the operating system's missing-purpose termination.
Packaging that purpose string is the parent CLI task's responsibility.

## Acquisition and background processing

- Library: `PHPickerViewController`, `.images`, selection limit one, and current
  asset representation. This is user-scoped selection, not broad photo-library
  authorization. No `PHPhotoLibrary` permission request or full `loadObject`
  `UIImage` decode is used.
- Provider originals are copied into bounded memory **inside** the provider's
  file-representation completion, before its temporary URL expires. The callback
  performs bounded I/O on its background queue. An unexpected main-thread
  callback fails without doing file I/O there. No adapter temporary files survive
  the callback; the provider owns its own temporary-file cleanup.
- Original reads require a regular, non-symlink local file, use `O_NOFOLLOW` and
  `fstat` on the opened descriptor, and never read more than **20 MiB**. Empty,
  oversized, truncated-during-read, or changed-size input is rejected. Actual
  reads are bounded rather than trusting provider metadata; handles always close.
- ImageIO creates an orientation-transformed thumbnail directly from bounded
  original data with decode caching disabled for the original source. New sRGB
  rasterization flattens transparency on white, and JPEG reencoding carries no
  original GPS, camera identity, comments, or other source metadata. ImageIO may
  synthesize harmless JPEG coding/color/dimension properties.
- Camera: `UIImagePickerController` checks availability, then requests only
  `AVCaptureDevice` `.video` permission when the user-triggered task is shown.
  Authorized capture displays a still-photo-only picker; denied/restricted access
  reports `denied`. Captured `UIImage` raster access, all eight orientation
  transforms, downsampling, encoding, and base64 run on the background file queue.
  Captures are **not** automatically saved to Photos; no microphone or photo-save
  permission is requested.
- JPEG quality is reduced first; if needed dimensions are halved until the
  requested byte limit is met or a one-pixel image cannot fit. File reads,
  decoding, rasterization, and encoding never run on the UI thread. Controller
  creation, presentation, dismissal, state transitions, and callbacks run on main.

## Lifecycle and limitations

The foreground factory uses the same `MobileForegroundCoordinator` slot as files
and presentations. Photo tasks reuse `MobileFileTaskState`, opaque runtime
`fileId`, `MobileFileResult`, the one-write `file-result` boundary, and read-only
`complete` settlement. There are no photo-specific writes or accepted-write
retries. No editor reconstruction is added: dirty buffers, marked text, caret,
and focus remain managed by the existing host/foreground chain.

Cancellation, selection, permission replies, provider reads, adaptive dismissal,
and late work completions compete for one terminal report. A thread-safe work
token stops obsolete reads/encodes; main-thread task state ignores late results.
Reporting cancels/releases load progress and provider references. Finish clears
delegates, waits for dismissal before chaining/restoring focus, and releases the
picker. A read already in flight can finish its current bounded chunk or encode
before observing cancellation; it cannot submit another result.

Outcomes remain distinct: user cancellation/provider cancellation is `cancelled`,
camera restriction or permission errors are `denied`, absent/misconfigured camera
and unsupported file kinds are `unsupported`, and invalid images/encoding/I/O/
unattainable limits are `failed`. PHPicker may resolve iCloud/provider failures
inside system UI without exposing their details. No timeout, background upload,
original-asset streaming, editing/cropping, Live Photo/video import, or photo save
is implemented; only one still raster is returned. The 20 MiB file cap applies to
library originals; UIKit owns the camera's initial in-memory `UIImage` allocation.
ImageIO/system codecs and camera/provider UI still need device acceptance.

## Verification and acceptance handoff

Run:

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./cmd/gofastr-native ./render/ios
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./render/ios
```

`photos_test.go` executes the real Swift helpers on macOS: optional/exclusive DTOs,
regular/symlink/directory/size-bounded reads, malformed inputs, valid JPEG decode
in Swift **and Go**, all eight library/camera orientation paths, metadata removal,
byte-budget downscaling, impossible limits, cancellation, and terminal-state race
orders. It also typechecks all UIKit sources without Go bindings and checks
source-level capability, permission, lifecycle, and foreground boundaries. These
tests do **not** establish real UIKit camera, permission-prompt, provider, or
foreground/editor acceptance.

Parent-owned GUI acceptance must cover simulator library selection/cancellation,
honest simulator-camera `unsupported`, a build missing camera purpose text,
physical-device first permission/allow/deny/restricted states, capture/retake/
cancel, HEIC/JPEG/rotated/large/iCloud assets, cancellation during processing,
photo/file/presentation chains, navigation blocking, and dirty-editor IME/caret/
focus retention. Confirm captures are not saved to the system photo library.
No physical-camera testing is claimed by this implementation.

## 2026-11-04 - Provider lifetime and metadata regression lessons
- Scope: `render/ios` photo acquisition.
- Trigger: Provider temporary URLs expire at callback return; ImageIO creates technical EXIF fields even after clean reencoding.
- Approach: Read the bounded original during the provider callback, then queue decoding; assert absence of original GPS/comments/device tags rather than absence of every EXIF dictionary.
- Evidence: `TestPhotosWireAndImageProcessing` compares all eight camera transforms with ImageIO and fully decodes the emitted JPEG with Go.
- Next time: Run the scoped tests, then perform physical-camera and editor/foreground GUI acceptance before claiming device support.
- Status: active.
