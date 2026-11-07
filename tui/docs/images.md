# Images

`widget.Image` is a regular retained-tree element. It scrolls and clips with its
ancestors; no application-owned terminal escape sequences are needed.

```go
source, err := widget.LoadImage("photo.webp")
// Handle err before building the view.
element := view.E(widget.Image{
    Source: source, Width: 40, Fit: widget.Contain,
    Align: layout.Center, Alt: "No image",
})
```

## Public API

- `cell.NewImage(pixels image.Image) *cell.Image`
- `(*cell.Image).SetPixels(pixels image.Image)`
- `(*cell.Image).Raster(crop image.Rectangle, width, height int) *image.NRGBA`
- `(*cell.Buffer).PlaceImage(img *cell.Image, rect, crop image.Rectangle)`
- `(*cell.Buffer).Images() []cell.ImagePlacement` (read-only frame view)
- `cell.ClipImage(rect, crop, clip image.Rectangle) (image.Rectangle, image.Rectangle)`
- `(cell.Options).CellPixels() (int, int)`
- `(cell.ImageProtocol).String() string`
- `widget.LoadImage(path string) (*cell.Image, error)`
- `widget.DecodeImage(reader io.Reader) (*cell.Image, error)`
- `(*view.Canvas).PlaceImage(img *cell.Image, rect layout.Rect, crop image.Rectangle)`
- `(*view.Canvas).ImageProtocol() cell.ImageProtocol`
- `(*view.Canvas).CellPixels() (int, int)`
- `(*term.Terminal).CellPixels() (int, int)` (zeroes when unavailable)

`cell.Image` contains `Pixels image.Image`, `ID uint32`, and `Version uint64`.
Construct once, reuse the pointer, and call `SetPixels` to change the decoded
image. Directly mutating pixels without changing the version is unsupported.
Images and buffers belong to the app-loop goroutine, not background workers.

`widget.Image` contains `Source *cell.Image`, `Fit widget.FitMode`,
`Width, Height int`, `Align layout.Align`, and `Alt string`. `Contain` is the zero
fit mode; `Cover`, `Stretch`, and `None` are the other modes. Alignment uses
`layout.Start` by default and applies to both axes. Zero dimensions derive
intrinsic cell size, bounded by layout constraints; one explicit dimension
derives the other from the pixel aspect ratio. `None` uses intrinsic size rounded
up to cells, clipped by its box. `Alt` is used for missing/empty images.

PNG, JPEG, GIF (first frame), WebP, and BMP decode through `image.Decode`.
Only `golang.org/x/image` is added; no external image tools are needed at runtime.

## Capability selection

The startup batch adds `CSI 16 t` and the kitty one-pixel query before the final
DA1 sentinel. A matching kitty `OK` wins, otherwise DA1 attribute 4 selects
sixel. Only when neither answered, `TERM_PROGRAM=iTerm.app` or WezTerm selects
OSC 1337. A silent/unknown terminal uses half-block cells. Query cell pixels win
over nonzero kernel `TIOCGWINSZ` dimensions; the final fallback is 10 by 20.

`probe.Result` and `cell.Options` expose `Images cell.ImageProtocol` and
`CellWidth, CellHeight int`. Protocol values are `ImageNone`, `ImageKitty`,
`ImageSixel`, and `ImageITerm`. `view.Tree` exposes the same three fields; `app`
sets them from its probe. A standalone tree defaults to text fallback.
`input.GraphicsReply{ID, Message}` and `input.CellSize{Width, Height}` represent
the new replies. Unsupported and malformed input replies are discarded.

## Rendering decisions

The buffer owns a frame's visible, non-overlapping `ImagePlacement` records:
`Image *cell.Image`, `ID uint32` (placement ID), `Version uint64`, and `Rect, Crop
image.Rectangle`. `Rect` is in screen cells, `Crop` in source pixels, and a zero
crop passed to `PlaceImage` means the whole image. Canvas converts node-local
rectangles to absolute cells and maps all ancestor clipping into source pixels.
Fractional pixel edges round outward, so enlarged tiny images do not disappear.

Pixel placements replace their underlying cells with reserved blanks
(`cell.Cell.Reserved`). Text diffing, blank-run erases, and cursor rewrites never
write through reserved cells. Later text and images split older placements into
visible rectangular pieces; wide-character edges are repaired. Removing an
image restores ordinary blank/text cells through the existing diff. Text remains
the only source for selection/copy; an image does not hide stale text in it.

Kitty receives PNG data once per ID/version, in base64 chunks of at most 4096
bytes. Placements are independently diffed, with source crops and cell sizes.
Moves delete the old placement and place the new one without retransmission.
Unused data is freed after three render frames, and Reset frees owned data.
Invalidate retransmits and re-places everything, recovering from partial writes
or external terminal resets. Kitty Stretch uses a cached pre-scaled image because
kitty's placement rectangle preserves aspect ratio.

Sixel and OSC 1337 use area-averaged, target-pixel-size crops. Sixel uses a
216-color RGB cube and transparent-background DCS data. OSC 1337 uses PNG with
`preserveAspectRatio=0` and WezTerm's `doNotMoveCursor=1` extension. Inline
protocols erase old footprints and repaint visible placements on image changes;
unrelated text changes do not resend them. Their cursor position is treated as
unknown afterward and restored explicitly. No real-terminal visual certification
is implied: bottom-edge cursor/scroll semantics and multiplexer passthrough
remain terminal-specific compatibility concerns.

Without a pixel protocol, cached area-averaged pixels become upper-half-block
cells (`▀`), with top RGB foreground and bottom RGB background. Transparent
pixels composite over black on this path. Existing renderer color downgrade
handles 256-color, 16-color, and no-color terminals. Warm unchanged frames allocate
nothing on every protocol, verified by `TestImageSteadyAllocations`.

## Demo and test oracle

Run `GOWORK=off go run ./cmd/tui-image <file>` from `tui/`. The image sits in a
scrollable panel and the status line names the selected protocol. Wheel events
scroll; `q` quits. The existing drawing demo is unchanged.

The `vt.Screen` oracle records per-screen `Images`, `Placements`, and cell-pixel
geometry. It accepts chunked kitty uploads/placements/deletions/queries, sixel
raster metadata, and OSC 1337 PNGs; malformed sequences accumulate in `Errors`.
Renderer properties exercise random placements, crops, overlays, moves, removals,
and invalidation for every pixel protocol. See [agent notes](agent-notes.md) for
reproducibility and compatibility pitfalls.

Protocol references: kitty's **Terminal graphics protocol**, iTerm2's **Inline
Images Protocol**, WezTerm's **iTerm Image Protocol**, and XTerm's **Control
Sequences / Sixel Graphics** (DEC VT330/VT340 Chapter 14 wire format).
