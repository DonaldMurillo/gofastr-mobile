//go:build !windows

package app

import (
	"os"
	"os/signal"
	"syscall"
)

func notifySignals() (<-chan os.Signal, func()) {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTSTP)
	return signals, func() { signal.Stop(signals) }
}

func isSuspend(sig os.Signal) bool { return sig == syscall.SIGTSTP }
