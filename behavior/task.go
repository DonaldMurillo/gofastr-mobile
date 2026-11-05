package behavior

import "sync"

type TaskKind string
type TaskState string

const (
	TaskRead      TaskKind  = "read"
	TaskMutation  TaskKind  = "mutation"
	TaskIdle      TaskState = "idle"
	TaskLoading   TaskState = "loading"
	TaskSucceeded TaskState = "success"
	TaskFailed    TaskState = "error"
	TaskCancelled TaskState = "cancelled"
	TaskUnknown   TaskState = "unknown"
)

// TaskSnapshot is immutable state for accessible loading/error/retry controls.
type TaskSnapshot struct {
	Generation uint64    `json:"generation"`
	Kind       TaskKind  `json:"kind,omitempty"`
	State      TaskState `json:"state"`
	Message    string    `json:"message,omitempty"`
	CanRetry   bool      `json:"canRetry"`
}

// Task is a deterministic generation gate. Its zero value is idle. Replacing a
// read invalidates old completions; pending/unknown mutations cannot be replaced.
type Task struct {
	mu       sync.Mutex
	snapshot TaskSnapshot
}

// Begin admits an explicit operation, never schedules or retries work itself.
func (task *Task) Begin(kind TaskKind) (uint64, bool) {
	task.mu.Lock()
	defer task.mu.Unlock()
	if kind != TaskRead && kind != TaskMutation || task.snapshot.State == TaskUnknown || task.snapshot.Kind == TaskMutation && task.snapshot.State == TaskLoading || task.snapshot.Generation == ^uint64(0) {
		return task.snapshot.Generation, false
	}
	task.snapshot = TaskSnapshot{Generation: task.snapshot.Generation + 1, Kind: kind, State: TaskLoading}
	return task.snapshot.Generation, true
}

// Complete applies only the current generation once. Cancellation is read-only;
// an unknown mutation stays locked until a separately reconciled Task is used.
func (task *Task) Complete(generation uint64, state TaskState, message string) bool {
	task.mu.Lock()
	defer task.mu.Unlock()
	if generation != task.snapshot.Generation || task.snapshot.State != TaskLoading || len(message) > 512 {
		return false
	}
	if state != TaskSucceeded && state != TaskFailed && !(state == TaskCancelled && task.snapshot.Kind == TaskRead) && !(state == TaskUnknown && task.snapshot.Kind == TaskMutation) {
		return false
	}
	task.snapshot.State, task.snapshot.Message = state, message
	task.snapshot.CanRetry = state == TaskFailed || state == TaskCancelled
	return true
}

// Cancel invalidates a current read without authorizing mutation cancellation.
func (task *Task) Cancel(generation uint64) bool {
	return task.Complete(generation, TaskCancelled, "")
}

// Snapshot returns a copy; callers cannot change the gate by editing UI state.
func (task *Task) Snapshot() TaskSnapshot {
	task.mu.Lock()
	defer task.mu.Unlock()
	snapshot := task.snapshot
	if snapshot.State == "" {
		snapshot.State = TaskIdle
	}
	return snapshot
}
