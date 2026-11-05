package behavior_test

import (
	"encoding/json"
	"maps"
	"reflect"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/behavior"
)

func newEditor(t *testing.T, baseline behavior.Values) *behavior.Editor {
	t.Helper()
	editor, err := behavior.NewEditor("screen-1", "/items/new", baseline)
	if err != nil {
		t.Fatal(err)
	}
	return editor
}

func saveResult(submission *behavior.Submission, outcome behavior.Outcome) behavior.Result {
	return behavior.Result{
		InstanceID: submission.InstanceID,
		Token:      submission.Token,
		Revision:   submission.Revision,
		Outcome:    outcome,
	}
}

func TestEditorCheckpointsAreCompleteDetachedSnapshots(t *testing.T) {
	if _, err := behavior.NewEditor("", "", nil); err == nil {
		t.Fatal("empty instance ID was accepted")
	}
	baseline := behavior.Values{"title": "original", "body": "body"}
	editor := newEditor(t, baseline)
	baseline["title"] = "outside mutation"
	initial := editor.Snapshot()
	if initial.InstanceID != "screen-1" || initial.Route != "/items/new" || initial.Revision != 0 || initial.Dirty || initial.Pending {
		t.Fatalf("initial snapshot = %+v", initial)
	}
	initial.Baseline["title"] = "returned mutation"
	initial.Current["title"] = "returned mutation"
	if snapshot := editor.Checkpoint(behavior.Values{"title": "original", "body": "body"}); snapshot.Revision != 0 {
		t.Fatalf("identical checkpoint advanced revision: %+v", snapshot)
	}
	current := behavior.Values{"title": "changed"}
	snapshot := editor.Checkpoint(current)
	current["title"] = "outside mutation"
	if snapshot.Revision != 1 || !snapshot.Dirty || len(snapshot.Current) != 1 || snapshot.Current["title"] != "changed" {
		t.Fatalf("complete checkpoint = %+v", snapshot)
	}
	if editor.Snapshot().Current["title"] != "changed" {
		t.Fatal("input map aliases editor state")
	}
	reverted := editor.Checkpoint(behavior.Values{"title": "original", "body": "body"})
	if reverted.Revision != 2 || reverted.Dirty {
		t.Fatalf("reverted checkpoint = %+v", reverted)
	}
}

func TestBeginFreezesSubmissionAndRejectsDuplicate(t *testing.T) {
	editor := newEditor(t, nil)
	first := editor.Begin(behavior.Values{"title": "submitted"})
	if !first.Admitted || first.Submission.Token != 1 || first.Submission.Revision != 1 || !first.Snapshot.Pending {
		t.Fatalf("first admission = %+v", first)
	}
	first.Submission.Values["title"] = "outside mutation"
	first.Snapshot.Submitted.Values["title"] = "snapshot mutation"
	first.Snapshot.Current["title"] = "current mutation"
	duplicate := editor.Begin(behavior.Values{"title": "newer"})
	if duplicate.Admitted || duplicate.Submission != nil || duplicate.Reason != behavior.ReasonPending {
		t.Fatalf("duplicate admission = %+v", duplicate)
	}
	if duplicate.Snapshot.Revision != 2 || duplicate.Snapshot.Submitted.Values["title"] != "submitted" || duplicate.Snapshot.Submitted.Revision != 1 {
		t.Fatalf("duplicate changed submission: %+v", duplicate.Snapshot)
	}
}

func TestSuccessDuringTypingPreservesDraftAndAdoptsSavedRoute(t *testing.T) {
	editor := newEditor(t, nil)
	begin := editor.Begin(behavior.Values{"title": "submitted", "body": "first"})
	result := saveResult(begin.Submission, behavior.SaveSucceeded)
	result.SavedRoute = "/items/42/edit"
	current := behavior.Values{"title": "newer", "body": "first"}
	finish := editor.Complete(result, current)
	if !finish.Applied || finish.MayLeave || !finish.Snapshot.Dirty || finish.Snapshot.Pending {
		t.Fatalf("completion = %+v", finish)
	}
	if finish.Snapshot.Baseline["title"] != "submitted" || finish.Snapshot.Current["title"] != "newer" || finish.Snapshot.Revision != 2 {
		t.Fatalf("save overwrote newer buffer: %+v", finish.Snapshot)
	}
	if finish.SavedRoute != result.SavedRoute || finish.Snapshot.Route != result.SavedRoute {
		t.Fatalf("saved route was not adopted: %+v", finish)
	}
	if finish.ClearDraft == nil || finish.ClearDraft.Revision != begin.Submission.Revision || finish.ClearDraft.Matches(finish.Snapshot) {
		t.Fatalf("cleanup covers newer draft: %+v", finish.ClearDraft)
	}
	if leave := editor.Leave(current); leave.Disposition != behavior.LeaveConfirm {
		t.Fatalf("dirty successful editor may leave without confirmation: %+v", leave)
	}
	next := editor.Begin(current)
	if !next.Admitted || next.Submission.Route != result.SavedRoute || next.Submission.Token != 2 {
		t.Fatalf("next save would repeat create: %+v", next)
	}
}

