package notes

import (
	"context"
	"crypto/rand"
	"strconv"
	"sync"
	"time"
)

const WatchTimeout = 10 * time.Second

type changeState struct {
	epoch    string
	revision uint64
	changed  chan struct{}
}

func (state *changeState) token() string {
	return state.epoch + "." + strconv.FormatUint(state.revision, 10)
}

type changeHub struct {
	mu     sync.Mutex
	owners map[string]*changeState
}

func (hub *changeHub) state(owner string) *changeState {
	if hub.owners == nil {
		hub.owners = make(map[string]*changeState)
	}
	if state := hub.owners[owner]; state != nil {
		return state
	}
	if len(hub.owners) >= 128 {
		for key, state := range hub.owners {
			close(state.changed)
			delete(hub.owners, key)
			break
		}
	}
	state := &changeState{epoch: rand.Text(), changed: make(chan struct{})}
	hub.owners[owner] = state
	return state
}

// Version is an opaque owner-scoped process epoch, never a durable sync token.
func (service *Service) Version(ctx context.Context) (string, error) {
	owner, err := service.undoOwner(ctx)
	if err != nil {
		return "", err
	}
	service.changes.mu.Lock()
	defer service.changes.mu.Unlock()
	return service.changes.state(owner).token(), nil
}

// AttachmentsChanged invalidates the owner's version only after a confirmed
// private attachment write. Note and metadata writes use the same hub.
func (service *Service) AttachmentsChanged(ctx context.Context) {
	owner, err := service.undoOwner(ctx)
	if err != nil {
		return
	}
	service.changes.mu.Lock()
	defer service.changes.mu.Unlock()
	state := service.changes.state(owner)
	state.revision++
	close(state.changed)
	state.changed = make(chan struct{})
}

// Watch observes changes for at most ten seconds and respects cancellation.
func (service *Service) Watch(ctx context.Context, version string) (string, error) {
	owner, err := service.undoOwner(ctx)
	if err != nil {
		return "", err
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, WatchTimeout)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		service.changes.mu.Lock()
		state := service.changes.state(owner)
		current, changed := state.token(), state.changed
		service.changes.mu.Unlock()
		if current != version {
			return current, nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			if parent.Err() == nil && ctx.Err() == context.DeadlineExceeded {
				return current, nil
			}
			return "", ctx.Err()
		}
	}
}
