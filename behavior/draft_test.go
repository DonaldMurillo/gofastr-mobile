package behavior_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/behavior"
)

type failingDraftStore struct {
	err error
}

func (store failingDraftStore) Load(context.Context, string) (behavior.Snapshot, bool, error) {
	return behavior.Snapshot{}, false, store.err
}

func (store failingDraftStore) Put(context.Context, string, behavior.Snapshot) error {
	return store.err
}

func (store failingDraftStore) ClearIfRevision(context.Context, string, behavior.DraftRevision) (bool, error) {
	return false, store.err
}

func TestDraftRevisionIncludesSessionAndSaveAttempt(t *testing.T) {
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "draft"}
	checkpoint := editor.Checkpoint(current)
	if !checkpoint.DraftRevision().Matches(checkpoint) {
		t.Fatal("checkpoint does not match its own revision")
	}
	begin := editor.Begin(current)
	if checkpoint.DraftRevision().Matches(begin.Snapshot) || checkpoint.Revision != begin.Snapshot.Revision {
		t.Fatal("new save attempt must change draft identity without inventing a field edit")
	}
	finish := editor.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), current)
	if finish.ClearDraft == nil || !finish.ClearDraft.Matches(begin.Snapshot) || !finish.ClearDraft.Matches(finish.Snapshot) {
		t.Fatal("successful save cleanup does not cover submitted/completed snapshots")
	}
	next := editor.Begin(current)
	if finish.ClearDraft.Matches(next.Snapshot) {
		t.Fatal("old cleanup covers a newer pending save at the same field revision")
	}
	reopened := newEditor(t, nil).Checkpoint(current)
	if checkpoint.DraftRevision().Matches(reopened) || checkpoint.DraftToken == reopened.DraftToken {
		t.Fatal("reopening reused a draft token, even with an accidentally reused instance ID")
	}
	if (behavior.DraftRevision{}).Matches(behavior.Snapshot{}) {
		t.Fatal("zero revision matched an unidentified draft")
	}
}

func TestDraftStoreFailureCannotAcknowledgeKeepDraftOrUndoCommittedSave(t *testing.T) {
	var store behavior.DraftStore = failingDraftStore{err: errors.New("disk full")}
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "draft"}
	leave := editor.Leave(current)
	if err := store.Put(context.Background(), "key", leave.Snapshot); err == nil {
		t.Fatal("checkpoint unexpectedly succeeded")
	}
	if snapshot := editor.Snapshot(); !snapshot.Dirty || !reflect.DeepEqual(snapshot, leave.Snapshot) {
		t.Fatalf("failed checkpoint changed policy: %+v", snapshot)
	}
	begin := editor.Begin(current)
	finish := editor.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), current)
	if cleared, err := store.ClearIfRevision(context.Background(), "key", *finish.ClearDraft); cleared || err == nil {
		t.Fatalf("cleanup unexpectedly succeeded: %v, %v", cleared, err)
	}
	if snapshot := editor.Snapshot(); snapshot.Outcome != behavior.SaveSucceeded || snapshot.Dirty {
		t.Fatalf("cleanup failure undid committed save: %+v", snapshot)
	}
}
