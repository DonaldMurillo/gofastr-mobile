# Implementation notes

## 2026-11-05 - Compiled adapters register before host manifests seal
- Scope: extensions, packaging, webview, UIKit.
- Trigger: registering extension JS in `webview.New` panicked when a legacy host had already sealed GoFastr's registry.
- Approach: library package init uses `extension/webassets.Register`; per-app `NewWithExtensions` still controls authorization/loading. CLI discovers manifests only in compiled iOS dependencies and copies Swift unchanged.
- Evidence: the legacy-builder coexistence suite catches the sealed-manifest case; Notes photo E2E covers both WebView styles, and CLI/UIKit tests cover core-only and extension-enabled sources.
- Next time: separate static asset publication from runtime registration; preserve opaque `file-result -> complete`, current authorization, cancellation and unknown-write locks instead of adding another task lifecycle.
- Details: [compiled extension authoring and limits](compiled-extensions.md). Native Android and physical camera/provider/VoiceOver/IME acceptance remain deferred.
- Status: active.

## 2026-11-05 - Swipe snap-back must not animate open action targets
- Scope: webview, gestures, acceptance.
- Trigger: bounded real-pointer traces showed a summary hit on pointerdown and its row link on pointerup; stage 11 then began offscreen and left the menu open.
- Approach: end snap-back when the row disclosure opens; keep the animation for closed menus and capture at most 80 interaction events on E2E failure.
- Evidence: stationary-target regression failed in both styles before the CSS fix; `go test ./sim -run '^TestMobileComponentsWebView$' -count=30 -timeout=8m` then passed.
- Next time: inspect down/up targets and computed geometry before blaming browser timing or changing event delivery.
- Status: active; this proves the stage 11 cause, not the cause of every earlier stage 8 timeout.

## 2026-11-05 - Rename branding without renaming compatibility contracts
- Scope: repository, module, cli, compatibility.
- Approach: use `github.com/DonaldMurillo/gofastr-native`, `cmd/gofastr-native`, and matching CI/docs; retain mobile package names, bridge hello/path, simulator debug variables, app IDs, and data locations.
- Evidence: [rename migration](project-rename.md), CLI help/dry-run checks, 127 Node tests, fresh complete workspace/public-pin Go suites and vet, and a full UIKit simulator build/install preserving its notes. Earlier stage 8 timeouts did not recur after the separately proven stage 11 menu fix.
- Next time: treat repository/module/CLI names separately from wire/storage identity; check both workspace and public-pin builds after import changes. A repository rename does not publish uncommitted source.
- Status: active; native Android stays deferred and WebView stays supported.

## 2026-11-05 - History departure needs the router's pre-navigation seam
- Scope: webview, reliability, navigation.
- Trigger: browser Back bypassed durable Keep, and capture-phase `popstate` interception did not stop the actual router despite passing DOM mocks.
- Approach: compose `_interceptPopstate`, repair the tagged source entry, await Go leave/Keep, then replay with revision-bound permission. Reuse history lineage after reload; untagged history fails closed.
- Evidence: `sim/reliability_test.go` holds real acknowledgement, cancels/retries, types during Keep, and recovers Forward without sessionStorage. Wait for dialog removal, not just `open=false`, before another traversal.
- Acceptance waits: synchronize with committed screen route and instance, not URL alone or outgoing fields. Preserve query strings when checking a return from About; its destination may still be a filtered Notes list.
- Next time: prove interception against the real bridge/router before trusting mocked event ordering; never build another navigator or replay a write to repair history.
- Status: active; termination, foreign-history and actual OS/IME limits are in [mobile reliability](mobile-reliability.md).

## 2026-11-05 - Durable acknowledgement and lifecycle fencing
- Scope: nativeui, ios, drafts, lifecycle.
- Trigger: failed checkpoints could admit writes/departure, and same-revision native replies could undo mutation state.
- Approach: gate action/leave/effects on storage success, preserve buffers on failed cleanup, persist canonical keys first. Fence replies in both adapters, defer completion/leave/discard checkpoints, sequence short native calls, and bound background batches.
- Evidence: Notes Stop/Start and ENOTDIR faults, wire/storage regressions, Foundation execution, and optimized UIKit linking. Details: [runtime](../nativeui/docs/agent-notes.md), [UIKit](../render/ios/docs/reliability.md).
- Next time: keep long saves out of the checkpoint dependency chain; test unknown restart locks and newer text separately from clean completion.
- Status: active; no crash exactly-once guarantee or native Android implementation.

