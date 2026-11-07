# Getting started

This page goes from an empty directory to a running app, then covers the
edit loop. It assumes a Go toolchain and, for Android, the SDK pieces that
`doctor` lists.

## Check the toolchain

```sh
go run ./cmd/gofastr-native doctor --platform android-emu
```

Run it from a gofastr-native checkout. `doctor` only reads: it runs
version queries and looks for files. It never installs anything, edits
your environment, or starts the adb server. Each line is one of:

| Mark | Meaning |
|---|---|
| `[ok  ]` | Present. |
| `[MISS]` | Missing. Doctor exits 1 and prints a `fix:` line. |
| `[warn]` | Works, but something looks wrong, such as `ANDROID_HOME` and `ANDROID_SDK_ROOT` naming different SDKs. Never changes the exit code. |
| `[info]` | Context: where the SDK root came from, the NDK version, emulator acceleration, which gofastr-native the module uses. |

The SDK fix text matches your shell. On Windows it shows `$env:ANDROID_HOME`
for the current PowerShell session and `[Environment]::SetEnvironmentVariable`
for your user account. Doctor prints these; it does not run them.

`doctor --devices` also lists attached devices and whether each emulator has
finished booting. That runs `adb devices`, which starts the adb server, so it
is opt-in.

## Create an app

```sh
go run ./cmd/gofastr-native init --out ../hello
```

`init` refuses to write into anything that exists. The parent of `--out`
must exist, and `--out` itself must not. It validates every flag before it
creates a file. If a write fails partway, it removes what it created.

| Flag | Default |
|---|---|
| `--module` | `example.com/<package>` |
| `--package` | the base name of `--out`, lowercased, letters and digits only |
| `--id` | `dev.gofastr.<package>` |
| `--name` | the package name, capitalized |
| `--native` | the gofastr-native the current module uses |

The package name must be a Go identifier that is also legal in a Java
package, so `class`, `func`, `screens`, and `main` are rejected. The id
follows the same rules as `build --id`.

`--dry-run` lists the files and the `go mod tidy` it would run, and writes
nothing.

### Which gofastr-native the app uses

gofastr-native has no published version yet. Until it does, the generated
`go.mod` points at a local checkout with a `replace` directive:

```
require github.com/DonaldMurillo/gofastr-native v0.0.0-00010101000000-000000000000
replace github.com/DonaldMurillo/gofastr-native => /path/to/gofastr-native
```

The app's `README.md` names the checkout and says how to switch to a
published version later:
`go mod edit -dropreplace github.com/DonaldMurillo/gofastr-native`, then
`go get github.com/DonaldMurillo/gofastr-native@<version>`. `--native` takes
either a checkout directory or a version such as `v0.2.0`.

The `go.mod` also pins the CLI, `gomobile`, and `gobind` with `tool`
directives. `go tool gofastr-native` then runs the CLI built from the same
source as the library, so the two cannot drift apart.

### Offline

After writing the files, `init` runs `go mod tidy` with `GOWORK=off`. Offline,
tidy can fail on test-only dependencies of your dependencies that the module
cache does not hold (for example `github.com/google/go-cmp`, needed only by
`golang.org/x/tools` tests). `init` keeps the files and says so. Run
`go mod tidy -e` in the new directory: it records what the cache holds, and
the app still builds and its tests pass. Run plain `go mod tidy` once you are
online.

## What init creates

| Path | What it is |
|---|---|
| `screens/screens.go` | A list of items, an editor with a required title and a multiline body, and an About screen. |
| `screens/screens_test.go` | Every route renders, every action dispatches, an empty title is rejected without writing, and saved items survive reopening the store. |
| `<package>/<package>.go` | The gomobile bind package: `Start`, `Stop`, `Root`, `Render`, `Dispatch`, `Command`. |
| `<package>/<package>_test.go` | Lifecycle rules, and a checkpointed draft surviving `Stop` and `Start`. |
| `go.mod`, `README.md`, `.gitignore` | `dist/` is ignored. |

