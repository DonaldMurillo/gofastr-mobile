package notes

import (
	"context"
	"errors"
	"testing"
	"time"
)

type partialRepository struct {
	recordingRepository
	items  map[string]Note
	writes int
}

func (repository *partialRepository) Owner(context.Context) (string, error) { return "owner", nil }
func (repository *partialRepository) Get(_ context.Context, id string) (Note, error) {
	item, ok := repository.items[id]
	if !ok {
		return Note{}, ErrNotFound
	}
	return item, nil
}
func (repository *partialRepository) SaveMetadata(_ context.Context, id string, details Details) (Note, error) {
	repository.writes++
	if repository.writes == 2 {
		return Note{}, errors.New("storage unavailable")
	}
	item := repository.items[id]
	item.Details = details
	repository.items[id] = item
	return item, nil
}

func TestPartialBulkWriteReportsProgressAndUndoPreservesNewText(t *testing.T) {
	repository := &partialRepository{items: map[string]Note{"one": {ID: "one", Title: "One", Body: "Original"}, "two": {ID: "two", Title: "Two"}}}
	service := New(repository)
	result, err := service.Change(context.Background(), []string{"one", "two"}, Change{Kind: "archive"})
	if err == nil || result.Count != 1 || result.Undo == "" || !repository.items["one"].Archived || repository.items["two"].Archived {
		t.Fatalf("partial result: %+v %v", result, err)
	}
	first := repository.items["one"]
	first.Body = "Edited after bulk action"
	repository.items["one"] = first
	undone, err := service.Undo(context.Background(), result.Undo)
	if err != nil || undone.Count != 1 || repository.items["one"].Archived || repository.items["one"].Body != "Edited after bulk action" {
		t.Fatal("undo lost partial progress or newer text")
	}
}

type metadataRepository struct {
	recordingRepository
	writes int
	failAt int
}

func (repository *metadataRepository) Owner(context.Context) (string, error) { return "owner", nil }

func (repository *metadataRepository) SaveMetadata(_ context.Context, _ string, details Details) (Note, error) {
	repository.writes++
	if repository.writes == repository.failAt {
		return Note{}, errors.New("storage unavailable")
	}
	repository.note.Details = details
	return repository.note, nil
}

func TestUndoIsBoundedExpiresAndRejectsMetadataConflicts(t *testing.T) {
	repository := &metadataRepository{recordingRepository: recordingRepository{note: Note{ID: "note", Title: "Original", Body: "Keep text"}}}
	service := New(repository)
	ctx := context.Background()
	first, err := service.Change(ctx, []string{"note"}, Change{Kind: "pin"})
	if err != nil {
		t.Fatal(err)
	}
	for range MaximumUndo {
		if _, err := service.Change(ctx, []string{"note"}, Change{Kind: "pin"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(service.undo) != MaximumUndo {
		t.Fatalf("unbounded undo receipts: %d", len(service.undo))
	}
	if _, err := service.Undo(ctx, first.Undo); !errors.Is(err, ErrUndoUnavailable) {
		t.Fatal("evicted undo was replayed")
	}
	latest := service.LatestUndo(ctx)
	entry := service.undo[latest]
	entry.expires = time.Now().Add(-time.Second)
	service.undo[latest] = entry
	if _, err := service.Undo(ctx, latest); !errors.Is(err, ErrUndoUnavailable) {
		t.Fatal("expired undo was replayed")
	}
	changed, err := service.Change(ctx, []string{"note"}, Change{Kind: "archive"})
	if err != nil {
		t.Fatal(err)
	}
	repository.note.Tag = "work"
	if _, err := service.Undo(ctx, changed.Undo); !errors.Is(err, ErrUndoUnavailable) {
		t.Fatal("undo overwrote newer metadata")
	}
	if repository.note.Tag != "work" || repository.note.Body != "Keep text" {
		t.Fatal("undo conflict changed note data")
	}
}

func TestFailedMetadataWriteDoesNotMintUndoOrReportSuccess(t *testing.T) {
	repository := &metadataRepository{recordingRepository: recordingRepository{note: Note{ID: "note", Title: "Original"}}, failAt: 1}
	service := New(repository)
	result, err := service.Change(context.Background(), []string{"note"}, Change{Kind: "trash"})
	if err == nil || result.Count != 0 || result.Undo != "" || len(service.undo) != 0 || repository.note.Trashed {
		t.Fatalf("failed write treated as success: %+v, %v", result, err)
	}
}
