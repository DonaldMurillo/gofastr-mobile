# Debugging native-renderer apps

This page covers three read-only tools for the `nativeui` Runtime:

- the trace observer, which records one redacted event per command;
- `Runtime.Inspect`, a redacted snapshot of live connections and screen sessions;
- `gofastr-native inspect`, which opens every reachable screen under assumed renderer profiles.

None of them is on by default, and none of them changes a reply. Each section below says what it records and what it leaves out.

## Trace observer

Set `RuntimeOptions.Observer`. A nil observer, the default, skips all trace work.

```go
logger := devtrace.NewLogger(os.Stderr, devtrace.DefaultSize)
defer logger.Close()
runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{Observer: logger.Observe})
```

The Runtime calls the observer once per command, after the reply is final and after it has released every runtime and session lock. The contract:

- The observer runs on the command's goroutine, so it must not block. `devtrace.Logger` hands events to a bounded queue drained by its own goroutine and drops events when the queue is full. `Dropped()` and `WriteFailures()` count both cases.
- The observer may call `Runtime.Inspect`. It must not call `Handle` or `CommandJSON` on the same Runtime.
- The Runtime recovers an observer panic. The reply is unchanged, and nothing is retried or re-read.
- Each event is a deep copy. The observer may keep or change it.

`devtrace.Recorder` keeps the last N events in a ring (at most `devtrace.MaxSize`) for tests and in-process tools. `Events()` returns copies, and `WriteJSONL` writes them one per line.

### What an event holds

`nativeui.TraceEvent` holds only:

- the command kind, or `other`/`invalid`;
- the renderer name from the negotiated contract;
- the registered route pattern (`/notes/{id}`), never the concrete route or its query;
- the action ID if the route declares it, else `undeclared`;
- the reply code, or `other` for a code the Runtime does not know;
- field-error keys the screen declares, plus a count of undeclared keys;
- the first capability an `open` was refused for;
- for a rejected screen definition, the route pattern and a fixed part name such as `row-destination`;
- up to four Go type names from an unexpected error's unwrap chain, for example `*fs.PathError>syscall.Errno`;
- editor flags, foreground kind, an accepted receipt's capability and status, effect names, and the changed flag;
- 12-hex salted hashes for correlation and instance.

It never holds titles, field values, messages, `err.Error()` text, concrete routes, query values, file names, paths, bytes, or raw connection, instance and request IDs. `DefinitionError.Error()` is never copied into a trace either, only its pattern and part.

The salt is random per Runtime, so hashes join the events of one request inside one process and cannot be linked across restarts or Runtimes.

Event timestamps and durations use `time.Now`, not `RuntimeOptions.Now`. The injected clock drives expiry policy and tests often freeze it. A frozen trace clock would make every duration zero.

### Line format

`devtrace.Format` prints one line per event. Empty fields are left out, and field order is fixed:

```
gofastr-trace seq=4 kind=action renderer=uikit route=/notes/{id} action=save corr=8a323fd2a53c inst=4fc5562df996 outcome=validation fields=title dur=996µs editor=dirty
gofastr-trace seq=9 kind=open renderer=webview route=/device/protected outcome=unsupported capability=privacy/v1 dur=0s
gofastr-trace seq=12 kind=ack renderer=android route=/feedback/effects/{effect} outcome=ok dur=0s receipt=haptics/v1:completed
```

The full key order is `seq kind renderer route action corr inst outcome fields undeclared-fields capability definition cause dur editor foreground receipt effects changed`. `definition` prints as `route#part`.

### Turning it on in an app build

Apps opt in with a build tag, so release builds carry no trace code. Notes and the gallery follow the same pattern:

- `debugtrace_off.go` (`//go:build !gofastr_debugtrace`) returns a nil observer.
- `debugtrace_on.go` (`//go:build gofastr_debugtrace`) returns `Logger.Observe` for one process-wide logger that writes to `os.Stderr`.
- The `Start` function passes `Observer: debugObserver()` to `NewRuntime`.

Build with `-tags gofastr_debugtrace` to enable it. `go test -tags gofastr_debugtrace ./examples/notes/notesnative` runs a privacy test that drives Notes with canary values and fails if any canary, the data directory, a drive path or a raw ID reaches the log.

The CLI passes the tag with `--debug-trace` on `build`, `run`, and `dev`:

