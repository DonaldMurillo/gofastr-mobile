// Package notes defines Notes behavior without a renderer, HTTP, or GoFastr UI.
package notes

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Note is the application data shared by presentations.
type Note struct {
	ID        string
	Title     string
	Body      string
	UpdatedAt time.Time
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
type Service struct{ repository Repository }

// New connects Notes behavior to a repository.
func New(repository Repository) *Service { return &Service{repository: repository} }

// List searches notes using the repository's scoped search implementation.
func (service *Service) List(ctx context.Context, search string) ([]Note, error) {
	search = strings.TrimSpace(search)
	if utf8.RuneCountInString(search) > 200 {
		return nil, &ValidationError{Message: "Search must be 200 characters or fewer."}
	}
	return service.repository.List(ctx, search)
}

// Get loads one visible note.
func (service *Service) Get(ctx context.Context, id string) (Note, error) {
	if id == "" {
		return Note{}, ErrNotFound
	}
	return service.repository.Get(ctx, id)
}

// Save creates a note when id is empty, otherwise updates it. Only a successful
// repository write means that the note has been saved.
func (service *Service) Save(ctx context.Context, id, title, body string) (Note, error) {
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
	return service.repository.Save(ctx, id, title, body)
}
