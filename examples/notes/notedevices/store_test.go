package notedevices

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
)

func TestForegroundGrantsAreOwnerScopedRevokedAndNeverPersisted(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory, "owner")
	if err != nil {
		t.Fatal(err)
	}
	ctx := handler.SetUser(context.Background(), localapp.ForID("owner"))
	foreign := handler.SetUser(context.Background(), localapp.ForID("foreign"))
	id := strings.Repeat("a", 32)
	epoch, _ := store.Epoch(ctx)
	token, err := store.Grant(ctx, id, epoch, ProtectedNote{Version: 1, Title: "secret", Body: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadLease(foreign, id, token); err == nil {
		t.Fatal("foreign owner obtained a grant")
	}
	store.CloseRoute("/protected/" + id + "/edit/" + token)
	if _, err := store.ReadLease(ctx, id, token); !errors.Is(err, ErrLocked) {
		t.Fatal("closed route retained its grant")
	}
	store.Lock()
	if _, err := store.Grant(ctx, id, epoch, ProtectedNote{}); !errors.Is(err, ErrLocked) {
		t.Fatal("late authentication recreated a grant after locking")
	}
	restarted, err := New(directory, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ReadLease(ctx, id, token); !errors.Is(err, ErrLocked) {
		t.Fatal("foreground grant survived restart")
	}
	if _, err := store.Snapshot(foreign); err == nil {
		t.Fatal("foreign owner read the feature index")
	}
}
