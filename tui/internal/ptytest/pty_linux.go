package ptytest

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// Open returns a new pty pair: the master side and the slave terminal device.
func Open() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	if err = unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	return os.NewFile(uintptr(fd), "ptmx"), slave, nil
}

// Termios reads the device settings of a pty endpoint.
func Termios(file *os.File) (*unix.Termios, error) {
	return unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
}
