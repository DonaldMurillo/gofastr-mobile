package behavior_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/behavior"
)

func newFileStore(t *testing.T) (*behavior.FileDraftStore, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "drafts")
	store, err := behavior.NewFileDraftStore(root)
	if err != nil {
		t.Fatal(err)
	}
	return store, root
}

func draftPath(root, key string) string {
	digest := sha256.Sum256([]byte(key))
	return filepath.Join(root, hex.EncodeToString(digest[:])+".json")
}

func TestFileDraftStorePersistsOwnerOnlyHashedAtomicFiles(t *testing.T) {
	ctx := context.Background()
	store, root := newFileStore(t)
	var seam behavior.DraftStore = store
	key := "../../installation/account/item/private-draft"
	snapshot := newEditor(t, nil).Checkpoint(behavior.Values{"title": "saved draft 🌎", "body": "first\nsecond"})
	if err := seam.Put(ctx, key, snapshot); err != nil {
		t.Fatal(err)
	}
	for path, permissions := range map[string]os.FileMode{root: 0o700, draftPath(root, key): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != permissions {
			t.Fatalf("permissions for %s = %v, %v", path, info, err)
		}
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 || files[0].Name() != filepath.Base(draftPath(root, key)) {
		t.Fatalf("unexpected files/leftover temporary writes: %v, %v", files, err)
	}
	snapshot.Current["title"] = "input mutation"
	reopened, err := behavior.NewFileDraftStore(root)
	if err != nil {
		t.Fatal(err)
	}
	recovered, exists, err := reopened.Load(ctx, key)
	if err != nil || !exists || recovered.Current["title"] != "saved draft 🌎" {
		t.Fatalf("reopened draft = %+v, %v, %v", recovered, exists, err)
	}
	recovered.Current["title"] = "read mutation"
	again, _, err := reopened.Load(ctx, key)
	if err != nil || again.Current["title"] != "saved draft 🌎" {
		t.Fatalf("read aliases stored draft: %+v, %v", again, err)
	}
	if _, exists, err := seam.Load(ctx, "not-written"); exists || err != nil {
		t.Fatalf("missing draft = %v, %v", exists, err)
	}
}

func TestFileDraftConditionalClearProtectsReopenedScreensAtSameSemanticKey(t *testing.T) {
	ctx := context.Background()
	store, root := newFileStore(t)
	key := "installation/account/item-42"
	old := newEditor(t, nil).Checkpoint(behavior.Values{"title": "old screen"})
	if err := store.Put(ctx, key, old); err != nil {
		t.Fatal(err)
	}
	reopened, err := behavior.NewFileDraftStore(root)
	if err != nil {
		t.Fatal(err)
	}
	newScreen, err := behavior.NewEditor("screen-2", "/items/42/edit", nil)
	if err != nil {
		t.Fatal(err)
	}
	newer := newScreen.Checkpoint(behavior.Values{"title": "new screen, same local revision"})
	if newer.Revision != old.Revision {
		t.Fatal("test must exercise colliding screen-local revisions")
	}
	if err := reopened.Put(ctx, key, newer); err != nil {
		t.Fatal(err)
	}
	if cleared, err := store.ClearIfRevision(ctx, key, old.DraftRevision()); cleared || err != nil {
		t.Fatalf("old screen cleared reopened draft: %v, %v", cleared, err)
	}
	if cleared, err := store.ClearIfRevision(ctx, "other-key", newer.DraftRevision()); cleared || err != nil {
		t.Fatalf("unrelated key cleanup = %v, %v", cleared, err)
	}
	recovered, exists, err := store.Load(ctx, key)
	if err != nil || !exists || !reflect.DeepEqual(recovered, newer) {
		t.Fatalf("newer draft was lost: %+v, %v, %v", recovered, exists, err)
	}
	if cleared, err := store.ClearIfRevision(ctx, key, newer.DraftRevision()); !cleared || err != nil {
		t.Fatalf("matching clear = %v, %v", cleared, err)
	}
	if cleared, err := store.ClearIfRevision(ctx, key, newer.DraftRevision()); cleared || err != nil {
		t.Fatalf("replayed clear = %v, %v", cleared, err)
	}
}

func TestFileDraftConcurrentOldClearCannotDeleteNewCheckpoint(t *testing.T) {
	ctx := context.Background()
	store, root := newFileStore(t)
	other, err := behavior.NewFileDraftStore(root)
	if err != nil {
		t.Fatal(err)
	}
	editor := newEditor(t, nil)
	old := editor.Checkpoint(behavior.Values{"title": "old"})
	if err := store.Put(ctx, "key", old); err != nil {
		t.Fatal(err)
	}
	newer := editor.Checkpoint(behavior.Values{"title": "newer"})
	results := make(chan error, 24)
	var workers sync.WaitGroup
	workers.Go(func() { results <- other.Put(ctx, "key", newer) })
	for range cap(results) - 1 {
		workers.Go(func() {
			_, err := store.ClearIfRevision(ctx, "key", old.DraftRevision())
			results <- err
		})
	}
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	latest, exists, err := store.Load(ctx, "key")
	if err != nil || !exists || !reflect.DeepEqual(latest, newer) {
		t.Fatalf("old clear raced away new draft: %+v, %v, %v", latest, exists, err)
	}
	if err := store.Put(ctx, "key", old); !errors.Is(err, behavior.ErrDraftConflict) {
		t.Fatalf("same-session revision regression = %v", err)
	}
}

func TestFileDraftCleanupCannotDeleteNewerPendingSaveAtSameFieldRevision(t *testing.T) {
	ctx := context.Background()
	store, _ := newFileStore(t)
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "same fields"}
	first := editor.Begin(current)
	finish := editor.Complete(saveResult(first.Submission, behavior.SaveSucceeded), current)
	if err := store.Put(ctx, "key", finish.Snapshot); err != nil {
		t.Fatal(err)
	}
	second := editor.Begin(current)
	if err := store.Put(ctx, "key", second.Snapshot); err != nil {
		t.Fatal(err)
	}
	if cleared, err := store.ClearIfRevision(ctx, "key", *finish.ClearDraft); cleared || err != nil {
		t.Fatalf("old success cleared a new pending action: %v, %v", cleared, err)
	}
	if err := store.Put(ctx, "key", finish.Snapshot); !errors.Is(err, behavior.ErrDraftConflict) {
		t.Fatalf("late earlier save checkpoint overwrote pending state: %v", err)
	}
}

