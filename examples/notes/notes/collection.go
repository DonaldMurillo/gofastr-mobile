package notes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	PageSize         = 10
	MaximumRows      = 100
	MaximumSelection = 20
	UndoLifetime     = 30 * time.Second
	MaximumUndo      = 32
)

// Query selects an owner-scoped collection and expands one bounded cursor window.
type Query struct {
	Search string
	Tag    string
	View   string
	Order  string
	Page   int
	After  *Cursor
}

type Cursor struct {
	ID        string
	Title     string
	UpdatedAt string
	Pinned    bool
}

func (query Query) scope() string {
	encoded, _ := json.Marshal([]string{query.Search, query.Tag, query.View, query.Order})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:12])
}

func (query Query) PageValue() string {
	if query.After == nil {
		return strconv.Itoa(max(query.Page, 1))
	}
	return "c:" + strconv.Itoa(max(query.Page, 1)) + ":" + query.scope() + ":" + base64.RawURLEncoding.EncodeToString([]byte(query.After.ID))
}

func (query *Query) SetPage(value string) error {
	if value == "" {
		query.Page = 1
		return nil
	}
	if page, err := strconv.Atoi(value); err == nil && page >= 1 && page <= MaximumRows/PageSize {
		query.Page = page
		return nil
	}
	if len(value) > 128 {
		return &ValidationError{Message: "Choose a valid notes page."}
	}
	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[0] != "c" || parts[2] != query.scope() {
		return &ValidationError{Message: "The notes query changed. Load the first page again."}
	}
	page, pageErr := strconv.Atoi(parts[1])
	identifier, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || pageErr != nil || page < 1 || page > MaximumRows/PageSize || len(identifier) == 0 || len(identifier) > 64 || !utf8.Valid(identifier) {
		return &ValidationError{Message: "Choose a valid notes page."}
	}
	query.After, query.Page = &Cursor{ID: string(identifier)}, page
	return nil
}

// Collection contains a bounded window, its known row count (not a database
// total), and an opaque next-page value. Pass Next unchanged to Query.SetPage.
type Collection struct {
	Notes []Note
	Total int
	Next  string
}

type CollectionRepository interface {
	Collection(context.Context, Query) (Collection, error)
}

// Collection delegates filtering, stable ordering and bounded keyset windows to
// the authorized repository. No unbounded snapshot or deep offset is read.
func (service *Service) Collection(ctx context.Context, query Query) (Collection, error) {
	query.Search = strings.TrimSpace(query.Search)
	if utf8.RuneCountInString(query.Search) > 200 || !ValidTag(query.Tag) ||
		(query.View != "" && query.View != "active" && query.View != "archived" && query.View != "trash") ||
		(query.Order != "" && query.Order != "recent" && query.Order != "title" && query.Order != "relevance") || query.Page < 0 || query.Page > MaximumRows/PageSize || !utf8.ValidString(query.Search) {
		return Collection{}, &ValidationError{Message: "Choose valid note filters and page."}
	}
	repository, ok := service.repository.(CollectionRepository)
	if !ok {
		return Collection{}, errors.New("notes: repository does not support paging")
	}
	return repository.Collection(ctx, query)
}

// Change describes one reversible metadata operation on at most 20 notes.
type Change struct {
	Kind string
	Tag  string
}

// Mutation reports successful individual writes, including before a partial
// failure. Undo is opaque, owner-scoped, process-local, and valid for 30 seconds.
type Mutation struct {
	Count int
	Undo  string
}

type undoItem struct {
	id       string
	before   Details
	expected Details
}

type undoEntry struct {
	sequence uint64
	owner    string
	expires  time.Time
	items    []undoItem
}

var ErrUndoUnavailable = errors.New("undo is expired, unavailable, or the notes have changed")

// LatestUndo returns the most recent live receipt for this service and owner.
func (service *Service) LatestUndo(ctx context.Context) string {
	service.undoMu.Lock()
	defer service.undoMu.Unlock()
	owner, err := service.undoOwner(ctx)
	if err != nil {
		return ""
	}
	latest := ""
	for token, entry := range service.undo {
		if time.Now().After(entry.expires) {
			delete(service.undo, token)
			continue
		}
		if entry.owner == owner && (latest == "" || entry.sequence > service.undo[latest].sequence) {
			latest = token
		}
	}
	return latest
}

