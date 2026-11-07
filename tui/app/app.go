// Package app runs a view tree on a terminal: it owns the terminal, probes
// its capabilities, reads and decodes input on a goroutine, routes events
// through the tree and redraws when something changed.
//
// Everything that touches the UI runs on one goroutine, the loop. Other
// goroutines hand work to it with Post.
package app

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/probe"
	"github.com/DonaldMurillo/gofastr-native/tui/term"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// Options configure Run. The zero value opens the controlling terminal and
// handles the usual signals.
type Options struct {
	// Terminal to run on. Run opens one with term.Open when nil, and
	// closes only the terminal it opened.
	Terminal *term.Terminal
	// Signals delivers signals to the loop: a terminating signal quits,
	// SIGTSTP suspends. Run installs handlers when nil.
	Signals <-chan os.Signal
	// Getenv reads the environment for capability detection; os.Getenv
	// when nil.
	Getenv func(string) string
	// ProbeTimeout bounds the startup capability probe;
	// probe.DefaultTimeout when zero.
	ProbeTimeout time.Duration
	// Unhandled sees input no widget consumed, before the defaults:
	// ctrl+c quits and ctrl+z suspends. It returns true when it used the
	// event, which also redraws.
	Unhandled func(a *App, e input.Event) bool
	// NoDefaultKeys turns off the ctrl+c and ctrl+z defaults.
	NoDefaultKeys bool
	// Start runs on the loop before the first frame, for example to start
	// background work with After or Every.
	Start func(a *App)
	// Copied runs on the loop after a mouse selection is copied with OSC 52.
	Copied func(a *App, text string)
}

// App is a running program. Its methods other than Post and Quit must be
// called on the loop: from the root function, a widget callback, a
// posted function or Unhandled.
type App struct {
	term     *term.Terminal
	caps     probe.Result
	tree     *view.Tree
	size     term.Size
	renderer *cell.Renderer
	back     *cell.Buffer
	mouse    term.MouseTracking
	cursor   cell.Cursor
	dirty    bool // state changed since the last build
	unsent   bool // built but not yet written to the terminal

	mu    sync.Mutex
	posts []func()
	quit  bool
	wake  chan struct{}
	done  chan struct{} // closed when Run returns
}

// Run starts the program and blocks until it quits. root builds the UI
// description; it runs before every frame.
func Run(root func(a *App) view.Element, opts Options) (err error) {
	t := opts.Terminal
	if t == nil {
		if t, err = term.Open(); err != nil {
			return err
		}
		defer t.Close()
	}
	signals := opts.Signals
	if signals == nil {
		var stop func()
		signals, stop = notifySignals()
		defer stop()
	}
	a := &App{term: t, wake: make(chan struct{}, 1), done: make(chan struct{})}
	defer close(a.done)
	defer func() {
		if r := recover(); r != nil {
			t.Close() // leave the user a working shell, then crash loudly
			panic(r)
		}
	}()
	return a.run(root, signals, opts)
}

// Caps returns what the terminal supports.
func (a *App) Caps() probe.Result { return a.caps }

// Tree returns the view tree, for focus control and selection.
func (a *App) Tree() *view.Tree { return a.tree }

// Size returns the screen size.
func (a *App) Size() term.Size { return a.size }

// Redraw asks for a frame after the current event. Widget callbacks and
// posted functions redraw on their own.
func (a *App) Redraw() { a.dirty = true }

// Post runs fn on the loop and redraws afterwards. It is safe from any
// goroutine and never blocks.
func (a *App) Post(fn func()) {
	a.mu.Lock()
	a.posts = append(a.posts, fn)
	a.mu.Unlock()
	a.signal()
}

// Quit stops the loop after the current event. It is safe from any
// goroutine.
func (a *App) Quit() {
	a.mu.Lock()
	a.quit = true
	a.mu.Unlock()
	a.signal()
}

func (a *App) signal() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// After posts fn to the loop once after d and redraws afterwards, unless
// stop is called first or the app has quit. The returned stop is idempotent
// and also cancels a callback already queued but not yet started.
func (a *App) After(d time.Duration, fn func()) (stop func()) {
	timer := time.NewTimer(d)
	canceled := make(chan struct{})
	var once sync.Once
	stop = func() {
		once.Do(func() {
			timer.Stop()
			close(canceled)
		})
	}
	go func() {
		defer timer.Stop()
		select {
		case <-timer.C:
			a.Post(func() {
				select {
				case <-canceled:
					return
				case <-a.done:
					return
				default:
					if !a.quitting() {
						fn()
					}
				}
			})
		case <-canceled:
		case <-a.done:
		}
	}()
	return stop
}

