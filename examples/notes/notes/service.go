// Package notes defines Notes behavior without a renderer, HTTP, or GoFastr UI.
package notes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Note is the application data shared by presentations.
type Note struct {
	Details
	ID        string
	Title     string
	Body      string
	UpdatedAt time.Time
}

// Details is persistent note metadata. Reminder values are local calendar/clock
// strings, not instants, timezones, scheduled jobs, or notification requests.
type Details struct {
	Pinned       bool
	Archived     bool
	Trashed      bool
	Tag          string
	ReminderDate string
	ReminderTime string
}

// DetailsRepository extends legacy repositories without breaking Save callers.
type DetailsRepository interface {
	Repository
	SaveDetails(context.Context, string, string, string, Details) (Note, error)
}

// Repository is the typed data boundary. A generated entity repository can be
// adapted here without changing either presentation.
type Repository interface {
	List(context.Context, string) ([]Note, error)
	Get(context.Context, string) (Note, error)
	Save(context.Context, string, string, string) (Note, error)
}

// ErrNotFound means that a note does not exist or is not visible to this user.
var ErrNotFound = errors.New("note not found")

// ValidationError is a user-correctable input error shared by every renderer.
type ValidationError struct{ Message string }

func (err *ValidationError) Error() string { return err.Message }

// Service holds application behavior, not navigation or screen state.
type Service struct {
	repository Repository
	mu         sync.Mutex
	undoMu     sync.Mutex
	undo       map[string]undoEntry
	changes    changeHub
}

// New connects Notes behavior to a repository.
func New(repository Repository) *Service {
	return &Service{repository: repository, undo: make(map[string]undoEntry)}
}

// List searches notes using the repository's scoped search implementation.
func (service *Service) List(ctx context.Context, search string) ([]Note, error) {
	search = strings.TrimSpace(search)
	if utf8.RuneCountInString(search) > 200 {
		return nil, &ValidationError{Message: "Search must be 200 characters or fewer."}
	}
	items, err := service.repository.List(ctx, search)
	if err != nil {
		return nil, err
	}
	visible := make([]Note, 0, len(items))
	for _, item := range items {
		if !item.Archived && !item.Trashed {
			visible = append(visible, item)
		}
	}
	return visible, nil
}

// Get loads one visible note.
func (service *Service) Get(ctx context.Context, id string) (Note, error) {
	if id == "" {
		return Note{}, ErrNotFound
	}
	return service.repository.Get(ctx, id)
}

// ReminderID authorizes a saved note and scopes its stable device identifier to
// both the installation owner and the note without exposing either identity.
func (service *Service) ReminderID(ctx context.Context, id string) (string, error) {
	if _, err := service.Get(ctx, id); err != nil {
		return "", err
	}
	owner, err := service.undoOwner(ctx)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte("notes.reminder.v1\x00" + owner + "\x00" + id))
	return "notes.reminder." + hex.EncodeToString(digest[:]), nil
}

// Save creates a note when id is empty, otherwise updates it. Only a successful
// repository write means that the note has been saved.
func (service *Service) Save(ctx context.Context, id, title, body string) (Note, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.save(ctx, id, title, body, nil)
}

// SaveDetails saves text and typed metadata through the same scoped entity.
func (service *Service) SaveDetails(ctx context.Context, id, title, body string, details Details) (Note, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.save(ctx, id, title, body, &details)
}

func (service *Service) save(ctx context.Context, id, title, body string, details *Details) (Note, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Note{}, &ValidationError{Message: "Give your note a title."}
	}
	if utf8.RuneCountInString(title) > 200 {
		return Note{}, &ValidationError{Message: "Titles must be 200 characters or fewer."}
	}
	if len(body) > 1<<20 {
		return Note{}, &ValidationError{Message: "This note is too long. Keep it under 1 MB."}
	}
	if details == nil {
		note, err := service.repository.Save(ctx, id, title, body)
		if err == nil {
			service.AttachmentsChanged(ctx)
		}
		return note, err
	}
	if err := ValidateDetails(*details); err != nil {
		return Note{}, err
	}
	if id != "" {
		if _, err := service.Get(ctx, id); err != nil {
			return Note{}, err
		}
	}
	repository, ok := service.repository.(DetailsRepository)
	if !ok {
		return Note{}, errors.New("notes: repository does not support details")
	}
	note, err := repository.SaveDetails(ctx, id, title, body, *details)
	if err == nil {
		service.AttachmentsChanged(ctx)
	}
	return note, err
}

// ValidateDetails rejects unsupported tags and noncanonical calendar metadata.
func ValidateDetails(details Details) error {
	if !ValidTag(details.Tag) {
		return &ValidationError{Message: "Choose an available tag."}
	}
	for _, field := range []struct{ value, layout, label string }{
		{details.ReminderDate, "2006-01-02", "date"},
		{details.ReminderTime, "15:04", "time"},
	} {
		if field.value == "" {
			continue
		}
		parsed, err := time.Parse(field.layout, field.value)
		if err != nil || parsed.Format(field.layout) != field.value || field.layout == "2006-01-02" && parsed.Year() < 1 {
			return &ValidationError{Message: "Choose a valid reminder " + field.label + "."}
		}
	}
	return nil
}

// ValidTag describes the supported, writable single-tag vocabulary.
func ValidTag(tag string) bool {
	return tag == "" || tag == "ideas" || tag == "work" || tag == "personal"
}
