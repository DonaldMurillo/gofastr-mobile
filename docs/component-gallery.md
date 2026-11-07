# Component gallery

`examples/gallery` is a separate app that exercises the `nativeui` screen contracts one at a time. Notes covers one product; the gallery covers the contracts, so you can watch how each renderer handles a single feature, see a refusal, or reproduce a bug without Notes data in the way.

The gallery keeps all state in memory except drafts. Saves never touch disk.

## Packages

| Package | Purpose |
|---|---|
| `examples/gallery/gallery` | The screens. `New()` returns the `*nativeui.App`. |
| `examples/gallery/gallerynative` | gomobile bind package for `--renderer native`: `Start`, `Stop`, `Root`, `Command`, `Render`, `Dispatch`. |
| `examples/gallery/galleryweb` | `Build(dataDir, platform, options)` composes the gallery with the WebView renderer inside an in-process `mobilehost`. It opens no listener. |
| `examples/gallery/cmd/gallery-sim` | Opens the WebView build in a phone-shaped Chromium window through `sim/browsersim`. |

## Running it

In a browser, with no HTTP listener:

```
go run ./examples/gallery/cmd/gallery-sim --platform android --trace
go run ./examples/gallery/cmd/gallery-sim --platform ios --headless --screenshot gallery.png
go run ./examples/gallery/cmd/gallery-sim --describe
```

`--trace` prints redacted `gofastr-trace` lines to stderr. `--describe` prints the route registry as JSON and exits. The data directory and browser profile are temporary and removed on exit. Set `SIM_CHROME` or pass `--chrome` if Chromium is not found.

On a device or simulator, bind `gallerynative` the same way as Notes:

```
gofastr-native run --platform android-emu --renderer native --pkg ./examples/gallery/gallerynative
```

Add `--debug-trace` to enable tracing in the gallery's native binding. The
x86_64 Android gallery APK was built with that flag. Device installation,
launch and native log routing have not been checked for this change. See
[debugging](debugging.md) and the [validation record](developer-workflow.md#validation-record).

To list which screens open under each assumed renderer profile without a device, see `gofastr-native inspect` in [debugging](debugging.md).

## Screens

The root has four tabs.

| Route | Shows | Requires |
|---|---|---|
| `/gallery/fields` | All field kinds and styles; Validate and Save | editor |
| `/gallery/document` | Document form style | editor |
| `/gallery/draft` | Draft restore; Finish clears the draft | editor, drafts (fallback allowed) |
| `/gallery/unknown` | A save that fails with a plain error, so the outcome is unknown and retry is blocked | editor |
| `/lists/browse` | 60 rows with search, a parity filter, refresh and paging by 20; row 7 is disabled | none |
| `/lists/rows/{id}` | Row detail; out-of-range IDs return `not_found` | none |
| `/lists/actions` | Swipe actions, one destructive | none |
| `/lists/select` | Multi-select, at most 3 | none |
| `/lists/empty` | Empty list state | none |
| `/lists/live` | A live list; Add bumps a version counter | live |
| `/feedback/present/{kind}` | alert, confirmation, menu, dialog, sheet, single and multiple choice | presentation |
| `/feedback/effects/{effect}` | haptic, announcement and share effects | haptics or share fallback; announcement |
| `/feedback/navigation` | Push and pop | navigation |
| `/feedback/sheet` | Sheet open and dismiss | sheets (fallback allowed) |
| `/feedback/text`, `/feedback/images` | Plain text; two PNGs drawn by Go | none |
| `/device/capabilities` | The negotiated contract as text | none |
| `/device/files` | Export and import; the import reports only size and media type | files |
| `/device/clipboard`, `/device/share-text` | Clipboard and system share through device services | clipboard, sharing |
| `/device/protected` | A protected screen with a lock route | privacy |

The capability screen lists every known capability. It shows the value the renderer advertised, "not advertised" for capabilities the renderer left out, or "unknown (legacy render path)" when the screen renders outside a Runtime command. The WebView renderer paints each page on the server before the browser says hello, so that first paint always shows the legacy line. The footer states the limit: an advertised capability is not proof the device supports it.

A screen whose requirement the renderer lacks is refused before any handler runs. Under the `webview` profile of `gofastr-native inspect`, `/device/protected`, `/device/clipboard` and `/device/share-text` are refused for privacy, clipboard and sharing. `TestInspectGalleryUnderSyntheticProfiles` checks this.

## Tests

- `go test ./examples/gallery/...` renders and reaches every route, runs every declared action, checks validation keys, unknown-save blocking, honest requirement refusals, the capability text, file import reporting and presentation results.
- `go test -tags gofastr_debugtrace ./examples/gallery/gallerynative` checks the tagged trace hook.
- `go test ./sim -run TestGalleryWebView` walks every tab and row in headless Chromium for both phone shapes and drives validation, save, unknown save, a refusal, the capability text, live updates, each presentation kind, an announcement, and push and pop. Set `SIM_SHOTS` to keep screenshots.