## 2026-11-05 - Retained controllers do not reactivate retired live sessions
- Scope: lifecycle, ios, webview, live
- Trigger: native tab restoration/reuse could display Retry because opening any other screen intentionally retires earlier watches on the connection.
- Approach: recover only stale/expired watch instances with one read-only reopen, preserving collection controls/selection/scroll; retain visible errors on repeated failure or revoked authorization.
- Cancellation race: the preceding worker may still be stopping after local abort; allow at most three 150ms `pending`/`cancelled` watch retries, never a mutation retry or unbounded reopen.
- Evidence: `sim/reliability_test.go` reproduced the real-bridge failure in both platform modes; renderer tests cover bounded recovery, permanent errors, and suspended results. See [mobile reliability](mobile-reliability.md).
- Next time: distinguish retained UI identity from active runtime identity; never auto-retry a save/device effect or reset the recovery budget during its own reopen.
- Validation caveat: full race runs can hit a stale browser-node handle in `sim/files_test.go:117`; it also reproduces on untouched checkpoint `16786d5`. The recovery/editor/component race flows passed three repeats separately.
- Component acceptance also intermittently times out at menu/touch stages 8/11; eight isolated final-code runs and the final public-pin suite passed. Failure output now includes the action index. Keep this caveat distinct from the three-repeat recovery race results.
- Status: active

## 2026-11-05 - Checkpoint source without deleting app data
- Scope: build, cleanup, verification
- Trigger: feature work accumulated alongside multiple generated simulator builds.
- Approach: commit shared Go definitions, both renderer libraries, tests, and docs together; keep `dist/` and local `go.work*` ignored. Remove only older `dist/mobile/*/work` intermediates, preserving app bundles and the latest design build.
- Evidence: the approved design build is `dist/mobile/ios-design-qa/Notesnative.app`; simulator data is separate from these rebuildable intermediates.
- Next time: inspect ignored outputs before cleanup; never use a blanket clean or remove simulator containers to tidy a source checkpoint. Run `GOWORK=off go test ./...` as well as workspace tests to verify the public pin.
- Status: active

## 2026-11-05 - Serve exit must not compete with host shutdown
- Scope: mobilehost, lifecycle, live
- Trigger: a ten-second live watch drained successfully, but Stop returned the Serve goroutine's five-second follower timeout.
- Approach: atomically claim the existing stop completion channel under the host mutex; only that owner invokes App.Shutdown. Cancel Host-owned bridge calls before drain, while direct Do/Open keep caller contexts.
- Evidence: `TestStopDrainsLongRequestWithOneShutdownOwner` and `TestStopCancelsOutstandingBridgeRequestsBeforeClosingStorage` exercise real in-memory HTTP, concurrent stops, and storage ordering.
- Next time: distinguish listener termination from completed request drain; do not fix this by increasing test timeouts or dropping shutdown errors.
- Status: active

## 2026-11-05 - Live collections must refresh action state as well as rows
- Scope: webview, live, undo, verification
- Trigger: an archive succeeded but live refresh left Undo invisible and omitted its new opaque receipt; old sharing tests still expected the retired text-only fallback.
- Approach: reconcile selection-toolbar actions and hidden values from the same authorized snapshot; wait for renderer instance readiness before clicking freshly hydrated list rows.
- Evidence: the toolbar/opaque-value Node regression, existing archive/undo browser workflow, and updated real service-sharing workflow.
- Next time: test mutation → visible Undo → confirmed restore, not only new list rows; update acceptance selectors when a deliberate capability replaces the old UI.
- Status: active

