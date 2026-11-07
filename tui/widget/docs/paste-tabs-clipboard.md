# Paste, clipboard images and tabs

`Input` and `TextArea` remain controlled: `Value string` is the current text and
`OnChange func(string)` reports edits. `MaxLength int` limits inserted text by
Unicode runes, not bytes or graphemes; zero means unlimited. A replacement counts
only the retained text against the limit. Existing over-limit controlled values
are not silently shortened. `TextArea` counts tabs and newlines as one rune each.

`OnPaste func(text string) bool` sees the original bracketed paste before any
normalization. Returning true consumes it without changing the value or caret.
Returning false uses normal text insertion. A read-only widget without an
intercepting callback leaves the paste unconsumed so ancestors or the app's
unhandled-event callback can process it.

Both widgets normalize CRLF and CR to LF and remove Unicode control characters
other than tab and LF. `Input` drops one final LF, joins lines with spaces and
replaces tabs with spaces. `TextArea` keeps tabs and newlines. Input supports
Shift+Left/Right/Home/End, shift word navigation, shift-click and pointer dragging
to select text; typing and paste replace that selection.

`TabBar.OnClose func(i int)` enables a separate ` ×` close target, middle-click
closing, and ctrl+w for the active tab. Closing is controlled: the callback
decides whether to remove a title or change the active index. Close marks have
independent hover styling and do not activate a tab. Left/Right and Home/End
select tabs; vertical or horizontal wheel events select adjacent tabs without
wrapping. All keyboard commands require bar focus.

Overflow reserves edge cells for `‹` and `›` as needed. Clicking a marker scrolls
one viewport without changing the active tab. An active-index change or resize
reveals the active tab when its width fits the viewport. An oversized title is
clipped from its start. `Tabs(titles []string, active int, onChange func(int),
content view.Element) view.Element` retains visited contents under index-keyed
children, laying out only the active child. Lists and text fields keep separate
offsets and carets. Index identity assumes tabs are not reordered; removing or
reordering tabs changes which data an index identifies.

For image-aware paste handlers, use package
`github.com/DonaldMurillo/gofastr-native/tui/clipboard`:

- `ReadImage(ctx context.Context) (data []byte, mime string, err error)` reads an
  image through platform clipboard tools. Use a context deadline.
- `ImagePath(paste string) (path string, ok bool)` accepts one existing regular
  file with a PNG/JPEG/GIF/WebP/BMP extension, including quoted, escaped-space,
  `file://` and `~/` paths. It does not read or decode that file.
- `Sniff(data []byte) (mime string, ok bool)` recognizes those formats by magic
  bytes; it does not validate the complete image.
- `ErrNoImage` and `ErrUnsupported` support `errors.Is` checks.

## Implementation notes — 2026-11-07

- Scope: widget state, clipboard tooling and regression checks.
- Keep inactive keyed children mounted; switching just one keyed child unmounts
  its predecessor and loses the predecessor's scroll offset and caret.
- Keep title and close nodes as siblings: `Node.Hovered` includes descendants.
- Use `osascript -s s` to serialize clipboard backups as AppleScript source;
  default human-readable output cannot safely be evaluated to restore a record.
- Preflight restoration before changing the pasteboard, then restore in cleanup.
- Next time: run `GOWORK=off go test -race ./widget/ ./view/ ./clipboard/` from
  the standalone `tui` module, and vet Windows/Linux without executing binaries.

## Mutation verification

All 37 temporary mutations were restored after confirming behavioral test
failures (not compilation failures):

- Paste: CRLF/CR conversion, trailing LF removal, line/tab spaces, control
  stripping, each widget's insertion position, keyboard selection replacement,
  drag selection, rune limits, each raw interceptor, root-handler consumption,
  each focused read-only widget's bubbling, and an injected 2.1-second delay.
- Tabs: mark click, middle title click, middle mark click, ctrl+w, ancestor hover
  leakage, outside-release closing, Home, End, wheel switching, right marker
  visibility, marker scrolling, active reveal, invalid-index close guards, and
  inactive-content retention (both list and caret tests).
- Clipboard: magic recognition, whitespace path parsing, missing-tool sentinel,
  real Mac image truncation, real Mac no-image sentinel, malformed tool output,
  cancellation propagation, and deadline propagation.

The first read-only mutations survived because the test fixture had no focused
child; dispatch correctly started at the root instead. Both fixtures now request
and assert focus, and both mutations fail. The root-handler mutation changes
only the test wrapper, never `view/`. The latency mutation verifies the 2-second
guard; it is not a claim of detecting every possible quadratic implementation.
