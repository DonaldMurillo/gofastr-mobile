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
| `probe` | Capability queries at startup, ended by a DA1 sentinel | done |
| `uni` | Grapheme clusters and column widths from generated Unicode 17 tables | done |
| `input` | Bytes to `Key`, `Mouse`, `Paste`, `Focus` and query replies | done |
| `term` | Raw mode, private modes, mouse tracking, size, resize, cancellable reads, restore | done |
| `vt` | In-memory terminal emulator used as the test oracle | done |

`probe` sits above `input` because it decodes the replies, and it reports
results as `cell` options and a `uni` width method.

`cmd/tui-demo` runs the whole startup path: probe, mode setup, then a paint
loop. Drag to paint, right-click to erase, scroll to change color. The status
line shows what the probe detected.

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

`probe.Run` writes one batch of queries: mode reports for 2026 and 2027, the
kitty keyboard flags, OSC 10 and 11 colors, XTVERSION, and DA1 last. Every
terminal answers DA1 and answers in order, so the DA1 reply ends the wait;
a terminal that answers nothing is given up on after 200 ms. Run takes the
caller's decoder and returns keys typed during the probe, so no input is lost.
Where the terminal is silent, `TERM`, `COLORTERM`, `NO_COLOR`, `TERM_PROGRAM`,
`VTE_VERSION` and `WT_SESSION` decide the color depth, styled underlines and
links.

`uni` splits text into the units a terminal draws. `uni.Codepoints` follows
terminals that advance per code point: a ZWJ family emoji takes 6 columns.
`uni.Graphemes` follows mode 2027, where each extended grapheme cluster
(UAX #29) is one character and the family takes 2. Lookups are a binary
search over 1,785 generated ranges with an ASCII fast path, and allocate
nothing.

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
  widths, so layout uses the model the terminal follows: grapheme clusters
  when the probe finds mode 2027 (turned on if it is off), per-code-point
  widths otherwise. A cluster takes the width of its widest code point;
  VS16 makes an emoji base 2 columns and VS15 makes it 1; a regional
  indicator pair is 2.
- **Unicode tables are generated, not hand-written.** `uni/internal/gen`
  builds them from the Unicode Character Database at the version Go's
  `unicode` package uses (17.0.0 for Go 1.27), so the two agree. The
  official GraphemeBreakTest.txt is checked in as test data.
- **Probe once, at startup, with a sentinel.** Querying later would mix
  replies into user input. A fixed timeout alone would either wait too long
  on every start or miss slow terminals; DA1 ends the wait as soon as the
  terminal has answered everything. Replies that arrive after the timeout
  reach the program as ordinary events, which it ignores.

## Verification

This module is not listed in the repository's local `go.work`, so run its
commands with `GOWORK=off` (or add `./tui` to your `go.work`):

```sh
cd tui
GOWORK=off go test -race ./...
GOWORK=off go test -run '^$' -fuzz FuzzDecoder -fuzztime 60s ./input
GOWORK=off go test -run '^$' -fuzz FuzzUnits -fuzztime 60s ./uni
GOWORK=off go test -run '^$' -bench . ./cell ./uni
(cd uni && GOWORK=off go generate)  # downloads the UCD once into the user cache
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
- `uni`: all 766 cases of the official grapheme break test, a width table
  covering emoji sequences, CJK, Hangul jamo, Indic conjuncts and controls in
  both methods, a sweep of every code point against the `unicode` package,
  and a fuzz target checking that units rebuild the input. Disabling the
  conjunct (GB9c), emoji ZWJ (GB11) or flag (GB12/13) rule fails the
  conformance test. Width of 180 bytes of ASCII takes 340 ns.
- `probe`: scripted terminals that answer fully, answer in 3-byte pieces,
  answer only DA1, or stay silent, with keys and clicks mixed into the
  replies, plus the environment fallbacks.
- `vt`: query replies, mode 2027 clustering (flags, ZWJ sequences, VS16
  widening) and per-code-point advance.
- `term`: raw mode and exact termios restore, size, unbuffered binary-clean
  reads, read timeout, `Close` interrupting a blocked read, and restore order,
  all against a real pseudo-terminal on macOS and Linux.
- `cmd/tui-demo`: the demo runs on a pty while tests send real mouse bytes and
  check the emulated screen: click and drag painting, right-click erase, wheel,
  resize, the Escape timeout, signals, and restore on exit. The emulator
  answers the probe, so the tests also cover mode 2027 being turned on, wide
  clusters laid out to the column, and the mode turned off on exit.

Not yet verified: Windows has been compiled and vetted, not run. The console
input path, including the mouse record conversion, needs a real Windows
Terminal and conhost session. SIGWINCH delivery and `Suspend` have not been
exercised by an automated test. The probe's terminal table and the
cluster-width rules come from documentation, not from runs against each
terminal; the per-terminal results still need checking by hand. Two clusters
drawn into adjacent cells by separate `Set` calls can merge on a mode 2027
terminal (two lone regional indicators become a flag), and the renderer does
not guard against that yet.