## 2026-11-05 - Browser audio needs narrow host-policy opt-ins
- Scope: webview, audio, security, verification
- Trigger: real MediaRecorder failed with microphone denied; after capture worked, private Blob playback remained blocked by the default CSP.
- Approach: opt only the shared-screen presentation into `microphone=(self)` and `media-src 'self' blob:`; preserve camera/geolocation denial and default script/form/frame restrictions. Pass framework options through UI-free application composition.
- Evidence: `TestLegacyBuildersKeepWebPresentationAndSharedService` checks actual headers; `TestNotesAudioRecordingBrowser` records a synthetic Chromium microphone through the real bridge, persists it, and loads the audio for decoding.
- Next time: test a real MediaRecorder and playback under the shipped response headers; a fake service result cannot expose browser policy failures, and a separate Service instance does not emit this composition's live invalidations.
- Status: active; synthetic capture is not physical-device microphone acceptance.

## 2026-11-05 - Native device services need a separate trusted host seam
- Scope: ios, webview, capabilities, packaging
- Trigger: sharing a Go service contract must not make browser fallbacks look like OS reminders or require app-authored JavaScript/templates.
- Approach: reuse the binding-independent Swift service runner through `mobilehost/ios`; accept only the exact app origin/main frame/current document, bound JSON, and never replay a lost OS action. Keep default-host wiring tiny and copy library sources verbatim.
- Evidence: microphone-purpose packaging, source-copy/dry-run checks, optimized Swift origin-policy execution, and binding-independent WebKit adapter typechecking pass. Full service acceptance is tracked separately in `mobile-services-plan.md`.
- Next time: exercise real hosted WKWebView services in addition to fake-adapter browser protocol tests; confirm missing microphone purpose text leaves recording unavailable.
- Status: active

## 2026-11-05 - Collection navigation can retain the screen DOM root
- Scope: webview, collections, navigation
- Trigger: after Load more, new rows appeared but selecting a checkbox immediately reset it and disabled the remaining controls.
- Evidence: Chromium confirmed the same `[data-gfw-screen]` element survived while its config and descendants changed; its previous query-pending state remained live.
- Approach: adapter initialization must recognize changed screen config/form identity, not only WeakMap membership. Retire stale async state before reopening the current screen instance.
- Next time: run `go test ./sim -run TestMobileComponentsWebView -count=1` and test interaction after navigation, not merely the new row count. Keep rendered-instance readiness separate from URL and DOM-root identity.
- Status: fixed; retained-root Node regressions and real browser selection/cancel/apply/undo flows pass. Full component acceptance also drives real CDP touch events.

## 2026-11-05 - Touch capture transfers emit descendant loss events
- Scope: webview, gestures, acceptance
- Trigger: a real full swipe reset after one frame; after that fix, a suppression marker swallowed the next intentional menu-close tap.
- Approach: transferring touch capture from the starting anchor emits a bubbled `lostpointercapture`; cancel only when the row loses capture. Clear the old click-suppression marker at the next pointerdown, preserving suppression of the swipe's synthetic click only.
- Evidence: `sim/components_test.go` drives CDP TouchStart/Move/End and verifies full-swipe unpin, trailing reveal without mutation, immediate menu-close taps, and vertical-scroll cancellation in three runs of both styles.
- Next time: start gesture acceptance on the row link, not its checkbox/menu, and include real pointer capture rather than only state-machine inputs.
- Status: active.

## 2026-11-05 - Empty native tables can hide filter accessibility
- Scope: ios, collections, accessibility
- Trigger: a background empty label overlapped filters; moving it to a zero-row table footer avoided overlap but the AX tree still hid header controls.
- Approach: represent the empty message as a non-action cell, bounds-check all row lookup paths, and keep footer sizing/targets Dynamic-Type aware. Map all public semantic tab icons to valid SF Symbols with a fallback.
- Evidence: fresh iOS 26.5 bind/link/run shows separate empty message and filters in both pixels and AX; selection and dirty tab-context checks succeed.
- Next time: inspect empty-state pixels and accessibility children, not just nonempty lists or successful compilation.
- Status: active.

## 2026-11-05 - File storage needs visible cards and dirty-navigation coverage
- Scope: files, native, webview, navigation
- Trigger: ordinary attachments were stored but invisible; dirty-editor Attachments pushes were silently suppressed, and WebView leave confirmation lacked a retry callback.
- Approach: emit `Screen.Attachments`, refresh metadata on return without replacing controls, allow non-save forward navigation only with a durable draft, and resume asynchronous leave guards explicitly.
- Evidence: binding restart tests, three repeated file/photo browser E2E runs, downloaded-byte checks, and a fresh bound iOS build's accessibility card/text-preview navigation. The native fixture used public Go settlement, not OS picking; current picker/export GUI retest is blocked by `noWindowsAvailable`/missing toolbar AX items.
- Next time: test the dirty note → Add file → visible card → detail → Back flow, not only storage or a clean attachment screen; distinguish protocol evidence from actual provider GUI acceptance.
- Status: active; public API and verification limits are in `docs/files-services.md`.

