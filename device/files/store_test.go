package files

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreOpaqueHandlesAndPrivateCollections(t *testing.T) {
	ctx, directory := context.Background(), t.TempDir()
	store, err := NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	document := Document{Name: "idea.md", MediaType: "text/markdown", Data: []byte("An idea")}
	item, err := store.Put(ctx, "note-one", document)
	if err != nil {
		t.Fatal(err)
	}
	if item.Name != document.Name || item.Size != 7 || len(item.Handle) != 32 || strings.Contains(item.Handle, "/") {
		t.Fatalf("invalid metadata: %+v", item)
	}
	info, err := os.Stat(filepath.Join(directory, item.Handle+".json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private file permissions: %v %v", info, err)
	}
	store, err = NewStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.Read(ctx, "note-one", item.Handle)
	if err != nil || string(restored.Data) != "An idea" {
		t.Fatalf("read after reopen: %+v %v", restored, err)
	}
	for _, handle := range []string{item.Handle, "../outside", strings.ToUpper(item.Handle)} {
		if _, err := store.Read(ctx, "other-note", handle); !errors.Is(err, ErrNotFound) {
			t.Fatalf("foreign/path handle accepted: %q %v", handle, err)
		}
	}
	if err := store.Delete(ctx, "other-note", item.Handle); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}
	if items, err := store.List(ctx, "other-note"); err != nil || len(items) != 0 {
		t.Fatalf("foreign list: %+v %v", items, err)
	}
	if err := store.Delete(ctx, "note-one", item.Handle); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, "note-one", item.Handle); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted file remained: %v", err)
	}
}

func TestStoreRejectsUnsafeContentAndQuota(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, document := range []Document{
		{Name: "../secret", MediaType: "text/plain"}, {Name: "folder\\secret", MediaType: "text/plain"},
		{Name: "bad\nname", MediaType: "text/plain"}, {Name: "ok.txt", MediaType: "invalid"},
		{Name: "big.txt", MediaType: "text/plain", Data: make([]byte, MaxBytes+1)},
	} {
		if _, err := store.Put(ctx, "note", document); err == nil {
			t.Fatalf("unsafe document accepted: %s", document.Name)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Put(ctx, "note", Document{Name: "no.txt", MediaType: "text/plain"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write: %v", err)
	}
	ctx = context.Background()
	document := Document{Name: "full.bin", MediaType: "application/octet-stream", Data: make([]byte, MaxBytes)}
	for index := 0; index < MaxStoredBytes/MaxBytes; index++ {
		if _, err := store.Put(ctx, "note", document); err != nil {
			t.Fatalf("fill %d: %v", index, err)
		}
	}
	if _, err := store.Put(ctx, "note", Document{Name: "extra.txt", MediaType: "text/plain", Data: []byte("x")}); err == nil {
		t.Fatal("aggregate quota was not enforced")
	}
}

func TestPickerPolicyRevalidatesRendererInput(t *testing.T) {
	encoded, err := json.Marshal(Document{Name: "empty.txt", MediaType: "text/plain"})
	if err != nil || !strings.Contains(string(encoded), `"data":""`) {
		t.Fatalf("empty file bytes must not be null: %s %v", encoded, err)
	}
	pick := Pick{Title: "Import", Extensions: []string{".md"}, MaxBytes: 10}
	if err := pick.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := pick.Accept(Document{Name: "OK.MD", MediaType: "text/markdown", Data: []byte("ok")}); err != nil {
		t.Fatal(err)
	}
	for _, document := range []Document{{Name: "no.bin", MediaType: "text/plain"}, {Name: "big.md", MediaType: "text/plain", Data: make([]byte, 11)}} {
		if err := pick.Accept(document); err == nil {
			t.Fatal("picker filter accepted invalid input")
		}
	}
}
