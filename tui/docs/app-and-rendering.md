# Scheduling, clipboard callbacks, and independent cells

`(*app.App).After(d time.Duration, fn func()) (stop func())` posts one callback
to the app loop after `d`, then redraws. Use it for a delayed state change;
`Every` remains the repeating timer. Calling `stop` repeatedly is safe, including
from another goroutine. It cancels the callback until the callback starts,
even if the timer has already queued it. A quit app never runs the callback,
and returning from `Run` releases a pending timer.

```go
stop := a.After(2*time.Second, func() { message = "" })
// Cancel when the message is dismissed early:
stop()
```

`app.Options.Copied func(a *app.App, text string)` runs on the app loop after
a mouse selection has been sent to the terminal with OSC 52. It receives the
selected text and can update UI state for the next frame. Direct calls to
`a.Copy` do not invoke this selection callback. OSC 52 is a request; the
callback cannot confirm whether the terminal accepted the clipboard update.

`cell.Buffer.Set` stores independent graphemes. When a cell would continue
the cluster of the cell written just before it (`uni.Joins`: two lone
regional indicators, Hangul jamo, a pictograph after a ZWJ), the renderer
positions the cursor explicitly between them, so a mode 2027 terminal cannot
combine them. A cursor move ends a cluster in every terminal we model. Other
text, box drawing included, keeps the contiguous fast path, and the renderer
allocates nothing once its buffers have warmed up. Rewriting unchanged cells
to move the cursor is limited to ASCII.
Text made entirely of zero-width code points has no standalone glyph: `Set`
with width zero still writes nothing, while a positive supplied width stores
`cell.Replacement`. Combining sequences with a base, such as `é`, are kept.

The app registers cursor hiding with `term`, so suspend and close restore the
shell cursor. A full renderer repaint restores the requested cursor visibility,
including after resume or another program changes it.

The subprocess tests in `app/process_test.go` cover `term.Suspend` and kernel
SIGWINCH delivery on macOS and Linux. Each child has its own session, process
group, and controlling pty (`Setsid`/`Setctty`); the parent alone sends SIGTSTP
and SIGCONT and inspects the stopped child's termios and emulated modes.
The resize test changes the controlling pty's size with `TIOCSWINSZ`, without
calling `NotifyResize` or explicitly sending SIGWINCH.
