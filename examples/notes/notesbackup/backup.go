// Package notesbackup owns portable ordinary-note backups and reviewed restore.
package notesbackup

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/internal/privatefile"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

const MaxDecoded = 24 << 20

type Record struct {
	Note  notes.Note       `json:"note"`
	Files []files.Document `json:"files"`
}
type Bundle struct {
	Format  string   `json:"format"`
	Version int      `json:"version"`
	Notes   []Record `json:"notes"`
}

func (bundle Bundle) Validate() error {
	if bundle.Format != "gofastr-notes" || bundle.Version != 1 || len(bundle.Notes) > notes.MaximumBackupNotes {
		return errors.New("Choose a supported Notes backup, version 1, with at most 500 notes.")
	}
	seen := map[string]bool{}
	textSize, fileSize, fileCount := 0, 0, 0
	for _, record := range bundle.Notes {
		id := record.Note.ID
		if id == "" || len(id) > 128 || !utf8.ValidString(id) || strings.ContainsAny(id, "/\\\x00") || seen[id] {
			return errors.New("The backup contains an invalid or repeated note identifier.")
		}
		seen[id] = true
		if err := notes.ValidateText(record.Note.Title, record.Note.Body); err != nil {
			return err
		}
		if err := notes.ValidateDetails(record.Note.Details); err != nil {
			return err
		}
		textSize += len(record.Note.Title) + len(record.Note.Body)
		for _, file := range record.Files {
			if err := file.Validate(); err != nil {
				return err
			}
			fileSize += len(file.Data)
			fileCount++
		}
	}
	if textSize > notes.MaximumBackupText || fileSize > files.MaxStoredBytes || fileCount > files.MaxStoredFiles {
		return errors.New("The backup exceeds the note-text or attachment limits.")
	}
	return nil
}

func Encode(bundle Bundle) (files.Document, error) {
	if err := bundle.Validate(); err != nil {
		return files.Document{}, err
	}
	raw, err := json.Marshal(bundle)
	if err != nil {
		return files.Document{}, err
	}
	if len(raw) > MaxDecoded {
		return files.Document{}, errors.New("The backup exceeds 24 MiB before compression. Select fewer notes.")
	}
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	if _, err = compressed.Write(raw); err != nil {
		return files.Document{}, err
	}
	if err = compressed.Close(); err != nil {
		return files.Document{}, err
	}
	if output.Len() > files.MaxBytes {
		return files.Document{}, errors.New("The backup exceeds the current 1 MiB file-transfer limit. Select fewer notes or export large attachments separately.")
	}
	return files.Document{Name: "Notes.gfnbackup", MediaType: "application/gzip", Data: output.Bytes()}, nil
}

func Decode(data []byte) (Bundle, error) {
	var bundle Bundle
	if len(data) == 0 || len(data) > files.MaxBytes {
		return bundle, errors.New("Choose a Notes backup of 1 MiB or smaller.")
	}
	input := bytes.NewReader(data)
	compressed, err := gzip.NewReader(input)
	if err != nil {
		return bundle, errors.New("This file is not a readable Notes backup.")
	}
	compressed.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(compressed, MaxDecoded+1))
	closeErr := compressed.Close()
	if readErr != nil || closeErr != nil || len(raw) > MaxDecoded || input.Len() != 0 || !utf8.Valid(raw) {
		return bundle, errors.New("The backup is damaged or exceeds its size limit.")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return Bundle{}, errors.New("The backup contents are invalid.")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Bundle{}, errors.New("The backup has unexpected trailing data.")
	}
	return bundle, bundle.Validate()
}

// Source identifies one immutable backup record. A later export with changed
// content restores a separate copy, never overwrites an existing note.
func Source(record Record) string {
	record.Note.UpdatedAt = time.Time{}
	encoded, _ := json.Marshal(record)
	digest := sha256.Sum256(append([]byte("notes.backup-record.v1\x00"), encoded...))
	return hex.EncodeToString(digest[:16])
}

