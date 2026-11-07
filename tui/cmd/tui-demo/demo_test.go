//go:build darwin || linux

package main

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/internal/ptytest"
	"github.com/DonaldMurillo/gofastr-native/tui/term"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
)

// session runs the demo on a pty and plays everything it writes into an
// emulator, so assertions are about the screen a user would see.
type session struct {
	t        *testing.T
	master   *os.File
	terminal *term.Terminal
	mu       sync.Mutex
	screen   *vt.Screen
	raw      strings.Builder
	signals  chan os.Signal
	done     chan error
}

func start(t *testing.T, cols, rows int) *session {
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
	s := &session{t: t, master: master, terminal: terminal, screen: vt.New(cols, rows), signals: make(chan os.Signal, 1), done: make(chan error, 1)}
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := master.Read(buf)
			s.mu.Lock()
			s.screen.Write(buf[:n])
			s.raw.Write(buf[:n])
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() {
		s.done <- run(terminal, s.signals, cell.Options{Colors: cell.ColorTrue, SyncOutput: true})
		terminal.Close()
	}()
	t.Cleanup(func() {
		terminal.Close()
		slave.Close()
		master.Close()
	})
	s.waitFor("waiting for input", func(*vt.Screen) bool { return true })
	return s
}

func (s *session) send(input string) {
	s.t.Helper()
	if _, err := s.master.Write([]byte(input)); err != nil {
		s.t.Fatal(err)
	}
}

// waitFor waits until the screen shows text and check passes.
func (s *session) waitFor(text string, check func(*vt.Screen) bool) {
	s.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ok := strings.Contains(s.screen.Text(), text) && check(s.screen)
		s.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t.Fatalf("screen never showed %q:\n%s\nerrors: %q", text, s.screen.Text(), s.screen.Errors)
}

func (s *session) exit() string {
	s.t.Helper()
	select {
	case err := <-s.done:
		if err != nil {
			s.t.Fatalf("run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		s.t.Fatal("demo did not exit")
	}
	time.Sleep(50 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw.String()
}

func TestClickAndDragPaint(t *testing.T) {
	s := start(t, 40, 12)
	painted := func(x, y int) func(*vt.Screen) bool {
		return func(screen *vt.Screen) bool { return !screen.Cell(x, y).Style.Bg.IsDefault() }
	}
	// Press at column 5, row 4 (SGR is 1-based), drag two cells right, release.
	s.send("\x1b[<0;5;4M")
	s.waitFor("mouse left press 4,3", painted(4, 3))
	s.send("\x1b[<32;6;4M\x1b[<32;7;4M\x1b[<0;7;4m")
	s.waitFor("mouse left release 6,3", func(screen *vt.Screen) bool {
		return painted(5, 3)(screen) && painted(6, 3)(screen) && screen.Cell(7, 3).Style.Bg.IsDefault()
	})
	// Right-click erases.
	s.send("\x1b[<2;6;4M")
	s.waitFor("mouse right press 5,3", func(screen *vt.Screen) bool { return screen.Cell(5, 3).Style.Bg.IsDefault() })
	s.send("q")
	s.exit()
}

func TestWheelChangesColor(t *testing.T) {
	s := start(t, 40, 12)
	s.send("\x1b[<0;2;4M")
	s.waitFor("press 1,3", func(screen *vt.Screen) bool { return !screen.Cell(1, 3).Style.Bg.IsDefault() })
	s.mu.Lock()
	first := s.screen.Cell(1, 3).Style.Bg
	s.mu.Unlock()
	s.send("\x1b[<64;2;4M\x1b[<0;3;4M")
	s.waitFor("press 2,3", func(screen *vt.Screen) bool {
		bg := screen.Cell(2, 3).Style.Bg
		return !bg.IsDefault() && bg != first
	})
	s.send("q")
	s.exit()
}

func TestResizeRepaintsAtNewSize(t *testing.T) {
	s := start(t, 40, 12)
	s.mu.Lock()
	s.screen.Resize(60, 15) // the emulator window grows first, as a real one would
	s.mu.Unlock()
	if err := unix.IoctlSetWinsize(int(s.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 60, Row: 15}); err != nil {
		t.Fatal(err)
	}
	// The pty is not this process's controlling terminal, so no SIGWINCH.
	s.terminal.NotifyResize()
	s.waitFor("resize 60x15", func(screen *vt.Screen) bool {
		return strings.Contains(screen.Line(14), "60x15") && !strings.Contains(screen.Line(11), "40x12")
	})
	s.send("q")
	s.exit()
}

func TestEscapeKeyResolvesAfterTimeout(t *testing.T) {
	s := start(t, 40, 12)
	s.send("\x1b")
	s.waitFor("key esc", func(*vt.Screen) bool { return true })
	s.send("\x1b[1;5C")
	s.waitFor("key ctrl+right", func(*vt.Screen) bool { return true })
	s.send("q")
	s.exit()
}

func TestExitRestoresTerminal(t *testing.T) {
	s := start(t, 40, 12)
	s.send("q")
	raw := s.exit()
	for _, want := range []string{"\x1b[?1002l", "\x1b[?2004l", "\x1b[?7h", "\x1b[?1049l", "\x1b[0m"} {
		if !strings.Contains(raw, want) {
			t.Errorf("exit output lacks %q", want)
		}
	}
	if strings.LastIndex(raw, "\x1b[?1049l") < strings.LastIndex(raw, "\x1b[?2004l") {
		t.Error("alternate screen was left before the modes enabled after it were disabled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.screen.Errors) > 0 {
		t.Fatalf("emulator errors: %q", s.screen.Errors)
	}
}

func TestTerminatingSignalExits(t *testing.T) {
	s := start(t, 40, 12)
	s.signals <- syscall.SIGTERM
	s.exit()
}
