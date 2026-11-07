package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// Conn is one socket of one process.
type Conn struct {
	Proto   string // tcp, tcp6, udp or udp6
	Local   string // address:port, * for any address
	Remote  string // address:port, *:* when not connected
	State   string // TCP state; empty for UDP
	PID     int
	Process string
}

// Key identifies a connection across refreshes.
func (c Conn) Key() string {
	return c.Proto + " " + c.Local + " " + c.Remote + " " + strconv.Itoa(c.PID)
}

// Source lists the current connections.
type Source func(ctx context.Context) ([]Conn, error)

// errUnsupported is what the source returns on systems it cannot read.
var errUnsupported = errors.New("listing connections is not supported on this system")

// dedupe removes repeated keys, which come from sockets shared by several
// descriptors of a process, keeping the first.
func dedupe(conns []Conn) []Conn {
	seen := make(map[string]bool, len(conns))
	out := conns[:0]
	for _, c := range conns {
		if k := c.Key(); !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}

// endpoint normalizes "addr:port" text: an unspecified address becomes *.
func endpoint(s string) string {
	switch s {
	case "", "*:*", "0.0.0.0:0", "[::]:0", "[::]:*", "0.0.0.0:*":
		return "*:*"
	}
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return s
	}
	return formatEndpoint(ap.Addr(), ap.Port())
}

func formatEndpoint(addr netip.Addr, port uint16) string {
	p := strconv.Itoa(int(port))
	if port == 0 {
		p = "*"
	}
	if addr.IsUnspecified() {
		if port == 0 {
			return "*:*"
		}
		return "*:" + p
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if addr.Is6() {
		return "[" + addr.String() + "]:" + p
	}
	return addr.String() + ":" + p
}

// parseLsof reads `lsof -nP -i -F pcftPnT` output.
func parseLsof(r io.Reader) ([]Conn, error) {
	var out []Conn
	var pid int
	var command string
	var cur *Conn
	var family string
	flush := func() {
		if cur != nil && cur.Proto != "" {
			if family == "IPv6" {
				cur.Proto += "6"
			}
			out = append(out, *cur)
		}
		cur, family = nil, ""
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		field, value := line[0], line[1:]
		switch field {
		case 'p':
			flush()
			pid, _ = strconv.Atoi(value)
			command = ""
		case 'c':
			command = value
		case 'f':
			flush()
			cur = &Conn{PID: pid, Process: command, Remote: "*:*"}
		case 't':
			family = value
		case 'P':
			if cur != nil {
				cur.Proto = strings.ToLower(value)
			}
		case 'n':
			if cur != nil {
				local, remote, connected := strings.Cut(value, "->")
				cur.Local = endpoint(local)
				if connected {
					cur.Remote = endpoint(remote)
				}
			}
		case 'T':
			if state, ok := strings.CutPrefix(value, "ST="); ok && cur != nil {
				cur.State = state
			}
		}
	}
	flush()
	return dedupe(out), sc.Err()
}

var tcpStates = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV", "04": "FIN_WAIT1",
	"05": "FIN_WAIT2", "06": "TIME_WAIT", "07": "CLOSE", "08": "CLOSE_WAIT",
	"09": "LAST_ACK", "0A": "LISTEN", "0B": "CLOSING",
}

// procSocket is a line of /proc/net/{tcp,udp}[6].
type procSocket struct {
	Conn
	inode uint64
}

// parseProcAddr decodes the kernel's hex address. The kernel prints each
// 32-bit word of the address as a host-order integer, so writing the
// numbers back in host order recovers the network-order bytes.
func parseProcAddr(s string) (string, error) {
	addrHex, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return "", fmt.Errorf("bad address %q", s)
	}
	raw, err := hex.DecodeString(addrHex)
	if err != nil || len(raw) != 4 && len(raw) != 16 {
		return "", fmt.Errorf("bad address %q", s)
	}
	for i := 0; i < len(raw); i += 4 {
		binary.NativeEndian.PutUint32(raw[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", fmt.Errorf("bad port %q", s)
	}
	addr, _ := netip.AddrFromSlice(raw)
	return formatEndpoint(addr, uint16(port)), nil
}

// parseProcNet reads one /proc/net table. proto is tcp, tcp6, udp or udp6.
func parseProcNet(r io.Reader, proto string) ([]procSocket, error) {
	var out []procSocket
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first { // header
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 10 {
			continue
		}
		local, err := parseProcAddr(f[1])
		if err != nil {
			return nil, err
		}
		remote, err := parseProcAddr(f[2])
		if err != nil {
			return nil, err
		}
		state := tcpStates[f[3]]
		if strings.HasPrefix(proto, "udp") {
			state = "" // UDP has no states; 07 just means unconnected
		}
		inode, _ := strconv.ParseUint(f[9], 10, 64)
		out = append(out, procSocket{Conn{Proto: proto, Local: local, Remote: remote, State: state}, inode})
	}
	return out, sc.Err()
}

// parseNetstat reads Windows `netstat -ano` output.
func parseNetstat(r io.Reader) ([]Conn, error) {
	var out []Conn
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 {
			continue
		}
		proto := strings.ToLower(f[0])
		if proto != "tcp" && proto != "udp" {
			continue
		}
		c := Conn{Local: endpoint(f[1]), Remote: endpoint(f[2])}
		pidField := f[len(f)-1]
		if proto == "tcp" && len(f) >= 5 {
			c.State = f[3]
			if c.State == "LISTENING" {
				c.State = "LISTEN"
			}
		}
		if strings.HasPrefix(f[1], "[") {
			proto += "6"
		}
		c.Proto = proto
		c.PID, _ = strconv.Atoi(pidField)
		out = append(out, c)
	}
	return dedupe(out), sc.Err()
}

// parseTasklist reads `tasklist /FO CSV /NH` output into process names.
func parseTasklist(r io.Reader) (map[int]string, error) {
	rd := csv.NewReader(r)
	rd.FieldsPerRecord = -1
	names := map[int]string{}
	for {
		rec, err := rd.Read()
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			return names, err
		}
		if len(rec) >= 2 {
			if pid, err := strconv.Atoi(rec[1]); err == nil {
				names[pid] = strings.TrimSuffix(rec[0], ".exe")
			}
		}
	}
}
