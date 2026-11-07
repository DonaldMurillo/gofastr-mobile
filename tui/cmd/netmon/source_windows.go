package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// systemSource runs netstat for the sockets and tasklist for the process
// names. netstat prints state names in English on all locales we know of;
// unknown names show as printed.
func systemSource(ctx context.Context) ([]Conn, error) {
	out, err := run(ctx, "netstat", "-ano")
	if err != nil {
		return nil, err
	}
	conns, err := parseNetstat(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	if list, err := run(ctx, "tasklist", "/FO", "CSV", "/NH"); err == nil {
		names, _ := parseTasklist(bytes.NewReader(list))
		for i := range conns {
			conns[i].Process = names[conns[i].PID]
		}
	}
	return conns, nil
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}