func TestSuccessUsesServerValuesWithoutOverwritingNewerEdits(t *testing.T) {
	for _, test := range []struct {
		name         string
		current      behavior.Values
		wantTitle    string
		wantMayLeave bool
	}{
		{name: "unchanged", current: behavior.Values{"title": " padded "}, wantTitle: "padded", wantMayLeave: true},
		{name: "typing", current: behavior.Values{"title": "newer"}, wantTitle: "newer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			editor := newEditor(t, nil)
			begin := editor.Begin(behavior.Values{"title": " padded "})
			result := saveResult(begin.Submission, behavior.SaveSucceeded)
			result.Values = behavior.Values{"title": "padded"}
			finish := editor.Complete(result, test.current)
			result.Values["title"] = "outside mutation"
			if !finish.Applied || finish.MayLeave != test.wantMayLeave || finish.Snapshot.Current["title"] != test.wantTitle || finish.Snapshot.Baseline["title"] != "padded" {
				t.Fatalf("server-confirmed completion = %+v", finish)
			}
			if editor.Snapshot().Baseline["title"] != "padded" {
				t.Fatal("server values alias editor baseline")
			}
		})
	}
}

func TestSuccessWithoutNewerEditsMayLeave(t *testing.T) {
	editor := newEditor(t, behavior.Values{"title": "original"})
	current := behavior.Values{"title": "saved"}
	begin := editor.Begin(current)
	finish := editor.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), current)
	if !finish.Applied || !finish.MayLeave || finish.Snapshot.Dirty || finish.Snapshot.Outcome != behavior.SaveSucceeded {
		t.Fatalf("successful completion = %+v", finish)
	}
	if !maps.Equal(finish.Snapshot.Baseline, current) || !finish.ClearDraft.Matches(finish.Snapshot) {
		t.Fatalf("successful save baseline/cleanup = %+v", finish)
	}
	if editor.Leave(current).Disposition != behavior.LeaveAllow {
		t.Fatal("clean editor cannot leave")
	}
}

func TestCanonicalEmptySuccessCleansOnlyItsAcknowledgedRevision(t *testing.T) {
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "submitted"}
	begin := editor.Begin(current)
	result := saveResult(begin.Submission, behavior.SaveSucceeded)
	result.Values = behavior.Values{}
	finish := editor.Complete(result, current)
	if !finish.Applied || !finish.MayLeave || finish.Snapshot.Dirty || len(finish.Snapshot.Current) != 0 || len(finish.Snapshot.Baseline) != 0 {
		t.Fatalf("empty canonical snapshot = %+v", finish)
	}
	if finish.ClearDraft == nil || !finish.ClearDraft.Matches(finish.Snapshot) {
		t.Fatalf("canonical completion cannot clean its acknowledged snapshot: %+v", finish.ClearDraft)
	}
	newer := editor.Checkpoint(behavior.Values{"title": "newer"})
	if finish.ClearDraft.Matches(newer) {
		t.Fatal("canonical completion cleanup covers later edits")
	}
}

func TestResettingValuesCannotResetUnknownOutcome(t *testing.T) {
	editor := newEditor(t, nil)
	begin := editor.Begin(behavior.Values{"title": "submitted"})
	editor.Complete(saveResult(begin.Submission, behavior.SaveUnknown), begin.Submission.Values)
	baseline := editor.Snapshot().Baseline
	reset := editor.Checkpoint(baseline)
	if reset.Dirty || reset.Outcome != behavior.SaveUnknown {
		t.Fatalf("reset values = %+v", reset)
	}
	if editor.Begin(baseline).Admitted || editor.Leave(baseline).Disposition != behavior.LeaveBlock {
		t.Fatal("discarding text silently authorized an uncertain mutation retry/leave")
	}
}

