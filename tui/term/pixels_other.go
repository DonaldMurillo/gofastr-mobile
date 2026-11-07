//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package term

// CellPixels returns zeroes when the console API cannot report pixel size.
func (t *Terminal) CellPixels() (int, int) { return 0, 0 }