Items are stored through `device/files`, which restricts the directory to the
current user and writes each record to a synced temporary file before
renaming it into place. Drafts use `behavior.NewFileDraftStore`. Both live in
the app's data directory. The starter store holds up to 63 items, one file
each; replace it when you outgrow it.

`examples/starter` in this repository is the same scaffold, rendered with
the module path `github.com/DonaldMurillo/gofastr-native/examples/starter`.
A test fails if the two drift apart.

## Run it

From the new directory:

```sh
go test ./...
go tool gofastr-native run --platform android-emu --renderer native --pkg ./hello
```

On a Mac with Xcode, use `--platform ios-sim`.

## The edit loop

```sh
go tool gofastr-native dev --platform android-emu --renderer native --pkg ./hello
```

`dev` builds, installs, and launches the app once, then polls the Go packages
the bind package imports. After a change it rebuilds, reinstalls, and
restarts the app. It is not hot reload.

What survives a restart:

- Saved items. A reinstall keeps the app's data directory.
- Drafts the renderer has checkpointed. Before each reinstall, `dev` sends
  the app to the background (the HOME intent on Android, opening Settings
  on the simulator) so open editors checkpoint, then waits 1.5 seconds.
  Nothing confirms the checkpoint finished. Edits newer than the last
  completed checkpoint can be lost. `--no-checkpoint` skips the step.

What does not survive: navigation, which starts again at the root screen,
and anything in memory.

How rebuilds are scheduled:

- A change starts a 400ms quiet period (`--settle`). A file that keeps
  changing still gets a build after 3 seconds.
- Saves made while a build runs cause exactly one more build, however many
  there were.
- `dev` reruns `go list -deps` after each build, so a new file or a new
  imported package joins the watch set. The module cache, `dist/`, `vendor/`,
  `testdata/`, hidden files, and editor swap files are not watched.
- `//go:embed` patterns are rechecked on every poll. Adding, editing, or
  removing a file a pattern embeds starts a build, including files in new
  subdirectories of an embedded directory. Hidden files count only under an
  `all:` pattern. Symbolic links are not followed, and directories named
  `dist`, `vendor`, `node_modules`, `testdata`, or `.git` inside an embedded
  directory are skipped.
- Changes to the gofastr-native CLI, its renderers, or host templates are
  compiled into the running `dev` and need a `dev` restart.

What `dev` does when something fails:

- A compile error or, with `--test`, a failing test: nothing is installed,
  and the app on the device keeps running the previous build.
- The package manager rejects the install: the previous build stays
  installed. `dev` does not retry; save a change to try again.
- `INSTALL_FAILED_UPDATE_INCOMPATIBLE` (the installed app was signed with a
  different key): `dev` stops. Only an uninstall fixes that, and an
  uninstall deletes the app's data, so `dev` prints the command and leaves
  the decision to you.
- Ctrl+C during an install: the outcome is unknown. `dev` runs one read-only
  check (`dumpsys package` on Android, `simctl get_app_container` on the
  simulator) and reports what it sees. The device may still finish an
  install it had already received.

`dev` never uninstalls, clears app data, force-stops, or retries an install.

`dev` resolves the device once, at start, and names it on every install. A
second emulator that boots later is never picked. `--device` pins one
explicitly.

`--platform go` runs `go vet` and `go build` on the bind package after every
change, with no device. Add `--test` to also run `go test ./...` in the
module; on a device platform, `--test` runs the tests before each build and a
failure skips the install.

## Limits

- `dev` targets the Android emulator and the iOS simulator. Physical devices
  and release builds go through `build` and `run`; see [release builds](release.md).
- Watching is polling: every 500ms by default (`--poll`), with no file-system
  events.
- The starter store keeps the last of two concurrent saves to the same item.
