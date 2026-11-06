//go:build !windows

// Package privatefile keeps installation files private on each host OS.
package privatefile

import (
	"errors"
	"os"
)

func Harden(path string, directory bool) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	return os.Chmod(path, mode)
}

func Check(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("file grants access to other users")
	}
	return nil
}

func Replace(from, to string) error { return os.Rename(from, to) }

func SyncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