func TestFailureRetainsValuesAndErrorsUntilTheirFieldChanges(t *testing.T) {
	editor := newEditor(t, behavior.Values{"title": "original", "body": "original"})
	current := behavior.Values{"title": "bad", "body": "bad"}
	begin := editor.Begin(current)
	result := saveResult(begin.Submission, behavior.SaveFailed)
	result.FieldErrors = behavior.FieldErrors{"title": "required", "body": "too long"}
	result.Message = "Validation failed"
	result.SavedRoute = "/must-not-adopt"
	finish := editor.Complete(result, current)
	if !finish.Applied || finish.MayLeave || finish.ClearDraft != nil || finish.Snapshot.Outcome != behavior.SaveFailed || !finish.Snapshot.Dirty {
		t.Fatalf("failed completion = %+v", finish)
	}
	if finish.Snapshot.Message != result.Message || !maps.Equal(finish.Snapshot.FieldErrors, result.FieldErrors) || finish.Snapshot.Route != "/items/new" {
		t.Fatalf("failure details = %+v", finish.Snapshot)
	}
	result.FieldErrors["title"] = "outside mutation"
	finish.Snapshot.FieldErrors["body"] = "returned mutation"
	current["title"] = "fixed"
	checkpoint := editor.Checkpoint(current)
	if _, exists := checkpoint.FieldErrors["title"]; exists || checkpoint.FieldErrors["body"] != "too long" {
		t.Fatalf("editing one field erased another field error: %+v", checkpoint)
	}
	retry := editor.Begin(current)
	if !retry.Admitted || retry.Submission.Token != begin.Submission.Token+1 || retry.Snapshot.FieldErrors["body"] != "too long" {
		t.Fatalf("known failure retry = %+v", retry)
	}
}

func TestLateValidationErrorsDoNotAttachToChangedFields(t *testing.T) {
	editor := newEditor(t, nil)
	begin := editor.Begin(behavior.Values{"title": "bad", "body": "bad", "optional": ""})
	result := saveResult(begin.Submission, behavior.SaveFailed)
	result.FieldErrors = behavior.FieldErrors{"title": "required", "body": "too long", "optional": "bad", "missing": "bad"}
	finish := editor.Complete(result, behavior.Values{"title": "fixed", "body": "bad", "missing": ""})
	if !reflect.DeepEqual(finish.Snapshot.FieldErrors, behavior.FieldErrors{"body": "too long"}) {
		t.Fatalf("stale field errors = %+v", finish.Snapshot.FieldErrors)
	}
	if finish.Snapshot.Current["title"] != "fixed" || finish.Snapshot.Baseline["title"] != "" {
		t.Fatalf("validation replaced current/baseline: %+v", finish.Snapshot)
	}
}

func TestUnknownBlocksRetryAndLeaveUntilAuthoritativeResolution(t *testing.T) {
	for _, outcome := range []behavior.Outcome{behavior.SaveSucceeded, behavior.SaveFailed} {
		t.Run(string(outcome), func(t *testing.T) {
			editor := newEditor(t, nil)
			begin := editor.Begin(behavior.Values{"title": "submitted"})
			unknown := saveResult(begin.Submission, behavior.SaveUnknown)
			unknown.Message = "The response was lost"
			unknown.SavedRoute = "/unverified"
			finish := editor.Complete(unknown, behavior.Values{"title": "newer"})
			if !finish.Applied || finish.MayLeave || finish.ClearDraft != nil || finish.Snapshot.Pending || finish.Snapshot.Outcome != behavior.SaveUnknown {
				t.Fatalf("unknown completion = %+v", finish)
			}
			if finish.Snapshot.Route != "/items/new" || len(finish.Snapshot.Baseline) != 0 || finish.Snapshot.Current["title"] != "newer" {
				t.Fatalf("unknown changed committed state: %+v", finish.Snapshot)
			}
			current := behavior.Values{"title": "still newer"}
			if retry := editor.Begin(current); retry.Admitted || retry.Reason != behavior.ReasonUnknown || retry.Submission != nil {
				t.Fatalf("unknown allowed unsafe retry: %+v", retry)
			}
			if leave := editor.Leave(current); leave.Disposition != behavior.LeaveBlock || leave.Reason != behavior.ReasonUnknown {
				t.Fatalf("unknown allowed automatic leave: %+v", leave)
			}
			if replay := editor.Complete(unknown, current); replay.Applied || replay.Reason != behavior.ReasonStale {
				t.Fatalf("duplicate unknown was accepted: %+v", replay)
			}
			resolved := editor.Complete(saveResult(begin.Submission, outcome), current)
			if !resolved.Applied || resolved.Snapshot.Outcome != outcome || resolved.MayLeave {
				t.Fatalf("authoritative resolution = %+v", resolved)
			}
			if !editor.Begin(current).Admitted {
				t.Fatal("authoritatively resolved outcome still blocks submission")
			}
		})
	}
}

