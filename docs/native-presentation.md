# Native Notes presentation

This prototype uses **real UIKit controls**, not HTML styled to resemble them.
The native presenter calls a typed Go binding directly. It never creates a
WebView, requests an HTML screen, or interprets a shared widget tree. GoFastr
remains web-first; no GoFastr source changes or production dependencies are
required.

## Ownership

| Layer | Owns |
| --- | --- |
| `examples/notes/notes` | Typed application behavior, validation, and a repository contract |
| `examples/notes/notesstore` | Temporary adapter to GoFastr's owner-scoped CRUD pipeline |
| `examples/notes/notesnative` | Installation identity, lifecycle, and gomobile-compatible command/data snapshots |
| `examples/notes/native/ios/App.swift.tmpl` | UIKit screens, navigation, drafts, native capabilities, and platform appearance |
| `cmd/gofastr-mobile` | Binding, compiling, packaging, and launching the selected native host |

The web, terminal, and UIKit presenters share behavior and persisted Notes, not
screen definitions. UIKit owns its native navigation stack; there is no browser
history or parallel Go screen stack to synchronize. A future Android, desktop,
canvas, or TUI presenter should choose its own controls and navigation instead
of implementing a UIKit-shaped renderer interface.

**Bootstrap limitation:** `notesnative.Start` still calls `notesapp.Build` and
starts the existing in-memory `mobilehost`. This initializes the same entity,
database, hooks, and owner scopes, but also mounts an unused web presentation.
Native commands do not use that HTTP transport or render its screens. This is
not an extracted host-neutral/headless GoFastr lifecycle. That lifecycle is the
next demonstrated upstream gap, not something to conceal behind a renderer API.

## Run

On a Mac with the existing Xcode toolchain:

```sh
go run ./cmd/gofastr-mobile run --platform ios-sim \
  --pkg ./examples/notes/notesnative \
  --host-template ./examples/notes/native/ios/App.swift.tmpl \
  --id dev.gofastr.notesnative --name 'Native Notes' \
  --out dist/mobile/ios-native
```

The separate bundle ID keeps this app and its data apart from the WebView demo.
Use `build` to stop after packaging or `--dry-run` to print the steps without
writing output. `--device` selects a simulator. The CLI does not yet provision
physical iPhones.

### Caller-owned host templates

`--host-template` is an optional build/run flag. It replaces **only** the host
source: `App.swift` for `ios-sim`, or `MainActivity.java` for `android-emu`.
The regular generated Info.plist/AndroidManifest and build pipeline remain in
use. Without the flag, the existing WebView hosts and binding contract are
unchanged.

The file must be a regular file no larger than 1 MiB; relative paths resolve
from the current directory. It uses Go `text/template` with the CLI's existing
application metadata, including `.Prefix` (the generated binding name), `.ID`,
and `.Name`. The example uses `.Prefix` for imports and bound function names.
The file is trusted native source compiled into the app, not downloaded code
or a runtime plugin. Select a template matching the platform and `--pkg` API;
the compiler checks that contract. There is no automatic translation between
the default HTTP bridge API and a custom native API.
Custom-host launch output omits WebView inspector instructions; debugging
capabilities belong to the chosen host.

This is deliberately a one-file prototype seam, not a general native resource,
entitlement, dependency, or project-manifest extension system. The Java override
uses the same CLI seam, but no native Android Notes presenter is included.

## Typed command boundary

`notesnative` exports `Start(dataDir)`, `Stop()`, `List(search)`, `Get(id)`, and
`Save(id, title, body)`. `Save` creates when the ID is empty and updates otherwise.
`Note` contains data strings; `NoteList.Count`/`Item` expose a bounded snapshot
without JSON or a platform collection dependency. `List` currently caps results
at 200, as does the shared service.

Call startup and CRUD off the platform UI thread. The UIKit adapter runs them
on a serial background queue and delivers results on the main queue. It ignores
stale search responses. Go holds lifecycle locks for in-flight operations;
`Stop` waits before shutdown. Commands use a 30-second context and shutdown a
10-second context. The binding, not the screen, supplies installation identity.
Validation, hooks, and authorization remain in the shared Go service/repository.

## What is native

- `UINavigationController` large titles, push/pop, and the clean editor's native
  interactive Back gesture. Dirty/saving editors disable that gesture so it
  cannot bypass the explicit draft decision.
- An inset-grouped `UITableView`, native search/no-results, and `UIRefreshControl`.
- A `UITextField` title and `UITextView` body, system selection/input behavior,
  and `keyboardLayoutGuide` for the editor's available height.
- Semantic system colors, system text styles, Dynamic Type participation,
  accessibility labels/IDs, and a successful-save announcement.
- A user-initiated `UIActivityViewController` share sheet and a native information
  sheet with medium/large detents. Merely opening Share sends nothing.
- Selection and save/error haptic calls. Simulator execution is not evidence of
  physical haptic feel.

Done is an explicit commit. Save errors preserve text; successful saves return
to the list. Unsaved Back offers Keep Editing, Keep Draft, or Discard Changes.
Drafts are keyed by note ID (one slot for a new note) in local `UserDefaults`
and restore after process restart. They are convenience drafts, **not encrypted
secret storage, database autosave, multi-account isolation, or conflict-aware
sync**. Saving/discarding clears the applicable draft; keeping it does not
create or update a persisted Note. The editor disables changes during a save.

Presentation and application code ship in the app bundle. This slice uses local
data only. Generated typed repositories can replace `notesstore` later without
moving UI decisions into those repositories or introducing remote screens.

## Verification and limits

```sh
go test ./...
GOWORK=off go test ./...
go vet ./...
```

The native command acceptance tests exercise first startup into a nonexistent
directory, validation, edit/search, restart persistence, snapshot bounds,
installation isolation, and a native-created note opened by the web and terminal
presenters. CLI tests render the native template, protect the unchanged plist,
reject invalid template paths, and verify dry-run behavior. They do not pretend
to execute UIKit.

On November 4, 2026, the real gomobile framework and Swift app were compiled,
signed, installed, and driven on an iPhone 17 Pro simulator running iOS 26.5.
Observed workflows: empty list, blank-title error, create/save/reopen, native
search/no-results, dirty Back/Keep Draft, draft restore after process restart,
system keyboard with visible editing area, opening/dismissing Share without
sending, and expanding the native information sheet from medium to large.
Light/dark appearance and list/editor layouts at the largest accessibility text
category were also inspected; this is a spot check, not full accessibility QA.
Local screenshots under `/tmp/gofastr-native-*.png` are actual running-app
evidence, not design mockups or checked-in golden images.

During final verification, shared Go cache files disappeared mid-build. The
native rebuild passed with an isolated `GOCACHE` under `/tmp`; use a fresh cache
directory if the same missing-cache-file error occurs, rather than changing
dependencies or clearing another checkout's cache.

Not yet complete: VoiceOver and Switch Control, the full accessibility text/
orientation matrix, real-device IME/haptics, physical-iPhone signing, Android
native UI, or conflict/background synchronization. This machine has no configured
Android SDK or JDK, so an uncompiled native Android presenter is intentionally
not shipped. This is a working Notes slice, not a native UI toolkit or a claim
of store readiness.
