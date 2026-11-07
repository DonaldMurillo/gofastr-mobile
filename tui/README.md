# tui

A terminal UI renderer written from scratch in Go. It depends on the standard
library and `golang.org/x/sys` only. It is its own module and does not import
`nativeui`; a `nativeui` adapter can sit on top of it later.

## Layers

Each layer depends only on the layers below it.

| Package | Owns | Status |
|---|---|---|
| `app` | Event loop, redraw scheduling, `Post` and `Every` for background work, OSC 52 copy | done |
| `widget` | Text, flex, box, panel, button, input, textarea, scroll, list, table, split, tabs, sparkline, modal | done |
| `view` | Element tree reconciled by key, focus and traps, hit testing, pointer capture, hover, selection, layers | done |
| `layout` | Constraints down, sizes up; sizing rules and space distribution | done |
| `cell` | Cells, styles, color downgrade, the `Buffer`, and the diffing `Renderer` | done |
| `probe` | Capability queries at startup, ended by a DA1 sentinel | done |
| `uni` | Grapheme clusters and column widths from generated Unicode 17 tables | done |
| `input` | Bytes to `Key`, `Mouse`, `Paste`, `Focus` and query replies | done |
| `term` | Raw mode, private modes, mouse tracking, size, resize, cancellable reads, restore | done |
| `vt` | In-memory terminal emulator used as the test oracle | done |

`probe` sits above `input` because it decodes the replies, and it reports
results as `cell` options and a `uni` width method.

`cmd/netmon` is the test app: a read-only network connection monitor built on
every layer. `cmd/tui-demo` runs the whole startup path: probe, mode setup, then a paint
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

`view` keeps a retained tree behind a declarative description. Each frame the
program returns an `Element` tree of plain values. The tree matches them to
the nodes it kept by widget type and key (position when unkeyed), so a node's
state, focus, hover and pointer capture survive the rebuild. A `Builder`
widget produces its children during layout, once it knows its size; the list
uses this to build only the visible rows. A press focuses the nearest
focusable ancestor of the cell under the pointer, then goes to the deepest
node that takes it, and that node captures the pointer until every button is
up. Presses on the same cell within 400 ms count as a double or triple click.
A left drag that no node takes selects screen text, and the release copies
it. A `FocusTrap` widget (the modal) keeps Tab inside itself and gives focus
back to the node that had it when it closes.

`widget` widgets are controlled: they draw the `Value` they are given and
report edits through `OnChange`, so the program's state is the only copy.
State that belongs to the widget, such as a caret or a scroll offset, lives on
its node. `List` and `Table` handle a million rows because they lay out only
the rows on screen. Column widths come from the `layout` sizing rules over
the visible rows.

`app` owns the terminal for the life of `Run`. It probes, enables the alt
screen, bracketed paste, focus events and the kitty keyboard protocol, then
reads input on a goroutine. Everything that touches the tree runs on one loop
goroutine; `Post` hands it work from elsewhere and `Every` posts on a timer,
dropping ticks while one is still queued. After any event that changed
state the loop rebuilds the tree before the next event. A burst of events
still costs one terminal write, because the loop drains the queue before
flushing. Ctrl+C quits and ctrl+z suspends unless a widget or `Unhandled`
takes the key. A panic restores the terminal before it propagates.

## netmon

`cmd/netmon` lists the machine's sockets and the processes holding them. It
reads `lsof -nP -i` on macOS and the BSDs, `/proc/net` and `/proc/*/fd` on
Linux, and `netstat -ano` with `tasklist` on Windows. It never closes a
connection or signals a process. Without root it sees only the current
user's processes.

```sh
cd tui
GOWORK=off go run ./cmd/netmon              # refresh every 2 s
GOWORK=off go run ./cmd/netmon -every 500ms
GOWORK=off go run ./cmd/netmon -fake 5000   # 5,000 simulated sockets that churn
```

