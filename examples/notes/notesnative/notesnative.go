// Package notesnative binds typed Notes commands directly to native hosts.
// It contains no screen schema, HTML response, or platform widget contract.
package notesnative

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
)

var state struct {
	sync.RWMutex
	host    *mobilehost.Host
	service *notes.Service
	user    *localapp.User
}

// Start opens Notes for this installation. Call off the platform UI thread.
// The current framework bootstrap still mounts an unused web presentation;
// native commands do not send HTTP requests or render that presentation.
func Start(dataDir string) error {
	state.Lock()
	defer state.Unlock()
	if state.host != nil {
		return errors.New("notesnative: already started")
	}
	if !filepath.IsAbs(dataDir) {
		return errors.New("notesnative: data directory must be absolute")
	}
	if localapp.Install() {
		return errors.New("notesnative: local identity cannot replace an authenticator")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	user, err := localapp.LoadOrMint(dataDir)
	if err != nil {
		return err
	}
	app, host, err := notesapp.Build(dataDir, "")
	if err != nil {
		return err
	}
	if err := host.Start(); err != nil {
		return err
	}
	state.host, state.service, state.user = host, notesapp.Service(app), user
	return nil
}

// Stop waits for in-flight commands before draining the shared lifecycle.
func Stop() error {
	state.Lock()
	defer state.Unlock()
	if state.host == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := state.host.Stop(ctx)
	state.host, state.service, state.user = nil, nil, nil
	return err
}

// Note is a data snapshot, not a view description. Its strings are bound by
// gomobile to platform strings; renderers decide how to present them.
type Note struct {
	ID, Title, Body, UpdatedAt string
}

// NoteList crosses gomobile without imposing JSON or a platform collection.
type NoteList struct{ items []*Note }

// Count returns the number of matching notes, currently capped at 200.
func (list *NoteList) Count() int { return len(list.items) }

// Item returns a note or nil when the index is outside the snapshot.
func (list *NoteList) Item(index int) *Note {
	if index < 0 || index >= len(list.items) {
		return nil
	}
	return list.items[index]
}

// List queries Notes directly. Call off the platform UI thread.
func List(search string) (*NoteList, error) {
	state.RLock()
	defer state.RUnlock()
	if state.service == nil {
		return nil, errors.New("notesnative: not started")
	}
	ctx, cancel := commandContext()
	defer cancel()
	items, err := state.service.List(ctx, search)
	if err != nil {
		return nil, err
	}
	result := &NoteList{items: make([]*Note, 0, len(items))}
	for _, item := range items {
		result.items = append(result.items, snapshot(item))
	}
	return result, nil
}

// Get returns one visible note. Call off the platform UI thread.
func Get(id string) (*Note, error) {
	state.RLock()
	defer state.RUnlock()
	if state.service == nil {
		return nil, errors.New("notesnative: not started")
	}
	ctx, cancel := commandContext()
	defer cancel()
	note, err := state.service.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return snapshot(note), nil
}

// Save creates or updates a note through shared validation and owner scopes.
// Call off the platform UI thread; only success means the data was committed.
func Save(id, title, body string) (*Note, error) {
	state.RLock()
	defer state.RUnlock()
	if state.service == nil {
		return nil, errors.New("notesnative: not started")
	}
	ctx, cancel := commandContext()
	defer cancel()
	note, err := state.service.Save(ctx, id, title, body)
	if err != nil {
		return nil, err
	}
	return snapshot(note), nil
}

func commandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(handler.SetUser(context.Background(), state.user), 30*time.Second)
}

func snapshot(note notes.Note) *Note {
	return &Note{ID: note.ID, Title: note.Title, Body: note.Body, UpdatedAt: note.UpdatedAt.Format(time.RFC3339)}
}