## 2026-11-05 - URL changes precede committed screen controls
- Scope: webview, navigation, e2e
- Trigger: repeated file/photo tests clicked old editor actions or Back links after URL history changed but before the destination DOM committed.
- Approach: wait for the rendered `data-gfw-config` route or destination-only controls, not only `location.pathname`, before the next click.
- Evidence: `sim/files_test.go` now uses `waitForFileScreen`; repeated file/photo E2E passes for both platform styles without arbitrary sleeps.
- Next time: synchronize navigation acceptance with committed screen identity, especially when old/new screens share an Add file or Back control.
- Status: active

## 2026-11-05 - Stored photos are not visible photos
- Scope: photos, ios, webview, verification
- Trigger: acquisition/storage/export tests passed while the note displayed no photos, and automatic UIKit overflow hid or reordered Done/Add file.
- Approach: add bounded `Screen.Images`, render actual image views in note/detail, refresh only native galleries on return, and use explicit primary-plus-More action composition.
- Evidence: browser regression first failed on missing decoded pixels; photo E2E covers Back, dirty text, 200% text and removal. Simulator displayed existing flower/fixture images and correctly labelled menus.
- Next time: assert loaded image pixels in the editor, not attachment metadata; test primary visibility and actual menu selection with large native text. Keep Swift executable checks as well as typechecking (initialization errors can escape `-typecheck`).
- Status: active; API and acceptance details in `docs/image-previews.md`.

## 2026-11-04 - Photo services reuse bounded file settlement
- Scope: photos, native, webview, protocol
- Trigger: Go-authored photo tasks need native acquisition without another modal stack, app platform code, or accidental write retries.
- Approach: add optional `FileRequest.Photo`, negotiate `photos/v1` and `camera/v1` independently of `files/v1`, normalize in each adapter, and validate the complete bounded JPEG again in Go.
- Evidence: full workspace/public-pin tests and vet, race tests, 48 Node tests, and three Chromium runs of photo/file/presentation workflows passed. Real UIKit selected the generated 3000×1800 fixture into a 2048×1229, 40,805-byte JPEG; cancel kept one attachment and note text unchanged.
- Next time: run `SIM_SHOTS=/tmp/gofastr-photo-qa GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./sim -run 'TestNotesPhotosWebView|TestNotesFilesWebView|TestNotesBehavior' -count=3`; keep physical-camera and actual WebView chooser acceptance separate.
- Status: active; public contract and limits are in `docs/photos-services.md`.

## 2026-11-04 - Simulator camera availability is not capture evidence
- Scope: ios, camera, verification
- Trigger: iOS 26.5 simulator returned camera availability and displayed a permission prompt despite this slice requiring real-device capture acceptance.
- Approach: make `MobilePhotoPlatform.cameraAvailable` return false for simulator builds before any permission call; retain purpose-key and actual hardware checks on devices.
- Evidence: rebuilt/bound UIKit app reports camera unavailable without another prompt, while library selection still succeeds. Both simulator and device Swift branches typecheck.
- Next time: test camera permission/capture on physical hardware; never equate a simulator availability API or browser capture hint with verified native capture.
- Status: active.

## 2026-11-04 - File tasks share presentation ownership
- Scope: files, native, webview, protocol
- Trigger: a picker needs a typed Go continuation without introducing app Swift/JS or a competing modal stack.
- Approach: issue `fileId` only on settlement, reuse the connection foreground slot, validate bounds/filename policy again in Go, and run mutating continuations outside UI/session locks.
- Evidence: native binding/storage tests plus `go test ./sim -run TestNotesFilesWebView -count=3` pass; a fresh iOS 26.5 simulator build exported an unsaved buffer and imported the saved Markdown through the real Files picker. API/limits and acceptance gaps are in `docs/files-services.md`.
- Next time: keep transfers at 1 MiB until streaming exists, never equate download initiation with durable external save, and never retry an uncertain file mutation.
- Status: active