Click a column header or press 1 to 6 to sort, and again to reverse. Type `/`
to filter: every word must match the protocol, an address, the state, the
PID or the process. Enter or a double click filters by the selected row's
process. Escape leaves the filter, then clears it. `p` pauses, `r` refreshes,
`?` lists the keys and `q` quits. The line above the details pane drags to
resize it. Sockets that appeared in the latest refresh are green, and the
selection stays on its socket when rows move.

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

2026-11-07, found while building the widgets and netmon:

- **Rebuild between events.** Controlled widgets compute an edit from the
  value they were built with. With one build per burst, typing "hello" in
  one read left only "o", since each key edited the same stale value. The
  loop now rebuilds after any event that changed state, and still writes to
  the terminal once per burst.
- **Flex sizes its cross axis to its content** unless the constraint is
  tight. A row inside a column used to stretch to the column's full height.
- **The split divider is its own node.** `Hovered` is true for every
  ancestor of the hovered node, so a divider drawn by the split highlighted
  whenever either pane was hovered.
- **Copy uses OSC 52.** It is the only clipboard path that works over ssh and
  needs no platform code. Terminals without it ignore the request.

## Verification

This module is not listed in the repository's local `go.work`, so run its
commands with `GOWORK=off` (or add `./tui` to your `go.work`):

```sh
cd tui
GOWORK=off go test -race ./...
GOWORK=off go test -run '^$' -fuzz FuzzDecoder -fuzztime 60s ./input
GOWORK=off go test -run '^$' -fuzz FuzzUnits -fuzztime 60s ./uni
GOWORK=off go test -run '^$' -bench . ./cell ./uni
GOWORK=off go run ./cmd/netmon -fake 5000   # by hand: a load test
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
- `layout` and `view`: space distribution, keyed reconcile keeping state
  across reorders, hit testing through layers and clips, capture, hover
  enter and leave, click counting, focus order, traps and restore, and text
  selection.
- `widget`: each widget rendered through a real tree into a buffer and driven
  with key and mouse events, including a list of 1,000,000 rows, table
  sorting by header click, split dragging, modal focus, and the nested-flex
  regression. Mutation checks confirmed that tests fail when the
  release-inside-button check or the modal's Tab trap is removed.
- `app`: the loop on a pty: typing and clicking, `Post` from another
  goroutine, `Every`, `Unhandled`, selection copied as OSC 52, resize, and
  terminal restore after a panic. A mutation check confirmed the typing test
  fails without the rebuild between events.
- `cmd/netmon`: the lsof, `/proc/net` (IPv4, IPv6 and IPv4-mapped),
  netstat and tasklist parsers against fixtures, and the model driven through
  a real tree: filter, sort by key and by header click, selection tracking
  across refreshes, double click and Enter, pause, the busy guard, errors,
  the help modal, new-row highlighting, an 80x24 layout check, and a
  50,000-row table. Removing the busy guard or the select-by-key lookup fails
  the tests. On macOS the binary also ran on a pty, against lsof and with
  `-fake 5000 -every 100ms`, and exited cleanly.
- `cmd/tui-demo`: the demo runs on a pty while tests send real mouse bytes and
  check the emulated screen: click and drag painting, right-click erase, wheel,
  resize, the Escape timeout, signals, and restore on exit. The emulator
  answers the probe, so the tests also cover mode 2027 being turned on, wide
  clusters laid out to the column, and the mode turned off on exit.

Not yet verified: Windows has been compiled and vetted, not run, and so has
netmon's netstat source. The console
input path, including the mouse record conversion, needs a real Windows
Terminal and conhost session. SIGWINCH delivery and `Suspend` have not been
exercised by an automated test. The probe's terminal table and the
cluster-width rules come from documentation, not from runs against each
terminal; the per-terminal results still need checking by hand. Two clusters
drawn into adjacent cells by separate `Set` calls can merge on a mode 2027
terminal (two lone regional indicators become a flag), and the renderer does
not guard against that yet.
