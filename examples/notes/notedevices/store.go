// Package notedevices stores Notes' device-feature metadata and reviewed shares.
// Protected note contents live only in the OS secret store and foreground memory.
package notedevices

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/device/sharein"
	"github.com/DonaldMurillo/gofastr-native/internal/privatefile"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

var ErrLocked = errors.New("protected note is locked")

type ProtectedEntry struct {
	ID      string    `json:"id"`
	Updated time.Time `json:"updated"`
	Pending bool      `json:"pending,omitempty"`
}

// State contains no protected titles or bodies.
type State struct {
	Version            int                       `json:"version"`
	Owner              string                    `json:"owner"`
	Shares             map[string]sharein.Item   `json:"shares"`
	Protected          map[string]ProtectedEntry `json:"protected"`
	WidgetNote         string                    `json:"widgetNote,omitempty"`
	WidgetClearPending bool                      `json:"widgetClearPending,omitempty"`
}

type ProtectedNote struct {
	Version int    `json:"version"`
	Title   string `json:"title"`
	Body    string `json:"body"`
}

type lease struct {
	ID    string
	Epoch uint64
	Note  ProtectedNote
}

type Store struct {
	mu          sync.Mutex
	root, owner string
	epoch       uint64
	leases      map[string]lease
}

func New(directory, owner string) (*Store, error) {
	if owner == "" || !filepath.IsAbs(directory) {
		return nil, errors.New("notes: feature storage requires an owner and absolute directory")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("notes: feature directory must not be a symlink")
	}
	if err := privatefile.Harden(directory, true); err != nil {
		return nil, err
	}
	return &Store{root: directory, owner: owner, epoch: 1, leases: make(map[string]lease)}, nil
}

func (store *Store) authorize(ctx context.Context) error {
	user, exists := handler.GetUser(ctx)
	identity, valid := user.(interface{ GetID() string })
	if !exists || !valid || identity.GetID() != store.owner {
		return errors.New("notes: feature owner is unavailable")
	}
	return ctx.Err()
}

func ValidID(id string) bool { return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == "" }

func Identifier() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func (store *Store) read() (State, error) {
	state := State{Version: 1, Owner: store.owner, Shares: make(map[string]sharein.Item), Protected: make(map[string]ProtectedEntry)}
	path := filepath.Join(store.root, "index.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return State{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 24<<20 {
		return State{}, errors.New("notes: invalid feature index")
	}
	if err := privatefile.Check(path); err != nil {
		return State{}, err
	}
	input, err := os.Open(path)
	if err != nil {
		return State{}, err
	}
	defer input.Close()
	decoder := json.NewDecoder(io.LimitReader(input, (24<<20)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return State{}, errors.New("notes: trailing index content")
	}
	if state.Version != 1 || state.Owner != store.owner || state.Shares == nil || state.Protected == nil || len(state.Shares) > 16 || len(state.Protected) > 128 {
		return State{}, errors.New("notes: invalid feature index")
	}
	for id, item := range state.Shares {
		if id != item.Identifier || item.Validate() != nil {
			return State{}, errors.New("notes: invalid reviewed share")
		}
	}
	for id, entry := range state.Protected {
		if !ValidID(id) || id != entry.ID {
			return State{}, errors.New("notes: invalid protected note index")
		}
	}
	return state, nil
}

func (store *Store) Snapshot(ctx context.Context) (State, error) {
	if err := store.authorize(ctx); err != nil {
		return State{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.read()
}

// Update serializes installation metadata and atomically replaces a synced file.
// Callbacks must not call another Store method.
func (store *Store) Update(ctx context.Context, change func(*State) error) error {
	if err := store.authorize(ctx); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	state, err := store.read()
	if err != nil {
		return err
	}
	if err := change(&state); err != nil {
		return err
	}
	if len(state.Shares) > 16 || len(state.Protected) > 128 {
		return errors.New("notes: device storage is full")
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(encoded) > 24<<20 {
		return errors.New("notes: device storage is full")
	}
	output, err := os.CreateTemp(store.root, ".index-*")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	defer output.Close()
	if err := privatefile.Harden(output.Name(), false); err != nil {
		return err
	}
	if _, err := output.Write(encoded); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := privatefile.Replace(output.Name(), filepath.Join(store.root, "index.json")); err != nil {
		return err
	}
	return privatefile.SyncDirectory(store.root)
}

func (store *Store) Stage(ctx context.Context, item sharein.Item) error {
	if err := item.Validate(); err != nil {
		return err
	}
	return store.Update(ctx, func(state *State) error {
		if existing, found := state.Shares[item.Identifier]; found {
			before, _ := json.Marshal(existing)
			after, _ := json.Marshal(item)
			if string(before) != string(after) {
				return errors.New("notes: shared item identity changed")
			}
			return nil
		}
		state.Shares[item.Identifier] = item
		return nil
	})
}

func EncodeProtected(title, body string) ([]byte, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 200 || !utf8.ValidString(title) || !utf8.ValidString(body) {
		return nil, errors.New("Enter a title of up to 200 characters and valid note text.")
	}
	encoded, err := json.Marshal(ProtectedNote{Version: 1, Title: title, Body: body})
	if err == nil && len(encoded) > securestore.MaxBytes {
		return nil, errors.New("Protected notes must fit within 16 KiB, including the title.")
	}
	return encoded, err
}

func DecodeProtected(data []byte) (ProtectedNote, error) {
	var note ProtectedNote
	if len(data) > securestore.MaxBytes || json.Unmarshal(data, &note) != nil || note.Version != 1 {
		return note, errors.New("Protected note content could not be read.")
	}
	if _, err := EncodeProtected(note.Title, note.Body); err != nil {
		return ProtectedNote{}, err
	}
	return note, nil
}

func (store *Store) Epoch(ctx context.Context) (string, error) {
	if err := store.authorize(ctx); err != nil {
		return "", err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return strconv.FormatUint(store.epoch, 10), nil
}

func (store *Store) Grant(ctx context.Context, id, epoch string, note ProtectedNote) (string, error) {
	if err := store.authorize(ctx); err != nil {
		return "", err
	}
	if !ValidID(id) {
		return "", ErrLocked
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if epoch != strconv.FormatUint(store.epoch, 10) || len(store.leases) >= 64 {
		return "", ErrLocked
	}
	token, err := Identifier()
	if err != nil {
		return "", err
	}
	store.leases[token] = lease{ID: id, Epoch: store.epoch, Note: note}
	return token, nil
}

func (store *Store) ReadLease(ctx context.Context, id, token string) (ProtectedNote, error) {
	if err := store.authorize(ctx); err != nil {
		return ProtectedNote{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, exists := store.leases[token]
	if !exists || entry.ID != id || entry.Epoch != store.epoch {
		return ProtectedNote{}, ErrLocked
	}
	return entry.Note, nil
}

func (store *Store) Lock() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.epoch++
	clear(store.leases)
}

func (store *Store) CloseRoute(route string) {
	parts := strings.Split(route, "/")
	if len(parts) != 5 || parts[1] != "protected" || parts[3] != "edit" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.leases, parts[4])
}
