package notesbackup_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp/application"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesbackup"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

type fixture struct {
	service *notes.Service
	files   *files.Store
	manager *notesbackup.Manager
	ctx     context.Context
	root    string
	host    *mobilehost.Host
}

func open(t *testing.T, root string) *fixture {
	t.Helper()
	shared, err := application.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.App.Shutdown(context.Background()); owner.SetExtractor(nil) })
	host, err := mobilehost.New(shared.App, mobilehost.Config{Data: shared.Data, ServiceOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	attachments, err := files.NewStore(filepath.Join(root, "files"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := notesbackup.New(filepath.Join(root, "backups"), shared.Service, attachments)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{shared.Service, attachments, manager, handler.SetUser(context.Background(), shared.Data.User), root, host}
}

func document(t *testing.T, records ...notesbackup.Record) files.Document {
	t.Helper()
	result, err := notesbackup.Encode(notesbackup.Bundle{Format: "gofastr-notes", Version: 1, Notes: records})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBackupRoundTripPreservesMetadataBytesAndSkipsOriginals(t *testing.T) {
	source := open(t, t.TempDir())
	for index, details := range []notes.Details{{Tag: "work", Pinned: true, ReminderDate: "2028-02-29", ReminderTime: "09:45"}, {Archived: true}, {Trashed: true}} {
		note, err := source.service.SaveDetails(source.ctx, "", fmt.Sprintf("Backup %d", index), "Exact Unicode text 😀", details)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = source.files.Put(source.ctx, note.ID, files.Document{Name: "brief.txt", MediaType: "text/plain", Data: []byte("Exact bytes\x00\xff")}); err != nil {
			t.Fatal(err)
		}
	}
	exported, err := source.manager.Export(source.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := notesbackup.Decode(exported.Data)
	if err != nil || len(bundle.Notes) != 3 {
		t.Fatalf("export: %v", err)
	}
	// Restoring into the original installation must not duplicate untouched notes.
	token, err := source.manager.Stage(source.ctx, exported)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := source.manager.Preview(source.ctx, token)
	if err != nil || preview.Existing != 3 || preview.New != 0 {
		t.Fatalf("original preview: %+v %v", preview, err)
	}
	target := open(t, t.TempDir())
	token, err = target.manager.Stage(target.ctx, exported)
	if err != nil {
		t.Fatal(err)
	}
	preview, err = target.manager.Preview(target.ctx, token)
	if err != nil || preview.New != 3 || preview.Files != 3 {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	before, _ := target.service.BackupNotes(target.ctx)
	if len(before) != 0 {
		t.Fatal("preview wrote notes")
	}
	restored, err := target.manager.Restore(target.ctx, token)
	if err != nil || restored.Imported != 3 {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	for _, record := range bundle.Notes {
		note, complete, err := target.service.FindBackupImport(target.ctx, notesbackup.Source(record))
		if err != nil || !complete || note.Details != record.Note.Details || note.Body != record.Note.Body {
			t.Fatalf("restored content: %+v %v", note, err)
		}
		attachments, _ := target.files.List(target.ctx, note.ID)
		if len(attachments) != 1 {
			t.Fatal("missing attachment")
		}
		read, err := target.files.Read(target.ctx, note.ID, attachments[0].Handle)
		if err != nil || !bytes.Equal(read.Data, record.Files[0].Data) {
			t.Fatal("file bytes changed")
		}
	}
}

func TestRestoreResumesAfterFullStorageAndRestartWithoutResurrectingRemovedFiles(t *testing.T) {
	root := t.TempDir()
	target := open(t, root)
	var occupied files.File
	for index := range files.MaxStoredFiles {
		var err error
		occupied, err = target.files.Put(target.ctx, "unrelated", files.Document{Name: fmt.Sprintf("%d.txt", index), MediaType: "text/plain", Data: []byte("full")})
		if err != nil {
			t.Fatal(err)
		}
	}
	record := notesbackup.Record{Note: notes.Note{ID: "source", Title: "Original title", Body: "Original body", Details: notes.Details{Archived: true, Tag: "ideas"}}, Files: []files.Document{{Name: "brief.txt", MediaType: "text/plain", Data: []byte("imported")}}}
	token, err := target.manager.Stage(target.ctx, document(t, record))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = target.manager.Restore(target.ctx, token); err == nil {
		t.Fatal("full storage reported success")
	}
	first, complete, err := target.service.FindBackupImport(target.ctx, notesbackup.Source(record))
	if err != nil || complete || first.ID == "" {
		t.Fatal("partial note has no durable receipt")
	}
	if err := target.files.Delete(target.ctx, "unrelated", occupied.Handle); err != nil {
		t.Fatal(err)
	}
	if err := target.host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	target = open(t, root)
	restored, err := target.manager.Restore(target.ctx, token)
	if err != nil || restored.Imported != 1 {
		t.Fatalf("resume: %+v %v", restored, err)
	}
	again, complete, err := target.service.FindBackupImport(target.ctx, notesbackup.Source(record))
	if err != nil || !complete || again.ID != first.ID {
		t.Fatal("resume duplicated the note")
	}
	attachments, _ := target.files.List(target.ctx, again.ID)
	if len(attachments) != 1 {
		t.Fatal("resume duplicated files")
	}
	if _, err = target.service.Save(target.ctx, again.ID, "Later edit", "Keep newer text"); err != nil {
		t.Fatal(err)
	}
	if err = target.files.Delete(target.ctx, again.ID, attachments[0].Handle); err != nil {
		t.Fatal(err)
	}
	result, err := target.manager.Restore(target.ctx, token)
	if err != nil || result.Skipped != 1 {
		t.Fatalf("repeat: %+v %v", result, err)
	}
	again, _ = target.service.Get(target.ctx, again.ID)
	attachments, _ = target.files.List(target.ctx, again.ID)
	if again.Title != "Later edit" || len(attachments) != 0 {
		t.Fatal("repeat overwrote later edits or resurrected a removed attachment")
	}
}

func TestRestoreConcurrentCallsOwnerScopeAndCancellation(t *testing.T) {
	target := open(t, t.TempDir())
	token, err := target.manager.Stage(target.ctx, document(t, notesbackup.Record{Note: notes.Note{ID: "one", Title: "Concurrent", Body: "Keep"}}))
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wait.Add(1)
		go func() { defer wait.Done(); _, err := target.manager.Restore(target.ctx, token); failures <- err }()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := target.service.BackupNotes(target.ctx)
	if err != nil || len(items) != 1 {
		t.Fatal("concurrent restore duplicated notes")
	}
	foreignUser, err := localapp.LoadOrMint(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	foreign := handler.SetUser(context.Background(), foreignUser)
	if _, err = target.manager.Preview(foreign, token); err == nil {
		t.Fatal("another owner read backup review")
	}
	cancelled, cancel := context.WithCancel(target.ctx)
	cancel()
	if _, err = target.manager.Restore(cancelled, token); err == nil {
		t.Fatal("cancelled restore continued")
	}
	if err = target.manager.Discard(target.ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = target.manager.Preview(target.ctx, token); err == nil {
		t.Fatal("discarded review remained available")
	}
}

func TestCorruptOversizedAndAmbiguousBackupsAreRejected(t *testing.T) {
	valid := document(t, notesbackup.Record{Note: notes.Note{ID: "one", Title: "Valid"}})
	for _, data := range [][]byte{[]byte("not a backup"), valid.Data[:len(valid.Data)-1], append(append([]byte{}, valid.Data...), 0), append(append([]byte{}, valid.Data...), valid.Data...)} {
		if _, err := notesbackup.Decode(data); err == nil {
			t.Fatal("invalid archive accepted")
		}
	}
	var bomb bytes.Buffer
	z := gzip.NewWriter(&bomb)
	_, _ = z.Write(bytes.Repeat([]byte("x"), notesbackup.MaxDecoded+1))
	_ = z.Close()
	if _, err := notesbackup.Decode(bomb.Bytes()); err == nil {
		t.Fatal("decompression limit was ignored")
	}
	for _, bundle := range []notesbackup.Bundle{
		{Format: "gofastr-notes", Version: 99},
		{Format: "gofastr-notes", Version: 1, Notes: []notesbackup.Record{{Note: notes.Note{ID: "../outside", Title: "Path"}}}},
		{Format: "gofastr-notes", Version: 1, Notes: []notesbackup.Record{{Note: notes.Note{ID: "one", Title: "A"}}, {Note: notes.Note{ID: "one", Title: "B"}}}},
	} {
		if _, err := notesbackup.Encode(bundle); err == nil {
			t.Fatal("invalid bundle accepted")
		}
	}
	random := make([]byte, files.MaxBytes)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	_, err := notesbackup.Encode(notesbackup.Bundle{Format: "gofastr-notes", Version: 1, Notes: []notesbackup.Record{{Note: notes.Note{ID: "large", Title: "Large"}, Files: []files.Document{{Name: "large.bin", MediaType: "application/octet-stream", Data: random}}}}})
	if err == nil || !strings.Contains(err.Error(), "1 MiB") {
		t.Fatal("oversized export silently omitted bytes")
	}
}