```sh
go run ./cmd/gofastr-native build --platform android --renderer native \
  --pkg ./examples/gallery/gallerynative --debug-trace
```

The flag requires the native renderer and is rejected with `--profile release`.
It enables logging only in bindings that supply the observer hook, such as
Notes and the gallery. Release binds explicitly select `gofastr_release`,
replacing ambient build tags so trace hooks and inspection exports stay out.
See [release builds](release.md).

### Reading the log on a device

NOT RUN. No device or simulator was used to check where gomobile routes Go's stderr. gomobile is expected to forward stderr to the system log under the tag `GoLog`, which would make these the commands to try:

```
adb logcat -s GoLog | grep gofastr-trace
xcrun simctl spawn booted log stream --predicate 'eventMessage CONTAINS "gofastr-trace"'
```

Treat both as unverified until someone runs them on a device.

## Runtime.Inspect

`Runtime.Inspect()` returns a `RuntimeSnapshot`:

- connections, each with renderer, protocol version and negotiated capabilities;
- screen sessions, each with hashed instance, route pattern, renderer, editor flags, foreground kind, protection and live flags, and counts of retained actions by settlement state (unfinished, unsettled, unknown).

The redaction rules match the trace. A session's `Instance` equals `TraceEvent.Instance` for the same Runtime, so you can join the two.

`Inspect` never holds the runtime lock and a session lock together, and it tries each session lock without waiting. A session busy in a long action comes back with `Busy: true` and only its instance filled. It does not expire sessions, touch idle clocks, call the configured clock or run app code. You can call it from an observer or a debug endpoint while actions run.

`render/webview.Renderer.Runtime()` exposes the WebView renderer's Runtime for this.

## Metadata without a Runtime

These calls run no application code:

- `App.Describe()` lists registered route patterns in registration order, each with its sorted action, presentation, file and service names and a live flag.
- `App.RoutePattern(destination)` maps a concrete destination, IDs and query included, to its registered pattern.
- `nativeui.Outline(app, screen)` reduces a rendered screen to its structure: requirements, action IDs, field keys and kinds, row and image counts, tab routes, list controls. It drops titles, labels, values, row IDs, unregistered action IDs and unregistered destinations.
- `nativeui.RendererContract(ctx)` returns the negotiated contract inside a Runtime command, and false elsewhere, for example in the WebView's server-rendered first paint.

Screen-definition failures are `*nativeui.DefinitionError` values. They carry the route pattern and a fixed part and unwrap to the original sentinel (`ErrScreenNotFound`, `ErrInvalidRoute`), so `errors.Is` keeps working.

## gofastr-native inspect

```
gofastr-native inspect --pkg ./examples/gallery/gallerynative --profile webview
```

The command:

1. Runs `go env GOMOD GOWORK`, then `go list -mod=readonly -tags gofastr_inspect -json` (`-mod=vendor` when the main module vendors) to find the package. It checks from source that the package exports `Start`, `Stop`, `Root` and `Command`. A package without `Command` exits 2 with "inspect needs the versioned Command export". A package whose imports need a `go.mod` or `go.sum` change exits 1. inspect never fixes the module for you.
2. Generates a small harness program and builds it with `go build -mod=readonly -tags gofastr_inspect -overlay` into a temporary directory. The harness directory exists only in the overlay, so the module tree, `go.mod` and `go.sum` are never written. The explicit `-mod` flag on both `go list` and `go build` overrides a `GOFLAGS=-mod=mod` setting.
3. Runs the harness binary with the plan on stdin and its stdout and stderr connected to the null device. It calls `Start` with an empty temporary data directory, then walks rows and tabs breadth-first from `Root()` (or `--route`) for each profile, up to `--depth` hops and `--max-screens` screens. Disabled rows are skipped.
4. Calls `Stop`, reads the result file the harness wrote, and removes the temporary directory, after both success and failure.

The harness sends only `hello`, `open` and `close`. Its single send function refuses any other kind, so no action, checkpoint, effect receipt or device service runs.

### Not a sandbox

App code runs with your permissions. `Start`, package `init` functions, screen render functions and `OnClose` hooks execute in the harness process, and they can have any side effect the app's code has: network calls, writes outside the data directory, spawned processes. The working directory is the temporary directory, which hides relative writes but stops nothing.

