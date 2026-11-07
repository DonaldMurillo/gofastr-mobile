# tui

A terminal UI renderer written from scratch in Go. It depends on the standard
library and `golang.org/x/sys` only. It is its own module and does not import
`nativeui`; a `nativeui` adapter can sit on top of it later.

## Layers

Each layer depends only on the layers below it.

| Package | Owns | Status |
|---|---|---|
| `app` | Event loop, frame scheduler, `Post` for background results | planned |
| `widget` | Text, list, table, input, textarea, scroll, split, menu, modal | planned |
| `view` | Element tree reconciled by key, focus, hit testing, pointer capture, layers | planned |
| `layout` | Constraints down, sizes up; flex rows and columns, wrapping, scrolling | planned |
| `cell` | Cells, styles, color downgrade, the `Buffer`, and the diffing `Renderer` | done |
| `uni` | Grapheme segmentation and width tables generated from Unicode data | planned |
| `input` | Bytes to `Key`, `Mouse`, `Paste`, `Focus` and query replies | done |
| `probe` | Capability queries at startup, ended by a DA1 sentinel | planned |
| `term` | Raw mode, private modes, mouse tracking, size, resize, cancellable reads, restore | done |
| `vt` | In-memory terminal emulator used as the test oracle | done |

`cmd/tui-demo` drives `term`, `input` and `cell` together: drag to paint,
right-click to erase, scroll to change color.

## How the pieces behave

`term` records every mode a program enables and `Close` disables them in
reverse order, then restores the device. `Close` is idempotent, safe from any
goroutine, and interrupts a blocked `Read`. Reads wait with `select` and a
wake pipe on Unix (poll does not work on terminal devices on macOS) and with
`WaitForMultipleObjects` and a cancel event on Windows. On Windows, console
input records are converted to the same bytes Unix terminals send: VT key
sequences pass through, and mouse records are re-encoded as SGR mouse
sequences, so one decoder serves every platform. When standard input or output
is redirected, `Open` uses `/dev/tty` (or `CONIN$`/`CONOUT$`). `OpenFiles`
takes explicit devices, for example an ssh session's pty.

`input.Decoder` parses at the head of its buffer and holds incomplete
sequences. A lone ESC is the only ambiguous input; the reader waits about
30 ms for more bytes and then calls `Flush`, which turns ESC into the Escape
key or alt+next key. With the kitty keyboard protocol enabled, Escape arrives
unambiguously and the timer never runs. Bracketed paste ends only on its end
marker, even across reads, and is capped (8 MiB by default).

`cell.Renderer` keeps a front buffer of what the terminal shows and writes
only changed cells. For each run it picks the shorter of an absolute or
relative cursor move, or rewrites a few unchanged cells instead of moving. It
changes SGR attributes relative to the current pen state, or resets, whichever
is shorter. Blank runs of six or more cells use EL or ECH. Frames are wrapped
in synchronized output (mode 2026) when enabled. Colors downgrade from RGB to
256, 16 or none (`NO_COLOR`), and underline shapes and OSC 8 links are dropped
when the terminal lacks them.

`Buffer.Set` replaces text that contains control characters or invalid UTF-8
with U+FFFD, and the renderer strips non-printable bytes from link URLs.
Application text therefore cannot inject escape sequences.

## Decisions

2026-11-06, agreed in conversation:

- **From scratch.** No dependencies outside the Go team's modules.
  `golang.org/x/sys` provides termios and console calls. `ReadConsoleInputW`
  has no wrapper there, so it is loaded from kernel32 through `x/sys/windows`.
- **Retained tree with a declarative description.** The app describes elements
  every frame and a reconciler matches them to retained nodes by key. Mouse
  needs this: a press and its release can be frames apart, and hit testing,
  pointer capture, hover, scroll offsets and carets need node identity across
  frames.
- **Mouse is required.** Button, drag and wheel tracking are on by default, with
  SGR encoding. Any-motion tracking (mode 1003) is turned on only while
  something on screen reacts to hover, since it sends an event per cell crossed.
- **macOS, Linux and Windows from the start.**
- **Its own module** inside this repository, so it can move out later unchanged.
- **Autowrap is off while running.** Writing the bottom-right cell can then never
  scroll the screen.
- **Width follows the terminal, not Unicode.** Terminals disagree about emoji
  widths. `uni` will use grapheme cluster widths when the terminal supports
  mode 2027 and per-codepoint widths otherwise.

## Verification

This module is not listed in the repository's local `go.work`, so run its
commands with `GOWORK=off` (or add `./tui` to your `go.work`):

```sh
cd tui
GOWORK=off go test -race ./...
GOWORK=off go test -run '^$' -fuzz FuzzDecoder -fuzztime 60s ./input
GOWORK=off go test -run '^$' -bench . ./cell
for t in windows/amd64 windows/arm64 linux/amd64 linux/arm64 freebsd/amd64 darwin/amd64; do
  GOOS=${t%/*} GOARCH=${t#*/} GOWORK=off go vet ./...
done
```

What the tests cover:

- `cell`: the property test renders random frame sequences (resizes, wide and
  combining characters, styles, links, cursor moves, invalidation), applies the
  output to the `vt` emulator, and requires the emulator's grid to match each
  frame. It caught each of seven deliberately planted renderer bugs. Benchmark
  on an M-series Mac: a full 250×80 repaint takes about 1.3 ms with zero
  allocations; a one-cell change takes about 72 µs, mostly the diff scan.
- `input`: a table of real terminal sequences, byte-at-a-time delivery decoding
  identically to one read, paste limits, and a fuzz target (32 million runs
  without a failure so far).
- `term`: raw mode and exact termios restore, size, unbuffered binary-clean
  reads, read timeout, `Close` interrupting a blocked read, and restore order,
  all against a real pseudo-terminal on macOS and Linux.
- `cmd/tui-demo`: the demo runs on a pty while tests send real mouse bytes and
  check the emulated screen: click and drag painting, right-click erase, wheel,
  resize, the Escape timeout, signals, and restore on exit.

Not yet verified: Windows has been compiled and vetted, not run. The console
input path, including the mouse record conversion, needs a real Windows
Terminal and conhost session. SIGWINCH delivery and `Suspend` have not been
exercised by an automated test.
