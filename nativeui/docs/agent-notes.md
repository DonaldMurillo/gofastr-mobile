# Native runtime reliability notes

## 2026-11-05 - Draft acknowledgement is an action and leave barrier
- Scope: shared-editor, nativeui, persistence, restart.
- Trigger: failed pending checkpoints dispatched mutations; unknown `leave keep` escaped; failed discard cleanup erased the live buffer; stale completions emitted effects.
- Approach: acknowledge the full pending/current snapshot before dispatch, report `draft` errors without executing actions, and allow neither leave nor effects after persistence failure. A failed admission is a known unexecuted save, not an unknown business outcome.
- Cleanup: retain the live buffer until conditional discard succeeds; checkpoint completed clean state before cleanup, and ignore clean residue when opening against fresh repository data. Treat a failed conditional match as a conflict unless readback proves absence.
- Identity: checkpoint the canonical draft key before clearing the create key; preserve the old pending record on either failure. Check both semantic owners during interrupted migration, including discard/close.
- Evidence: `nativeui/runtime_reliability_test.go` uses external `CommandJSON` calls, actual file storage, lost acknowledgements, cleanup conflicts, active writes, and fresh runtime instances; `examples/notes/notesnative/reliability_test.go` covers actual `Stop`/`Start` and an `ENOTDIR` storage fault through the gomobile binding.
- Next time: run `GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test -race ./nativeui ./behavior ./examples/notes/notesnative`, repeat with `GOWORK=off`, and keep protocol/process-lifecycle evidence distinct from abrupt process-kill or renderer/device acceptance.
