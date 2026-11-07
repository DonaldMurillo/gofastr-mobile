//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package term

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/unix"
)

type system struct {
	in, out      int
	tty          *os.File // set when Open had to open /dev/tty itself
	saved        unix.Termios
	saveValid    bool
	wakeR, wakeW int
}

func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	return err == nil
}

func openSystem() (*system, error) {
	s := &system{in: int(os.Stdin.Fd()), out: int(os.Stdout.Fd())}
	if !isTerminal(s.in) || !isTerminal(s.out) {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return nil, ErrNotTerminal
		}
		s.tty = tty
		s.in, s.out = int(tty.Fd()), int(tty.Fd())
		if !isTerminal(s.in) {
			tty.Close()
			return nil, ErrNotTerminal
		}
	}
	return s.init()
}

func openFiles(in, out *os.File) (*system, error) {
	s := &system{in: int(in.Fd()), out: int(out.Fd())}
	if !isTerminal(s.in) || !isTerminal(s.out) {
		return nil, ErrNotTerminal
	}
	return s.init()
}

func (s *system) init() (*system, error) {
	if s.in >= 1024 { // FD_SETSIZE: select cannot watch it
		s.release()
		return nil, errors.New("term: terminal descriptor too large for select")
	}
	var pipe [2]int
	if err := unix.Pipe(pipe[:]); err != nil {
		s.release()
		return nil, err
	}
	s.wakeR, s.wakeW = pipe[0], pipe[1]
	for _, fd := range pipe {
		unix.CloseOnExec(fd)
		if err := unix.SetNonblock(fd, true); err != nil {
			s.release()
			return nil, err
		}
	}
	return s, nil
}

func (s *system) makeRaw() error {
	current, err := unix.IoctlGetTermios(s.in, ioctlGetTermios)
	if err != nil {
		return err
	}
	if !s.saveValid {
		s.saved, s.saveValid = *current, true
	}
	raw := *current
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	return unix.IoctlSetTermios(s.in, ioctlSetTermios, &raw)
}

func (s *system) restore() error {
	if !s.saveValid {
		return nil
	}
	saved := s.saved
	return unix.IoctlSetTermios(s.in, ioctlSetTermios, &saved)
}

func (s *system) size() (Size, error) {
	ws, err := unix.IoctlGetWinsize(s.out, unix.TIOCGWINSZ)
	if err != nil {
		if ws, err = unix.IoctlGetWinsize(s.in, unix.TIOCGWINSZ); err != nil {
			return Size{}, err
		}
	}
	return Size{Cols: int(ws.Col), Rows: int(ws.Row)}, nil
}

// read waits with select, not poll: poll does not work on terminal devices
// on macOS.
func (s *system) read(p []byte, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		var set unix.FdSet
		set.Set(s.in)
		set.Set(s.wakeR)
		var tv *unix.Timeval
		if timeout >= 0 {
			remaining := max(time.Until(deadline), 0)
			t := unix.NsecToTimeval(remaining.Nanoseconds())
			tv = &t
		}
		n, err := unix.Select(max(s.in, s.wakeR)+1, &set, nil, nil, tv)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, nil
		}
		if set.IsSet(s.wakeR) {
			var drain [64]byte
			for {
				if n, _ := unix.Read(s.wakeR, drain[:]); n <= 0 {
					break
				}
			}
			return 0, errCanceled
		}
		n, err = unix.Read(s.in, p)
		switch {
		case err == unix.EINTR || err == unix.EAGAIN:
			continue
		case err != nil:
			return 0, err
		case n == 0:
			return 0, io.EOF // the terminal hung up
		}
		return n, nil
	}
}

func (s *system) wake() {
	unix.Write(s.wakeW, []byte{1})
}

func (s *system) write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		n, err := unix.Write(s.out, p[written:])
		if err == unix.EINTR || err == unix.EAGAIN {
			continue
		}
		if err != nil {
			return written, err
		}
		written += n
	}
	return written, nil
}

func (s *system) watchResize(notify func()) func() {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, unix.SIGWINCH)
	go func() {
		for {
			select {
			case <-signals:
				notify()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

func (s *system) setMouse(MouseTracking) {}

func (s *system) canSuspend() bool { return true }

// suspend stops the process group, as ctrl+z would, with SIGSTOP. SIGTSTP is
// not used because a program that watches SIGTSTP would catch its own signal.
func (s *system) suspend() error {
	return unix.Kill(0, unix.SIGSTOP)
}

func (s *system) release() {
	if s.wakeR > 0 {
		unix.Close(s.wakeR)
		unix.Close(s.wakeW)
		s.wakeR, s.wakeW = 0, 0
	}
	if s.tty != nil {
		s.tty.Close()
		s.tty = nil
	}
}
