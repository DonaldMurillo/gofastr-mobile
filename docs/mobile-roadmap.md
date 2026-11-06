# Mobile presentation roadmap

Implementation roadmap, updated November 5, 2026. The M1 shared-screen rendering proof
is implemented. Shared editor/runtime services and UI-independent startup now
have working implementations; the remaining acceptance gaps are listed below.
The current APIs and renderer differences are described in
[shared mobile screens](shared-mobile-screens.md),
[native presentation](native-presentation.md), and
[the external presentation prototype](presentation-prototype.md).

## Direction

Build an independent Go mobile-presentation module with shared task behaviors
and multiple renderers. Keep GoFastr core web-first. Application authors write
Go mobile screens and actions; the library and extension authors implement
platform controls, web markup, and host integration.

Support three explicit paths:

1. **Go mobile definitions to native controls:** UIKit today; Android native is
   a later implementation milestone, not an existing capability.
2. **The same Go mobile definitions to mobile WebView:** a new renderer using
   the existing `mobileui` design system and public GoFastr web seams.
3. **Existing GoFastr web applications in WebView:** preserve the current host
   contract, defaults, and ability to write ordinary GoFastr web applications.

The first two paths now consume the same `notesscreens` Go package. The separate
`notesweb` presentation remains a compatibility example of the third path.
Shared Go policy and capability negotiation are implemented for the documented
editor and presentation slices; this does not imply complete renderer parity.

Desktop, canvas, and TUI projects can reuse domain services without implementing
the mobile schema. Selecting native versus WebView is initially a build/boot
choice. Mixed native/web screens within one navigation stack are a later,
explicit experiment, not an automatic fallback.

## Industry patterns to borrow

These are architectural references, not dependencies selected for installation.

| Reference | Borrow | Do not import wholesale |
| --- | --- | --- |
| React Native Fabric and Codegen | Stable identity, update existing host controls, UI-thread mounting, generated typed boundaries | React scheduler, C++ shadow tree, JSI/JavaScript runtime |
| Crux | Core/shell separation, typed effects and results, replaceable test shells | A Rust runtime or the requirement that Go database/network work run in Swift |
| Elm and TCA | Small state transitions, explicit commands, scoped cancellation, deterministic clocks and effect tests | A compulsory global store or a competing Swift application-state model |
| Zag and XState | Focused behavior machines, lifecycle-bound work, separate platform connectors | DOM-dependent machines inside UIKit or a general statechart DSL |
| React Aria async lists and Radix | Loading/filtering/paging/selection contracts; accessibility includes focus and interaction | React merely to obtain hooks or a claim that DOM accessibility maps directly to native |
| Expo Modules and Flutter channels/Pigeon | Typed capability registration, results/events, execution affinity, generated glue | Their runtime, rendering engine, or generated protocol as our public Go API |
| NativeScript | Native-control mapping, property updates, lifecycle discipline | Arbitrary platform reflection or business work on the UI thread |
| Hotwire Native | WebView as a first-class presentation with selective native capabilities | Remote-page deployment or two competing navigation stacks |

Direct reuse for the first slice is existing Go services/repositories, Go
contexts, GoFastr's public registry/router/host seams, UIKit controls, semantic
HTML controls, and the existing transports. No new production dependency is
needed to prove the shared-definition WebView renderer. Dependencies proposed
later require confirmation. Port behavior specifications rather than copying
source without reviewing its license.

## Ownership

| Layer | Owns | Must not own |
| --- | --- | --- |
| Shared application services | Authorization, validation, transactions, entities, business commands | Mobile widgets or platform navigation |
| Go mobile definitions | Task-shaped screens, labels, field semantics, actions, destinations | Swift/Java/HTML/CSS, wire JSON, renderer branches |
| Headless Go behaviors | Form submission, draft/leave policy, search/loading/paging/selection, semantic command state | Caret positions, IME composition, pixels, actual navigation stacks |
| Go execution/session layer | Instance/request identity, effect coordination, cancellation, bounded work, revisions | A duplicate business repository or independent navigation truth |
| Library-owned renderers | Controls/layout, live editing buffer, focus, scrolling, gestures, accessibility mapping, actual navigation | Notes validation, persistence policy, authorization |
| Host capabilities | OS lifecycle, share UI, permissions, files/media, haptics, platform settings | Product decisions or app-owned screen source |

