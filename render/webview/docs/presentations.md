# WebView presentation adapter

Applications author `nativeui.Presentation` and Go continuations only. The renderer's
`presentation.js` is a DOM adapter, not an application behavior runtime. The
`gfm-webview` behavior requires `gfm-webview-presentation`; both are embedded and
served by the existing UIHost, with no application-authored JavaScript.
The companion [file adapter](files.md) shares this modal slot: file tasks cannot
open over a presentation, and presentations cannot open over a file task. Accepted
file/presentation chains close their consumed dialog before opening the next.
The [controls and collections adapter](components.md) documents retained-root
lifecycle, selection-preserving list refresh, tabs, and row-action entry points.

The renderer advertises `presentation/v1: available`. It opens overlays only from
an observed `complete` reply containing both `response.presentation` and the opaque
`presentationId`. `Presentation.ID` remains the Go continuation key and is never
used as a response identity. Choice clicks send `respond` with a new request ID,
the issued presentation ID, choice ID, selected option IDs, and the latest screen
field values and revision. Accepted responses are observed through `complete`
before handling normal effects. A failed transport is recovered with `complete`,
never by replaying a continuation.

All five kinds use a labelled HTML modal `dialog`. It is mounted inside the
screen's `[data-gfm-platform]` section so scoped fonts and touch-target tokens
inherit correctly; `showModal()` places it in the browser's top layer without
moving it to `document.body`. Menus are dialog-contained
action buttons, not ARIA menus with a competing keyboard behavior engine. Options
are labelled native radios or checkboxes, including detail descriptions and disabled
states. Go owns minimum/maximum selection, cancellation, admission, and stale or
duplicate responses. Escape and backdrop gestures send an empty choice only when
`Dismissible` is true; explicit cancel choices remain available independently.
Choices and options are locked while a response is pending.

Pre-admission `invalid_request` or `validation` errors without `response` retain
the open overlay and current selection. No `complete` is sent for that rejected
response. An error with `response` means the continuation was already accepted:
the renderer completes it, closes the consumed overlay, and announces the failure.
Chained presentations remove the old dialog before showing the next, retain the
original opener, and restore it when the flow ends.

Presentations never replace the screen, navigate, or clear its local editor buffer.
Existing `Navigation{Kind: "sheet"}` remains a separate, full-page compatibility
fallback using GoFastr history. It is not the new presentation sheet, which is a
real bottom-aligned modal overlay.

## E2E selectors

- `[data-gfw-presentation]`: the single active overlay; its value is its kind.
- `[data-gfw-choice="<ID>"]`: a declared choice button.
- `[data-gfw-option="<ID>"]`: a selection radio or checkbox.
- `[data-gfw-presentation-status]`: response progress or corrective validation text.

## Scoped checks

```sh
node --check render/webview/presentation.js
node --check render/webview/webview.js
node --test render/webview/presentation.test.cjs
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test -race ./render/webview
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go vet ./render/webview
```

The dependency-free Node tests cover renderer DOM/wire glue using a small DOM
fixture. `go test ./sim -run TestNotesBehaviorShowcaseWebView -count=1` exercises
all seven Notes flows through the real bridge in Chromium, with iOS/Android
styles, corrective validation, 200% text, dark mode, and opener focus restoration.
A November 4, 2026 iOS 26.5 simulator smoke check also verified menu/confirmation
chaining and cancellation/focus restoration inside actual WKWebView. Neither
check establishes physical-device or screen-reader accessibility conformance.

## Implementation notes

- **Scope:** WebView presentation glue; **lesson:** only `complete` issues the
  opaque ID and releases an accepted presentation for chaining. **Next time:**
  preserve `action/respond → complete`, and never substitute the continuation key.
- **Scope:** validation and focus; **lesson:** an error with a `response` differs
  from a pre-admission rejection. **Next time:** keep only unaccepted validation
  overlays correctable, and restore focus after unlocking the opener.
- **Scope:** scoped design tokens; **lesson:** the runtime wrapper surrounds the
  themed screen and does not itself inherit its tokens. **Next time:** mount
  overlays inside the platform section and verify computed target sizes in E2E.
