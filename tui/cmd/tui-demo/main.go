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
	"github.com/DonaldMurillo/gofastr-native/tui/term"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
)

func main() {
	t, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui-demo:", err)
		os.Exit(1)
	}
	err = run(t, notifySignals(), colorOptions())
	t.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui-demo:", err)
		os.Exit(1)
	}
}

func colorOptions() cell.Options {
	options := cell.Options{Colors: cell.Color256, SyncOutput: true}
	if ct := os.Getenv("COLORTERM"); ct == "truecolor" || ct == "24bit" {
		options.Colors = cell.ColorTrue
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		options.Colors = cell.ColorNone
	}
	return options
}

var palette = []cell.Color{
	cell.RGB(231, 76, 60), cell.RGB(241, 196, 15), cell.RGB(46, 204, 113),
	cell.RGB(52, 152, 219), cell.RGB(155, 89, 182), cell.RGB(236, 240, 241),
}

type demo struct {
	size    term.Size
	painted map[[2]int]cell.Color
	color   int
	last    string
	quit    bool
	suspend bool
}

// run drives one session until q, a terminating signal, or a read error.
// It restores the terminal on panic before re-panicking.
func run(t *term.Terminal, signals <-chan os.Signal, options cell.Options) (err error) {
	defer func() {
		if r := recover(); r != nil {
			t.Close()
			panic(r)
		}
	}()
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
	d := &demo{size: size, painted: map[[2]int]cell.Color{}, last: "waiting for input"}
	back := cell.NewBuffer(size.Cols, size.Rows)
	renderer := cell.NewRenderer(size.Cols, size.Rows, options)
	defer func() { t.Write(renderer.Reset()) }()

	events := make(chan input.Event, 256)
	readErr := make(chan error, 1)
	go readInput(t, events, readErr)

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

// readInput owns the decoder. When a lone ESC is held it waits only briefly
// for the rest of a sequence before deciding it was the Escape key.
func readInput(t *term.Terminal, events chan<- input.Event, readErr chan<- error) {
	var decoder input.Decoder
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
	drawText(b, 1, 0, "gofastr tui · drag to paint · right-click erases · scroll for color · c clears · q quits", title)
	drawText(b, 1, 1, "wide 界🙂 · combining é · color", cell.Style{Attrs: cell.Dim})
	b.Set(32, 1, " ", 1, cell.Style{Bg: palette[d.color]})
	b.Set(33, 1, " ", 1, cell.Style{Bg: palette[d.color]})
	for at, color := range d.painted {
		if d.inCanvas(at[0], at[1]) {
			b.Set(at[0], at[1], " ", 1, cell.Style{Bg: color})
		}
	}
	status := cell.Style{Fg: cell.BrightBlack}
	drawText(b, 1, b.Height()-1, fmt.Sprintf("%dx%d · %s", d.size.Cols, d.size.Rows, d.last), status)
}

// drawText draws a single line of text, clipped at the right edge.
// Width rules here are minimal until the uni package lands.
func drawText(b *cell.Buffer, x, y int, text string, s cell.Style) {
	runes := []rune(text)
	for i := 0; i < len(runes) && x < b.Width(); i++ {
		grapheme, width := string(runes[i]), max(vt.RuneWidth(runes[i]), 1)
		for i+1 < len(runes) && vt.RuneWidth(runes[i+1]) == 0 {
			i++
			grapheme += string(runes[i])
		}
		x += b.Set(x, y, grapheme, width, s)
	}
}
