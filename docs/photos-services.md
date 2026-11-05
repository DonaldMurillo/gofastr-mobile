# Typed photo acquisition

The one Notes application adds **Choose photo** and **Take photo** to a saved
note's Attachments screen. Both feed the existing private attachment store,
detail, export, and confirmed-removal workflow. Photos appear below the body
inside the note editor, and tapping one opens a larger preview. Returning from
attachments refreshes the photos without rebuilding the native editor or
resetting its input. The note body is not changed. Application authors still write only Go mobile definitions and typed
continuations, not JavaScript, Swift, templates, wire JSON, or device paths.

This is a bounded still-photo capability, not a general media framework or
arbitrary camera/filesystem access. Native Android remains deferred.

## Go API

```go
nativeui.Result{Files: &nativeui.FileRequest{
    ID: "photo", Commit: true,
    Photo: &photos.Request{
        Title: "Choose photo", Source: photos.Library,
        MaxBytes: files.MaxBytes, MaxDimension: photos.MaxDimension,
    },
}}
```

Use `photos.Camera` to request capture. Register the same ordinary
`nativeui.FileRespond` continuation under the request ID in `Route.Files`.
The continuation receives a validated `files.Document` with JPEG bytes, a plain
`.jpg`/`.jpeg` filename, and `image/jpeg`. It authorizes its business mutation
again before persisting anything. Notes uses its owner-scoped service to check
the saved note before requesting acquisition and again before attaching.

Exactly one of `FileRequest.Pick`, `Photo`, or `Export` is allowed. Photo
acquisition deliberately reuses the bounded file transfer and its existing
foreground lifecycle rather than introducing another modal stack or protocol.
It does not require the unrelated `files/v1` capability to acquire a photo.

## Capabilities, permission, and settlement

- `photos/v1` describes library selection; `camera/v1` describes capture.
  Their availability is independent. A camera-less simulator can select a
  library photo without pretending it can capture one.
  UIKit simulator builds explicitly disable capture, even if the simulated
  platform advertises camera availability.
- A capability is not a permission grant. Camera authorization is checked and,
  if needed, requested only after the user invokes capture. There is no prompt
  at startup. The library picker does not request broad photo-library access.
- UIKit uses the system photo picker for library selection and the native
  still-camera controller for capture. Capture does not automatically save a
  second copy into the system photo library.
- The WebView adapter advertises a fallback: an explicit image file chooser,
  with a camera hint for capture. It may offer a camera, an existing image, or
  no camera depending on the host. It never labels browser support as verified
  capture. A screen needing actual native capture can require `camera/v1`
  without allowing fallback through the existing screen requirement contract.
- `action → complete` issues an opaque `fileId`. A renderer sends one
  `file-result → complete`. The existing connection/screen identity, foreground
  slot, current editor values/revision, and unknown-write recovery apply.
- `cancelled`, `denied`, `unsupported`, and `failed` carry no content and invoke
  no application continuation. No accepted write is automatically replayed.
  An unavailable camera never silently switches to library selection.

## Image and privacy policy

- One still image per request. The transferred JPEG is at most **1 MiB** and
  **2048 pixels on either edge**, or the request's smaller bounds. The existing
  **16 MiB / 64 files per installation** private attachment quota still applies.
- Renderers downsample and re-encode rather than returning original assets.
  Orientation is applied to pixels, alpha is flattened for JPEG, and original
  EXIF/GPS metadata is not copied. This is not redaction of image content.
- Go verifies actual JPEG configuration, bounded dimensions, and a complete
  decode. A MIME label or extension alone is not accepted as an image.
- Library inputs are bounded at 20 MiB before normalization. Adapter-specific
  decoding/resource limits may reject unsupported, malformed, or exceptionally
  large images. Browser codecs and memory behavior are not a streaming or
  isolation guarantee; see the renderer notes.
- UIKit delegates supported still-image formats to ImageIO. The WebView fallback
  currently accepts only JPEG/PNG content, even when its chooser offers other
  image types. Unsupported formats do not become attachments.
- No original paths, photo-library identifiers, security-scoped bookmarks,
  or permission handles reach Go screens. No automatic network upload occurs.
- Display uses separate bounded, reencoded previews: 512-pixel thumbnails in
  the note and a preview up to 1024 pixels in attachment detail. Export still
  returns the stored attachment, not the smaller display preview. See
  [image preview definitions](image-previews.md).
- No video, audio, live camera stream, batch selection, original/RAW retention,
  image editing/cropping UI, inline rich-text image placement, or background capture
  is supplied by this slice. Large media needs a separate handle/stream design.

## Packaging

The library's iOS Info.plist includes a camera purpose string; the application
does not provide a native template. Customize it with the CLI setting:

```sh
go run ./cmd/gofastr-native run --platform ios-sim --renderer native \
  --pkg ./examples/notes/notesnative \
  --camera-usage 'Take a photo to attach privately to your note.'
```

Use product-specific, truthful wording before distributing an app. Passing
`--camera-usage ''` omits the purpose key and disables native camera support.
The renderer checks for the key before any permission request. No microphone
or broad photo-library purpose key is added by this feature.

## Verification boundaries

`device/photos/photos_test.go` checks the pure content/limit policy.
`nativeui/runtime_photos_test.go` exercises capability separation, the real
command envelope, validation, cancellation, and duplicate outcomes.
`examples/notes/notesnative/photos_test.go` exercises the public binding and
private photo persistence across restart without changing the note.
`sim/photos_test.go` exercises actual image normalization, attachment/export
bytes, camera chooser hints, cancellation, invalid images, and large text in
Chromium through the in-memory bridge for both platform styles.

On November 4, 2026, a fresh UIKit bind/build/install on iPhone 17 Pro / iOS 26.5
simulator selected a generated 3000×1800 PNG through the real system picker.
The stored result was a 2048×1229 JPEG of 40,805 bytes. Picker cancellation
added no second attachment, returning to the editor preserved its text, and
capture explicitly reported unavailable on the simulator. Both platform-style
Chromium photo/file/presentation workflows passed three repeated runs, including
actual JPEG download-byte checks. The full workspace and public-pin tests/vet,
focused race checks, and 48 renderer Node tests also passed.

UIKit typechecking/helper tests and a simulator picker test do not verify a
physical camera, its system permission prompt, iCloud/provider behavior,
VoiceOver, or actual WKWebView/Android WebView file integration. Keep those
acceptance claims separate. Record observed runs in `docs/agent-notes.md`.

## Platform references

- [Apple: selecting photos and videos](https://developer.apple.com/documentation/photokit/selecting-photos-and-videos-in-ios)
- [Apple: capture authorization and purpose keys](https://developer.apple.com/documentation/avfoundation/requesting-authorization-to-capture-and-save-media)
- [Apple: ImageIO thumbnails](https://developer.apple.com/documentation/imageio/cgimagesourcecreatethumbnailatindex(_:_:_:))
- [W3C: HTML Media Capture](https://www.w3.org/TR/html-media-capture/)
