# Notes implementation notes

## 2026-11-05 - Bounded collection cursors and service persistence
- Scope: Notes mobile services and repository paging.
- Trigger: fixed 200-row list snapshots hid older notes and service outcomes risked conflating drafts with saved data.
- Approach: use ≤128-byte query-bound keyset IDs, resolve the anchor through the owner scope, fetch ≤101 projected rows, and persist audio duration with bytes in one private versioned envelope.
- Evidence: `notesnative/live_test.go`, `notesnative/services_test.go`; full contract/action IDs in [mobile-services.md](mobile-services.md).
- Next time: forward observed Paging.Next and service IDs verbatim; format decoded timestamps as RFC3339 before building typed repository filters, and never treat metadata or a share-sheet completion as OS delivery confirmation.
- Status: active.