inspect discards what the app writes to stdout, stderr and the standard `log` package, and never relays app error messages or panic values. It does not stop the app from logging through other channels, such as a file, a socket or the system log. Because app code shares the harness process, it could also tamper with the harness result. inspect checks every field of the result against the redaction rules below and discards a result that breaks them, but that check is not a security boundary.

### What the report holds

The report is redacted by default, and there is no option to turn redaction off. It never contains titles, prompts, text, labels, field values, row titles or IDs, field or list keys, concrete routes, query strings, raw connection, instance or request IDs, file names, the data directory path, app output, app error messages or panic values.

Screens are opaque IDs, `s1`, `s2` and so on, assigned in discovery order. One concrete route keeps one ID for the whole report, so the same screen has the same ID in every profile and a refusal under `webview` lines up with the `ok` under `uikit`. IDs are not stable across runs. A concrete route never leaves the harness process.

Each screen has its ID, the ID it was reached from, its depth and a status (`ok`, `refused` or `empty`). A refusal carries its code and, for `unsupported`, the capability. Codes outside nativeui's known set print as `other`, and capability names outside the built-in set are dropped. An opened screen carries a structural outline:

- content kind: list, form, file-preview, text, components or none;
- counts of rows, disabled rows, filters, navigation actions, tabs, components, images and attachments;
- field counts by kind (text, multiline, toggle, date, time, select, or other) and the number of required fields;
- flags for protected, live, draft, search, selection, paging and refreshable;
- required capabilities from the built-in set;
- links: the opaque IDs of tabs and enabled rows, up to 64 per screen, with a count of the rest.

### Route patterns need InspectApp

A concrete route such as `/notes/8f2c?tab=history` does not say which registered pattern served it, and stripping the query does not either. inspect does not guess. To get patterns, export the app's registry to inspect:

```go
//go:build gofastr_inspect

package gallerynative

import "github.com/DonaldMurillo/gofastr-native/nativeui"

func InspectApp() *nativeui.App {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	return screens
}
```

inspect builds with the `gofastr_inspect` tag, so gomobile and release builds never see this export. With it, the harness calls `App.RoutePattern` on each concrete route and `nativeui.Outline` on each screen, and the report adds:

- `pattern`: the registered pattern (`/notes/{id}`);
- `actionIds` and `rowActionIds`: action IDs the route registered;
- `componentNames`: compiled component extensions with counts;
- `destinations`: registered patterns the screen links to.

Field and list keys stay out even then: no registry declares them, so they could carry data. `InspectApp` with another signature is a usage error. The gallery exports it in `examples/gallery/gallerynative/inspect_on.go`. Notes does not, so its report uses opaque IDs only.

### Profiles

The profiles are assumptions taken from renderer source, not device measurements, and the output says so.

| Profile | Renderer | Assumed capabilities |
|---|---|---|
| `uikit` | uikit | navigation, editor, drafts, share, haptics, sheets, announcement, presentation, files, live, privacy. No services or extensions. |
| `android` | android | The uikit set plus sharing, clipboard, notifications. Audio, incoming shares, secure storage and widgets unavailable (build flags off). |
| `webview` | webview | navigation, editor, drafts, announcement, presentation, live. Sheets, share and files as fallback. Haptics, photos and camera unavailable. No services host, so no clipboard, sharing or privacy. |
| `minimal` | webview | navigation and editor only. |

`--profile all`, the default, runs all four.

### Output and exit codes

Text output prints one line per screen: status, ID, pattern if known, depth, parent and either the outline or the refusal code. `--json` prints the profiles, per-screen results and any failure. `--dry-run` prints the package and profiles and builds nothing.

When the harness stops early, the report names a fixed phase (`start`, `inspect-app`, `root`, `hello`, `open`, `close`, `stop`, `harness` or `result`), the profile and screen ID if any, and a cause: `panic`, a fixed harness word, or up to four Go type names from the error's unwrap chain, such as `*fs.PathError>syscall.Errno`. It never prints the error text. Partial results up to that point are kept.

A harness build failure prints `go build`'s own diagnostics, capped at 64 KiB, with temporary paths replaced by `<harness>`. The build runs no app code, so those lines hold compiler messages about source, not runtime content.

Exit 0 means the walk finished, even with refusals. Exit 1 means resolution or the build failed, the harness stopped early, or the timeout hit. Exit 2 means a usage error.

The subcommand lives in `cmd/gofastr-native/inspect_command.go`, and the harness template in `inspect_harness.txt`.