There are two different meanings of headless here: reusable presentation
behavior, and presentation-independent application startup. They need separate
milestones. Neither means moving GoFastr's server-side interactive state into
process RAM; mobile session state belongs to the installed app, not the web core.

### Behavior and effect loop

User or lifecycle event → Go task behavior → updated semantic state/screen
snapshot plus commands → Go service runner or platform adapter → typed outcome
back to the task behavior.

Existing Go services continue to perform database and network work in Go. Only
OS-specific effects cross to platform code. Keep the app-facing route/action
authoring model; library behaviors should supply ordinary defaults without
requiring every screen to implement a reducer or state machine.

## First headless behaviors

| Behavior | Shared contract | Renderer responsibility |
| --- | --- | --- |
| Form | Submitted baseline/revision, field errors, pending/success/failure, duplicate-submit admission | Editable buffer, input type, focus, text selection, composition, native undo |
| Draft | Installation/account/entity/edit-instance key, checkpoint revision, recovery/discard/save policy | Send versioned snapshots and show truthful recovery/checkpoint status |
| Leave guard | Allow, confirm, or block; Keep Editing / Keep Draft / Discard decisions | Intercept each supported Back/dismiss gesture and present the decision |
| Search/loading | Query generation, one debounce policy, active request, content versus empty/error/refreshing | Native search or HTML input; composition handling and immediate local feedback |
| Paging | Query-bound cursor, append/retry/exhausted state, stable-ID deduplication | Viewport/scroll observation and virtualized or bounded rendering |
| Selection | Stable selected record IDs, mode, disabled items, persistence across filtering/removal | Platform interaction; not shared text selection or temporary row highlighting |

Do not rebuild text input, scrolling, gestures, keyboard handling, or native
navigation transitions as Go state machines. Accessibility labels, errors, and
selected/disabled/busy intent are shared; actual native traits or DOM semantics,
focus management, modal isolation, and keyboard interaction remain adapter work.

### Draft and write guarantees

- Keep Draft leaves only after the requested revision is durably checkpointed.
  A checkpoint failure keeps the editor open.
- Flush a versioned editing snapshot before Save or Leave. Use documented,
  asynchronous checkpoint boundaries during editing, not synchronous Go calls
  per keystroke. Uncheckpointed text has a loss window; do not promise otherwise.
- Clean up only the draft revision covered by the successful save. A late
  completion must not erase newer edits. Drafts and committed records are
  different stores and concepts; neither implies encryption or cloud sync.
- A committed write remains successful even if navigation or sharing fails.
  Leaving a screen can cancel disposable reads, not retroactively undo a commit.
- Admit a mutation once in Go, not only by disabling a button. Do not
  automatically retry a create after a lost response without atomic repository
  idempotency or a proven safe reconciliation mechanism.
- The current Notes repository has `Save`, not an atomic operation-deduplication
  contract. Record this constraint rather than adding a separate, non-atomic
  journal and claiming exactly-once creation.

## Runtime contract

### Identity, updates, and asynchronous work

Use different identities for different lifetimes: screen instance, read request
and generation, admitted mutation, presentation effect, and edit/model revision.
Two editors at the same route must not share lifetime or draft identity by
accident.

Serialize state transitions, not every I/O operation. Execute service work
outside the transition loop, then return completion events. Cancel superseded
reads through their Go contexts and independently reject stale completions even
when a dependency ignores cancellation. Mutations use a bounded service/command
lifetime. An in-process task does not imply survival after OS termination.