func TestStaleAndInvalidCompletionsCannotMutateState(t *testing.T) {
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "submitted"}
	begin := editor.Begin(current)
	for _, test := range []struct {
		name   string
		change func(*behavior.Result)
		reason behavior.Reason
	}{
		{name: "instance", change: func(result *behavior.Result) { result.InstanceID = "replacement-screen" }, reason: behavior.ReasonStale},
		{name: "token", change: func(result *behavior.Result) { result.Token++ }, reason: behavior.ReasonStale},
		{name: "revision", change: func(result *behavior.Result) { result.Revision++ }, reason: behavior.ReasonStale},
		{name: "outcome", change: func(result *behavior.Result) { result.Outcome = "cancelled" }, reason: behavior.ReasonInvalidOutcome},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := editor.Snapshot()
			result := saveResult(begin.Submission, behavior.SaveSucceeded)
			test.change(&result)
			finish := editor.Complete(result, behavior.Values{"title": "stale buffer"})
			if finish.Applied || finish.MayLeave || finish.ClearDraft != nil || finish.Reason != test.reason || !reflect.DeepEqual(before, editor.Snapshot()) {
				t.Fatalf("rejected result changed state: %+v", finish)
			}
		})
	}
	editor.Complete(saveResult(begin.Submission, behavior.SaveFailed), current)
	before := editor.Snapshot()
	if replay := editor.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), nil); replay.Applied || !reflect.DeepEqual(before, editor.Snapshot()) {
		t.Fatalf("terminal result replay was accepted: %+v", replay)
	}
	newer := editor.Begin(behavior.Values{"title": "retry"})
	before = editor.Snapshot()
	if stale := editor.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), nil); stale.Applied || !reflect.DeepEqual(before, editor.Snapshot()) {
		t.Fatalf("old request overwrote new request: %+v", stale)
	}
	if !editor.Complete(saveResult(newer.Submission, behavior.SaveSucceeded), newer.Submission.Values).Applied {
		t.Fatal("current request was rejected")
	}
	replacement, err := behavior.NewEditor("screen-2", "/items/new", nil)
	if err != nil {
		t.Fatal(err)
	}
	replacement.Begin(current)
	if replacement.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), current).Applied {
		t.Fatal("same-route replacement accepted previous instance completion")
	}
	unsubmitted := newEditor(t, nil)
	if unsubmitted.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), current).Applied {
		t.Fatal("unsubmitted editor accepted completion")
	}
}

func TestLeaveCheckpointsBufferAndBlocksPendingSave(t *testing.T) {
	editor := newEditor(t, behavior.Values{"title": "original"})
	if decision := editor.Leave(behavior.Values{"title": "original"}); decision.Disposition != behavior.LeaveAllow {
		t.Fatalf("clean leave = %+v", decision)
	}
	current := behavior.Values{"title": "newer"}
	if decision := editor.Leave(current); decision.Disposition != behavior.LeaveConfirm || decision.Reason != behavior.ReasonDirty || decision.Snapshot.Revision != 1 {
		t.Fatalf("dirty leave = %+v", decision)
	}
	editor.Begin(current)
	current["title"] = "typing"
	if decision := editor.Leave(current); decision.Disposition != behavior.LeaveBlock || decision.Reason != behavior.ReasonPending || decision.Snapshot.Current["title"] != "typing" {
		t.Fatalf("pending leave = %+v", decision)
	}
}

