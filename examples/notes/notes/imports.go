package notes

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// ImportRepository commits the source receipt in the same row as the note.
type ImportRepository interface {
	ImportShared(context.Context, string, string, string) (Note, bool, error)
	FinishImport(context.Context, string) error
}

// ImportShared returns the original note for a previously accepted source.
// Completed means all attachments were committed before inbox acknowledgement.
func (service *Service) ImportShared(ctx context.Context, source, title, body string) (Note, bool, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(source) != 32 || strings.Trim(source, "0123456789abcdef") != "" {
		return Note{}, false, &ValidationError{Message: "This shared item has an invalid identifier."}
	}
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 200 || !utf8.ValidString(title) || len(body) > 1<<20 || !utf8.ValidString(body) {
		return Note{}, false, &ValidationError{Message: "Enter a title of up to 200 characters and a note under 1 MB."}
	}
	repository, ok := service.repository.(ImportRepository)
	if !ok {
		return Note{}, false, errors.New("notes: repository does not support shared imports")
	}
	note, complete, err := repository.ImportShared(ctx, source, title, body)
	if err == nil {
		service.AttachmentsChanged(ctx)
	}
	return note, complete, err
}

func (service *Service) FinishImport(ctx context.Context, id string) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	repository, ok := service.repository.(ImportRepository)
	if !ok {
		return errors.New("notes: repository does not support shared imports")
	}
	return repository.FinishImport(ctx, id)
}