Start with revisioned whole-screen snapshots and keyed renderer reconciliation.
Full snapshots do not require recreating controls. Update changed rows and
properties while preserving the live editor, focus, composition, and scroll.
Prefer typed internal Go methods over JSON round-tripping inside the Go WebView
adapter; keep JSON as the initial local cross-language boundary. Defer a patch
wire protocol or binary transport until measurement justifies it.

### Navigation

One owner per renderer: UIKit's stack for iOS native; the existing GoFastr client
router/history for mobile WebView; the existing web navigation for hosted web
applications. Do not install a second JavaScript mobile router. Additional tab
entry metadata must integrate with that owner rather than become another stack.

Go requests push/pop/replace/sheet/dismiss/select-tab intent. The owner reports
applied/cancelled/failed outcomes, including native/system Back and gesture
cancellation. Every supported exit path uses the same leave policy. Deep links
resolve only bundled, registered routes and use the same entry point.

### Platform capabilities and extensions

Evolve individual response fields into a small typed request/result contract,
starting with sharing, haptics, and navigation. Keep unsupported, denied,
cancelled, failed, and successful outcomes distinct. Request/effect IDs prevent
accidental duplicate presentation within their defined session lifetime; they
do not guarantee exactly-once OS effects across a crash. Opening/completing the
share UI is not a guarantee of external delivery.

Negotiate a protocol major version and supported features at startup. Reject
unsupported required features. Allow only declared optional fallbacks; never
silently replace a requested native renderer with WebView. Haptics and incidental
announcements are best-effort and must not replay after restart.

Later, a compiled extension can register typed Go operations, platform handlers,
and renderer-specific components without editing the central host for every
feature. Its contract declares permissions, execution affinity, cancellation,
and cleanup. Extension authors may write native integration; application users
consume Go APIs. Use opaque file/media handles instead of huge JSON byte blobs.
All code/assets are bundled; this is not downloaded executable screen code.

### WebView security and compatibility

Bundle renderer assets, escape all app/data text, and use the existing local
transport. Privileged capabilities are restricted to trusted bundled content,
with frame/origin checks as supported by each platform. External links open
outside the privileged rendering surface. Origin checks alone do not establish
a complete network policy: reject remote presentation/code/assets and allow
network data only through the application's typed repository/service boundary.

Keep existing `--renderer webview` behavior and bindings compatible. Make the
presentation source explicit when adding shared-mobile-screen rendering; do not
silently reinterpret an existing web app. Do not choose new public flag/package
names before the second renderer proves the seam.

## Suggested module map

This is a responsibility map, not an instruction to create every package now.
Keep current names until implementation validates the boundaries.

```text
gofastr-native/
  nativeui/                         current mobile screen/action definitions
  behavior/                        proposed small Go task controllers
  mobileapp/                       proposed sessions, command runner, host adapter
  platform/                        proposed typed capability contracts
  render/ios/                      ordinary reusable UIKit renderer source
  render/webview/                  current mobile-definition web renderer
  mobileui/                        existing external web design system
  mobilehost/                      existing local WebView transport/compatibility
  cmd/gofastr-native/templates/
    ios/NativeApp.swift.tmpl        minimal native binding/bootstrap glue
    ios/App.swift.tmpl             existing WebView shell
    android/                       WebView today; native renderer later
  examples/notes/
    notes/                         shared domain and repository interface
    notesstore/                    current GoFastr repository adapter
    notesscreens/                  one mobile screen set for both renderers
    notesapp/                      composition; split startup from presentation
    notesnative/, notesmobile/     thin bindings; simplify after contract proof
    notesweb/                      existing independently authored web presentation
```

Revisit the native-only name `nativeui` separately now that both adapters exist.
A compatibility-preserving naming change is housekeeping, not another runtime.

## Build order and acceptance gates

Milestones are ordered by working proofs, not calendar estimates. Android setup
is deferred because no Android runtime is available in this environment.

