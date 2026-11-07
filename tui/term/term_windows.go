//go:build windows

package term

import (
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ReadConsoleInputW has no wrapper in x/sys/windows.
var procReadConsoleInput = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleInputW")

// inputRecord mirrors INPUT_RECORD: a WORD event type, two bytes of padding,
// and a 16-byte union. kind is read as a DWORD to keep 4-byte alignment.
type inputRecord struct {
	kind  uint32
	event [16]byte
}

const (
	keyEvent        = 0x0001
	mouseEvent      = 0x0002
	windowSizeEvent = 0x0004

	vkMenu = 0x12

	shiftPressed = 0x0010
	altPressed   = 0x0001 | 0x0002
	ctrlPressed  = 0x0004 | 0x0008

	mouseMoved    = 0x0001
	mouseWheeled  = 0x0004
	mouseHWheeled = 0x0008
)

type system struct {
	in, out         windows.Handle
	ownIn, ownOut   bool
	inMode, outMode uint32
	inCP, outCP     uint32
	saveValid       bool
	cancel          windows.Handle
	records         [64]inputRecord
	pending         []byte
	highSurrogate   uint16
	buttons         uint32
	motion          bool
	notifyMu        sync.Mutex
	notify          func()
}

func isConsole(h windows.Handle) bool {
	var mode uint32
	return windows.GetConsoleMode(h, &mode) == nil
}

func openConsole(name string) (windows.Handle, error) {
	path, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(path, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

func openSystem() (*system, error) {
	s := &system{in: windows.Handle(windows.Stdin), out: windows.Handle(windows.Stdout)}
	if !isConsole(s.in) {
		h, err := openConsole("CONIN$")
		if err != nil || !isConsole(h) {
			return nil, ErrNotTerminal
		}
		s.in, s.ownIn = h, true
	}
	if !isConsole(s.out) {
		h, err := openConsole("CONOUT$")
		if err != nil || !isConsole(h) {
			s.release()
			return nil, ErrNotTerminal
		}
		s.out, s.ownOut = h, true
	}
	return s.init()
}

func openFiles(in, out *os.File) (*system, error) {
	s := &system{in: windows.Handle(in.Fd()), out: windows.Handle(out.Fd())}
	if !isConsole(s.in) || !isConsole(s.out) {
		return nil, ErrNotTerminal
	}
	return s.init()
}

func (s *system) init() (*system, error) {
	cancel, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		s.release()
		return nil, err
	}
	s.cancel = cancel
	return s, nil
}

func (s *system) makeRaw() error {
	if !s.saveValid {
		if err := windows.GetConsoleMode(s.in, &s.inMode); err != nil {
			return err
		}
		if err := windows.GetConsoleMode(s.out, &s.outMode); err != nil {
			return err
		}
		s.inCP, _ = windows.GetConsoleCP()
		s.outCP, _ = windows.GetConsoleOutputCP()
		s.saveValid = true
	}
	in := s.inMode&^(windows.ENABLE_ECHO_INPUT|windows.ENABLE_LINE_INPUT|windows.ENABLE_PROCESSED_INPUT|windows.ENABLE_QUICK_EDIT_MODE) |
		windows.ENABLE_VIRTUAL_TERMINAL_INPUT | windows.ENABLE_WINDOW_INPUT | windows.ENABLE_MOUSE_INPUT | windows.ENABLE_EXTENDED_FLAGS
	if err := windows.SetConsoleMode(s.in, in); err != nil {
		return err
	}
	out := s.outMode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(s.out, out); err != nil {
		windows.SetConsoleMode(s.in, s.inMode)
		return errors.New("term: this console does not support VT sequences (Windows 10 1809 or later is required)")
	}
	windows.SetConsoleCP(65001)
	windows.SetConsoleOutputCP(65001)
	return nil
}

func (s *system) restore() error {
	if !s.saveValid {
		return nil
	}
	err := errors.Join(windows.SetConsoleMode(s.in, s.inMode), windows.SetConsoleMode(s.out, s.outMode))
	if s.inCP != 0 {
		windows.SetConsoleCP(s.inCP)
	}
	if s.outCP != 0 {
		windows.SetConsoleOutputCP(s.outCP)
	}
	return err
}

func (s *system) window() (windows.SmallRect, error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(s.out, &info); err != nil {
		return windows.SmallRect{}, err
	}
	return info.Window, nil
}

func (s *system) size() (Size, error) {
	w, err := s.window()
	if err != nil {
		return Size{}, err
	}
	return Size{Cols: int(w.Right-w.Left) + 1, Rows: int(w.Bottom-w.Top) + 1}, nil
}

// read waits on the console input handle and the cancel event, then converts
// input records to bytes: key characters (VT sequences in VT input mode) and
// mouse records re-encoded as SGR mouse sequences, so one decoder serves
// every platform. Resize records notify watchResize.
func (s *system) read(p []byte, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for len(s.pending) == 0 {
		wait := uint32(windows.INFINITE)
		if timeout >= 0 {
			wait = uint32(max(time.Until(deadline), 0) / time.Millisecond)
		}
		event, err := windows.WaitForMultipleObjects([]windows.Handle{s.in, s.cancel}, false, wait)
		switch {
		case err != nil:
			return 0, err
		case event == uint32(windows.WAIT_TIMEOUT):
			return 0, nil
		case event == windows.WAIT_OBJECT_0+1:
			windows.ResetEvent(s.cancel)
			return 0, errCanceled
		}
		var count uint32
		r, _, callErr := procReadConsoleInput.Call(uintptr(s.in), uintptr(unsafe.Pointer(&s.records[0])), uintptr(len(s.records)), uintptr(unsafe.Pointer(&count)))
		if r == 0 {
			return 0, callErr
		}
		for i := range s.records[:count] {
			s.convert(&s.records[i])
		}
		if len(s.pending) == 0 && timeout >= 0 && time.Now().After(deadline) {
			return 0, nil
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[:copy(s.pending, s.pending[n:])]
	return n, nil
}

func (s *system) convert(record *inputRecord) {
	e := record.event[:]
	switch record.kind & 0xffff {
	case keyEvent:
		down := binary.LittleEndian.Uint32(e[0:]) != 0
		repeat := max(int(binary.LittleEndian.Uint16(e[4:])), 1)
		vk := binary.LittleEndian.Uint16(e[6:])
		char := binary.LittleEndian.Uint16(e[10:])
		// Alt+numpad entry delivers its character on the alt key's release.
		if char == 0 || !down && vk != vkMenu {
			return
		}
		for range repeat {
			s.addUTF16(char)
		}
	case mouseEvent:
		s.addMouse(e)
	case windowSizeEvent:
		s.notifyMu.Lock()
		notify := s.notify
		s.notifyMu.Unlock()
		if notify != nil {
			notify()
		}
	}
}

func (s *system) addUTF16(unit uint16) {
	switch {
	case utf16.IsSurrogate(rune(unit)) && unit < 0xdc00:
		s.highSurrogate = unit
		return
	case utf16.IsSurrogate(rune(unit)):
		if s.highSurrogate == 0 {
			return
		}
		r := utf16.DecodeRune(rune(s.highSurrogate), rune(unit))
		s.highSurrogate = 0
		s.pending = utf8.AppendRune(s.pending, r)
		return
	}
	s.highSurrogate = 0
	s.pending = utf8.AppendRune(s.pending, rune(unit))
}

// addMouse re-encodes a MOUSE_EVENT_RECORD as SGR mouse input. Consoles that
// pass VT mouse sequences through deliver them as key characters instead.
func (s *system) addMouse(e []byte) {
	window, err := s.window()
	if err != nil {
		return
	}
	x := int(int16(binary.LittleEndian.Uint16(e[0:]))) - int(window.Left)
	y := int(int16(binary.LittleEndian.Uint16(e[2:]))) - int(window.Top)
	state := binary.LittleEndian.Uint32(e[4:])
	control := binary.LittleEndian.Uint32(e[8:])
	flags := binary.LittleEndian.Uint32(e[12:])
	if x < 0 || y < 0 {
		return
	}
	mods := 0
	if control&shiftPressed != 0 {
		mods |= 4
	}
	if control&altPressed != 0 {
		mods |= 8
	}
	if control&ctrlPressed != 0 {
		mods |= 16
	}
	emit := func(code int, release bool) {
		s.pending = append(s.pending, "\x1b[<"...)
		s.pending = strconv.AppendInt(s.pending, int64(code|mods), 10)
		s.pending = append(s.pending, ';')
		s.pending = strconv.AppendInt(s.pending, int64(x+1), 10)
		s.pending = append(s.pending, ';')
		s.pending = strconv.AppendInt(s.pending, int64(y+1), 10)
		if release {
			s.pending = append(s.pending, 'm')
		} else {
			s.pending = append(s.pending, 'M')
		}
	}
	delta := int16(state >> 16)
	switch {
	case flags&mouseWheeled != 0:
		if delta > 0 {
			emit(64, false)
		} else {
			emit(65, false)
		}
		return
	case flags&mouseHWheeled != 0:
		if delta > 0 {
			emit(67, false)
		} else {
			emit(66, false)
		}
		return
	}
	// Console bits: 1 left, 2 right, 4 middle. SGR codes: 0 left, 1 middle, 2 right.
	buttons := state & 0x7
	changed := false
	for _, b := range [...]struct {
		bit  uint32
		code int
	}{{1, 0}, {4, 1}, {2, 2}} {
		now, before := buttons&b.bit != 0, s.buttons&b.bit != 0
		if now != before {
			emit(b.code, !now)
			changed = true
		}
	}
	s.buttons = buttons
	if changed || flags&mouseMoved == 0 {
		return
	}
	switch {
	case buttons&1 != 0:
		emit(32, false)
	case buttons&4 != 0:
		emit(33, false)
	case buttons&2 != 0:
		emit(34, false)
	case s.motion:
		emit(35, false)
	}
}

func (s *system) wake() { windows.SetEvent(s.cancel) }

func (s *system) write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		var n uint32
		if err := windows.WriteFile(s.out, p[written:], &n, nil); err != nil {
			return written, err
		}
		written += int(n)
	}
	return written, nil
}

func (s *system) watchResize(notify func()) func() {
	s.notifyMu.Lock()
	s.notify = notify
	s.notifyMu.Unlock()
	return func() {
		s.notifyMu.Lock()
		s.notify = nil
		s.notifyMu.Unlock()
	}
}

func (s *system) setMouse(level MouseTracking) { s.motion = level == MouseMotion }

func (s *system) canSuspend() bool { return false }

func (s *system) suspend() error { return errors.ErrUnsupported }

func (s *system) release() {
	if s.cancel != 0 {
		windows.CloseHandle(s.cancel)
		s.cancel = 0
	}
	if s.ownIn {
		windows.CloseHandle(s.in)
		s.ownIn = false
	}
	if s.ownOut {
		windows.CloseHandle(s.out)
		s.ownOut = false
	}
}
