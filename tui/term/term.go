// Package term owns the terminal device: raw mode, private modes, size and
// resize notification, cancellable reads, and restoring everything on exit.
//
// Every mode a program turns on is recorded, and Close turns them off in
// reverse order before restoring the original device settings. Close is safe
// to call more than once and from any goroutine, so it can run from a signal
// handler or a deferred panic recovery.
package term

import (
	"errors"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	// ErrNotTerminal means no terminal device is available.
	ErrNotTerminal = errors.New("term: not a terminal")
	// ErrClosed is returned by reads and writes after Close.
	ErrClosed = errors.New("term: closed")

	errCanceled = errors.New("term: read canceled")
)

// Size is the terminal size in cells.
type Size struct{ Cols, Rows int }

// Mode is a private terminal mode with its enable and disable sequences.
type Mode struct {
	Name    string
	Enable  string
	Disable string
}

// Modes a full-screen program typically enables, in this order.
var (
	AltScreen      = Mode{"alt-screen", "\x1b[?1049h", "\x1b[?1049l"}
	HideCursor     = Mode{"hide-cursor", "\x1b[?25l", "\x1b[?25h"}
	NoAutowrap     = Mode{"no-autowrap", "\x1b[?7l", "\x1b[?7h"}
	BracketedPaste = Mode{"bracketed-paste", "\x1b[?2004h", "\x1b[?2004l"}
	FocusEvents    = Mode{"focus-events", "\x1b[?1004h", "\x1b[?1004l"}
	// KittyKeyboard pushes the kitty keyboard protocol's disambiguate flag.
	// Terminals without the protocol ignore it.
	KittyKeyboard = Mode{"kitty-keyboard", "\x1b[>1u", "\x1b[<u"}
)

// MouseTracking is how much mouse input the terminal reports. The levels are
// exclusive: setting one replaces the previous one. All levels use SGR
// encoding, which has no coordinate limit and reports which button was released.
type MouseTracking uint8

const (
	MouseOff MouseTracking = iota
	// MouseButtons reports presses, releases and the wheel.
	MouseButtons
	// MouseDrag also reports motion while a button is held.
	MouseDrag
	// MouseMotion reports all motion, for hover. It sends an event per cell
	// crossed, so enable it only while something on screen reacts to hover.
	MouseMotion
)

var mouseEnable = [...]string{
	MouseOff:     "",
	MouseButtons: "\x1b[?1000h\x1b[?1006h",
	MouseDrag:    "\x1b[?1002h\x1b[?1006h",
	MouseMotion:  "\x1b[?1003h\x1b[?1006h",
}

var mouseDisable = [...]string{
	MouseOff:     "",
	MouseButtons: "\x1b[?1000l\x1b[?1006l",
	MouseDrag:    "\x1b[?1002l\x1b[?1006l",
	MouseMotion:  "\x1b[?1003l\x1b[?1006l",
}

// Terminal is an open terminal device in raw mode.
//
// One goroutine may call Read at a time. Write, Set and SetMouse may run
// concurrently with Read, and are serialized with each other and with Close
// so restore sequences never interleave with a frame.
type Terminal struct {
	sys *system

	writeMu sync.Mutex
	mu      sync.Mutex
	closed  bool
	reading bool
	freed   bool

	modes      []Mode
	mouse      MouseTracking
	resize     chan struct{}
	stopResize func()
}

// Open puts the terminal into raw mode. It uses standard input and output when
// both are terminals, and otherwise opens the controlling terminal directly
// (/dev/tty, or CONIN$ and CONOUT$ on Windows), so a program can draw a UI
// while its standard input or output is redirected.
func Open() (*Terminal, error) {
	sys, err := openSystem()
	if err != nil {
		return nil, err
	}
	return start(sys)
}

// OpenFiles uses the given terminal devices, for example the two ends a
// program received for an ssh session's pty. Both must be terminals, and on
// Windows console handles. The caller keeps ownership of the files. Resize
// signals only arrive for the process's controlling terminal; for other
// devices call NotifyResize when the size changes.
func OpenFiles(in, out *os.File) (*Terminal, error) {
	sys, err := openFiles(in, out)
	if err != nil {
		return nil, err
	}
	return start(sys)
}

func start(sys *system) (*Terminal, error) {
	if err := sys.makeRaw(); err != nil {
		sys.release()
		return nil, err
	}
	t := &Terminal{sys: sys, resize: make(chan struct{}, 1)}
	t.stopResize = sys.watchResize(t.notifyResize)
	return t, nil
}

// NotifyResize makes Resized fire, for size changes that do not arrive as a
// signal.
func (t *Terminal) NotifyResize() { t.notifyResize() }

func (t *Terminal) notifyResize() {
	select {
	case t.resize <- struct{}{}:
	default:
	}
}

// Resized receives a value after the terminal size changes, and after Suspend
// returns. Values coalesce: one receive may stand for several changes.
func (t *Terminal) Resized() <-chan struct{} { return t.resize }

