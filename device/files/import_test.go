package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestImportIdentitySurvivesRestartAndFullQuota(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	document := Document{Name: "brief.txt", MediaType: "text/plain", Data: []byte("original")}
	first, err := store.PutOnce(ctx, "note-1", "share-1.file-0", document)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < MaxStoredFiles; i++ {
		if _, err := store.Put(ctx, fmt.Sprint(i), document); err != nil {
			t.Fatal(err)
		}
	}
	store, err = NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.PutOnce(ctx, "note-1", "share-1.file-0", document)
	if err != nil || again.Handle != first.Handle {
		t.Fatalf("repeated import used quota or changed identity: %v", err)
	}
	document.Data = []byte("different")
	if _, err := store.PutOnce(ctx, "note-1", "share-1.file-0", document); err == nil {
		t.Fatal("import identity overwrote its original bytes")
	}
	if _, err := store.Read(ctx, "another-note", first.Handle); err == nil {
		t.Fatal("another collection read the imported bytes")
	}
}

func TestImportResumesAfterInterruptedTemporaryWrite(t *testing.T) {
	directory := t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	document := Document{Name: "brief.txt", MediaType: "text/plain", Data: []byte("complete content")}
	first, err := store.PutOnce(context.Background(), "note", "share", document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, first.Handle+".json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, first.Handle+".tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.PutOnce(context.Background(), "note", "share", document)
	if err != nil || again.Handle != first.Handle {
		t.Fatalf("interrupted import could not resume: %v", err)
	}
	read, err := store.Read(context.Background(), "note", again.Handle)
	if err != nil || string(read.Data) != string(document.Data) {
		t.Fatalf("resumed bytes differ: %v", err)
	}
}
