// Command tui-demo exercises the terminal, input and cell layers end to end:
// click or drag to paint, right-click to erase, scroll to change color.
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/probe"
	"github.com/DonaldMurillo/gofastr-native/tui/term"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

func main() {
	t, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui-demo:", err)
		os.Exit(1)
	}
	err = run(t, notifySignals(), os.Getenv)
	t.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui-demo:", err)
		os.Exit(1)
	}
}

var palette = []cell.Color{
	cell.RGB(231, 76, 60), cell.RGB(241, 196, 15), cell.RGB(46, 204, 113),
	cell.RGB(52, 152, 219), cell.RGB(155, 89, 182), cell.RGB(236, 240, 241),
}

type demo struct {
	caps    probe.Result
	method  uni.Method
	size    term.Size
	painted map[[2]int]cell.Color
	color   int
	last    string
	quit    bool
	suspend bool
}

// run drives one session until q, a terminating signal, or a read error.
// It restores the terminal on panic before re-panicking.
func run(t *term.Terminal, signals <-chan os.Signal, getenv func(string) string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			t.Close()
			panic(r)
		}
	}()
	var decoder input.Decoder
	caps, early, err := probe.Run(t, &decoder, probe.DefaultTimeout, getenv)
	if err != nil {
		return err
	}
	if caps.Graphemes == probe.Available {
		if err := t.Set(term.GraphemeClusters, true); err != nil {
			return err
		}
	}
	for _, m := range []term.Mode{term.AltScreen, term.NoAutowrap, term.BracketedPaste, term.FocusEvents, term.KittyKeyboard} {
		if err := t.Set(m, true); err != nil {
			return err
		}
	}
	if err := t.SetMouse(term.MouseDrag); err != nil {
		return err
	}
	size, err := t.Size()
	if err != nil {
		return err
	}
	d := &demo{caps: caps, method: caps.Method(), size: size, painted: map[[2]int]cell.Color{}, last: "waiting for input"}
	for _, e := range early {
		d.handle(e)
	}
	back := cell.NewBuffer(size.Cols, size.Rows)
	renderer := cell.NewRenderer(size.Cols, size.Rows, caps.Options())
	defer func() { t.Write(renderer.Reset()) }()

	events := make(chan input.Event, 256)
	readErr := make(chan error, 1)
	go readInput(t, &decoder, events, readErr)

	for {
		back.Resize(d.size.Cols, d.size.Rows)
		d.draw(back)
		if _, err := t.Write(renderer.Render(back, cell.Cursor{})); err != nil {
			renderer.Invalidate()
			return err
		}
		select {
		case e := <-events:
			d.handle(e)
			for drained := false; !drained; {
				select {
				case e := <-events:
					d.handle(e)
				default:
					drained = true
				}
			}
		case <-t.Resized():
			if d.size, err = t.Size(); err != nil {
				return err
			}
			d.last = fmt.Sprintf("resize %dx%d", d.size.Cols, d.size.Rows)
		case sig := <-signals:
			if !isSuspend(sig) {
				return nil
			}
			d.suspend = true
		case err := <-readErr:
			if errors.Is(err, term.ErrClosed) {
				return nil
			}
			return err
		}
		if d.quit {
			return nil
		}
		if d.suspend {
			d.suspend = false
			if err := t.Suspend(); err == nil {
				renderer.Invalidate()
			}
		}
	}
}

// readInput takes over the decoder from the probe. When a lone ESC is held
// it waits only briefly for the rest of a sequence before deciding it was the
// Escape key.
func readInput(t *term.Terminal, decoder *input.Decoder, events chan<- input.Event, readErr chan<- error) {
	emit := func(e input.Event) { events <- e }
	buf := make([]byte, 4096)
	for {
		timeout := time.Duration(-1)
		if decoder.Pending() {
			timeout = 30 * time.Millisecond
		}
		n, err := t.Read(buf, timeout)
		if err != nil {
			readErr <- err
			return
		}
		if n == 0 {
			decoder.Flush(emit)
			continue
		}
		decoder.Feed(buf[:n], emit)
	}
}

