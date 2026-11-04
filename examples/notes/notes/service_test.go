package notes

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type recordingRepository struct {
	called bool
	note   Note
	err    error
}

func (repository *recordingRepository) List(context.Context, string) ([]Note, error) {
	return nil, repository.err
}
func (repository *recordingRepository) Get(context.Context, string) (Note, error) {
	return repository.note, repository.err
}
func (repository *recordingRepository) Save(_ context.Context, id, title, body string) (Note, error) {
	repository.called = true
	repository.note = Note{ID: id, Title: title, Body: body}
	return repository.note, repository.err
}

func TestSharedSaveValidation(t *testing.T) {
	for _, test := range []struct{ name, title, body string }{
		{"empty", "  ", "draft"}, {"long title", strings.Repeat("界", 201), "draft"},
		{"long body", "Title", strings.Repeat("x", (1<<20)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &recordingRepository{}
			_, err := New(repository).Save(context.Background(), "", test.title, test.body)
			var validation *ValidationError
			if !errors.As(err, &validation) || repository.called {
				t.Fatalf("validation = %v, repository called = %v", err, repository.called)
			}
		})
	}
	repository := &recordingRepository{}
	note, err := New(repository).Save(context.Background(), "existing", "  A thought  ", "\nKeep the whitespace.\n")
	if err != nil || note.Title != "A thought" || note.Body != "\nKeep the whitespace.\n" || note.ID != "existing" {
		t.Fatalf("shared save = %+v, %v", note, err)
	}
	repository.err = errors.New("disk full")
	if _, err := New(repository).Save(context.Background(), "", "New", "Text"); !errors.Is(err, repository.err) {
		t.Fatal("repository failure was reported as a successful save")
	}
}
