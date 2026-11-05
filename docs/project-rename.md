# gofastr-native: project rename

The project is renamed from `gofastr-mobile` to `gofastr-native` on November 5,
2026. The name reflects Go-authored screens, reusable behavior, and typed device
services with interchangeable renderers. WebView remains supported alongside
UIKit; the rename does not claim a desktop renderer or native Android support.

## Updated names

| Surface | New name |
|---|---|
| GitHub repository | `DonaldMurillo/gofastr-native` |
| Go module | `github.com/DonaldMurillo/gofastr-native` |
| CLI source | `cmd/gofastr-native` |
| CLI executable | `gofastr-native` |
| Local checkout | `/Users/dom/programming/gofastr-native` |

From the renamed checkout:

```sh
go run ./cmd/gofastr-native --help
go run ./cmd/gofastr-native build --platform ios-sim \
  --pkg ./examples/notes/notesnative --renderer native
go run ./cmd/gofastr-native build --platform ios-sim \
  --pkg ./examples/notes/notesmobile --renderer webview
```

Downstream applications must update their module requirement and imports from
`github.com/DonaldMurillo/gofastr-mobile/...` to
`github.com/DonaldMurillo/gofastr-native/...` together. Build scripts must use the
new command path/name. The old Go module and CLI path are not compatibility
aliases. Renaming the hosted repository alone does not publish the local source
changes: commit/push and version publication remain separate operations.

## Deliberately unchanged

- Package names such as `nativeui`, `mobileui`, and `mobilehost` retain their
  existing roles and exported APIs.
- WebView bridge script path `/__gofastr-mobile/bridge.js` and MessagePort hello
  `gofastr-mobile-bridge` are protocol identifiers, not product branding. Both
  host shells, the bridge, and the browser acceptance harness keep matching them.
- Simulator-only `GOFASTR_MOBILE_EVAL` / `SIMCTL_CHILD_GOFASTR_MOBILE_EVAL`
  environment variables retain their existing debug contract.
- `dist/mobile`, installation IDs, application data directories, owner identity,
  drafts, private files, and protocol versions are not migrated or reset.
- Historical screenshot paths under `/tmp/gofastr-mobile-prototype` still refer
  to their actual evidence files.

This is a naming/import-path migration, not a new rendering architecture or a
data migration. Existing uncommitted work is preserved.

## Verification

- The GitHub repository retains ID `1444582614`; `origin` points at the renamed
  repository, and the existing T3 project uses the new title and checkout path.
- The renamed CLI's help, dry-run, usage, and native-host tests pass. An actual
  `go run ./cmd/gofastr-native --help` reports the new command name.
- Fresh complete workspace and public-pin Go suites pass after fixing moving
  open-menu targets in the shared WebView renderer. The collection workflow
  also passes 30 repetitions per platform style. Earlier stage 8 pin timeouts
  did not recur; their specific cause was not independently captured. See
  [reliability evidence and caveats](mobile-reliability.md#verification).
- All 127 WebView Node tests, workspace/public-pin vet, formatting, and whitespace
  checks pass. The real UIKit Notes simulator build also binds, links, and signs
  successfully from the renamed module and checkout.
- The latest UIKit showcase was installed and launched without resetting its
  data; row menus, filter disclosure, and live pause/retry were checked in the
  simulator. Coordinate-only tab/editor acceptance remains limited by the
  automation environment. No Android runtime execution, commit, or push is part
  of this continuation. The GitHub repository rename itself is already live.
