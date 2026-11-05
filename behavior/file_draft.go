package behavior

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"
)

const (
	MaxDraftBytes    = 1 << 20
	MaxDraftKeyBytes = 4096
	MaxDraftFields   = 1024
)

var (
	ErrDraftConflict = errors.New("behavior: draft version conflict")
	ErrDraftTooLarge = errors.New("behavior: draft exceeds storage limits")
	draftStoreLocks  sync.Map
)

type FileDraftStore struct {
	root string
	mu   *sync.Mutex
}

type draftFile struct {
	Format   int      `json:"format"`
	Snapshot Snapshot `json:"snapshot"`
}

func NewFileDraftStore(root string) (*FileDraftStore, error) {
	if root == "" {
		return nil, errors.New("behavior: trusted draft directory is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("behavior: draft directory must not be a symlink")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	lock, _ := draftStoreLocks.LoadOrStore(root, &sync.Mutex{})
	store := &FileDraftStore{root: root, mu: lock.(*sync.Mutex)}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *FileDraftStore) Load(ctx context.Context, key string) (Snapshot, bool, error) {
	path, err := store.path(key)
	if err != nil {
		return Snapshot{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	return readDraftFile(path)
}

func (store *FileDraftStore) Put(ctx context.Context, key string, snapshot Snapshot) error {
	path, err := store.path(key)
	if err != nil {
		return err
	}
	if err := validateDraftSnapshot(snapshot); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	existing, exists, err := readDraftFile(path)
	if err != nil {
		return err
	}
	if exists && existing.InstanceID == snapshot.InstanceID &&
		(existing.Revision > snapshot.Revision || snapshotSaveToken(existing) > snapshotSaveToken(snapshot)) {
		return ErrDraftConflict
	}
	encoded, err := json.Marshal(draftFile{Format: 1, Snapshot: snapshot})
	if err != nil {
		return err
	}
	if len(encoded) > MaxDraftBytes {
		return ErrDraftTooLarge
	}
	temporary, err := os.CreateTemp(store.root, ".draft-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		return err
	}
	return store.syncDirectory()
}

func (store *FileDraftStore) ClearIfRevision(ctx context.Context, key string, expected DraftRevision) (bool, error) {
	path, err := store.path(key)
	if err != nil {
		return false, err
	}
	if expected.InstanceID == "" || expected.Token == "" {
		return false, errors.New("behavior: valid draft version is required for cleanup")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	draft, exists, err := readDraftFile(path)
	if err != nil || !exists || !expected.Matches(draft) {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, store.syncDirectory()
}

func (store *FileDraftStore) path(key string) (string, error) {
	if len(key) == 0 || len(key) > MaxDraftKeyBytes || !utf8.ValidString(key) {
		return "", errors.New("behavior: draft key must be valid UTF-8 and 1..4096 bytes")
	}
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(store.root, hex.EncodeToString(digest[:])+".json"), nil
}

func (store *FileDraftStore) syncDirectory() error {
	directory, err := os.Open(store.root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func readDraftFile(path string) (Snapshot, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return Snapshot{}, false, errors.New("behavior: draft must be an owner-only regular file")
	}
	if info.Size() > MaxDraftBytes {
		return Snapshot{}, false, ErrDraftTooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, false, err
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, MaxDraftBytes+1))
	if err != nil {
		return Snapshot{}, false, err
	}
	if len(encoded) > MaxDraftBytes {
		return Snapshot{}, false, ErrDraftTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var disk draftFile
	if err := decoder.Decode(&disk); err != nil {
		return Snapshot{}, false, fmt.Errorf("behavior: invalid draft file: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Snapshot{}, false, errors.New("behavior: draft file has trailing content")
	}
	if disk.Format != 1 {
		return Snapshot{}, false, errors.New("behavior: unsupported draft format")
	}
	if err := validateDraftSnapshot(disk.Snapshot); err != nil {
		return Snapshot{}, false, err
	}
	return disk.Snapshot, true, nil
}

func snapshotSaveToken(snapshot Snapshot) uint64 {
	if snapshot.Submitted == nil {
		return 0
	}
	return snapshot.Submitted.Token
}

func validateDraftSnapshot(snapshot Snapshot) error {
	if snapshot.InstanceID == "" || snapshot.DraftToken == "" {
		return errors.New("behavior: draft snapshot instance ID and token are required")
	}
	if snapshot.Outcome != "" && snapshot.Outcome != SaveSucceeded && snapshot.Outcome != SaveFailed && snapshot.Outcome != SaveUnknown {
		return errors.New("behavior: invalid draft save outcome")
	}
	total := 0
	add := func(value string) error {
		if len(value) > MaxDraftBytes-total {
			return ErrDraftTooLarge
		}
		if !utf8.ValidString(value) {
			return errors.New("behavior: draft text must be valid UTF-8")
		}
		total += len(value)
		return nil
	}
	fields := []map[string]string{snapshot.Baseline, snapshot.Current, snapshot.FieldErrors}
	texts := []string{snapshot.InstanceID, snapshot.DraftToken, snapshot.Route, snapshot.Message, string(snapshot.Outcome)}
	if snapshot.Submitted != nil {
		fields = append(fields, snapshot.Submitted.Values)
		texts = append(texts, snapshot.Submitted.InstanceID, snapshot.Submitted.DraftToken, snapshot.Submitted.Route)
	}
	for _, text := range texts {
		if err := add(text); err != nil {
			return err
		}
	}
	for _, values := range fields {
		if len(values) > MaxDraftFields {
			return ErrDraftTooLarge
		}
		for field, value := range values {
			if err := add(field); err != nil {
				return err
			}
			if err := add(value); err != nil {
				return err
			}
		}
	}
	return nil
}
