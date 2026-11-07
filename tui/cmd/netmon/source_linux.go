package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// systemSource reads the kernel's socket tables and matches socket inodes
// to the processes holding them. Without root only the user's own
// processes can be matched; other sockets show with no process.
func systemSource(ctx context.Context) ([]Conn, error) {
	var sockets []procSocket
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		f, err := os.Open("/proc/net/" + proto)
		if err != nil {
			continue // the protocol may be disabled
		}
		s, err := parseProcNet(f, proto)
		f.Close()
		if err != nil {
			return nil, err
		}
		sockets = append(sockets, s...)
	}
	owners := socketOwners(ctx)
	out := make([]Conn, 0, len(sockets))
	for _, s := range sockets {
		if o, ok := owners[s.inode]; ok {
			s.PID, s.Process = o.pid, o.name
		}
		out = append(out, s.Conn)
	}
	return dedupe(out), nil
}

type owner struct {
	pid  int
	name string
}

func socketOwners(ctx context.Context) map[uint64]owner {
	owners := map[uint64]owner{}
	procs, _ := os.ReadDir("/proc")
	for _, p := range procs {
		if ctx.Err() != nil {
			break
		}
		pid, err := strconv.Atoi(p.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join("/proc", p.Name())
		fds, err := os.ReadDir(filepath.Join(dir, "fd"))
		if err != nil {
			continue // not ours to read
		}
		var name string
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(dir, "fd", fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := strings.CutPrefix(link, "socket:[")
			if !ok {
				continue
			}
			n, err := strconv.ParseUint(strings.TrimSuffix(inode, "]"), 10, 64)
			if err != nil {
				continue
			}
			if name == "" {
				comm, _ := os.ReadFile(filepath.Join(dir, "comm"))
				name = strings.TrimSpace(string(comm))
			}
			if _, taken := owners[n]; !taken {
				owners[n] = owner{pid, name}
			}
		}
	}
	return owners
}
