# Mobile application lifecycle: M3

## Gate A — UI-independent startup

Native Notes composes the same entity, owner-scoped repository, service and
installation data as both web presentations, without mounting a `UIHost` or
importing `notesweb`, `mobileui`, or `render/webview` into the native binding.
This is **UI-independent startup, not HTTP-free or listener-free startup**.

- `examples/notes/notesapp/application.Open(dataDir)` constructs the shared
  `Application`: `App`, `Data`, and `Service`. It does not choose a presentation,
  install identity middleware, initialize plugins, migrate, or start the app.
  The absolute directory is opened once through `localapp.Open`, which creates
  private first-launch files and reuses the persisted identity and session
  secret. `Data.User` is the identity for authorized native service contexts.
- The application registers `Data.Close` as its first stop hook. Later hooks
  and battery cleanup run while the database is still open. A caller abandoning
  construction calls `App.Shutdown`; after host startup, use `Host.Stop`.
  `WithDB` alone does not transfer database ownership to GoFastr.
- `mobilehost.New(app, mobilehost.Config{Data: shared.Data, ServiceOnly: true})`
  explicitly omits the WebView script route, script injection, and `UIHost`
  requirement. It still installs the local identity middleware, respecting a
  foreign authenticator, and starts the real `App.Serve` on a `memory` listener
  made of `net.Pipe` connections. It opens no TCP socket or loopback port.
- `mobilehost.New` defaults are unchanged: without `ServiceOnly`, a mounted
  `uihost.UIHost` is required at startup and the bridge is installed normally.
  `notesapp.Build`, `BuildWithPlatform`, `BuildScreensWithPlatform`, and
  `notesapp.Service` retain their existing signatures and presentations.
- Native `Start` rejects a foreign owner extractor before opening installation
  data. It publishes command state only after startup readiness. Native
  `Render`, `Dispatch`, `List`, `Get`, and `Save` call Go screens or the shared
  service directly, never HTTP endpoints. The service adapter retains the same
  owner scopes, validation, hooks, not-found behavior and persisted data as HTTP.

### One lifecycle owner

`Host.Start` delegates to the public `App.Serve` lifecycle. The current public
pin and active workspace perform migration/entity seeding, app seed hooks,
plugin/battery initialization, a second migration pass for schema registered
during initialization, battery/application start hooks, readiness and handler
composition. `App.Shutdown` owns cancellation, request/stream drain, reverse
battery stop and stop hooks. Mobile code does not reproduce those phases.

`InitPlugins` alone is not a replacement: it initializes registrations but does
not provide migration/seeding, start hooks, serving readiness, or complete
startup-failure handling. Nor does `RunWithSignals` initialize an application;
it waits for signals and invokes shutdown. Mobile disables process signal
handling because the OS host owns its lifecycle.

Host completion is a closed, repeatably observable channel, not a consumed
error receive. A failed `Serve`, including an error before GoFastr establishes
its lifecycle context, is followed by idempotent `App.Shutdown` before Start
reports completion. Startup timeout requests bounded shutdown. `Stop` closes
the in-memory transport, closes idle client connections, joins Serve within
the supplied context and reports deadline errors. Repeat/concurrent stops share
their result. A Host/App is one-shot; restart constructs a fresh pair over the
same data directory.

Host shutdown has one owner: either the first `Stop`, or the Serve-return path
if the server exits unexpectedly. Listener closure during a normal Stop does
not start a second, shorter shutdown budget. Long requests such as live watches
drain against the Stop caller's context rather than producing an unrelated
five-second follower timeout. The HTTP-boundary regression is
`TestStopDrainsLongRequestWithOneShutdownOwner` in `mobilehost/lifecycle_test.go`.

The Host also owns the bridge-request context. Stop cancels outstanding
native-channel requests before HTTP drain, so abandoned live polls cannot
consume the entire shutdown deadline. Cancellation does not imply that an
already-started write failed; no action is replayed. Direct `Do`/`Open` callers
keep their own contexts and graceful-drain behavior. Storage closes only after
request handlers drain; `TestStopCancelsOutstandingBridgeRequestsBeforeClosingStorage`
exercises that boundary.

### Native command admission and drain

Native entry points acquire a service/screen/identity snapshot and increment
an active-operation counter under the lifecycle mutex. They release that mutex
before running Go application code or waiting for a capability callback, and
decrement the counter on every return. Each operation has a 30-second context.

`Stop` closes admission first, rejects new commands and overlapping Start, and
waits for all admitted operations without holding the business/lifecycle lock.
Only then does it invoke Host.Stop with a 10-second shutdown budget and discard
state. Concurrent Stop calls join the same result. An already-admitted callback
can finish; a callback attempting a new command after stop admission closes
receives an error instead of deadlocking behind a held lifecycle lock.

This is not an unconditional hard wall-clock bound: Go commands and platform
capabilities must honor their contexts and return. A command ignoring its
context can delay the admission drain; the 10-second host shutdown budget starts
after that drain. Code running inside an admitted operation must not synchronously
call `Stop`, because Stop necessarily waits for that operation itself.

## Gate B — intentionally not complete

There is no complete public listener-free equivalent of `App.Serve` in the
currently pinned GoFastr API. Gate A therefore deliberately retains an explicit
temporary in-memory Serve adapter, an HTTP server and its transport/client.
Service-only mode does not remove framework HTTP routes or HTTP dependencies;
it only removes the unused web presentation and WebView bridge. No GoFastr core
change is made or required for Gate A.

The minimal upstream proposal is a public lifecycle operation that performs
the same pre-serving pipeline, including schema registered by plugins, app
lifecycle context, startup hooks, readiness and failure rollback, then pairs
with the existing idempotent Shutdown without creating a server/listener. That
operation must specify concurrent startup/stop and startup-failure guarantees.
Do not substitute manual `InitPlugins`/migration calls or a worker-role setting
for this contract. Until such a seam exists and is tested, do not claim Gate B.

## Regression checks and implementation notes

The acceptance boundaries are real SQLite installation files, native exported
commands/screens, the service's scoped CRUD adapter and requests through the
in-memory HTTP server. Coverage includes fresh nested directories and file
modes, migration of an existing Notes table, identity/secret persistence,
schema declared during battery initialization, seed/start order, migration,
seed, initialization and start failures, timeout cancellation, database cleanup,
repeat shutdown/restart, foreign/anonymous authorization, forged HTTP ownership,
service/HTTP interoperability, legacy web builders and native operation drain
with a reentrant callback. Dependency checks prevent web adapters returning to
the native bootstrap.

Next time: inspect `go.work` before auditing upstream APIs; the sibling checkout
may not be the selected core. Check both the selected workspace and the public
pin, and keep application construction separate from lifecycle execution.
Use the admission helper for new native commands so asynchronous waits cannot
reintroduce a lifecycle lock held across business code.

```sh
export GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL
go test ./mobilehost ./examples/notes/...
go test -race ./mobilehost ./examples/notes/...
GOWORK=off go test ./mobilehost ./examples/notes/...
GOWORK=off go test -race ./mobilehost ./examples/notes/...
GOWORK=off go vet ./mobilehost ./examples/notes/...
```

These checks do not establish renderer pixel quality, capability negotiation,
Android native support or listener-free startup. Those have separate gates.
