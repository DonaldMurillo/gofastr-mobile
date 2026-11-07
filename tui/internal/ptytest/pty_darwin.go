package ptytest

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Open returns a new pty pair: the master side and the slave terminal device.
func Open() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	if err = unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err == nil {
		err = unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0)
	}
	var name [128]byte
	if err == nil {
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
			err = errno
		}
	}
	if err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	path := unix.ByteSliceToString(name[:])
	slave, err = os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	return os.NewFile(uintptr(fd), "ptmx"), slave, nil
}
