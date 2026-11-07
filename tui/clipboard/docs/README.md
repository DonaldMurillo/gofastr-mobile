# Clipboard images

This standard-library-only package exposes:

```go
func ReadImage(ctx context.Context) (data []byte, mime string, err error)
func ImagePath(paste string) (path string, ok bool)
func Sniff(data []byte) (mime string, ok bool)
var ErrNoImage error
var ErrUnsupported error
```

macOS runs `osascript` to coerce the clipboard to `«class PNGf»` and hex-decodes
its `«data PNGf…»` result. A failed image coercion returns `ErrNoImage`. Linux
uses `wl-paste --type image/png` when `WAYLAND_DISPLAY` is set and otherwise
`xclip -selection clipboard -t image/png -o`. Windows uses PowerShell's
`Get-Clipboard -Format Image`, saves the image into a PNG memory stream and
base64-decodes stdout. Missing tools return `ErrUnsupported`; all subprocesses
receive the caller's context. Other platforms return `ErrUnsupported`.

`ImagePath` trims surrounding whitespace and quotes, unescapes shell-style
backslash spaces, accepts local percent-decoded `file://` URLs, and expands
`~/`. It rejects directories, nonexistent paths, remote file URL hosts and
unsupported extensions. `Sniff` recognizes PNG, JPEG, GIF, WebP and BMP headers,
not complete image validity.

The Darwin clipboard round-trip test saves all available clipboard formats as
an AppleScript record, verifies restoration works before making any changes,
then restores it in cleanup. It skips unavailable tools or pasteboard sessions.
Use `GOWORK=off go test -v ./clipboard/` from `tui` to see whether the real
pasteboard test ran. Never run this test concurrently with clipboard-dependent
manual work or another process that writes the clipboard.
