package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/internal/privatefile"
)

const MaxStoredBytes = 16 << 20
const MaxStoredFiles = 64

var ErrNotFound = errors.New("files: attachment not found")

// Store owns one private directory. Share a single Store per installation process.
type Store struct {
	mu   sync.Mutex
	root string
}

type stored struct {
	Collection string   `json:"collection"`
	Document   Document `json:"document"`
}

// NewStore opens a private attachment directory without exposing its paths to UI.
func NewStore(directory string) (*Store, error) {
	if directory == "" {
		return nil, errors.New("files: a storage directory is required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("files: storage must be a directory, not a symlink")
	}
	if err := privatefile.Harden(directory, true); err != nil {
		return nil, err
	}
	return &Store{root: directory}, nil
}

// Put atomically stores a bounded attachment under an application-owned collection.
func (store *Store) Put(ctx context.Context, collection string, document Document) (File, error) {
	var identifier [16]byte
	if _, err := rand.Read(identifier[:]); err != nil {
		return File{}, err
	}
	return store.put(ctx, collection, document, hex.EncodeToString(identifier[:]), false)
}

// PutOnce binds an application import identity to immutable attachment bytes.
// Repeating an interrupted import returns the same handle without using quota.
func (store *Store) PutOnce(ctx context.Context, collection, identity string, document Document) (File, error) {
	if identity == "" || len(identity) > 512 {
		return File{}, errors.New("files: an import identity is required")
	}
	digest := sha256.Sum256([]byte("files.import.v1\x00" + collection + "\x00" + identity))
	return store.put(ctx, collection, document, hex.EncodeToString(digest[:16]), true)
}

func (store *Store) put(ctx context.Context, collection string, document Document, handle string, once bool) (File, error) {
	if err := document.Validate(); err != nil {
		return File{}, err
	}
	if collection == "" || len(collection) > 512 {
		return File{}, errors.New("files: an application collection is required")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return File{}, err
	}
	defer root.Close()
	if once {
		existing, err := read(ctx, root, handle)
		if err == nil {
			if existing.Collection != collection || existing.Document.Name != document.Name || existing.Document.MediaType != document.MediaType || !bytes.Equal(existing.Document.Data, document.Data) {
				return File{}, errors.New("files: import identity already contains different content")
			}
			return metadata(handle, document), nil
		}
		if !errors.Is(err, ErrNotFound) {
			return File{}, err
		}
	}
	items, total, err := scan(ctx, root)
	if err != nil {
		return File{}, err
	}
	if len(items) >= MaxStoredFiles || total+len(document.Data) > MaxStoredBytes {
		return File{}, errors.New("files: attachment storage is full")
	}
	encoded, err := json.Marshal(stored{Collection: collection, Document: document})
	if err != nil {
		return File{}, err
	}
	temporary := handle + ".tmp"
	if once {
		// One Store owns writes to this directory. An import may resume after
		// process death before its temporary file was renamed or removed.
		if err := root.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
			return File{}, err
		}
	}
	output, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return File{}, err
	}
	defer root.Remove(temporary)
	_, writeErr := output.Write(encoded)
	if writeErr == nil {
		writeErr = output.Sync()
	}
	closeErr := output.Close()
	if err := errors.Join(writeErr, closeErr, ctx.Err()); err != nil {
		return File{}, err
	}
	if err := root.Rename(temporary, handle+".json"); err != nil {
		return File{}, err
	}
	if err := privatefile.SyncDirectory(store.root); err != nil {
		return File{}, err
	}
	return metadata(handle, document), nil
}

// List lists only the requested application's collection, sorted by opaque identity.
func (store *Store) List(ctx context.Context, collection string) ([]File, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	items, _, err := scan(ctx, root)
	if err != nil {
		return nil, err
	}
	result := []File{}
	for handle, item := range items {
		if item.Collection == collection {
			result = append(result, metadata(handle, item.Document))
		}
	}
	sort.Slice(result, func(first, second int) bool { return result[first].Handle < result[second].Handle })
	return result, nil
}

// Read checks collection membership before returning attachment content.
func (store *Store) Read(ctx context.Context, collection, handle string) (Document, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return Document{}, err
	}
	defer root.Close()
	item, err := read(ctx, root, handle)
	if err != nil {
		return Document{}, err
	}
	if item.Collection != collection {
		return Document{}, ErrNotFound
	}
	return item.Document, nil
}

// Delete removes only an attachment belonging to the requested collection.
func (store *Store) Delete(ctx context.Context, collection, handle string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return err
	}
	defer root.Close()
	item, err := read(ctx, root, handle)
	if err != nil {
		return err
	}
	if item.Collection != collection {
		return ErrNotFound
	}
	return root.Remove(handle + ".json")
}

func scan(ctx context.Context, root *os.Root) (map[string]stored, int, error) {
	directory, err := root.Open(".")
	if err != nil {
		return nil, 0, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, 0, err
	}
	items, total := make(map[string]stored), 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		handle := strings.TrimSuffix(entry.Name(), ".json")
		item, err := read(ctx, root, handle)
		if err != nil {
			return nil, 0, err
		}
		items[handle], total = item, total+len(item.Document.Data)
		if len(items) > MaxStoredFiles || total > MaxStoredBytes {
			return nil, 0, errors.New("files: stored attachments exceed quota")
		}
	}
	return items, total, nil
}

func read(ctx context.Context, root *os.Root, handle string) (stored, error) {
	if ctx.Err() != nil {
		return stored{}, ctx.Err()
	}
	decoded, err := hex.DecodeString(handle)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != handle {
		return stored{}, ErrNotFound
	}
	input, err := root.Open(handle + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return stored{}, ErrNotFound
	}
	if err != nil {
		return stored{}, err
	}
	defer input.Close()
	var item stored
	decoder := json.NewDecoder(io.LimitReader(input, 2<<20))
	if err := decoder.Decode(&item); err != nil {
		return stored{}, err
	}
	if err := item.Document.Validate(); err != nil {
		return stored{}, err
	}
	return item, nil
}

func metadata(handle string, document Document) File {
	return File{Handle: handle, Name: document.Name, MediaType: document.MediaType, Size: len(document.Data)}
}
