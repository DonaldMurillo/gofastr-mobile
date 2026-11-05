# Mobile document design

The Notes application is a document workspace, not a catalogue of device APIs.
Its shared Go definitions express content hierarchy; renderer libraries own the
visual treatment. There is no app-specific CSS, JavaScript, Swift, or screen
template. GoFastr core remains unchanged.

## Hierarchy

- **Notes:** title, short body preview, then secondary pin/tag metadata. Search
  stays visible; query controls live in a collapsed Filters section. WebView
  selection is an explicit mode instead of permanent checkboxes and bulk tools.
- **Editor:** title and writing come first. Photos and file cards follow the
  document, before a collapsed Note details section. Pin, tag, and reminder
  controls remain fully labelled and expand when validation needs attention.
  Done remains the primary action; secondary operations use a deliberate menu.
- **Files:** an attachment library, with human-readable type/size and the owning
  note, not a text page explaining the file service. The current library covers
  the most recent 200 active notes; its heading says From recent notes. It is
  not a claim of exhaustive search across archived notes or all note history.
- **Attachment:** full filename, semantic file icon, readable metadata, and
  actual image/audio/plain-text preview where supported. Export is primary;
  sharing and confirmed removal stay in the secondary menu.

## Reusable Go definitions

`nativeui.Form.Style = "document"` selects a writing-first form. Fields marked
`Style: "detail"` enter the disclosure labelled by `Form.DetailsTitle`. Fields
outside it retain their declared order. Hidden visual title/body labels retain
accessible names; validation must reveal any collapsed target.

`Row.Metadata` separates secondary context from `Row.Subtitle`. `Row.Icon` and
the Notes/Files/Settings tab icons are semantic identifiers, translated into
system symbols in UIKit and library-owned SVGs in WebView.

`nativeui.FileDescription(mediaType, size)` returns readable category/size text;
`FileIcon(mediaType)` returns a semantic category icon. Attachment JSON includes
these derived `detail`/`icon` values. UIKit retains a fallback for older payloads.
No application must format MIME names or byte counts for each renderer.

`Screen.FilePreview` is an alternative content kind alongside list, form, and
text. Its `File` contains authorized metadata, `Caption` provides context,
`PreviewTitle` labels an optional plain-text `Text` preview. Text is bounded to
16 KiB of valid UTF-8 without NUL; caption/title are bounded too. Binary audio
belongs in the existing attachment playback contract, not this metadata.
The normal route/action authorization and destination validation still apply.

Photos use existing `Screen.Images`, and recordings use `Screen.Attachments`.
No untrusted HTML/Markdown execution or new PDF/Office renderer is introduced.
Unsupported binary files still have useful metadata and export/share actions.

## Renderer rules

- UIKit uses Dynamic Type, semantic system surfaces, system symbols, and a
  contrast-aware amber tint. Mobile WebView uses the corresponding iOS palette
  or the existing Material-oriented Android palette. Share hierarchy, not
  identical platform pixels.
- Use consistent gutters, rounded grouped surfaces, restrained separators,
  compact labelled metadata controls, and one visually primary action.
- Keep file names multiline/wrappable and avoid raw MIME/debug messages.
- Make unset dates/times visibly unset. Keep setting and clearing explicit.
- Render photo thumbnails inside the editor; open an aspect-fit full preview.
  UIKit uses one column; the WebView document gallery uses two.
- Menus must stay between actual navigation/tab-bar bounds, scroll when needed,
  and expose labelled controls. Do not solve overflow by hiding undo or actions.
- Narrow WebView navigation uses an icon-only Back affordance with its accessible
  label intact. Tab roots do not display a misleading hierarchical Back button.
- Live DOM replacements and refreshed attachment cards must preserve the same
  icons/metadata/hierarchy as the first server-rendered screen. Preserve local
  buffers, selection, and focus; styling must not rebuild an editor session.

## Verification

The browser acceptance surface is `TestNotesVisualHierarchyWebView`, plus the
existing components, files, photos, device-services, and live-paging tests in
`sim/`. The design test seeds real notes, a Markdown file, and a photo, then
captures list/editor/Files/file/photo previews for both platform modes in
light/dark. It also checks 200% text at 320 CSS pixels without horizontal overflow.

```sh
SIM_SHOTS=/tmp/gofastr-design-shots \
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL \
go test ./sim -run 'TestNotesVisualHierarchyWebView|TestMobileComponentsWebView|TestNotesFilesWebView|TestNotesPhotosWebView|TestNotesLivePagingWebView|TestNotesDeviceServicesProtocolWebView' -count=1
```

Screenshots require visual inspection, not just successful DOM assertions.
The native QA build uses the separate `dev.gofastr.designqa` simulator bundle
so design verification does not replace the user's existing Notes installation.
On November 5, 2026, the six browser suites above (including device services),
103 renderer Node tests, and three repeated component/gesture runs passed.
The bound UIKit app was rebuilt, installed separately, and visually inspected
in light/dark across Notes, the editor/disclosure, Files, and file/photo previews.
Simulator and Chromium checks do not establish physical camera/microphone,
VoiceOver/TalkBack, Android-native, or real-device acceptance.

During native QA, returning to a retained Notes list sometimes exposed the
existing stale-list Retry state; Retry recovered it. The lifecycle issue is
separate from this design pass and remains unresolved, not visually suppressed.
