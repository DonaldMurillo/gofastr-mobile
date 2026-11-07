package main

import (
	"os"
	"os/signal"
)

func notifySignals() <-chan os.Signal {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt)
	return signals
}

func isSuspend(os.Signal) bool { return false }
