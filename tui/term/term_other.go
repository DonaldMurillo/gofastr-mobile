//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package term

import (
	"os"
	"time"
)

type system struct{}

func openFiles(*os.File, *os.File) (*system, error)     { return nil, ErrNotTerminal }
func openSystem() (*system, error)                      { return nil, ErrNotTerminal }
func (*system) makeRaw() error                          { return ErrNotTerminal }
func (*system) restore() error                          { return nil }
func (*system) size() (Size, error)                     { return Size{}, ErrNotTerminal }
func (*system) read([]byte, time.Duration) (int, error) { return 0, ErrNotTerminal }
func (*system) wake()                                   {}
func (*system) write([]byte) (int, error)               { return 0, ErrNotTerminal }
func (*system) watchResize(func()) func()               { return func() {} }
func (*system) setMouse(MouseTracking)                  {}
func (*system) canSuspend() bool                        { return false }
func (*system) suspend() error                          { return nil }
func (*system) release()                                {}