### M1 — Same Notes definitions, two real renderers

**Rendering proof implemented and verified.**

- `render/webview` maps the same `notesscreens` definitions to existing components,
  transport, and client navigation; UIKit uses ordinary `render/ios` source.
  No Notes-specific renderer code or app-owned platform source/HTML/CSS is needed.
- Fresh UIKit bind/build/install/launch and a real WKWebView flow verify the
  shared package. WKWebView exercises create, invalid/corrected save, search,
  reopen, and About/dismiss without reloading the document. Both web presentation
  modes also run browser acceptance tests through the in-memory bridge.
- Legacy web-app hosting and CLI defaults remain supported. Renderer-specific
  draft behavior and full-page WebView sheets are documented, not behavior parity.
- The original rendering proof is now extended with negotiated capabilities,
  instance/revision coordination, observed effect receipts and editable inputs
  during saves. See [the implemented contract](mobile-runtime.md).

### M2 — Shared editor policy and async correctness

**Editor/runtime slice implemented; full acceptance gate remains open.**
`behavior.Editor` and the file draft store serve both adapters. The transport
E2E proves editing/caret retention during create, canonical ID adoption without
duplicate records, validation and recovery after clearing browser session data.
UIKit simulator interaction verifies validation, checkpointed Keep Draft/restart,
save/reopen and cancelled system sharing. Pure command tests cover unknown
outcomes and bounded request deduplication. Remaining: asynchronous durable
browser/OS Back gating, exhaustive IME/assistive-technology fault runs, and keyed
collection updates. Do not label the entire M2 gate complete from this slice.

- Move form/draft/leave decisions into small Go behaviors. Add injectable clock,
  draft-store and effect interfaces, structured errors, command admission, and
  observable capability outcomes. Implement keyed editor-preserving updates.
- Gate: both renderers pass failed save, duplicate submit, each supported Back/
  dismiss path, acknowledged draft checkpoint/relaunch/recovery, and save cleanup
  that cannot delete a newer draft. Late read results cannot overwrite newer
  search results or a replacement screen instance. Delayed work never blocks
  typing/scrolling or resets composition/caret.
- Fault-test commit-before-response loss. Reconcile only where the actual
  repository supports it; unknown outcomes must remain truthful and not trigger
  an unsafe automatic retry.

### M3 — Separate application startup from presentation

**Gate A complete; Gate B explicitly incomplete.** Shared
`notesapp/application.Open` and `mobilehost.ServiceOnly` remove the unused native
web presentation while preserving full in-memory Serve lifecycle. See
[lifecycle guarantees and tests](mobile-lifecycle.md).

- Split entity/service/identity/data composition from web mounting and transport.
  Native boot must stop mounting unused web screens. Reuse public lifecycle
  mechanisms; do not copy migrations, plugin startup, readiness, or shutdown.
- Audit `App.Serve`, `InitPlugins`, readiness, and cleanup separately. Calling
  `InitPlugins` alone is not equivalent to the complete startup pipeline.
- Gate A: UI-independent startup with the same identity, scoped repository,
  migrations, fresh-install behavior, and failure cleanup; no unused `UIHost`.
- Gate B: listener-free native startup only if a complete public lifecycle seam
  supports it. If it does not, retain an explicit temporary in-memory Serve
  adapter and propose the minimal upstream lifecycle seam separately. No
  GoFastr core changes are authorized by this roadmap; do not label Gate A
  HTTP-free or Gate B complete prematurely.

### M4 — Android native against the same contract

**Deferred at the user's request.** Do not install/provision Android tooling or
claim Android-device verification. Existing WebView presentation regressions
continue to run in Chromium; they are not Android-native evidence.

- Provision the missing Android SDK/JDK, implement the same mobile screens,
  behavior/effect contracts, lifecycle, system Back, keyboard, and accessibility.
