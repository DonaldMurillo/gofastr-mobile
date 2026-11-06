package notesstore

import (
	"context"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

func (store *Store) BackupNotes(ctx context.Context) ([]notes.Note, error) {
	var result []notes.Note
	last, size := "", 0
	for {
		options := crud.ListOptions{Limit: 8, Sorts: []filter.ParsedSort{{Field: "id"}}}
		if last != "" {
			options.Filters = []filter.ParsedFilter{{Field: "id", Op: filter.OpGt, Value: last}}
		}
		rows, err := store.handler.ListAll(ctx, options)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			note, err := decode(row)
			if err != nil {
				return nil, err
			}
			size += len(note.Title) + len(note.Body)
			if len(result) >= notes.MaximumBackupNotes || size > notes.MaximumBackupText {
				return nil, &notes.ValidationError{Message: "Back up at most 500 notes and 4 MiB of note text at a time. Select a smaller group."}
			}
			result = append(result, note)
			last = note.ID
		}
		if len(rows) < 8 {
			return result, nil
		}
	}
}

func (store *Store) ImportBackup(ctx context.Context, source string, note notes.Note) (notes.Note, bool, error) {
	values := detailValues(note.Details)
	values["title"], values["body"] = note.Title, note.Body
	return store.importSource(ctx, "backup", source, values)
}

func (store *Store) FindBackupImport(ctx context.Context, source string) (notes.Note, bool, error) {
	note, complete, found, err := store.findSource(ctx, "backup", source)
	if err == nil && !found {
		err = notes.ErrNotFound
	}
	return note, complete, err
}
