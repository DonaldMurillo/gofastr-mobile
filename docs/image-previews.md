# Go-authored image previews

`nativeui.Screen.Images` supplements a form or text screen with private,
accessible image previews. Applications compose this in Go; UIKit and WebView
own their views, styling, scrolling, and input preservation. No application
Swift, JavaScript, templates, filesystem paths, or remote image URLs are needed.

```go
preview, err := nativeui.NewImage(attachment.Handle, attachment.Name, document.Data, 512)
if err != nil {
    return nativeui.Screen{}, err
}
preview.Destination = attachmentDetailRoute
screen.Images = append(screen.Images, preview)
```

Authorize access before reading the attachment. `Destination` is optional and,
when present, must resolve to a registered local screen. `ID` is stable attachment
identity, and `Alt` is required. Supply a descriptive accessible name when the
application knows more than a filename; the renderer cannot infer image content.

## Bounds and ownership

- `NewImage` accepts complete JPEG/PNG content up to 1 MiB, at most 2048 pixels
  on either edge. It preserves aspect ratio without upscaling and flattens alpha
  onto white. The requested maximum edge is 1–1024 pixels.
- Display bytes are reencoded JPEGs without original EXIF/GPS metadata, bounded
  to 128 KiB per preview. Unsupported/corrupt inputs return an error.
- Screens admit at most 64 previews. Image IDs must be unique; dimensions and
  JPEG headers are validated. Previews are not supported as list content yet.
- Original files stay in the private attachment store. Images are display data,
  never form values or draft content. Text export does not embed photos; file
  export returns the original stored attachment bytes.
- This remains a bounded snapshot protocol, not media streaming or an image
  cache. Avoid issuing a screen render on every keystroke. Large galleries need
  a later handle/resource protocol instead of raising these bounds.

## Notes behavior

Saved notes show all supported, valid photo attachments below their text. The
editor uses 512-pixel previews; attachment detail uses up to 1024 pixels. Other
files and unsupported image attachments remain available in Attachments without
preventing the note from opening. Image removal removes the preview on the next
screen read, without changing the note's text or locally recovered draft.

UIKit uses an outer keyboard-aware scroll view and native image controls. On
return it rerenders only the gallery from a read-only Go render; it does not open
a replacement editor session, replace text controls, or adopt stored text over
the current editing buffer. Tapping a photo checkpoints dirty input before
pushing its detail screen. WebView uses escaped SSR markup and real `img`
elements; a fresh runtime open reconciles the gallery independently of fields,
including when client navigation restores cached HTML.

This is an attachment gallery, not arbitrary placement between paragraphs, an
image editor, zoom/pan viewer, or a new media-acquisition capability. Existing
saved-note action navigation still follows the editor's leave/save policy;
this change does not relax unknown-save or unsaved-navigation protection.

## Navigation action composition

UIKit explicitly lays out one trailing primary action and a labelled **More
actions** menu. An explicitly primary action wins; otherwise the first trailing
action is primary. Other trailing actions appear exactly once in declaration
order. Leading actions remain leading. Busy state disables the same controls
rather than reconstructing the toolbar and relying on OS automatic overflow.
Thus **Done** and **Add file** stay visible, and photo-source labels do not
shuffle into the primary position. Menu callbacks remain registered Go actions.

## Regression checks

- `nativeui/image_test.go`: bounded decoding, aspect ratio, real JPEG data,
  dimensions, and registered destinations.
- `examples/notes/notesnative/photos_test.go`: public Go command boundary,
  persisted photos in the note/detail, byte-identical export, and removal.
- `render/ios/files_test.go`: executed Swift toolbar ordering and DTO checks;
  CLI/UIKit tests also compile the actual renderer sources.
- `sim/photos_test.go`: real rendered image loading through the mobile bridge,
  attachment-to-editor Back, tappable detail, dirty-text restoration, 200% text,
  and removal. Android here is Chromium styling, not Android-device validation.

The iOS simulator was also checked with multiple existing photos and large text:
inline pictures, detail view, explicit menus, native picker opening, and cancel.
Physical camera and actual WKWebView OS chooser acceptance remain separate gates.
