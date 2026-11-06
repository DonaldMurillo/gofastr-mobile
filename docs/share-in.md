# Receiving shares

`device/sharein` is the common Go contract for receiving text and files from
other apps. Android receives `ACTION_SEND` and `ACTION_SEND_MULTIPLE`. iOS uses
a separate share extension and an App Group inbox. Neither adapter opens shared
URLs, chooses a Go route, or imports data into application records automatically.

Enable receiving shares when packaging the app. iOS also needs an App Group
registered for both the app and its share extension. The same group identifier
must appear in both entitlements and the extension's `GofastrAppGroup` plist
value. The extension executable contains `GofastrIncomingShares.swift` and
`shareextension/GofastrShareViewController.swift`, with principal class
`GofastrShareViewController`. It links UIKit, Social, and UniformTypeIdentifiers
and requires iOS 14 or newer.

## Shared Go flow

An admitted app action requests `sharein.Request{Operation: sharein.Next}` through
`nativeui.ServiceRequest.IncomingShare`. A completed result's `IncomingShare`
contains the oldest item, or is nil when the inbox is empty. Reading does not
consume it. Show the item to the user before importing it. The returned
identifier is stable through repeated reads and app restarts.

After accepting or discarding the content, send
`sharein.Request{Operation: sharein.Acknowledge, Identifier: item.Identifier}`.
Acknowledgement is idempotent and cannot return content. Persist the identifier
alongside an imported record, in the same application transaction, so a crash
between saving and acknowledging does not create a duplicate. An uncertain
application write must settle through its existing completion path.

The adapters retain at most 16 items for seven days. One item can contain four
files totaling 1 MiB and 64 KiB of UTF-8 text. Provider URIs and OS paths stay in
the native adapter; Go receives filenames, MIME types, and bytes. A share that
exceeds these limits fails as a whole. The adapters do not silently drop files
or truncate text. Acknowledgement tombstones prevent a retained launch from
recreating a consumed item, for seven days or the most recent 256 acknowledgements.

## Android lifecycle

The host calls `IncomingShares.capture(activity, intent, callback)` for cold and
warm incoming intents only when receiving shares is enabled. It assigns
`IncomingShares.TOKEN` before background work. Preserve that token across
recreation, and clear the share intent only after the callback reports completed.
Capture copies provider streams while temporary read grants are valid, on a
bounded serial worker. A synced private inbox file is published before success.
The manifest should declare only the MIME types the application accepts. No
broad storage permission is needed. Only `content:` file URIs are accepted.

`IncomingShares.perform(activity, request, callback)` implements the common
next/acknowledge flow. Its callback runs on the main thread. Capturing a share
does not replace the visible editor or bypass its departure policy.

## iOS extension lifecycle

The share sheet's Post button confirms the transfer into the inbox. The extension
loads file representations inside the provider callback, because their temporary
URLs can expire when that callback returns. It copies bounded bytes and never
launches the containing app. The app reads the inbox when it next runs and the
user requests it. Directory coordination serializes quota checks, publication,
and acknowledgement across the two processes. Inbox files are excluded from
backup and use protection until the first unlock after boot.

## Checks and limits

`go test ./device/sharein` checks identifiers, operation shapes, content validation,
and combined byte limits. `go test ./render/ios -run IncomingShares` compiles and
executes the Foundation inbox checks on macOS. These cover repeated reads,
acknowledgement, retained-launch deduplication, quota, and expiry. Windows skips
the executable Swift check. Java sources compile against Android SDK 36 with
the framework's API 24 minimum.

OS chooser behavior, provider grant expiry, iOS extension installation, and
physical-device App Group entitlements require platform acceptance. A compile
check cannot establish those behaviors. The inbox is private transfer storage;
it does not encrypt application records or provide biometric protection.

Android 14 x86-64 acceptance covers warm text shares, repeated reads, explicit
acknowledgement, and a cold share from a separate app's private content provider
with a temporary read grant. The copied file bytes and identifier survive an
app process restart. A provider file over 1 MiB is rejected before publication.