// Size returns the current terminal size. Some devices (serial consoles, a
// pty nobody sized) report zero; then the size comes from the COLUMNS and
// LINES environment variables, and failing those is 80 by 24.
func (t *Terminal) Size() (Size, error) {
	size, err := t.sys.size()
	if err != nil {
		return Size{}, err
	}
	return withFallback(size, os.Getenv), nil
}

func withFallback(size Size, getenv func(string) string) Size {
	if size.Cols <= 0 {
		size.Cols = 80
		if n, err := strconv.Atoi(getenv("COLUMNS")); err == nil && n > 0 {
			size.Cols = n
		}
	}
	if size.Rows <= 0 {
		size.Rows = 24
		if n, err := strconv.Atoi(getenv("LINES")); err == nil && n > 0 {
			size.Rows = n
		}
	}
	return size
}

// Read reads input bytes. It returns 0, nil when timeout elapses first; a
// negative timeout waits indefinitely. After Close it returns ErrClosed, and
// Close interrupts a Read that is waiting.
func (t *Terminal) Read(p []byte, timeout time.Duration) (int, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return 0, ErrClosed
	}
	t.reading = true
	t.mu.Unlock()

	n, err := t.sys.read(p, timeout)

	t.mu.Lock()
	defer t.mu.Unlock()
	t.reading = false
	if t.closed {
		t.freeLocked()
		if err == errCanceled || err == nil && n == 0 {
			return 0, ErrClosed
		}
	}
	if err == errCanceled {
		return 0, ErrClosed
	}
	return n, err
}

// Write writes all of p, typically one rendered frame.
func (t *Terminal) Write(p []byte) (int, error) {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.isClosed() {
		return 0, ErrClosed
	}
	return t.sys.write(p)
}

func (t *Terminal) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// Set enables or disables a mode. Enabled modes are disabled by Close in
// reverse order of enabling.
func (t *Terminal) Set(m Mode, on bool) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.isClosed() {
		return ErrClosed
	}
	index := -1
	for i, active := range t.modes {
		if active.Name == m.Name {
			index = i
		}
	}
	switch {
	case on && index < 0:
		if _, err := t.sys.write([]byte(m.Enable)); err != nil {
			return err
		}
		t.modes = append(t.modes, m)
	case !on && index >= 0:
		if _, err := t.sys.write([]byte(m.Disable)); err != nil {
			return err
		}
		t.modes = append(t.modes[:index], t.modes[index+1:]...)
	}
	return nil
}

// SetMouse changes the mouse tracking level.
func (t *Terminal) SetMouse(level MouseTracking) error {
	if int(level) >= len(mouseEnable) {
		return errors.New("term: invalid mouse tracking level")
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.isClosed() {
		return ErrClosed
	}
	if level == t.mouse {
		return nil
	}
	sequence := mouseEnable[level]
	if level == MouseOff {
		sequence = mouseDisable[t.mouse]
	}
	if _, err := t.sys.write([]byte(sequence)); err != nil {
		return err
	}
	t.mouse = level
	t.sys.setMouse(level)
	return nil
}

// resetSequence turns off the mouse and every enabled mode, newest first,
// and resets the pen.
func (t *Terminal) resetSequence() []byte {
	out := []byte(mouseDisable[t.mouse])
	for i := len(t.modes) - 1; i >= 0; i-- {
		out = append(out, t.modes[i].Disable...)
	}
	return append(out, "\x1b[0m"...)
}

func (t *Terminal) enableSequence() []byte {
	var out []byte
	for _, m := range t.modes {
		out = append(out, m.Enable...)
	}
	return append(out, mouseEnable[t.mouse]...)
}

// Suspend restores the terminal, stops the process the way ctrl+z does, and
// re-enters raw mode with the same modes when the shell resumes it. The
// caller should repaint everything afterwards; Resized also fires.
// On Windows it returns errors.ErrUnsupported.
func (t *Terminal) Suspend() error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.isClosed() {
		return ErrClosed
	}
	if !t.sys.canSuspend() {
		return errors.ErrUnsupported
	}
	t.sys.write(t.resetSequence())
	if err := t.sys.restore(); err != nil {
		return err
	}
	stopErr := t.sys.suspend()
	if err := t.sys.makeRaw(); err != nil {
		return err
	}
	if _, err := t.sys.write(t.enableSequence()); err != nil {
		return err
	}
	t.notifyResize()
	return stopErr
}

// Close disables every mode, restores the original device settings and
// interrupts a waiting Read. It is idempotent and safe from any goroutine.
func (t *Terminal) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()

	t.writeMu.Lock()
	t.stopResize()
	_, writeErr := t.sys.write(t.resetSequence())
	restoreErr := t.sys.restore()
	t.modes, t.mouse = nil, MouseOff
	t.writeMu.Unlock()

	t.mu.Lock()
	if t.reading {
		t.sys.wake() // the reader frees the device when it returns
	} else {
		t.freeLocked()
	}
	t.mu.Unlock()
	return errors.Join(restoreErr, writeErr)
}

func (t *Terminal) freeLocked() {
	if !t.freed {
		t.freed = true
		t.sys.release()
	}
}
