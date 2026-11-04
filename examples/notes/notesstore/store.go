// Package notesstore adapts the existing scoped CRUD pipeline to Notes' typed
// repository. This is a compatibility adapter, not a replacement repo generator.
package notesstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
)

// Store preserves GoFastr's validation, hooks, and owner scopes.
type Store struct{ handler *crud.CrudHandler }

// New adapts a registered notes entity's handler.
func New(handler *crud.CrudHandler) *Store { return &Store{handler: handler} }

// List returns the newest notes first, with repository-side search.
func (store *Store) List(ctx context.Context, search string) ([]notes.Note, error) {
	rows, err := store.handler.ListAll(ctx, crud.ListOptions{
		Search: search, Limit: 200,
		Sorts: []filter.ParsedSort{{Field: "updated_at", Desc: true}},
	})
	if err != nil {
		return nil, err
	}
	result := make([]notes.Note, 0, len(rows))
	for _, row := range rows {
		note, err := decode(row)
		if err != nil {
			return nil, err
		}
		result = append(result, note)
	}
	return result, nil
}

// Get loads one owner-scoped note.
func (store *Store) Get(ctx context.Context, id string) (notes.Note, error) {
	row, err := store.handler.GetOne(ctx, id, nil)
	if crud.IsNotFound(err) || (err == nil && row == nil) {
		return notes.Note{}, notes.ErrNotFound
	}
	if err != nil {
		return notes.Note{}, err
	}
	return decode(row)
}

// Save writes through the same scoped pipeline used by the web API.
func (store *Store) Save(ctx context.Context, id, title, body string) (notes.Note, error) {
	values := map[string]any{"title": title, "body": body}
	var row map[string]any
	var err error
	if id == "" {
		row, err = store.handler.CreateOne(ctx, values)
	} else {
		row, err = store.handler.UpdateOne(ctx, id, values)
	}
	if crud.IsNotFound(err) {
		return notes.Note{}, notes.ErrNotFound
	}
	if err != nil {
		return notes.Note{}, err
	}
	return decode(row)
}

func decode(row map[string]any) (notes.Note, error) {
	title, valid := row["title"].(string)
	if !valid || row["id"] == nil {
		return notes.Note{}, errors.New("notesstore: invalid note row")
	}
	body, _ := row["body"].(string)
	note := notes.Note{ID: fmt.Sprint(row["id"]), Title: title, Body: body}
	updated := row["updated_at"]
	if updated == nil {
		updated = row["updatedAt"]
	}
	switch updated := updated.(type) {
	case time.Time:
		note.UpdatedAt = updated
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, updated); err == nil {
				note.UpdatedAt = parsed
				break
			}
		}
	}
	return note, nil
}
