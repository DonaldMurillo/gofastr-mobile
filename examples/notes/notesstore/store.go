// Package notesstore adapts the existing scoped CRUD pipeline to Notes' typed
// repository. This is a compatibility adapter, not a replacement repo generator.
package notesstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
)

// Store preserves GoFastr's validation, hooks, and owner scopes.
type Store struct{ handler *crud.CrudHandler }

// New adapts a registered notes entity's handler.
func New(handler *crud.CrudHandler) *Store { return &Store{handler: handler} }

// Owner supplies the authenticated scope for process-local undo receipts.
func (store *Store) Owner(ctx context.Context) (string, error) {
	user, exists := handler.GetUser(ctx)
	identity, identified := user.(interface{ GetID() string })
	if !exists || !identified || identity.GetID() == "" {
		return "", errors.New("notesstore: authenticated owner required")
	}
	return identity.GetID(), nil
}

// List returns the newest notes first, with repository-side search.
func (store *Store) List(ctx context.Context, search string) ([]notes.Note, error) {
	options := collectionOptions(notes.Query{Search: search})
	options.Limit = 200
	options.Fields = nil
	rows, err := store.handler.ListAll(ctx, options)
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

func collectionOptions(query notes.Query) crud.ListOptions {
	options := crud.ListOptions{Search: query.Search, Limit: max(query.Page, 1)*notes.PageSize + 1,
		Sorts:  []filter.ParsedSort{{Field: "pinned", Desc: true}},
		Fields: []string{"id", "title", "body", "updated_at", "pinned", "archived", "trashed", "tag"},
	}
	if query.Order == "title" {
		options.Sorts = append(options.Sorts, filter.ParsedSort{Field: "title"})
	} else {
		options.Sorts = append(options.Sorts, filter.ParsedSort{Field: "updated_at", Desc: true})
	}
	options.Sorts = append(options.Sorts, filter.ParsedSort{Field: "id"})
	options.Filters = append(options.Filters, filter.ParsedFilter{Field: "trashed", Op: filter.OpEq, Value: strconv.FormatBool(query.View == "trash")})
	if query.View != "trash" {
		options.Filters = append(options.Filters, filter.ParsedFilter{Field: "archived", Op: filter.OpEq, Value: strconv.FormatBool(query.View == "archived")})
	}
	if query.Tag != "" {
		options.Filters = append(options.Filters, filter.ParsedFilter{Field: "tag", Op: filter.OpEq, Value: query.Tag})
	}
	if cursor := query.After; cursor != nil {
		field, value, operator := "updated_at", cursor.UpdatedAt, filter.OpLt
		if query.Order == "title" {
			field, value, operator = "title", cursor.Title, filter.OpGt
		}
		pinned := strconv.FormatBool(cursor.Pinned)
		options.Where = &filter.Predicate{Children: []filter.Predicate{
			{Children: []filter.Predicate{{Field: "pinned", Op: filter.OpEq, Value: pinned}, {Or: true, Children: []filter.Predicate{
				{Field: field, Op: operator, Value: value},
				{Children: []filter.Predicate{{Field: field, Op: filter.OpEq, Value: value}, {Field: "id", Op: filter.OpGt, Value: cursor.ID}}},
			}}}},
		}}
		if cursor.Pinned {
			options.Where.Or = true
			options.Where.Children = append(options.Where.Children, filter.Predicate{Field: "pinned", Op: filter.OpEq, Value: "false"})
		}
	}
	return options
}

func (store *Store) Collection(ctx context.Context, query notes.Query) (notes.Collection, error) {
	if query.After != nil {
		anchorQuery := query
		anchorQuery.After = nil
		anchor := collectionOptions(anchorQuery)
		anchor.Limit = 1
		anchor.Filters = append(anchor.Filters, filter.ParsedFilter{Field: "id", Op: filter.OpEq, Value: query.After.ID})
		rows, err := store.handler.ListAll(ctx, anchor)
		if err != nil {
			return notes.Collection{}, err
		}
		if len(rows) != 1 {
			return notes.Collection{}, &notes.ValidationError{Message: "The page boundary changed or is unavailable. Load the first page again."}
		}
		note, err := decode(rows[0])
		if err != nil {
			return notes.Collection{}, err
		}
		query.After = &notes.Cursor{ID: note.ID, Title: note.Title, Pinned: note.Pinned, UpdatedAt: note.UpdatedAt.Format(time.RFC3339Nano)}
	}
	options := collectionOptions(query)
	if options.Limit > notes.MaximumRows+1 {
		return notes.Collection{}, errors.New("notesstore: invalid page limit")
	}
	rows, err := store.handler.ListAll(ctx, options)
	if err != nil {
		return notes.Collection{}, err
	}
	more := len(rows) == options.Limit
	if more {
		rows = rows[:len(rows)-1]
	}
	result := notes.Collection{Notes: make([]notes.Note, 0, len(rows)), Total: len(rows)}
	for _, row := range rows {
		note, err := decode(row)
		if err != nil {
			return notes.Collection{}, err
		}
		result.Notes = append(result.Notes, note)
	}
	if more {
		if len(rows) == notes.MaximumRows {
			last := result.Notes[len(result.Notes)-1]
			if len(last.ID) > 64 {
				return notes.Collection{}, errors.New("notesstore: page boundary identity exceeds 64 bytes")
			}
			query.After = &notes.Cursor{ID: last.ID}
			query.Page = 1
		} else {
			query.Page = max(query.Page, 1) + 1
		}
		result.Next = query.PageValue()
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
	return store.write(ctx, id, values)
}

// SaveDetails stores typed metadata in the existing owner-scoped notes entity.
func (store *Store) SaveDetails(ctx context.Context, id, title, body string, details notes.Details) (notes.Note, error) {
	values := detailValues(details)
	values["title"], values["body"] = title, body
	return store.write(ctx, id, values)
}

// SaveMetadata changes no text fields and never deletes attachments.
func (store *Store) SaveMetadata(ctx context.Context, id string, details notes.Details) (notes.Note, error) {
	return store.write(ctx, id, detailValues(details))
}

// ImportShared keeps the deduplication receipt and text in one database write.
func (store *Store) ImportShared(ctx context.Context, source, title, body string) (notes.Note, bool, error) {
	owner, err := store.Owner(ctx)
	if err != nil {
		return notes.Note{}, false, err
	}
	digest := sha256.Sum256([]byte("notes.shared.v1\x00" + owner + "\x00" + source))
	identifier := hex.EncodeToString(digest[:])
	find := func() (notes.Note, bool, bool, error) {
		rows, err := store.handler.ListAll(ctx, crud.ListOptions{Limit: 1, Filters: []filter.ParsedFilter{
			{Field: "source_id", Op: filter.OpEq, Value: identifier},
		}})
		if err != nil || len(rows) == 0 {
			return notes.Note{}, false, false, err
		}
		note, err := decode(rows[0])
		complete := boolean(rows[0]["source_complete"]) || boolean(rows[0]["sourceComplete"])
		return note, complete, true, err
	}
	if note, complete, found, err := find(); err != nil || found {
		return note, complete, err
	}
	row, err := store.handler.CreateOne(crud.WithServerWrites(ctx), map[string]any{
		"source_id": identifier, "title": title, "body": body,
	})
	if err != nil {
		// A concurrent importer may have won the unique source receipt.
		if note, complete, found, lookupErr := find(); lookupErr == nil && found {
			return note, complete, nil
		}
		return notes.Note{}, false, err
	}
	note, err := decode(row)
	return note, false, err
}

func (store *Store) FinishImport(ctx context.Context, id string) error {
	_, err := store.handler.UpdateOne(crud.WithServerWrites(ctx), id, map[string]any{"source_complete": true})
	return err
}

func detailValues(details notes.Details) map[string]any {
	return map[string]any{
		"pinned":   details.Pinned,
		"archived": details.Archived, "trashed": details.Trashed, "tag": details.Tag,
		"reminder_date": details.ReminderDate, "reminder_time": details.ReminderTime,
	}
}

func (store *Store) write(ctx context.Context, id string, values map[string]any) (notes.Note, error) {
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
	note.Pinned = boolean(row["pinned"])
	note.Archived = boolean(row["archived"])
	note.Trashed = boolean(row["trashed"])
	note.Tag, _ = row["tag"].(string)
	note.ReminderDate, _ = row["reminder_date"].(string)
	note.ReminderTime, _ = row["reminder_time"].(string)
	if note.ReminderDate == "" {
		note.ReminderDate, _ = row["reminderDate"].(string)
	}
	if note.ReminderTime == "" {
		note.ReminderTime, _ = row["reminderTime"].(string)
	}
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

func boolean(value any) bool {
	parsed, _ := strconv.ParseBool(fmt.Sprint(value))
	return parsed
}