func (d *demo) handle(e input.Event) {
	switch e := e.(type) {
	case input.Key:
		d.last = "key " + e.String()
		switch {
		case e.Code == input.KeyRune && e.Rune == 'q' && e.Mods == 0, e.Code == input.KeyRune && e.Rune == 'c' && e.Mods == input.ModCtrl:
			d.quit = true
		case e.Code == input.KeyRune && e.Rune == 'z' && e.Mods == input.ModCtrl:
			d.suspend = true
		case e.Code == input.KeyRune && e.Rune == 'c' && e.Mods == 0:
			clear(d.painted)
		}
	case input.Mouse:
		d.last = fmt.Sprintf("mouse %s %s %d,%d %s", e.Button, e.Action, e.X, e.Y, e.Mods)
		switch {
		case e.Button == input.ButtonWheelUp:
			d.color = (d.color + 1) % len(palette)
		case e.Button == input.ButtonWheelDown:
			d.color = (d.color + len(palette) - 1) % len(palette)
		case e.Button == input.ButtonLeft && (e.Action == input.MousePress || e.Action == input.MouseDrag):
			if d.inCanvas(e.X, e.Y) {
				d.painted[[2]int{e.X, e.Y}] = palette[d.color]
			}
		case e.Button == input.ButtonRight && (e.Action == input.MousePress || e.Action == input.MouseDrag):
			delete(d.painted, [2]int{e.X, e.Y})
		}
	case input.Paste:
		d.last = fmt.Sprintf("paste %d bytes", len(e.Text))
	case input.Focus:
		d.last = fmt.Sprintf("focus %v", e.Focused)
	}
}

func (d *demo) inCanvas(x, y int) bool {
	return x >= 0 && x < d.size.Cols && y >= 2 && y < d.size.Rows-1
}

func (d *demo) draw(b *cell.Buffer) {
	b.Clear()
	title := cell.Style{Attrs: cell.Reverse | cell.Bold}
	b.Fill(0, 0, b.Width(), 1, title)
	d.drawText(b, 1, 0, "gofastr tui · drag to paint · right-click erases · scroll for color · c clears · q quits", title)
	x := d.drawText(b, 1, 1, "wide 界🙂 · combining e\u0301 · flag 🇯🇵 · family 👨\u200d👩\u200d👧 · color ", cell.Style{Attrs: cell.Dim})
	b.Fill(x, 1, 2, 1, cell.Style{Bg: palette[d.color]})
	for at, color := range d.painted {
		if d.inCanvas(at[0], at[1]) {
			b.Set(at[0], at[1], " ", 1, cell.Style{Bg: color})
		}
	}
	status := cell.Style{Fg: cell.BrightBlack}
	d.drawText(b, 1, b.Height()-1, fmt.Sprintf("%dx%d · %s · %s", d.size.Cols, d.size.Rows, d.last, d.capabilities()), status)
}

func (d *demo) capabilities() string {
	name := d.caps.Name
	if name == "" {
		name = "unknown terminal"
	}
	colors := map[cell.ColorMode]string{cell.ColorNone: "no color", cell.Color16: "16 colors", cell.Color256: "256 colors", cell.ColorTrue: "truecolor"}[d.caps.Colors]
	return fmt.Sprintf("%s, %s, %s", name, colors, d.method)
}

// drawText draws one line of text clipped at the right edge and returns the
// column after it.
func (d *demo) drawText(b *cell.Buffer, x, y int, text string, s cell.Style) int {
	for unit, width := range d.method.Units(text) {
		if x >= b.Width() {
			break
		}
		if width > 0 {
			x += b.Set(x, y, unit, width, s)
		}
	}
	return x
}