// Every posts fn every d until the returned stop is called or the app
// quits. Ticks that find the previous one still queued are dropped.
func (a *App) Every(d time.Duration, fn func()) (stop func()) {
	ticker := time.NewTicker(d)
	done := make(chan struct{})
	var once sync.Once
	var pending sync.Mutex // held while a tick is queued
	go func() {
		for {
			select {
			case <-ticker.C:
				if pending.TryLock() {
					a.Post(func() {
						pending.Unlock()
						fn()
					})
				}
			case <-done:
				return
			case <-a.done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() {
		once.Do(func() {
			ticker.Stop()
			close(done)
		})
	}
}

// Copy puts text on the system clipboard with OSC 52. Terminals that do
// not support it ignore the request.
func (a *App) Copy(text string) {
	a.term.Write(fmt.Appendf(nil, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text))))
}

func (a *App) run(root func(*App) view.Element, signals <-chan os.Signal, opts Options) error {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	timeout := opts.ProbeTimeout
	if timeout == 0 {
		timeout = probe.DefaultTimeout
	}
	var decoder input.Decoder
	caps, early, err := probe.Run(a.term, &decoder, timeout, getenv)
	if err != nil {
		return err
	}
	a.caps = caps
	if caps.Graphemes == probe.Available {
		if err := a.term.Set(term.GraphemeClusters, true); err != nil {
			return err
		}
	}
	for _, m := range []term.Mode{term.AltScreen, term.HideCursor, term.NoAutowrap, term.BracketedPaste, term.FocusEvents, term.KittyKeyboard} {
		if err := a.term.Set(m, true); err != nil {
			return err
		}
	}
	if a.size, err = a.term.Size(); err != nil {
		return err
	}
	a.tree = view.NewTree(caps.Method())
	a.tree.Images, a.tree.CellWidth, a.tree.CellHeight = caps.Images, caps.CellWidth, caps.CellHeight
	a.tree.OnCopy = func(text string) {
		a.Copy(text)
		if opts.Copied != nil {
			opts.Copied(a, text)
		}
	}
	a.back = cell.NewBuffer(a.size.Cols, a.size.Rows)
	a.renderer = cell.NewRenderer(a.size.Cols, a.size.Rows, caps.Options())
	defer func() { a.term.Write(a.renderer.Reset()) }()

	events := make(chan input.Event, 256)
	readErr := make(chan error, 1)
	stopReader := make(chan struct{})
	defer close(stopReader)
	go readInput(a.term, &decoder, events, readErr, stopReader)

	if opts.Start != nil {
		opts.Start(a)
		a.runPosts()
	}
	a.build(root)
	for _, e := range early {
		a.dispatch(root, e, opts)
	}
	for {
		if a.quitting() {
			return nil
		}
		if a.dirty {
			a.build(root)
		}
		if a.unsent {
			if err := a.flush(); err != nil {
				return err
			}
		}
		select {
		case e := <-events:
			a.dispatch(root, e, opts)
			// Take everything already waiting before writing, so a burst of
			// mouse motion costs one terminal update.
			for more := true; more; {
				select {
				case e := <-events:
					a.dispatch(root, e, opts)
				default:
					more = false
				}
			}
		case <-a.wake:
		case <-a.term.Resized():
			if a.size, err = a.term.Size(); err != nil {
				return err
			}
			a.dirty = true
		case sig := <-signals:
			if !isSuspend(sig) {
				return nil
			}
			a.suspend()
		case err := <-readErr:
			if errors.Is(err, term.ErrClosed) {
				return nil
			}
			return err
		}
		a.runPosts()
	}
}

func (a *App) quitting() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quit
}

func (a *App) runPosts() {
	for {
		a.mu.Lock()
		posts := a.posts
		a.posts = nil
		a.mu.Unlock()
		if len(posts) == 0 {
			return
		}
		for _, fn := range posts {
			fn()
		}
		a.dirty = true
	}
}

// dispatch handles one event against an up-to-date tree. Controlled
// widgets compute edits from the values they were built with, so after an
// event that changed something the tree is rebuilt before the next one.
func (a *App) dispatch(root func(*App) view.Element, e input.Event, opts Options) {
	if a.dirty {
		a.build(root)
	}
	a.handle(e, opts)
}

// build runs the root function and lays out and draws the tree into the
// back buffer.
func (a *App) build(root func(*App) view.Element) {
	a.dirty, a.unsent = false, true
	a.back.Resize(a.size.Cols, a.size.Rows)
	a.cursor = a.tree.Frame(root(a), a.back)
}

// flush writes the last build to the terminal.
func (a *App) flush() error {
	a.unsent = false
	// Ask for every pointer movement only while something reacts to hover.
	want := term.MouseDrag
	if a.tree.WantsMotion() {
		want = term.MouseMotion
	}
	if want != a.mouse {
		if err := a.term.SetMouse(want); err != nil {
			return err
		}
		a.mouse = want
	}
	if _, err := a.term.Write(a.renderer.Render(a.back, a.cursor)); err != nil {
		a.renderer.Invalidate()
		return err
	}
	return nil
}

func (a *App) handle(e input.Event, opts Options) {
	switch e.(type) {
	case input.Key, input.Mouse, input.Paste:
		if a.tree.Root() != nil && a.tree.Dispatch(e) {
			a.dirty = true
			return
		}
	}
	if opts.Unhandled != nil && opts.Unhandled(a, e) {
		a.dirty = true
		return
	}
	if k, ok := e.(input.Key); ok && !opts.NoDefaultKeys && k.Code == input.KeyRune && k.Mods == input.ModCtrl {
		switch k.Rune {
		case 'c':
			a.Quit()
		case 'z':
			a.suspend()
		}
	}
}

func (a *App) suspend() {
	if err := a.term.Suspend(); err == nil {
		a.renderer.Invalidate()
		a.dirty = true
	}
}

// readInput decodes terminal input into events. When a lone ESC is held
// it waits only briefly for the rest of a sequence before deciding it was
// the Escape key.
func readInput(t *term.Terminal, decoder *input.Decoder, events chan<- input.Event, readErr chan<- error, stop <-chan struct{}) {
	emit := func(e input.Event) {
		select {
		case events <- e:
		case <-stop:
		}
	}
	buf := make([]byte, 4096)
	for {
		timeout := time.Duration(-1)
		if decoder.Pending() {
			timeout = 30 * time.Millisecond
		}
		n, err := t.Read(buf, timeout)
		if err != nil {
			select {
			case readErr <- err:
			case <-stop:
			}
			return
		}
		if n == 0 {
			decoder.Flush(emit)
			continue
		}
		decoder.Feed(buf[:n], emit)
	}
}