func TestFileDraftBoundsAndCancellationKeepExistingCheckpoint(t *testing.T) {
	ctx := context.Background()
	store, root := newFileStore(t)
	snapshot := newEditor(t, nil).Checkpoint(behavior.Values{"title": "safe"})
	if err := store.Put(ctx, "key", snapshot); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", strings.Repeat("x", behavior.MaxDraftKeyBytes+1), string([]byte{0xff})} {
		if err := store.Put(ctx, key, snapshot); err == nil {
			t.Fatal("invalid key was accepted")
		}
	}
	for _, values := range []behavior.Values{
		{"title": strings.Repeat("x", behavior.MaxDraftBytes+1)},
		{"title": strings.Repeat("\x00", behavior.MaxDraftBytes/2)},
		tooManyFields(),
	} {
		oversized := snapshot
		oversized.Current = values
		if err := store.Put(ctx, "key", oversized); !errors.Is(err, behavior.ErrDraftTooLarge) {
			t.Fatalf("oversized input error = %v", err)
		}
	}
	invalid := snapshot
	invalid.Current = behavior.Values{"title": string([]byte{0xff})}
	if err := store.Put(ctx, "key", invalid); err == nil {
		t.Fatal("invalid UTF-8 would silently change field contents")
	}
	if err := store.Put(ctx, "key", behavior.Snapshot{}); err == nil {
		t.Fatal("unidentified draft was accepted")
	}
	if cleared, err := store.ClearIfRevision(ctx, "key", behavior.DraftRevision{}); cleared || err == nil {
		t.Fatal("cleanup without a full revision was accepted")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := store.Load(cancelled, "key"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read = %v", err)
	}
	if err := store.Put(cancelled, "key", snapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write = %v", err)
	}
	if cleared, err := store.ClearIfRevision(cancelled, "key", snapshot.DraftRevision()); cleared || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled clear = %v, %v", cleared, err)
	}
	latest, exists, err := store.Load(ctx, "key")
	if err != nil || !exists || !reflect.DeepEqual(latest, snapshot) {
		t.Fatalf("rejected operation changed persisted draft: %+v, %v, %v", latest, exists, err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("rejected write left temporary files: %v, %v", files, err)
	}
}