## 2026-11-04 - A modal guard can cancel its own download
- Scope: webview, files, verification
- Trigger: the file dialog reported completion, but Chromium never saved the Blob download because the modal's before-unload guard cancelled it.
- Approach: exempt only the library-generated download activation, delay Blob URL revocation, and verify the actual downloaded bytes rather than trusting the result announcement.
- Evidence: `sim/files_test.go` verifies unsaved Markdown bytes and passes three repeated runs for both platform styles; `render/webview/files.test.cjs` covers the guard and cleanup.
- Next time: keep ordinary navigation blocked while a file task is open, and distinguish download initiation from verified external persistence.
- Status: active

## 2026-11-04 - Shared-screen acceptance needs the real bridge
- Scope: mobile, webview, verification
- Trigger: the same Go screen package must work through Chromium's Android-body constraints and actual WKWebView, not only adapter unit tests.
- Approach: test create/validation/save/drafts/share/search with the in-memory bridge; use the simulator eval hook for WKWebView and retain a window marker across navigation.
- Evidence: `TestSharedMobileScreensWebView`; real iOS 26.5 flow returned `ok:true` with `same document`; `/tmp/gofastr-shared-screens/wk-shared-verified.png`.
- Next time: click GoFastr's dirty-leave HTML dialog explicitly; delay the test MessagePort rather than replacing the read-only transport, and normalize visually transformed text in predicates.
- Status: active

## 2026-11-04 - Renderer source is not an application template
- Scope: native, ios, packaging
- Trigger: a generic renderer inside a large `.tmpl` still obscured the intended reusable-library boundary.
- Approach: keep ordinary UIKit source in `render/ios/`, inject `GofastrNativeBackend` closures, and generate only the small app entry plus binding glue.
- Evidence: CLI tests check copied source bytes and separate compilation inputs; `render/ios/GofastrNative.swift` imports no application binding.
- Next time: extend ordinary renderer source, never add screen behavior or application routes to the bootstrap template.
- Status: active

## 2026-11-04 - Shared mobile definitions do not replace WebView hosting
- Scope: architecture, mobile, native, webview, planning
- Trigger: the experiment needs native and WebView as first-class renderers, with reusable headless behaviors rather than another universal UI framework.
- Approach: use one mobile screen set with UIKit and WebView adapters; keep interaction/navigation renderer-owned and preserve independent whole-web-app hosting.
- Evidence: `render/webview`, `examples/notes/notesscreens`, and `docs/shared-mobile-screens.md`; both mobile bindings consume the same definitions.
- Next time: extend shared `behavior.Editor`/`nativeui.Runtime`; keep browser-Back, keyed collection and listener-free lifecycle gaps explicit.
- Status: active

## 2026-11-04 - External presentation boundary
- Scope: presentation, mobile, terminal
- Trigger: native-looking mobile screens must not turn GoFastr into a cross-platform widget framework.
- Approach: keep application services typed and UI-free; let external packages own screens, CSS, and behavior through public registry APIs.
- Evidence: `go test ./examples/notes/notesapp -run 'TestTerminalAndWebSharePersistedNotes|TestPresentationCoreDoesNotImportWeb'`; `mobileui/`; `docs/presentation-prototype.md`.
- Next time: inspect `go.work` before surveying GoFastr, and run `GOWORK=off go test ./mobileui ./examples/notes/...` to verify the public pin too.
- Status: active

## 2026-11-04 - Go-defined native UI verification
- Scope: native, ios, verification
- Trigger: Go API tests alone cannot prove the library's Swift decoder and UIKit composition.
- Approach: build with `--renderer native`, then exercise validation/save/search, draft restart, system share UI without sending, and Go-defined sheet dismissal.
- Evidence: iPhone 17 Pro / iOS 26.5; `/tmp/gofastr-go-native-editor-accessibility-light.png`, `/tmp/gofastr-go-native-editor-keyboard-light.png`; largest accessibility text and standard keyboard spot-checked.
- Next time: run the actual generated/bound host and inspect rendered pixels; keep physical-device, VoiceOver, haptics, and native Android claims separate.
- Status: active