// Change authorizes every ID before writing, then reauthorizes each write via
// SaveDetails. Multiple writes are deliberately not claimed to be a transaction.
func (service *Service) Change(ctx context.Context, ids []string, change Change) (Mutation, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	owner, err := service.undoOwner(ctx)
	if err != nil {
		return Mutation{}, err
	}
	if change.Kind != "pin" && change.Kind != "archive" && change.Kind != "unarchive" && change.Kind != "trash" && change.Kind != "restore" && change.Kind != "tag" {
		return Mutation{}, &ValidationError{Message: "Choose an available note action."}
	}
	if change.Kind == "tag" && !ValidTag(change.Tag) {
		return Mutation{}, &ValidationError{Message: "Choose an available tag."}
	}
	items, err := service.loadSelection(ctx, ids)
	if err != nil {
		return Mutation{}, err
	}
	entry := undoEntry{owner: owner, expires: time.Now().Add(UndoLifetime)}
	for _, item := range items {
		details := item.Details
		switch change.Kind {
		case "pin":
			details.Pinned = !details.Pinned
		case "archive":
			details.Archived = true
		case "unarchive":
			details.Archived = false
		case "trash":
			details.Trashed = true
		case "restore":
			details.Trashed = false
		case "tag":
			details.Tag = change.Tag
		}
		if details == item.Details {
			continue
		}
		updated, writeErr := service.saveMetadata(ctx, item.ID, details)
		if writeErr != nil {
			return service.remember(entry), writeErr
		}
		entry.items = append(entry.items, undoItem{id: item.ID, before: item.Details, expected: updated.Details})
	}
	return service.remember(entry), nil
}

func (service *Service) loadSelection(ctx context.Context, ids []string) ([]Note, error) {
	if len(ids) == 0 || len(ids) > MaximumSelection {
		return nil, &ValidationError{Message: "Select between 1 and 20 notes."}
	}
	seen := make(map[string]bool, len(ids))
	items := make([]Note, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			return nil, &ValidationError{Message: "Select each note only once."}
		}
		seen[id] = true
		item, err := service.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (service *Service) remember(entry undoEntry) Mutation {
	service.undoMu.Lock()
	defer service.undoMu.Unlock()
	result := Mutation{Count: len(entry.items)}
	if result.Count == 0 {
		return result
	}
	for token, retained := range service.undo {
		if time.Now().After(retained.expires) {
			delete(service.undo, token)
		}
	}
	if len(service.undo) >= MaximumUndo {
		var oldest string
		for token, retained := range service.undo {
			if oldest == "" || retained.sequence < service.undo[oldest].sequence {
				oldest = token
			}
		}
		delete(service.undo, oldest)
	}
	result.Undo = rand.Text()
	service.undoSequence++
	entry.sequence = service.undoSequence
	service.undo[result.Undo] = entry
	return result
}

// Undo restores only metadata, preserving current text and all attachments.
// It consumes its receipt before writes, so retries cannot replay a partial undo.
func (service *Service) Undo(ctx context.Context, token string) (Mutation, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	owner, err := service.undoOwner(ctx)
	if err != nil {
		return Mutation{}, err
	}
	service.undoMu.Lock()
	entry, exists := service.undo[token]
	service.undoMu.Unlock()
	if !exists || entry.owner != owner || time.Now().After(entry.expires) {
		return Mutation{}, ErrUndoUnavailable
	}
	items := make([]Note, 0, len(entry.items))
	for _, retained := range entry.items {
		item, err := service.Get(ctx, retained.id)
		if err != nil {
			return Mutation{}, err
		}
		if item.Details != retained.expected {
			return Mutation{}, ErrUndoUnavailable
		}
		items = append(items, item)
	}
	service.undoMu.Lock()
	delete(service.undo, token)
	service.undoMu.Unlock()
	result := Mutation{}
	for index, item := range items {
		if _, err := service.saveMetadata(ctx, item.ID, entry.items[index].before); err != nil {
			return result, err
		}
		result.Count++
	}
	return result, nil
}

func (service *Service) saveMetadata(ctx context.Context, id string, details Details) (Note, error) {
	if err := ValidateDetails(details); err != nil {
		return Note{}, err
	}
	if _, err := service.Get(ctx, id); err != nil {
		return Note{}, err
	}
	repository, ok := service.repository.(interface {
		SaveMetadata(context.Context, string, Details) (Note, error)
	})
	if !ok {
		return Note{}, errors.New("notes: repository does not support metadata operations")
	}
	note, err := repository.SaveMetadata(ctx, id, details)
	if err == nil {
		service.AttachmentsChanged(ctx)
	}
	return note, err
}

func (service *Service) undoOwner(ctx context.Context) (string, error) {
	repository, ok := service.repository.(interface {
		Owner(context.Context) (string, error)
	})
	if !ok {
		return "", errors.New("notes: authenticated owner required")
	}
	return repository.Owner(ctx)
}
