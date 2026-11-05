# UIKit presentation adapter

`GofastrPresentation.swift` decodes the shared Go presentation model and maintains
only local selection state. `GofastrPresentationController.swift` maps alerts and
confirmations to UIKit alerts, menus to action sheets, and selection dialogs and
sheets to accessible, Dynamic Type table controllers. Sheets use medium/large
detents; dismissal and VoiceOver escape obey `dismissible` and pending state.

The host's `MobileForegroundCoordinator` owns one foreground presentation or file
task per renderer connection (see `files.md`). The app's
`Presentation.id` is never an action or an instance identity. Only the opaque
`Reply.presentationId` from `complete` is returned in `respond`, with a fresh
request ID, declared choice, selected option IDs, and current values/revision.
Gesture dismissal sends an empty choice. Go validates every response and owns
continuations, cancellation, commit policy, and selection min/max limits.

`respond` is never retried automatically. A transport failure still permits the
read-only `complete` settlement lookup. Definitive validation failures are shown
inside the task; a subsequent user response gets a new request ID. Pending UI
disables choices, selection, close, and interactive dismissal. Settlement fully
dismisses the previous transient before applying navigation, sharing, or a
chained presentation. Editor views are not rebuilt, and marked/dirty text is not
replaced when a presentation opens. Input and VoiceOver focus survive chains.

Response `presentation` and reply `presentationId` are optional so older payloads
still decode. A legacy backend without versioned commands gets an explicit
`unsupported` error, never a local Swift continuation.

## Validation

Run `GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./cmd/gofastr-mobile ./render/ios`.
On macOS, this typechecks all embedded UIKit sources without Go bindings and runs
Swift Foundation JSON/selection regression checks. A fresh gomobile bind,
Swift compile/link, install, and launch passed on the iOS 26.5 simulator on
November 4, 2026. Manual checks covered alerts, disabled menu choices, chained
menu/confirmation, explicit cancellation, non-dismissible dialog policy,
dismissible sheet backdrop, single selection, and multiple-selection validation
with visible status feedback after applying a valid result. Physical-device,
VoiceOver, interactive swipe cancellation, and broader dirty-editor/IME checks
remain acceptance work; typechecking does not establish those behaviors.

## Implementation notes

- Never replace `UIAlertController.presentationController.delegate`: UIKit
  asserts at runtime for alert-style controllers. Attach adaptive dismissal
  delegates only to our own controllers; use alert actions and supported popover
  callbacks for system alerts/menus. Next time, open an actual alert after
  typechecking: compile success cannot catch this UIKit contract violation.
- Announcements on text-only screens need a visible, Dynamic Type status label
  as well as `UIAccessibility.post`. Preserve the editor's existing dirty-status
  rules rather than replacing them with transient feedback.
