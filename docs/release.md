# Release builds

`build` and `run` take `--profile debug` (the default) or `--profile release`.
Debug output is unchanged from earlier versions: the same commands, the same
debug-signed APK, the same ad hoc simulator app. Release builds sign with
credentials you supply. The CLI does not create release keys, download SDKs
or bundletool, unlock keychains, or upload artifacts. Go dependencies resolve
through your normal Go module configuration.

| | debug | release |
| --- | --- | --- |
| WebView inspection (`setWebContentsDebuggingEnabled`, `isInspectable`) | compiled in | left out |
| Go build flags | defaults | `-trimpath`, `-s -w`, and `-tags=gofastr_release` |
| `android:debuggable` | absent | absent; the build checks badging |
| Build number | 1 unless `--build-number` | `--build-number` required |
| Default `--out` | `dist/mobile/<platform>` | `dist/mobile/<platform>-release` |
| Signing | Android debug key / simulator ad hoc | your upload key / your Apple identity |
| `release-report.json` | not written | written next to the artifact |

`--build-number` is Android `versionCode` and iOS `CFBundleVersion`: a plain
integer from 1 to 2100000000 with no leading zeros. Each store upload needs a
higher number than the last; the CLI does not track it for you.

`--profile release` is refused for `--platform ios-sim`. A simulator build is
never a release artifact.

Release binds explicitly select `gofastr_release`, replacing ambient
`GOFLAGS` build tags. This keeps the `gofastr_inspect` export and
`gofastr_debugtrace` hooks out of the generated library. `--debug-trace` is
rejected with `--profile release`. Custom release build tags are not supported.

## Android

```powershell
$env:NOTES_UPLOAD_PASS = '...'   # from your secret store or CI, never typed into the command
go run ./cmd/gofastr-native build --platform android --pkg ./examples/notes/notesnative --renderer native `
  --profile release --build-number 12 `
  --keystore C:\keys\notes-upload.p12 --key-alias upload --keystore-pass-env NOTES_UPLOAD_PASS
```

`--keystore-pass-env` and `--key-pass-env` take the *name* of an environment
variable. The value never appears in a command line, a log or the report:
`keytool`, `apksigner` and `jarsigner` read it themselves through their
`env:` options, and the CLI scrubs it from any tool output it relays.
`--key-pass-env` defaults to the store variable, which is what PKCS12
keystores need.

The CLI refuses the debug keystore, the `androiddebugkey` alias, a
certificate whose subject is `CN=Android Debug`, an expired certificate, and a
keystore or other signing input that lives inside `--out`. It checks the key
with `keytool -list` before running gomobile, so a wrong password or alias
fails in seconds instead of after the bind.

Create the upload key yourself, once, and keep it outside the repository. For
example (the CLI never runs this):

```sh
keytool -genkeypair -keystore notes-upload.p12 -storetype PKCS12 -alias upload \
  -keyalg RSA -keysize 4096 -validity 9125 -dname "CN=Your Name, O=Your Org" \
  -storepass:env NOTES_UPLOAD_PASS -keypass:env NOTES_UPLOAD_PASS
```

Back it up. With Play App Signing, losing the upload key means a reset request
to Google; without it, losing the key means you can never update the app.

### ABIs

