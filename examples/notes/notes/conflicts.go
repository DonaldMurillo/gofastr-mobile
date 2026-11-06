package notes

import (
	"context"
	"errors"
	"maps"
	"strconv"
)

var ErrChanged = errors.New("saved note changed since this editor opened")

func EditorValues(note Note) map[string]string {
	return map[string]string{"title": note.Title, "body": note.Body, "pinned": strconv.FormatBool(note.Pinned), "tag": note.Tag, "reminder_date": note.ReminderDate, "reminder_time": note.ReminderTime}
}

// SaveDetailsIfUnchanged serializes comparison and write with this application's
// shared Service. Independent processes still need repository-level concurrency
// control; this installation-local guard does not claim cross-process CAS.
func (service *Service) SaveDetailsIfUnchanged(ctx context.Context, id, title, body string, details Details, baseline map[string]string) (Note, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if id != "" {
		current, err := service.Get(ctx, id)
		if err != nil {
			return Note{}, err
		}
		if !maps.Equal(EditorValues(current), baseline) {
			return Note{}, ErrChanged
		}
		// Archive and Trash are not editable form fields. Preserve their latest state.
		details.Archived, details.Trashed = current.Archived, current.Trashed
	}
	return service.save(ctx, id, title, body, &details)
}