## 2026-11-04 - Simulator child environment and real saves
- Scope: mobile, ios, verification
- Trigger: setting `GOFASTR_MOBILE_EVAL` on the parent CLI does not forward it into the simulator app.
- Approach: launch with `SIMCTL_CHILD_GOFASTR_MOBILE_EVAL` and use the existing simulator-only async evaluation hook.
- Evidence: real WKWebView list navigation and Done-save retained a window marker; screenshots are under `/tmp/gofastr-mobile-prototype/wkwebview-final-*.png`.
- Next time: verify a component-submitted save through the native bridge, not just a direct transport call or Chromium emulation.
- Status: active

## 2026-11-04 - Native rendering is not a headless lifecycle
- Scope: host, lifecycle, native, terminal
- Trigger: UIKit/terminal share Notes behavior, but `mobilehost.Init` still requires a mounted `UIHost`.
- Approach: superseded by `notesapp/application.Open` plus `mobilehost.ServiceOnly`: UIKit mounts no unused UIHost but retains in-memory Serve for the complete lifecycle.
- Evidence: `examples/notes/notesnative/notesnative.go`, `docs/native-presentation.md`.
- Next time: read `docs/mobile-lifecycle.md`; do not replace complete Serve with InitPlugins or claim listener-free Gate B.
- Status: superseded by UI-independent Gate A; listener-free Gate B remains open

## 2026-11-04 - Presentations are typed Go continuations
- Scope: behavior, native, webview, examples
- Trigger: transient UI must share decisions without adding a JS behavior runtime or another example app.
- Approach: Go validates presentation instance/choice/selection; adapters submit respond then read-only complete. Cancel never invokes product code.
- Evidence: `behavior/presentation.go`, `nativeui/runtime_presentation_test.go`, Notes `showcase.go`, `sim/behavior_showcase_test.go`.
- Next time: extend the single Notes Behaviors screen and test stale/duplicate/cancel outcomes; do not run an action from a client-provided handler name.
- Status: active

## 2026-11-04 - Showcase limits must not look like component limits
- Scope: examples, selection, usability
- Trigger: The Notes multi-select demo rejected all three enabled tags because its sample imposed Max: 2.
- Approach: Leave Max unset for the normal showcase, keep Shared disabled, and explain the nonempty-selection rule inside the presentation.
- Evidence: Native command and browser showcase tests apply Ideas, Work, and Personal together; policy tests retain bounded-selection coverage.
- Next time: Keep validation stress cases in tests and make intentional product limits explicit in the Go-authored presentation message.
- Status: active

## 2026-11-04 - UIKit alert delegates are not replaceable
- Scope: native, ios, presentations
- Trigger: Opening an alert crashed despite successful Swift typechecking.
- Approach: Leave UIAlertController's presentation delegate alone; coordinate our own sheets through adaptive dismissal and system menus through supported callbacks.
- Evidence: `render/ios/GofastrPresentationController.swift`; fresh iOS 26.5 simulator alert, chaining, and selection-result checks passed.
- Next time: Exercise actual UIKit alerts after compiling; also verify visible status feedback, not only accessibility announcements.
- Status: active

## 2026-11-04 - WebView overlays must inherit screen tokens
- Scope: webview, presentation, verification
- Trigger: Modal controls lost platform fonts and Android's 48px target because the runtime wrapper surrounds the themed section.
- Approach: Mount the HTML dialog inside `[data-gfm-platform]`; showModal supplies top-layer placement without reparenting to body.
- Evidence: `sim/behavior_showcase_test.go` checks computed targets, 200% text, selection recovery, and focus; actual WKWebView chaining/cancel also passed.
- Next time: Verify computed inherited tokens in a real browser; DOM-fixture tests cannot prove styling.
- Status: active

## 2026-11-04 - Shared editor protocol must preserve zero values
- Scope: native, webview, protocol, async
- Trigger: omitted revision zero caused repeated WebView completion reads; omitted restored=false made Swift reject an otherwise valid editor.
- Approach: serialize required false/zero fields; finish actions through read-only `complete` using the latest local buffer, then adopt saved route identity in place.
- Evidence: `TestReplyAlwaysEncodesZeroRevisionAndEditorFlags`, delayed-create E2E, real UIKit validation/restart/save/reopen.
- Next time: exercise actual cross-language decoding/navigation, not just Go marshaling or Swift typechecking; never retry a write to recover its response.
- Status: active

