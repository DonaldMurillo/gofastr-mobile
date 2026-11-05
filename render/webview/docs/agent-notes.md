# WebView implementation notes

## 2026-11-05 - Compiled file adapters use the shared foreground lifecycle
- Scope: extension assets and file acquisition.
- Approach: library init registers content-addressed assets through `extension/webassets`; app-specific config loads selected modules before hello. Never register new behaviors after a host seals its manifest.
- Evidence: legacy/shared host coexistence tests and Notes photo E2E, plus a non-photo adapter in `files.test.cjs`.
- Next time: use the `prepare`/job/normalize seam in [compiled extensions](../../../docs/compiled-extensions.md); keep result submission, recovery and focus ownership in `files.js`.
- Status: active; no app-authored JavaScript or new navigator.

Service/live integration and stable parent acceptance selectors:
[`services-live.md`](services-live.md).

## 2026-11-05 - Open swipe menus need stationary hit targets
- Scope: WebView row gestures and action disclosures.
- Trigger: snap-back moved the summary from under a real pointer between down/up; the next gesture then started at an offscreen transformed coordinate.
- Approach: disable the content transition while its row menu is open, retaining normal closed-menu snap-back and reduced-motion behavior.
- Evidence: the computed-transform assertion in `sim/components_test.go` failed in both styles before the fix; 30 repetitions per style then passed with real CDP taps/swipes.
- Next time: assert stationary targets as soon as actions are visible; do not hide moving hit areas with sleeps, retries, or scripted clicks.
- Status: active.

## 2026-11-05 - Device work is one-shot; uncertain results recover by observation
- Scope: WebView service settlement, foreground ownership and recorder cleanup.
- Trigger: host performs may outlive browser activation; lost result receipts cannot safely repeat device operations.
- Approach: use explicit library dialogs, opaque service IDs and fresh result request IDs; retain only completion recovery, and share `foreground.js` with files/presentations.
- Evidence: `services.test.cjs` tests explicit calls, lost receipts, current revision/values, bounded recording, interruption and private playback; `services_test.go` exercises HTTP settlement.
- Next time: keep host calls decoded, pass AbortSignals through host transport, and use selectors documented in `services-live.md` for parent browser E2E.
- Status: active.

## 2026-11-05 - Long polls must never own navigation lifecycle
- Scope: WebView live collection reads.
- Trigger: a visible list's ten-second watch must not delay leave/action/close or overwrite a newer query/editor.
- Approach: abort without awaiting, guard generation/query/instance twice, open a fresh read-only instance and replace collection rows; reconcile selection/scroll and hidden query/page values, and clear prior row operands.
- Evidence: `live.test.cjs` covers retirement, stale reads, retry/cancel and nonblocking leave; `services_test.go` checks concurrent watch/leave over the HTTP boundary.
- Next time: preserve the existing form/query nodes and close late unused open instances instead of routing live changes through editor replacement.
- Status: active.

## 2026-11-05 - Retained roots require fresh screen generations
- Scope: WebView collection/navigation lifecycle.
- Trigger: paging retained `[data-gfw-screen]`; WeakMap-only initialization kept old `queryPending`, deselected clicks, and disabled other rows.
- Approach: retire state on committed `gofastr:navigate` or changed config/form; guard late callbacks by current state identity and close late old instances.
- Evidence: `presentation.test.cjs` retained-root paging/Cancel→Apply/undo and delayed-open regressions; parent E2E identified retained DOM identity.
- Next time: wait for the new Go instance, not root disconnection, and test same-config refresh with retained roots/forms.
- Status: active.

## 2026-11-05 - Native control properties differ from attributes
- Scope: WebView serialization/checkpoint restoration.
- Trigger: checkbox `value` is not its boolean; select defaultValue is not its selected baseline; readonly is editable protocol data, not disabled.
- Approach: read `checked` and option `defaultSelected`; preserve optional empty toggles until interaction and wait for compositionend before checkpoint/query.
- Evidence: `components.test.cjs` serialization and optional-empty tests; `components_test.go` HTTP validation/fallback coverage.
- Next time: preserve browser control semantics rather than mapping every control to `value/defaultValue`.
- Status: active.

## 2026-11-05 - Action operands are not screen-render inputs
- Scope: WebView HTML form fallback.
- Trigger: forwarding action-only `row` into `App.Render` rejected the subsequent screen with 422.
- Approach: re-render from the registered route without action operands, then retain allowed visible form values; disabled baseline remains server-owned.
- Evidence: `components_test.go` native selection and row-menu fallback HTTP tests.
- Next time: separate mutation operands from render query/form buffers.
- Status: active.

## 2026-11-05 - Uncertain effects use read-only completion recovery
- Scope: WebView files/presentations and effect settlement.
- Trigger: lost write replies must not become duplicate mutations or permanently disabled retry focus.
- Approach: retain the request ID and read `complete`; clear uncertainty only after observed completion and never replay the write.
- Evidence: existing `files.test.cjs` and `presentation.test.cjs` transport-recovery/focus regressions.
- Next time: keep recovery on the same request identity and avoid freezing the opener after read-only recovery.
- Status: active.
