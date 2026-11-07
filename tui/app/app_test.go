//go:build darwin || linux

package app

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/internal/ptytest"
	"github.com/DonaldMurillo/gofastr-native/tui/term"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
	"github.com/DonaldMurillo/gofastr-native/tui/widget"
)

// session runs an app on a pty and plays its output into an emulator.
type session struct {
	t       *testing.T
	master  *os.File
	term    *term.Terminal
	mu      sync.Mutex
	screen  *vt.Screen
	raw     strings.Builder
	signals chan os.Signal
	done    chan any // the error Run returned, or what it panicked with
}

func start(t *testing.T, cols, rows int, root func(*App) view.Element, opts Options) *session {
	t.Helper()
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)}); err != nil {
		t.Fatal(err)
	}
	terminal, err := term.OpenFiles(slave, slave)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{t: t, master: master, term: terminal, screen: vt.New(cols, rows), signals: make(chan os.Signal, 1), done: make(chan any, 1)}
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := master.Read(buf)
			s.mu.Lock()
			s.screen.Write(buf[:n])
			s.raw.Write(buf[:n])
			replies := s.screen.Replies
			s.screen.Replies = nil
			s.mu.Unlock()
			if len(replies) > 0 {
				master.Write(replies)
			}
			if err != nil {
				return
			}
		}
	}()
	opts.Terminal, opts.Signals = terminal, s.signals
	opts.Getenv = func(string) string { return "" }
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.done <- r
			}
		}()
		err := Run(root, opts)
		terminal.Close()
		s.done <- err
	}()
	t.Cleanup(func() {
		terminal.Close()
		slave.Close()
		master.Close()
	})
	return s
}

func (s *session) send(in string) {
	s.t.Helper()
	if _, err := s.master.Write([]byte(in)); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) waitFor(text string) {
	s.t.Helper()
	s.until(text, func() bool { return strings.Contains(s.screen.Text(), text) })
}

func (s *session) until(what string, check func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ok := check()
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t.Fatalf("never saw %s:\n%s\nerrors: %q", what, s.screen.Text(), s.screen.Errors)
}

func (s *session) exit() any {
	s.t.Helper()
	select {
	case r := <-s.done:
		time.Sleep(50 * time.Millisecond) // let the reader drain the restore
		return r
	case <-time.After(3 * time.Second):
		s.t.Fatal("app did not exit")
	}
	return nil
}

func (s *session) rawOutput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.String()
}

// mouse returns an SGR mouse report for zero-based x, y.
func mouse(button, x, y int, final byte) string {
	return fmt.Sprintf("\x1b[<%d;%d;%d%c", button, x+1, y+1, final)
}

type counter struct {
	n     int
	value string
	boom  bool
}

func (c *counter) root(*App) view.Element {
	return widget.Column(
		widget.Label(fmt.Sprintf("count %d", c.n)),
		widget.Row(widget.NewButton("Add", func() {
			if c.boom {
				panic("boom")
			}
			c.n++
		})),
		view.E(widget.Input{Value: c.value, OnChange: func(s string) { c.value = s }, AutoFocused: true, Placeholder: "type here"}),
	)
}

func TestClickTypeAndQuit(t *testing.T) {
	c := &counter{}
	s := start(t, 30, 5, c.root, Options{})
	s.waitFor("type here")
	s.send("hi there")
	s.waitFor("hi there")
	s.send(mouse(0, 1, 1, 'M') + mouse(0, 1, 1, 'm'))
	s.waitFor("count 1")
	s.send(" ") // the click focused the button
	s.waitFor("count 2")
	s.send("\x03") // ctrl+c
	if r := s.exit(); r != nil {
		t.Fatalf("run returned %v", r)
	}
	raw := s.rawOutput()
	for _, want := range []string{"\x1b[?1003h", "\x1b[?1049l", "\x1b[?2004l"} {
		if !strings.Contains(raw, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

func TestPostFromAnotherGoroutine(t *testing.T) {
	var app *App
	ready := make(chan struct{})
	text := "waiting"
	s := start(t, 20, 2, func(a *App) view.Element {
		if app == nil {
			app = a
			close(ready)
		}
		return widget.Label(text)
	}, Options{})
	s.waitFor("waiting")
	<-ready
	go app.Post(func() { text = "posted" })
	s.waitFor("posted")
	app.Quit()
	s.exit()
}

func TestEveryTicksOnTheLoop(t *testing.T) {
	ticks := 0
	var stop func()
	s := start(t, 20, 2, func(a *App) view.Element {
		if stop == nil {
			stop = a.Every(10*time.Millisecond, func() {
				ticks++
				if ticks == 3 {
					a.Quit()
				}
			})
		}
		return widget.Label(fmt.Sprintf("ticks %d", ticks))
	}, Options{})
	s.exit()
	stop()
	if ticks != 3 {
		t.Fatalf("ticks %d", ticks)
	}
}

func TestUnhandledSeesUnusedKeys(t *testing.T) {
	var seen []string
	s := start(t, 20, 2, func(*App) view.Element { return widget.Label("ready") }, Options{
		Unhandled: func(a *App, e input.Event) bool {
			if k, ok := e.(input.Key); ok {
				seen = append(seen, k.String())
				if k.Rune == 'q' {
					a.Quit()
				}
			}
			return false
		},
	})
	s.waitFor("ready")
	s.send("xq")
	s.exit()
	if strings.Join(seen, " ") != "x q" {
		t.Fatalf("seen %v", seen)
	}
}

func TestSelectionCopiesWithOSC52(t *testing.T) {
	s := start(t, 20, 2, func(*App) view.Element { return widget.Label("copy me please") }, Options{})
	s.waitFor("copy me")
	s.send(mouse(0, 0, 0, 'M') + mouse(32, 6, 0, 'M') + mouse(0, 6, 0, 'm'))
	s.until("clipboard", func() bool { return s.screen.Clipboard == "copy me" })
	s.signals <- syscall.SIGTERM
	s.exit()
}

func TestResizeRedraws(t *testing.T) {
	s := start(t, 20, 3, func(a *App) view.Element {
		return widget.Label(fmt.Sprintf("size %dx%d", a.Size().Cols, a.Size().Rows))
	}, Options{})
	s.waitFor("size 20x3")
	s.mu.Lock()
	s.screen.Resize(30, 4)
	s.mu.Unlock()
	unix.IoctlSetWinsize(int(s.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 30, Row: 4})
	// The pty is not this process's controlling terminal, so no SIGWINCH
	// arrives; a real terminal would send one.
	s.term.NotifyResize()
	s.waitFor("size 30x4")
	s.signals <- syscall.SIGTERM
	s.exit()
}

func TestPanicRestoresTerminal(t *testing.T) {
	c := &counter{boom: true}
	s := start(t, 30, 5, c.root, Options{})
	s.waitFor("count 0")
	s.send(mouse(0, 1, 1, 'M') + mouse(0, 1, 1, 'm'))
	if r := s.exit(); r != "boom" {
		t.Fatalf("exit %v, want the panic", r)
	}
	if raw := s.rawOutput(); !strings.Contains(raw, "\x1b[?1049l") {
		t.Fatal("alt screen not left after panic")
	}
}
