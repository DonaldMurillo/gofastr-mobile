//go:build darwin || linux

package term

import (
	"golang.org/x/sys/unix"
	"testing"
)

func TestImageKernelCellPixels(t *testing.T) {
	terminal := newPTY(t)
	if err := unix.IoctlSetWinsize(int(terminal.master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 80, Row: 24, Xpixel: 960, Ypixel: 576}); err != nil {
		t.Fatal(err)
	}
	width, height := terminal.CellPixels()
	if width != 12 || height != 24 {
		t.Fatalf("pixels %d x %d", width, height)
	}
}