type Manager struct {
	mu      sync.Mutex
	root    string
	service *notes.Service
	files   *files.Store
}

func New(directory string, service *notes.Service, attachments *files.Store) (*Manager, error) {
	if !filepath.IsAbs(directory) || service == nil || attachments == nil {
		return nil, errors.New("notesbackup: an absolute directory, service and attachment store are required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("notesbackup: directory must not be a symlink")
	}
	if err := privatefile.Harden(directory, true); err != nil {
		return nil, err
	}
	return &Manager{root: directory, service: service, files: attachments}, nil
}

func owner(ctx context.Context) (string, error) {
	user, exists := handler.GetUser(ctx)
	identity, valid := user.(interface{ GetID() string })
	if !exists || !valid || identity.GetID() == "" {
		return "", errors.New("notesbackup: authenticated owner required")
	}
	return identity.GetID(), ctx.Err()
}

func (manager *Manager) Export(ctx context.Context, ids []string) (files.Document, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if _, err := owner(ctx); err != nil {
		return files.Document{}, err
	}
	version, err := manager.service.Version(ctx)
	if err != nil {
		return files.Document{}, err
	}
	var items []notes.Note
	if ids == nil {
		items, err = manager.service.BackupNotes(ctx)
	} else {
		seen := map[string]bool{}
		if len(ids) == 0 || len(ids) > notes.MaximumSelection {
			return files.Document{}, errors.New("Select between 1 and 20 notes for a backup.")
		}
		for _, id := range ids {
			if seen[id] {
				return files.Document{}, errors.New("Select each note once.")
			}
			seen[id] = true
			var note notes.Note
			note, err = manager.service.Get(ctx, id)
			if err != nil {
				break
			}
			items = append(items, note)
		}
	}
	if err != nil {
		return files.Document{}, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	bundle := Bundle{Format: "gofastr-notes", Version: 1, Notes: []Record{}}
	for _, note := range items {
		record, err := manager.record(ctx, note)
		if err != nil {
			return files.Document{}, err
		}
		bundle.Notes = append(bundle.Notes, record)
	}
	current, err := manager.service.Version(ctx)
	if err != nil {
		return files.Document{}, err
	}
	if current != version {
		return files.Document{}, errors.New("Notes changed while preparing the backup. Export again to capture the current notes.")
	}
	return Encode(bundle)
}

func (manager *Manager) record(ctx context.Context, note notes.Note) (Record, error) {
	result := Record{Note: note, Files: []files.Document{}}
	items, err := manager.files.List(ctx, note.ID)
	if err != nil {
		return result, err
	}
	for _, item := range items {
		file, err := manager.files.Read(ctx, note.ID, item.Handle)
		if err != nil {
			return result, err
		}
		result.Files = append(result.Files, file)
	}
	// Storage handles differ after restoring, so order by immutable file content.
	sort.Slice(result.Files, func(i, j int) bool {
		a, b := result.Files[i], result.Files[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.MediaType != b.MediaType {
			return a.MediaType < b.MediaType
		}
		return bytes.Compare(a.Data, b.Data) < 0
	})
	return result, nil
}

type staged struct {
	Owner string
	Token string
	Data  []byte
}

func (manager *Manager) Stage(ctx context.Context, document files.Document) (string, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	identity, err := owner(ctx)
	if err != nil {
		return "", err
	}
	if err := document.Validate(); err != nil {
		return "", err
	}
	if _, err := Decode(document.Data); err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte(identity+"\x00"), document.Data...))
	token := hex.EncodeToString(digest[:16])
	encoded, err := json.Marshal(staged{identity, token, document.Data})
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(manager.root, ".review-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = privatefile.Harden(file.Name(), false); err != nil {
		return "", err
	}
	if _, err = file.Write(encoded); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = privatefile.Replace(file.Name(), filepath.Join(manager.root, "review.json")); err != nil {
		return "", err
	}
	return token, privatefile.SyncDirectory(manager.root)
}

func (manager *Manager) load(ctx context.Context, token string) (Bundle, error) {
	identity, err := owner(ctx)
	if err != nil {
		return Bundle{}, err
	}
	path := filepath.Join(manager.root, "review.json")
	info, err := os.Lstat(path)
	if err != nil {
		return Bundle{}, errors.New("Choose the backup file again to review it.")
	}
	if !info.Mode().IsRegular() || info.Size() > 2*files.MaxBytes {
		return Bundle{}, errors.New("The stored backup review is invalid.")
	}
	if err := privatefile.Check(path); err != nil {
		return Bundle{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	var entry staged
	if json.Unmarshal(data, &entry) != nil || entry.Owner != identity || entry.Token != token {
		return Bundle{}, errors.New("This backup review expired. Choose the file again.")
	}
	digest := sha256.Sum256(append([]byte(identity+"\x00"), entry.Data...))
	if entry.Token != hex.EncodeToString(digest[:16]) {
		return Bundle{}, errors.New("The stored backup review changed. Choose the file again.")
	}
	return Decode(entry.Data)
}

type Preview struct {
	Bundle               Bundle
	New, Existing, Files int
}

func (manager *Manager) Preview(ctx context.Context, token string) (Preview, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	bundle, err := manager.load(ctx, token)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{Bundle: bundle}
	for _, record := range bundle.Notes {
		skip, _, err := manager.existing(ctx, record, Source(record))
		if err != nil {
			return Preview{}, err
		}
		if skip {
			preview.Existing++
		} else {
			preview.New++
		}
		preview.Files += len(record.Files)
	}
	return preview, nil
}

func (manager *Manager) existing(ctx context.Context, record Record, source string) (bool, string, error) {
	imported, complete, err := manager.service.FindBackupImport(ctx, source)
	if err == nil {
		return complete, imported.ID, nil
	}
	if !errors.Is(err, notes.ErrNotFound) {
		return false, "", err
	}
	original, err := manager.service.Get(ctx, record.Note.ID)
	if errors.Is(err, notes.ErrNotFound) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if original.Title != record.Note.Title || original.Body != record.Note.Body || original.Details != record.Note.Details {
		return false, "", nil
	}
	local, err := manager.record(ctx, original)
	if err != nil {
		return false, "", err
	}
	return reflect.DeepEqual(local.Files, record.Files), "", nil
}

type Restored struct{ Imported, Skipped int }

// Restore is resumable through durable note receipts and immutable attachments.
// Individual writes may commit before an error; no uncertain write is replayed.
func (manager *Manager) Restore(ctx context.Context, token string) (Restored, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	bundle, err := manager.load(ctx, token)
	if err != nil {
		return Restored{}, err
	}
	result := Restored{}
	for _, record := range bundle.Notes {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		source := Source(record)
		skip, _, err := manager.existing(ctx, record, source)
		if err != nil {
			return result, err
		}
		if skip {
			result.Skipped++
			continue
		}
		note, complete, err := manager.service.ImportBackup(ctx, source, record.Note)
		if err != nil {
			return result, err
		}
		if complete {
			result.Skipped++
			continue
		}
		if !complete {
			for index, file := range record.Files {
				if _, err = manager.files.PutOnce(ctx, note.ID, fmt.Sprintf("backup.%s.%d", source, index), file); err != nil {
					return result, err
				}
			}
			if err = manager.service.FinishImport(ctx, note.ID); err != nil {
				return result, err
			}
			manager.service.AttachmentsChanged(ctx)
		}
		result.Imported++
	}
	return result, nil
}

func (manager *Manager) Discard(ctx context.Context, token string) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if _, err := manager.load(ctx, token); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(manager.root, "review.json")); err != nil {
		return err
	}
	return privatefile.SyncDirectory(manager.root)
}