func tooManyFields() behavior.Values {
	values := make(behavior.Values, behavior.MaxDraftFields+1)
	for index := range behavior.MaxDraftFields + 1 {
		values[string(rune(index))] = "value"
	}
	return values
}

func TestFileDraftRejectsCorruptOversizedAndUnsafeFiles(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{name: "invalid JSON", edit: func([]byte) []byte { return []byte("{") }},
		{name: "trailing JSON", edit: func(encoded []byte) []byte { return append(encoded, []byte(" {}")...) }},
		{name: "unknown format", edit: func(encoded []byte) []byte {
			return []byte(strings.Replace(string(encoded), `"format":1`, `"format":2`, 1))
		}},
		{name: "JSON-owned path", edit: func(encoded []byte) []byte { return append([]byte(`{"path":"../../victim",`), encoded[1:]...) }},
		{name: "oversized file", edit: func([]byte) []byte { return []byte(strings.Repeat("x", behavior.MaxDraftBytes+1)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root := newFileStore(t)
			snapshot := newEditor(t, nil).Snapshot()
			if err := store.Put(ctx, "key", snapshot); err != nil {
				t.Fatal(err)
			}
			path := draftPath(root, "key")
			encoded, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, test.edit(encoded), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := store.Load(ctx, "key"); err == nil {
				t.Fatal("invalid file was read")
			}
			if err := store.Put(ctx, "key", snapshot); err == nil {
				t.Fatal("invalid file was blindly overwritten")
			}
			if cleared, err := store.ClearIfRevision(ctx, "key", snapshot.DraftRevision()); cleared || err == nil {
				t.Fatal("invalid file was blindly removed")
			}
		})
	}
	store, root := newFileStore(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, draftPath(root, "key")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(ctx, "key"); err == nil {
		t.Fatal("draft symlink was followed")
	}
	if err := store.Put(ctx, "key", newEditor(t, nil).Snapshot()); err == nil {
		t.Fatal("symlink was blindly replaced")
	}
	encoded, err := os.ReadFile(victim)
	if err != nil || string(encoded) != "untouched" {
		t.Fatalf("outside file changed: %q, %v", encoded, err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := behavior.NewFileDraftStore(alias); err == nil {
		t.Fatal("symlink store root was accepted")
	}
	if _, err := behavior.NewFileDraftStore(""); err == nil {
		t.Fatal("empty store root was accepted")
	}
}

func TestFileDraftKeepsUnknownSubmissionMetadata(t *testing.T) {
	ctx := context.Background()
	store, _ := newFileStore(t)
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "submitted"}
	begin := editor.Begin(current)
	finish := editor.Complete(saveResult(begin.Submission, behavior.SaveUnknown), current)
	if err := store.Put(ctx, "key", finish.Snapshot); err != nil {
		t.Fatal(err)
	}
	recovered, exists, err := store.Load(ctx, "key")
	if err != nil || !exists || !reflect.DeepEqual(finish.Snapshot, recovered) || recovered.Outcome != behavior.SaveUnknown {
		t.Fatalf("unknown metadata was lost: %+v, %v, %v", recovered, exists, err)
	}
}

func TestFileDraftTightensDirectoryAndRejectsSharedFiles(t *testing.T) {
	ctx := context.Background()
	store, root := newFileStore(t)
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := behavior.NewFileDraftStore(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("existing directory was not tightened: %v, %v", info, err)
	}
	snapshot := newEditor(t, nil).Snapshot()
	if err := store.Put(ctx, "key", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(draftPath(root, "key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Load(ctx, "key"); err == nil {
		t.Fatal("group/world-readable draft was accepted")
	}
	if cleared, err := store.ClearIfRevision(ctx, "key", snapshot.DraftRevision()); cleared || err == nil {
		t.Fatal("unsafe file was blindly cleared")
	}
}
