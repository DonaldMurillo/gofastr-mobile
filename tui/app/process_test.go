//go:build darwin || linux

package app

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/DonaldMurillo/gofastr-native/tui/internal/ptytest"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
	"github.com/DonaldMurillo/gofastr-native/tui/widget"
)

// TestAppProcess is re-executed only in a fresh session whose controlling
// terminal is the pty. In particular, Suspend's Kill(0, SIGSTOP) can never
// reach the parent's process group.
func TestAppProcess(t *testing.T) {
	if os.Getenv("GOFASTR_TEST_APP_PROCESS") != "1" {
		return
	}
	frames := 0
	err := Run(func(a *App) view.Element {
		frames++
		return widget.Label(fmt.Sprintf("size %dx%d frame %d", a.Size().Cols, a.Size().Rows, frames))
	}, Options{Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
}

type processSession struct {
	*session
	cmd      *exec.Cmd
	slave    *os.File
	original unix.Termios
	waited   bool
}

func startProcess(t *testing.T) *processSession {
	t.Helper()
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	t.Cleanup(func() { master.Close(); slave.Close() })
	original, err := ptytest.Termios(slave)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 30, Row: 3}); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestAppProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), "GOFASTR_TEST_APP_PROCESS=1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &processSession{
		session:  &session{t: t, master: master, screen: vt.New(30, 3)},
		cmd:      cmd,
		slave:    slave,
		original: *original,
	}
	t.Cleanup(func() {
		if !p.waited {
			cmd.Process.Kill() // SIGKILL also works while the child is stopped.
			cmd.Wait()
		}
	})
	pgid, err := unix.Getpgid(cmd.Process.Pid)
	if err != nil || pgid != cmd.Process.Pid || pgid == unix.Getpgrp() {
		t.Fatalf("child is not isolated: pgid %d, pid %d, error %v", pgid, cmd.Process.Pid, err)
	}
	go p.readOutput()
	p.waitFor("size 30x3 frame 1")
	return p
}

func (p *processSession) finish() {
	p.t.Helper()
	p.send("\x03")
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case err := <-done:
		p.waited = true
		if err != nil {
			p.t.Fatalf("child exited: %v\n%s", err, p.rawOutput())
		}
	case <-time.After(5 * time.Second):
		// Reap through the same waiter even on failure; cleanup must not
		// call Wait concurrently with it.
		p.cmd.Process.Kill()
		<-done
		p.waited = true
		p.t.Fatal("child did not exit")
	}
}

func (p *processSession) waitStopped() {
	p.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var status unix.WaitStatus
		pid, err := unix.Wait4(p.cmd.Process.Pid, &status, unix.WUNTRACED|unix.WNOHANG, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			p.t.Fatal(err)
		}
		if pid != 0 {
			// BSD's WaitStatus.Stopped treats SIGSTOP's encoding as a
			// continued event. We request only WUNTRACED, not WCONTINUED,
			// so this status unambiguously reports the SIGSTOP itself.
			if status != unix.WaitStatus(int(unix.SIGSTOP)<<8|0x7f) {
				p.t.Fatalf("child status %v, want SIGSTOP", status)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.t.Fatalf("child did not suspend\n%s", p.rawOutput())
}

func TestSuspendRestoresAndReentersTerminal(t *testing.T) {
	p := startProcess(t)
	raw, err := ptytest.Termios(p.slave)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Lflag&(unix.ICANON|unix.ECHO|unix.ISIG) != 0 {
		t.Fatalf("child terminal is not raw: %#x", raw.Lflag)
	}
	if err := p.cmd.Process.Signal(syscall.SIGTSTP); err != nil {
		t.Fatal(err)
	}
	p.waitStopped()
	restored, err := ptytest.Termios(p.slave)
	if err != nil {
		t.Fatal(err)
	}
	if *restored != p.original {
		t.Fatalf("suspend did not restore termios:\ngot %+v\nwant %+v", *restored, p.original)
	}
	p.until("private modes restored on suspend", func() bool {
		for _, mode := range []int{1049, 2004, 1004, 2027, 1002, 1006} {
			if p.screen.Modes[mode] {
				return false
			}
		}
		return p.screen.Autowrap && p.screen.CursorVisible && len(p.screen.Errors) == 0
	})
	if err := p.cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	p.until("repaint after resume", func() bool {
		var cols, rows, frame int
		_, err := fmt.Sscanf(p.screen.Text(), "size %dx%d frame %d", &cols, &rows, &frame)
		return err == nil && cols == 30 && rows == 3 && frame >= 2
	})
	resumed, err := ptytest.Termios(p.slave)
	if err != nil {
		t.Fatal(err)
	}
	if *resumed != *raw {
		t.Fatalf("resume did not restore raw mode:\ngot %+v\nwant %+v", *resumed, *raw)
	}
	p.until("private modes re-entered on resume", func() bool {
		for _, mode := range []int{1049, 2004, 1004, 2027, 1002, 1006} {
			if !p.screen.Modes[mode] {
				return false
			}
		}
		return !p.screen.Autowrap && !p.screen.CursorVisible && len(p.screen.Errors) == 0
	})
	p.finish()
}

func TestSIGWINCHReachesApp(t *testing.T) {
	p := startProcess(t)
	p.mu.Lock()
	p.screen.Resize(40, 4)
	p.mu.Unlock()
	// This is the child's controlling terminal: the kernel delivers
	// SIGWINCH to its foreground group, without NotifyResize or kill.
	if err := unix.IoctlSetWinsize(int(p.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 40, Row: 4}); err != nil {
		t.Fatal(err)
	}
	p.waitFor("size 40x4 frame 2")
	p.finish()
}
