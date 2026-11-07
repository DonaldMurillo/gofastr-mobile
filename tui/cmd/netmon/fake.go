package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
)

var fakeProcesses = []string{"browser", "mail", "chat", "sync-agent", "music", "editor", "updater", "vpn", "dns-proxy", "backup"}
var fakeStates = []string{"ESTABLISHED", "ESTABLISHED", "ESTABLISHED", "TIME_WAIT", "CLOSE_WAIT", "SYN_SENT", "LISTEN"}

// fakeSource simulates n connections. Each call replaces about a tenth of
// them, so the table churns the way a busy machine's does.
func fakeSource(n int) Source {
	var mu sync.Mutex
	rng := rand.New(rand.NewPCG(1, 2))
	next := 0
	make1 := func() Conn {
		next++
		p := rng.IntN(len(fakeProcesses))
		c := Conn{
			Proto:   "tcp",
			Local:   fmt.Sprintf("192.168.1.20:%d", 49152+next%16000),
			Remote:  fmt.Sprintf("203.0.%d.%d:%d", rng.IntN(256), rng.IntN(256), []int{443, 443, 80, 993, 5222}[rng.IntN(5)]),
			State:   fakeStates[rng.IntN(len(fakeStates))],
			PID:     1000 + p*37,
			Process: fakeProcesses[p],
		}
		if c.State == "LISTEN" {
			c.Remote = "*:*"
		}
		if rng.IntN(6) == 0 {
			c.Proto, c.State = "udp", ""
		}
		return c
	}
	conns := make([]Conn, n)
	for i := range conns {
		conns[i] = make1()
	}
	return func(context.Context) ([]Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		for range max(n/10, 1) {
			conns[rng.IntN(n)] = make1()
		}
		return dedupe(append([]Conn(nil), conns...)), nil
	}
}
