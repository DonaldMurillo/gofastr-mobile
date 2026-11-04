# Implementation notes

## 2026-11-04 - External presentation boundary
- Scope: presentation, mobile, terminal
- Trigger: native-looking mobile screens must not turn GoFastr into a cross-platform widget framework.
- Approach: keep application services typed and UI-free; let external packages own screens, CSS, and behavior through public registry APIs.
- Evidence: `go test ./examples/notes/notesapp -run 'TestTerminalAndWebSharePersistedNotes|TestPresentationCoreDoesNotImportWeb'`; `mobileui/`; `docs/presentation-prototype.md`.
- Next time: inspect `go.work` before surveying GoFastr, and run `GOWORK=off go test ./mobileui ./examples/notes/...` to verify the public pin too.
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
- Approach: use typed direct commands for UIKit and document the unused web bootstrap instead of inventing a widget IR or claiming an extracted headless lifecycle.
- Evidence: `examples/notes/notesnative/notesnative.go`, `docs/native-presentation.md`.
- Next time: extract host-neutral lifecycle from the proven native command flow; keep authorization context outside renderers.
- Status: active

## 2026-11-04 - Caller-owned native host and gomobile names
- Scope: native, ios, binding
- Trigger: a native presenter must not implement HTTP/HTML just to satisfy the default WebView shell.
- Approach: select trusted compiled source with `--host-template`; keep plist/manifest/build defaults; inspect generated Objective-C names before writing Swift (`Note.ID` binds as `id_`).
- Evidence: `cmd/gofastr-mobile/native_test.go`, `examples/notes/native/ios/App.swift.tmpl`, `docs/native-presentation.md#run`.
- Next time: compile/link/run the real bound host; string-template tests alone cannot prove the Swift/Go contract.
- Status: active

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
