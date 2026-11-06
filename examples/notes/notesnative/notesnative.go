// Package notesnative binds Go-authored mobile screens and shared Notes commands
// to the library-owned native host. Applications do not supply Swift screens.
package notesnative

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notedevices"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp/application"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesscreens"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

var state struct {
	sync.Mutex
	host       *mobilehost.Host
	service    *notes.Service
	user       *localapp.User
	screens    *nativeui.App
	runtime    *nativeui.Runtime
	operations sync.WaitGroup
	stopping   *stopResult
}

type stopResult struct {
	done chan struct{}
	err  error
}

// Start opens Notes for this installation. Call off the platform UI thread.
// Shared startup uses App.Serve in memory, with no mounted web presentation.
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
	shared, err := application.Open(dataDir)
	if err != nil {
		return err
	}
	attachments, err := files.NewStore(filepath.Join(dataDir, "mobile-files"))
	if err != nil {
		_ = shared.App.Shutdown(context.Background())
		return err
	}
	devices, err := notedevices.New(filepath.Join(dataDir, "mobile-devices"), shared.Data.User.GetID())
	if err != nil {
		_ = shared.App.Shutdown(context.Background())
		return err
	}
	screens, err := notesscreens.NewWithDevices(shared.Service, attachments, devices)
	if err != nil {
		_ = shared.App.Shutdown(context.Background())
		return err
	}
	drafts, err := behavior.NewFileDraftStore(filepath.Join(dataDir, "mobile-drafts"))
	if err != nil {
		_ = shared.App.Shutdown(context.Background())
		return err
	}
	runtime, err := nativeui.NewRuntime(screens, nativeui.RuntimeOptions{Drafts: drafts, OnLock: devices.Lock, OnClose: devices.CloseRoute})
	if err != nil {
		_ = shared.App.Shutdown(context.Background())
		return err
	}
	host, err := mobilehost.New(shared.App, mobilehost.Config{Data: shared.Data, ServiceOnly: true})
	if err != nil {
		_ = shared.App.Shutdown(context.Background())
		return err
	}
	if err := host.Start(); err != nil {
		return err
	}
	state.host, state.service, state.user, state.screens = host, shared.Service, shared.Data.User, screens
	state.runtime = runtime
	return nil
}

// Stop waits for in-flight commands before draining the shared lifecycle.
func Stop() error {
	state.Lock()
	if stopping := state.stopping; stopping != nil {
		state.Unlock()
		<-stopping.done
		return stopping.err
	}
	if state.host == nil {
		state.Unlock()
		return nil
	}
	host := state.host
	stopping := &stopResult{done: make(chan struct{})}
	state.stopping = stopping
	state.Unlock()
	state.operations.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := host.Stop(ctx)
	state.Lock()
	state.host, state.service, state.user, state.screens = nil, nil, nil, nil
	state.runtime = nil
	state.stopping = nil
	stopping.err = err
	close(stopping.done)
	state.Unlock()
	return err
}

// Root is the native renderer's initial Go screen route.
func Root() string { return notesscreens.Root }

// Command executes the versioned renderer/editor protocol off the UI thread.
func Command(payload string) (string, error) {
	operation, err := beginCommand()
	if err != nil {
		return "", err
	}
	defer operation.finish()
	return operation.runtime.CommandJSON(operation.ctx, payload)
}

// Render resolves a Go screen definition over the local gomobile boundary.
func Render(route, values string) (string, error) {
	command, err := beginCommand()
	if err != nil {
		return "", err
	}
	defer command.finish()
	return command.screens.RenderJSON(command.ctx, route, values)
}

// Dispatch runs a registered Go screen action, not a platform-specific callback.
func Dispatch(route, action, values string) (string, error) {
	command, err := beginCommand()
	if err != nil {
		return "", err
	}
	defer command.finish()
	return command.screens.DispatchJSON(command.ctx, route, action, values)
}

// Note is a data snapshot, not a view description. Its strings are bound by
// gomobile to platform strings; renderers decide how to present them.
type Note struct {
	ID, Title, Body, UpdatedAt      string
	Pinned, Archived, Trashed       bool
	Tag, ReminderDate, ReminderTime string
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
	command, err := beginCommand()
	if err != nil {
		return nil, err
	}
	defer command.finish()
	items, err := command.service.List(command.ctx, search)
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
	command, err := beginCommand()
	if err != nil {
		return nil, err
	}
	defer command.finish()
	note, err := command.service.Get(command.ctx, id)
	if err != nil {
		return nil, err
	}
	return snapshot(note), nil
}

// Save creates or updates a note through shared validation and owner scopes.
// Call off the platform UI thread; only success means the data was committed.
func Save(id, title, body string) (*Note, error) {
	command, err := beginCommand()
	if err != nil {
		return nil, err
	}
	defer command.finish()
	note, err := command.service.Save(command.ctx, id, title, body)
	if err != nil {
		return nil, err
	}
	return snapshot(note), nil
}

type operation struct {
	ctx     context.Context
	cancel  context.CancelFunc
	service *notes.Service
	screens *nativeui.App
	runtime *nativeui.Runtime
}

func beginCommand() (*operation, error) {
	state.Lock()
	defer state.Unlock()
	if state.host == nil || state.stopping != nil {
		return nil, errors.New("notesnative: not started or stopping")
	}
	state.operations.Add(1)
	ctx, cancel := context.WithTimeout(handler.SetUser(context.Background(), state.user), 30*time.Second)
	return &operation{ctx: ctx, cancel: cancel, service: state.service, screens: state.screens, runtime: state.runtime}, nil
}

func (command *operation) finish() {
	command.cancel()
	state.operations.Done()
}

func snapshot(note notes.Note) *Note {
	return &Note{ID: note.ID, Title: note.Title, Body: note.Body, UpdatedAt: note.UpdatedAt.Format(time.RFC3339),
		Pinned: note.Pinned, Archived: note.Archived, Trashed: note.Trashed,
		Tag: note.Tag, ReminderDate: note.ReminderDate, ReminderTime: note.ReminderTime}
}
