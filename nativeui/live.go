package nativeui

import "context"

// Live identifies an authorized read snapshot, not a durable remote-sync token.
type Live struct {
	Version string `json:"version"`
}

// Watch waits for a different version of the current authorized route/query.
// Implementations must honor cancellation; the runtime bounds waiting to 10s.
type Watch func(context.Context, Request, string) (string, error)
