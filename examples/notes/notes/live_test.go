package notes

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestChangeHubEpochFailureCancellationAndOwnerBound(t *testing.T) {
	repository := &metadataRepository{recordingRepository: recordingRepository{note: Note{ID: "note", Title: "Title"}}}
	service := New(repository)
	ctx := context.Background()
	initial, err := service.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repository.err = errors.New("write rejected")
	if _, err := service.Save(ctx, "note", "Title", "Body"); err == nil {
		t.Fatal("fixture write did not fail")
	}
	unchanged, _ := service.Version(ctx)
	if unchanged != initial {
		t.Fatal("failed write bumped version")
	}
	repository.err = nil
	if _, err := service.Save(ctx, "note", "Title", "Body"); err != nil {
		t.Fatal(err)
	}
	updated, err := service.Watch(ctx, initial)
	if err != nil || updated == initial {
		t.Fatalf("confirmed write not observable: %q %v", updated, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.Watch(cancelled, updated); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	deadline, stop := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer stop()
	if _, err := service.Watch(deadline, updated); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline lost: %v", err)
	}
	restarted, _ := New(repository).Version(ctx)
	if restarted == initial || restarted == updated {
		t.Fatal("process-local epoch was reused")
	}
	var hub changeHub
	for index := range 256 {
		hub.state(fmt.Sprintf("owner-%d", index))
	}
	if len(hub.owners) != 128 {
		t.Fatalf("unbounded owner hub: %d", len(hub.owners))
	}
}