- Review the backend before implementation: platform Views fit the current
  direct-SDK/no-Gradle host; Compose introduces a different Kotlin/Gradle/AndroidX
  build/dependency commitment. Prefer existing tooling for the first proof;
  approve a backend/dependency change explicitly rather than hiding it.
- Gate: run the same Notes acceptance flows on a real Android emulator, plus
  TalkBack/font scale and supported system Back behavior. Until then the CLI
  continues to reject unsupported native Android builds.

### M5 — Broader tasks, not a widget catalog

**Presentation, file/photo, and first component slices implemented in Notes.**
Shared Go decisions cover alerts, confirmations, menus, dialogs, sheets, and
single/multiple selection. Typed continuations remain in the application Go
registry; UIKit and WebView own controls. See [contract and limits](presentation-behaviors.md).
The first typed file-service slice adds import/export and private attachments
to that same Notes application; see [file services and limits](files-services.md).
The still-photo slice adds library selection and camera acquisition to saved
note attachments using the same task lifecycle; see [photo services](photos-services.md).
Saved notes now display their photos, with tappable bounded previews shared by
UIKit and WebView. Native navigation uses explicit primary/overflow composition
rather than automatic toolbar collapse; see [image previews](image-previews.md).
Physical-camera and actual WebView OS integration remain acceptance gates.
The [component slice](mobile-components.md) now adds typed controls, filters,
stable-ID selection/bulk actions, bounded undo, accumulated Load more, tab
destinations, native custom-scheme entry, and swipe/menu actions. UIKit retains
per-tab navigation controllers; WebView preserves last-route/scroll/focus per
tab while keeping GoFastr's shared browser history. This is not full parity.
The [device-service slice](mobile-services.md) adds typed reminders,
sharing/clipboard, private voice recordings, and cancellable live collection
reads. Notes now pages through owner-scoped SQL projections using bounded
100-row keyset windows rather than slicing a 200-record snapshot. Reminder
metadata remains separate from explicit scheduling. Real OS delivery/capture,
hosted-WebView provider acceptance, and M6 extensions remain separate gates.

- Add sections/accessories and richer sheets. Extend
  task-shaped screens within the one Notes application to
  expose accidental coupling; do not create another example app.
- Expand sharing/files, media and permission workflows through typed capabilities.
  Complete device acceptance for custom-scheme entry and route restoration;
  verified Universal Links and full scene restoration are separate work.
- Paging gate: a dataset exceeding the original 200-note cap actually pages through the
  repository; query changes reject old pages, selection follows stable IDs,
  tabs preserve the declared context, and required/optional capabilities behave
  explicitly on every renderer. Do not simulate paging by slicing capped data.

### M6 — Extensions, developer experience, and measured optimization

The [compiled extensions](compiled-extensions.md) cover explicit Go registration,
package-owned UIKit/WebView adapters and dependency discovery. Photo acquisition
reuses the existing file lifecycle; [rendering components](rendering-extensions.md)
own Notes media screen pieces with keyed reconciliation and guarded navigation.
[Authoring tools](extension-tooling.md) and a separate
[device readiness/acceptance gate](ios-device-acceptance.md) accompany these seams.
Arbitrary service extensions, generated bindings, inspector, full signing/deploy
automation and measured optimization remain future work, not completed parity.

- Prove one separately packaged compiled capability/component extension with an
  explicit support matrix and cleanup. Add contract-generated Swift/Android/JS
  DTOs and validators only after the contract stabilizes; generation checks must
  catch drift. This is binding generation, not Go-to-Swift screen transpilation.
- Simplify app binding/scaffolding; improve errors and a local opt-in inspector.
  Keep user field contents out of normal logs. Add physical-device signing and
  deployment checks as separate release-tooling work.
- Measure startup, memory, payload size, queue wait, Go/service time, encoding,
  decoding, reconciliation, mounting, and frame stalls on real hosts. Optimize
  the demonstrated bottleneck, not an assumed JSON problem.

