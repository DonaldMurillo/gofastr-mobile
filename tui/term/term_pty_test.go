//go:build darwin || linux

package term

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DonaldMurillo/gofastr-native/tui/internal/ptytest"
)

// ptyTerminal opens a Terminal on the slave side of a fresh pty. The master
// side plays the terminal emulator: what the program writes arrives there.
type ptyTerminal struct {
	*Terminal
	master, slave *os.File
	mu            sync.Mutex
	output        bytes.Buffer
}

func newPTY(t *testing.T) *ptyTerminal {
	t.Helper()
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	p := &ptyTerminal{master: master, slave: slave}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			p.mu.Lock()
			p.output.Write(buf[:n])
			p.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	p.Terminal, err = OpenFiles(slave, slave)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.Terminal.Close()
		slave.Close()
		master.Close()
	})
	return p
}

// waitOutput waits until everything the program wrote contains want.
func (p *ptyTerminal) waitOutput(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := p.output.String()
		p.mu.Unlock()
		if strings.Contains(got, want) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t.Fatalf("output %q does not contain %q", p.output.String(), want)
	return ""
}

func TestRawModeAndRestore(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	before, err := unix.IoctlGetTermios(int(slave.Fd()), ioctlGetTermios)
	if err != nil {
		t.Fatal(err)
	}
	if before.Lflag&(unix.ICANON|unix.ECHO) == 0 {
		t.Fatal("a fresh pty should start in cooked mode")
	}
	terminal, err := OpenFiles(slave, slave)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := unix.IoctlGetTermios(int(slave.Fd()), ioctlGetTermios)
	if raw.Lflag&(unix.ICANON|unix.ECHO|unix.ISIG) != 0 || raw.Oflag&unix.OPOST != 0 || raw.Iflag&unix.ICRNL != 0 {
		t.Fatalf("not raw: lflag=%#x oflag=%#x iflag=%#x", raw.Lflag, raw.Oflag, raw.Iflag)
	}
	if err := terminal.Close(); err != nil {
		t.Fatal(err)
	}
	after, _ := unix.IoctlGetTermios(int(slave.Fd()), ioctlGetTermios)
	if after.Lflag != before.Lflag || after.Iflag != before.Iflag || after.Oflag != before.Oflag || after.Cflag != before.Cflag {
		t.Fatalf("termios not restored:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestSize(t *testing.T) {
	p := newPTY(t)
	if err := unix.IoctlSetWinsize(int(p.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 37, Col: 121}); err != nil {
		t.Fatal(err)
	}
	size, err := p.Size()
	if err != nil {
		t.Fatal(err)
	}
	if size != (Size{Cols: 121, Rows: 37}) {
		t.Fatalf("size = %+v", size)
	}
}

func TestReadIsUnbufferedAndBinaryClean(t *testing.T) {
	p := newPTY(t)
	// No newline: raw mode must deliver bytes immediately, CR untranslated,
	// and ctrl+c as a byte rather than a signal.
	if _, err := p.master.Write([]byte("a\r\x03\x1b[A")); err != nil {
		t.Fatal(err)
	}
	var got []byte
	buf := make([]byte, 64)
	deadline := time.Now().Add(2 * time.Second)
	for len(got) < 6 && time.Now().Before(deadline) {
		n, err := p.Read(buf, 500*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if string(got) != "a\r\x03\x1b[A" {
		t.Fatalf("read %q", got)
	}
}

func TestReadTimeout(t *testing.T) {
	p := newPTY(t)
	start := time.Now()
	n, err := p.Read(make([]byte, 8), 50*time.Millisecond)
	elapsed := time.Since(start)
	if n != 0 || err != nil {
		t.Fatalf("Read = %d, %v", n, err)
	}
	if elapsed < 40*time.Millisecond || elapsed > time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}

func TestCloseInterruptsBlockedRead(t *testing.T) {
	p := newPTY(t)
	done := make(chan error, 1)
	go func() {
		_, err := p.Read(make([]byte, 8), -1)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("blocked Read returned %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not interrupt Read")
	}
	if _, err := p.Write([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Write after Close = %v", err)
	}
	if _, err := p.Read(make([]byte, 8), 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("Read after Close = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}

func TestCloseDisablesModesInReverseOrder(t *testing.T) {
	p := newPTY(t)
	for _, m := range []Mode{AltScreen, HideCursor, NoAutowrap, BracketedPaste} {
		if err := p.Set(m, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.SetMouse(MouseDrag); err != nil {
		t.Fatal(err)
	}
	if err := p.SetMouse(MouseMotion); err != nil { // replaces drag tracking
		t.Fatal(err)
	}
	if err := p.Set(BracketedPaste, false); err != nil {
		t.Fatal(err)
	}
	p.waitOutput(t, "\x1b[?2004l")
	p.mu.Lock()
	p.output.Reset()
	p.mu.Unlock()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[?1003l\x1b[?1006l" + NoAutowrap.Disable + HideCursor.Disable + AltScreen.Disable + "\x1b[0m"
	if got := p.waitOutput(t, want); got != want {
		t.Fatalf("restore output %q, want %q", got, want)
	}
}

func TestNotifyResizeCoalesces(t *testing.T) {
	p := newPTY(t)
	p.NotifyResize()
	p.NotifyResize()
	select {
	case <-p.Resized():
	case <-time.After(time.Second):
		t.Fatal("no resize notification")
	}
	select {
	case <-p.Resized():
		t.Fatal("notifications did not coalesce")
	default:
	}
}

func TestZeroSizeFallsBack(t *testing.T) {
	env := map[string]string{"COLUMNS": "132", "LINES": "x"}
	got := withFallback(Size{}, func(k string) string { return env[k] })
	if got != (Size{Cols: 132, Rows: 24}) {
		t.Fatalf("fallback = %+v", got)
	}
	if got := withFallback(Size{Cols: 10, Rows: 5}, func(string) string { return "99" }); got != (Size{Cols: 10, Rows: 5}) {
		t.Fatalf("a reported size was replaced: %+v", got)
	}
}