func TestRestoreKeepsFreshIdentityRouteAndEntityBaseline(t *testing.T) {
	old := newEditor(t, behavior.Values{"title": "old baseline"})
	old.Begin(behavior.Values{"title": "recovered draft"})
	for _, pending := range []bool{true, false} {
		snapshot := old.Snapshot()
		if !pending {
			snapshot.Pending = false
			snapshot.Outcome = behavior.SaveUnknown
		}
		fresh, err := behavior.NewEditor("fresh-screen", "/items/42/edit", behavior.Values{"title": "fresh entity"})
		if err != nil {
			t.Fatal(err)
		}
		if err := fresh.Restore(snapshot); err != nil {
			t.Fatal(err)
		}
		restored := fresh.Snapshot()
		if restored.InstanceID != "fresh-screen" || restored.Route != "/items/42/edit" || restored.Baseline["title"] != "fresh entity" || restored.Current["title"] != "recovered draft" {
			t.Fatalf("restore overwrote fresh editor identity/baseline: %+v", restored)
		}
		if restored.Pending || restored.Submitted != nil || restored.Outcome != behavior.SaveUnknown || restored.DraftToken == snapshot.DraftToken {
			t.Fatalf("restore revived old mutation identity: %+v", restored)
		}
		if fresh.Begin(restored.Current).Admitted || fresh.Leave(restored.Current).Disposition != behavior.LeaveBlock {
			t.Fatal("restored uncertain save allowed retry/automatic leave")
		}
		if fresh.Complete(saveResult(snapshot.Submitted, behavior.SaveSucceeded), restored.Current).Applied {
			t.Fatal("restored screen accepted original screen completion")
		}
		if err := fresh.Restore(snapshot); err == nil {
			t.Fatal("restore reset an already restored editor")
		}
	}
	known := newEditor(t, nil).Checkpoint(behavior.Values{"title": "known draft"})
	fresh := newEditor(t, behavior.Values{"title": "saved entity"})
	if err := fresh.Restore(known); err != nil || !fresh.Begin(known.Current).Admitted {
		t.Fatalf("known draft cannot be resumed: %v", err)
	}
	if err := newEditor(t, nil).Restore(behavior.Snapshot{}); err == nil {
		t.Fatal("invalid restore was accepted")
	}
}

func TestConcurrentBeginAdmitsOnlyOneMutation(t *testing.T) {
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "submitted"}
	decisions := make(chan behavior.BeginDecision, 32)
	var workers sync.WaitGroup
	for range cap(decisions) {
		workers.Go(func() { decisions <- editor.Begin(current) })
	}
	workers.Wait()
	close(decisions)
	var admitted []*behavior.Submission
	for decision := range decisions {
		if decision.Admitted {
			admitted = append(admitted, decision.Submission)
		} else if decision.Reason != behavior.ReasonPending {
			t.Fatalf("concurrent denial = %+v", decision)
		}
	}
	if len(admitted) != 1 {
		t.Fatalf("admitted %d concurrent mutations", len(admitted))
	}
	for range 16 {
		workers.Go(func() {
			editor.Checkpoint(current)
			editor.Snapshot()
			editor.Leave(current)
			editor.Complete(saveResult(admitted[0], behavior.SaveSucceeded), current)
		})
	}
	workers.Wait()
	if snapshot := editor.Snapshot(); snapshot.Pending || snapshot.Outcome != behavior.SaveSucceeded || snapshot.Dirty {
		t.Fatalf("concurrent final state = %+v", snapshot)
	}
}

func TestDecisionsAndSnapshotsRoundTripJSON(t *testing.T) {
	editor := newEditor(t, nil)
	current := behavior.Values{"title": "saved"}
	begin := editor.Begin(current)
	assertJSONRoundTrip(t, begin)
	assertJSONRoundTrip(t, begin.Snapshot)
	finish := editor.Complete(saveResult(begin.Submission, behavior.SaveSucceeded), current)
	assertJSONRoundTrip(t, finish)
	assertJSONRoundTrip(t, editor.Leave(current))
	result := behavior.Result{Values: behavior.Values{}}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded behavior.Result
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Values == nil {
		t.Fatalf("empty confirmed snapshot became absent: %s, %v", encoded, err)
	}
}

func assertJSONRoundTrip[Value any](t *testing.T, value Value) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Value
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value, decoded) {
		t.Fatalf("JSON roundtrip mismatch: %s", encoded)
	}
}