## Verification policy

Real end-to-end flows are acceptance evidence. Pure transition, clock, schema,
and reconciliation helpers also deserve focused tests; string-template or Go
API tests alone cannot prove platform behavior.

Use the same scenario definitions/fixtures across adapters, with renderer-specific
drivers. Keep the existing Go test command, public-boundary scenarios, and
Chromium harness; add real WKWebView/UIKit acceptance and Android execution as
those hosts become supported. Do not introduce an integration-only test lane or
new automation dependencies without review.

Required checks include large text, light/dark, software keyboard, composition,
focus restoration, VoiceOver/TalkBack, gesture cancellation, reduced motion,
process restart after acknowledged checkpoints, cross-owner data denial,
untrusted text/bridge rejection, and compatibility with existing web hosting.
Do not claim accessibility or performance parity from semantic labels or phone
screenshots alone.

## Decisions to keep explicit

- **Recommended:** full snapshots plus local keyed reconciliation first.
  Alternative: incremental wire patches after profiling demonstrates a need.
- **Recommended:** small Go controllers using current services and stdlib.
  Alternative: a general behavior engine only after multiple concrete tasks
  show repeated machinery that ordinary functions cannot express cleanly.
- **Recommended:** one renderer selected per build/boot initially.
  Alternative: mixed presentation inside one stack after ownership/restoration
  and leave guards are proven.
- **Recommended:** renderer-owned actual navigation with shared intents/policy.
  Avoid an independent Go navigation stack mirrored against native/browser state.
- **Recommended:** platform extension code belongs to library/module authors.
  Avoid making each application handwrite native screen templates again.

Ambition should increase supported tasks and platform depth, not create a
universal widget language, layout engine, JavaScript runtime dependency, or
parallel implementation of GoFastr's services and repositories.

## Primary sources

Reviewed November 4, 2026. Framework-specific mechanisms are evidence for the
patterns above, not proof of Go compatibility or adoption suitability.

- [React Native render/commit/mount](https://reactnative.dev/architecture/render-pipeline)
  and [Codegen](https://reactnative.dev/docs/the-new-architecture/what-is-codegen).
- [Crux overview and managed effects](https://redbadger.github.io/crux/latest_master/overview.html).
- [Elm Architecture](https://guide.elm-lang.org/architecture/) and
  [commands/subscriptions](https://guide.elm-lang.org/effects/).
- [TCA](https://github.com/pointfreeco/swift-composable-architecture), including
  its search example, lifetime cancellation, and TestStore.
- [Android UI-layer architecture](https://developer.android.com/topic/architecture/ui-layer)
  and [UI-event handling](https://developer.android.com/topic/architecture/ui-layer/events).
- [Zag introduction](https://www.zagjs.com/overview/introduction) and
  [machine/DOM connectors](https://zagjs.com/guides/building-machines).
- [XState invoked actors](https://stately.ai/docs/invoke) and
  [promise actor cancellation](https://stately.ai/docs/promise-actors).
- [React Aria async lists](https://react-aria.adobe.com/useAsyncList) and
  [Radix accessibility](https://www.radix-ui.com/primitives/docs/overview/accessibility).
- [Expo Modules](https://docs.expo.dev/modules/overview/) and
  [module execution/API](https://docs.expo.dev/modules/module-api/).
- [Flutter platform channels](https://docs.flutter.dev/platform-integration/platform-channels)
  and [Pigeon](https://docs.flutter.dev/interop/pigeon).
- [NativeScript buttons](https://docs.nativescript.org/ui/button),
  [properties](https://docs.nativescript.org/guide/property-system), and
  [threading](https://docs.nativescript.org/guide/multithreading).
- [Hotwire Native bridge components](https://native.hotwired.dev/overview/bridge-components).
- [Android WebView bridge security](https://developer.android.com/privacy-and-security/risks/insecure-webview-native-bridges).
