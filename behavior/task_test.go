package behavior

import "testing"

func TestTaskReplacementAndReadCancellation(t *testing.T) {
	var task Task
	if task.Snapshot().State != TaskIdle {
		t.Fatal("zero task is not idle")
	}
	first, admitted := task.Begin(TaskRead)
	if !admitted || first != 1 {
		t.Fatal("first read not admitted")
	}
	second, admitted := task.Begin(TaskRead)
	if !admitted || second != 2 || task.Complete(first, TaskSucceeded, "obsolete") {
		t.Fatal("stale read applied")
	}
	if task.Complete(second, TaskUnknown, "") || !task.Cancel(second) || task.Complete(second, TaskSucceeded, "") {
		t.Fatal("invalid read settlement")
	}
	if !task.Snapshot().CanRetry {
		t.Fatal("cancelled read is not explicitly retryable")
	}
	third, _ := task.Begin(TaskRead)
	if !task.Complete(third, TaskFailed, "offline") || !task.Snapshot().CanRetry {
		t.Fatal("read error did not expose retry")
	}
	last, _ := task.Begin(TaskRead)
	if !task.Complete(last, TaskSucceeded, "") || task.Snapshot().CanRetry {
		t.Fatal("successful read should not auto retry")
	}
}

func TestTaskUnknownMutationCannotReplay(t *testing.T) {
	var task Task
	token, admitted := task.Begin(TaskMutation)
	if !admitted || task.Cancel(token) {
		t.Fatal("mutation is cancellable")
	}
	if _, admitted := task.Begin(TaskRead); admitted {
		t.Fatal("read replaced an active mutation")
	}
	if !task.Complete(token, TaskUnknown, "inspect data") {
		t.Fatal("unknown mutation not retained")
	}
	for _, kind := range []TaskKind{TaskRead, TaskMutation, "invalid"} {
		if _, admitted := task.Begin(kind); admitted {
			t.Fatal("unknown mutation automatically retried")
		}
	}
	if task.Snapshot().CanRetry {
		t.Fatal("unknown outcome exposes blind retry")
	}
}
