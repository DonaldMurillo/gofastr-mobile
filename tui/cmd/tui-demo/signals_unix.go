//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func notifySignals() <-chan os.Signal {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTSTP)
	return signals
}

func isSuspend(sig os.Signal) bool { return sig == syscall.SIGTSTP }
