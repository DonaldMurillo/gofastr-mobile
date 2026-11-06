package notes

import (
	"context"
	"errors"
	"strings"
)

const MaximumBackupNotes = 500
const MaximumBackupText = 4 << 20

type BackupRepository interface {
	BackupNotes(context.Context) ([]Note, error)
	ImportBackup(context.Context, string, Note) (Note, bool, error)
	FindBackupImport(context.Context, string) (Note, bool, error)
}

// BackupNotes includes active, archived and trashed ordinary notes. Protected
// notes and drafts live outside this repository and cannot enter a backup.
func (service *Service) BackupNotes(ctx context.Context) ([]Note, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	repository, ok := service.repository.(BackupRepository)
	if !ok {
		return nil, errors.New("notes: backups are unavailable")
	}
	return repository.BackupNotes(ctx)
}

func (service *Service) ImportBackup(ctx context.Context, source string, note Note) (Note, bool, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	note.Title = strings.TrimSpace(note.Title)
	if err := validateBackupSource(source); err != nil {
		return Note{}, false, err
	}
	if err := ValidateText(note.Title, note.Body); err != nil {
		return Note{}, false, err
	}
	if err := ValidateDetails(note.Details); err != nil {
		return Note{}, false, err
	}
	repository, ok := service.repository.(BackupRepository)
	if !ok {
		return Note{}, false, errors.New("notes: backups are unavailable")
	}
	imported, complete, err := repository.ImportBackup(ctx, source, note)
	if err == nil {
		service.AttachmentsChanged(ctx)
	}
	return imported, complete, err
}

func (service *Service) FindBackupImport(ctx context.Context, source string) (Note, bool, error) {
	if err := validateBackupSource(source); err != nil {
		return Note{}, false, err
	}
	repository, ok := service.repository.(BackupRepository)
	if !ok {
		return Note{}, false, errors.New("notes: backups are unavailable")
	}
	return repository.FindBackupImport(ctx, source)
}

func validateBackupSource(source string) error {
	if len(source) != 32 || strings.Trim(source, "0123456789abcdef") != "" {
		return &ValidationError{Message: "This backup record has an invalid identifier."}
	}
	return nil
}
