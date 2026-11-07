//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package term

import "golang.org/x/sys/unix"

// CellPixels returns the kernel's cell pixel size, or zeroes when unknown.
func (t *Terminal) CellPixels() (int, int) {
	window, err := unix.IoctlGetWinsize(t.sys.out, unix.TIOCGWINSZ)
	if err != nil {
		window, err = unix.IoctlGetWinsize(t.sys.in, unix.TIOCGWINSZ)
	}
	if err != nil || window.Col == 0 || window.Row == 0 {
		return 0, 0
	}
	return int(window.Xpixel) / int(window.Col), int(window.Ypixel) / int(window.Row)
}
