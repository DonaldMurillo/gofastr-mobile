package app

import (
	"os"
	"os/signal"
)

func notifySignals() (<-chan os.Signal, func()) {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt)
	return signals, func() { signal.Stop(signals) }
}

func isSuspend(os.Signal) bool { return false }