`--abi arm64-v8a,x86_64` is the default. Use `--abi arm64-v8a` for a
phone-only artifact. 32-bit ABIs (`armeabi-v7a`, `x86`) are refused. Nobody has
validated 32-bit Go output or the SQLite syscall overlay
([build compatibility](android.md#build-compatibility)) on them. Android
builds keep the 16 KiB `max-page-size` link flag in both profiles.

### The APK pipeline

After gomobile bind and the existing ELF alignment check, a release APK runs:

1. `aapt2 link` with the release manifest, then the dex and native libraries.
2. `zipalign -P 16 -f 4` (build-tools 35 or newer; older ones fall back to `-p`).
3. `apksigner sign` with `--ks-pass env:NAME --key-pass env:NAME`.
4. `zipalign -c -P 16 -v 4` on the signed APK.
5. `apksigner verify --print-certs -Werr`. The signer's SHA-256 must equal the
   certificate `keytool` reported for your alias.
6. `aapt2 dump badging`. Package, `versionCode`, `versionName` and the
   `native-code` ABI set must match the request, and the APK must not be
   debuggable.

Only then does the signed file move to `<out>/<App>.apk`. A failed check
leaves no APK and no report.

### Android App Bundles

`--package aab` (or `apk,aab`) builds an AAB for Play. It needs bundletool,
which the CLI does not download. Pass `--bundletool path/to/bundletool.jar` or
set `GOFASTR_BUNDLETOOL`.

The AAB path links resources with `aapt2 --proto-format`, repacks them into a
`base` module (manifest, `resources.pb`, `res/`, `dex/`, `lib/<abi>/`), and
writes a `BundleConfig.json` that keeps native libraries uncompressed with
`PAGE_ALIGNMENT_16K`. Then:

1. `bundletool build-bundle`
2. `jarsigner` with `-storepass:env` / `-keypass:env`
3. `jarsigner -verify` must print `jar verified.`
4. `bundletool validate`
5. `bundletool dump config` must still contain `PAGE_ALIGNMENT_16K`

`run` with an AAB uses `bundletool build-apks --mode=universal` to make a local
test APK. That APK is signed with bundletool's debug key, so it is for local
testing only and cannot update a Play-installed copy.

### Installing next to a debug build

A release build signs with a different key than the debug build. Android
refuses to update one with the other (`INSTALL_FAILED_UPDATE_INCOMPATIBLE`).
Use a separate `--id` for release testing, or uninstall the debug app yourself.
`run` never uninstalls.

## iPhone (`--platform ios-device`)

```sh
go run ./cmd/gofastr-native build --platform ios-device --pkg ./examples/notes/notesnative --renderer native \
  --id dev.example.notes --team-id ABCDE12345 \
  --sign-identity 0123456789ABCDEF0123456789ABCDEF01234567 \
  --provisioning-profile ~/profiles/notes-dev.mobileprovision
go run ./cmd/gofastr-native run --platform ios-device ... --device <CoreDevice identifier or UDID>
```

This needs a Mac with Xcode, your Apple Developer team, a codesigning identity
already in a keychain, and a provisioning profile per bundle. `--dry-run` works
on any OS and prints the plan.

The build compiles against the `iphoneos` SDK (`gomobile bind -target=ios`,
`arm64-apple-ios<min>`), signs inside-out (extensions first, then the app),
verifies, and writes `<out>/<App>.ipa`. Both `debug` and `release` produce an
IPA; only `release` writes the report.

`--sign-identity` takes the identity's SHA-1 (recommended, shown by
`security find-identity -v -p codesigning`) or its exact name. A name must
match exactly one identity. `--keychain` selects a keychain; the CLI never
unlocks it.

### Profile kinds

The CLI decodes each profile with `security cms -D` and classifies it:

| Kind | How it is recognised | Accepted for |
| --- | --- | --- |
| development | device list, `get-task-allow` true | `--profile debug` |
| ad hoc | device list, `get-task-allow` false | `--profile release` |
| App Store | no device list | refused |
| enterprise | `ProvisionsAllDevices` | refused |

App Store distribution needs more than a signature: an asset-catalog app icon
(actool), a privacy manifest, `DT*` build metadata, and an upload tool. None of
those exist here yet, so an App Store profile is refused with that list rather
than producing an IPA that App Store Connect would reject. Enterprise
distribution is out of scope.

Each profile must also match `--team-id`, be unexpired, cover `<prefix>.<id>`,
include the signing certificate, and grant every entitlement the build asks
for.

### Extensions, app groups and Keychain

`--receive-shares` adds `<id>.Share` and `--widgets` adds `<id>.Widget`. Each
needs its own profile (`--share-provisioning-profile`,
`--widget-provisioning-profile`) and both need `--app-group`. A wildcard App
ID cannot carry an app group, so these profiles must be explicit.

The CLI generates device entitlements in Go from the profile: the
`<prefix>.<id>` application identifier, team, `get-task-allow`, and the app
group. Only the app gets `keychain-access-groups` (with `--secure-storage`).
Extensions never do, so they cannot read the app's private Keychain items.
After signing, `codesign -d --entitlements` must return exactly the generated
set for every bundle.

### Installing and launching

`run` checks that `--device` names a connected, paired iPhone with Developer
Mode on, then runs `xcrun devicectl device install app` and `devicectl device
process launch`. If the install outcome is unclear, the CLI prints UNKNOWN with
the command to check and does not retry. It never uninstalls, resets, pairs or
enables Developer Mode.

## release-report.json

The report sits next to the artifact with mode 0600. It holds the profile,
build number, ABIs, each artifact's SHA-256 and size, the signer certificate
SHA-256 (Android) or identity SHA-1 and profile UUIDs, kinds and entitlements
(iOS), and every check as PASS, FAIL or NOT RUN. It never holds a password,
an environment variable name or a keystore path. The CLI refuses to write a
report that contains a FAIL.

Checks that need hardware or a store always stay NOT RUN:

| Gate | Status until someone runs it |
| --- | --- |
| Launch on an ARM64 Android phone | NOT RUN |
| Play Console upload of the AAB | NOT RUN |
| Install and launch on a registered iPhone | NOT RUN |
| Share extension and widget through the app-group container on device | NOT RUN |
| Keychain isolation on device (extension cannot read the app's item) | NOT RUN |

## Unconfirmed tool flags

These flags come from vendor documentation and have not run on a real host for
this CLI yet. If one fails, the step's command is in the log.

- `codesign --generate-entitlement-der --timestamp=none` for device signing.
- `xcrun devicectl device install app --json-output` and its `info.outcome`
  field.
- bundletool `build-bundle`, `validate`, `dump config` and
  `build-apks --mode=universal`.

`zipalign -P 16`, `zipalign -c`, `apksigner verify -Werr --print-certs` and
`gomobile bind -trimpath` were confirmed on Windows with build-tools 36.