## 2026-11-04 - Draft ownership belongs above file storage
- Scope: behavior, persistence, lifecycle
- Trigger: delayed work from an older screen can overwrite a newer screen's semantic-key draft even when each file write is atomic.
- Approach: Runtime grants one active owner per draft key, rejects superseded checkpoints, and conditionally clears with snapshot.DraftRevision including its token.
- Evidence: `TestRuntimeNewEditorOwnsSemanticDraft`, file-store races and binding restart recovery.
- Next time: keep ownership runtime-local explicit; do not claim cross-process or crash-safe mutation deduplication from file locks and request IDs.
- Status: active

## 2026-11-04 - Caller-owned native host and gomobile names
- Scope: native, ios, binding
- Trigger: a native presenter must not implement HTTP/HTML just to satisfy the default WebView shell.
- Approach: select trusted compiled source with `--host-template`; keep plist/manifest/build defaults; inspect generated Objective-C names before writing Swift (`Note.ID` binds as `id_`).
- Evidence: checkpoint `82e936f`; the current app-owned template is removed in favor of Go definitions and the bundled renderer.
- Next time: compile/link/run the real bound host; string-template tests alone cannot prove the Swift/Go contract.
- Status: superseded

## 2026-11-04 - First native startup needs a data directory
- Scope: native, storage, startup
- Trigger: `localapp.LoadOrMint` fails on a first-launch directory that does not exist yet.
- Approach: validate an absolute path and create it with `0700` before loading installation identity; run commands off the native UI thread.
- Evidence: `TestNativeCommandsPersistAcrossRestarts` starts in a nested nonexistent path; the real simulator reproduced the original error.
- Next time: test a fresh installation path, not only `t.TempDir()` or an already-running simulator's data directory.
- Status: active

## 2026-11-04 - Missing shared Go build-cache files
- Scope: build, gomobile, verification
- Trigger: gomobile/link/vet reported missing `~/Library/Caches/go-build/*-d` files during repeated builds.
- Approach: use an isolated cache (`GOCACHE="$(mktemp -d /tmp/gofastr-native-build-cache.XXXXXX)"`) instead of changing dependencies or clearing the global cache.
- Evidence: the real UIKit framework/Swift build completed using `/tmp/gofastr-native-build-cache.IUXdIL`.
- Next time: isolate `GOCACHE` for the command and rerun before attributing missing-cache errors to source changes.
- Status: active

## 2026-11-04 - App screens are Go, platform rendering is library-owned
- Scope: native, architecture, ios
- Trigger: application-owned Swift screens violated the intended GoFastr-like authoring model.
- Approach: compose `nativeui` Go definitions/actions in `examples/notes/notesscreens`; select the reusable UIKit renderer with `--renderer native`.
- Evidence: `TestGoDefinedNativeScreenFlow`, `TestNotesApplicationHasNoPlatformScreenSource`, `TestBundledGoNativeRenderer`; checkpoint `82e936f` preserves the earlier implementation.
- Next time: add reusable native primitives to the library and app composition in Go; never introduce app Swift/Java templates or force mobile definitions onto web/TUI/canvas.
- Status: active

## 2026-11-05 - Verify the complete mobile document hierarchy
- Scope: mobile, design, renderer, acceptance
- Trigger: accumulated feature controls and attachment metadata made Notes look like an API demonstration rather than a document app.
- Approach: follow [mobile document design](mobile-design.md); keep document/detail/file-preview semantics in Go and reusable styling in the adapters. Update SSR, live replacement rows, and refreshed attachment cards together.
- Evidence: `sim/design_test.go`, `sim/components_test.go`, `sim/files_test.go`, and `sim/photos_test.go` exercise real host screens, metadata, previews, and menu bounds. Isolate browser state per scheme: root navigation can restore the last tab route.
- Next time: inspect screenshots in light/dark and large text; center offscreen rows before real menu clicks, assert menus clear fixed tabs, and suppress compatibility taps after cancelled/vertical drags too. Keep Swift wire DTOs Foundation-only, separate from UIKit views.
- Status: active
