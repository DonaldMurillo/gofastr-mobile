//go:build darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// systemSource runs lsof, which lists the sockets of processes the user
// may inspect: their own, or all of them as root.
func systemSource(ctx context.Context) ([]Conn, error) {
	cmd := exec.CommandContext(ctx, "lsof", "-nP", "-i", "-FpcftPnT")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && stderr.Len() == 0 {
		err = nil // lsof exits 1 when nothing matched
	}
	if err != nil {
		return nil, fmt.Errorf("lsof: %v %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return parseLsof(bytes.NewReader(out))
}
