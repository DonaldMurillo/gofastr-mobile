# Notes device features

Notes composes shared imports, widgets, and protected text notes in Go under
`examples/notes/notesscreens`. Native renderer libraries own the OS interactions.

Build the Android app with all three integrations enabled.

```powershell
go run ./cmd/gofastr-native build --platform android-emu --renderer native --pkg ./examples/notes/notesnative --id dev.gofastr.androidqa --name NativeNotes --receive-shares --secure-storage --biometric-usage 'Open and save encrypted protected notes.' --widgets --url-scheme notes-android --microphone-usage 'Record private voice notes.' --out dist/mobile/android-native
```

For an iOS simulator build on a Mac, select `--platform ios-sim` and provide
`--app-group group.dev.gofastr.notesqa`. Device signing and matching App Group
provisioning are required for a physical iOS installation.

## Shared items

Share text or files to Notes, then open Shared items from the Notes menu or
Settings and choose Check inbox. Review the title, text, and attachment previews
before choosing Save note. A review draft survives an app restart. Discard is
available from the inbox row's actions and asks for confirmation.

The note's owner-scoped source receipt is committed with its text in SQLite.
Attachments use stable import identities, so a partial import can finish without
duplicating files. A completion marker prevents an acknowledged import from
restoring an attachment the user later removed. Inbox acknowledgement follows
the confirmed save. Repeated reads, interrupted acknowledgement, and retained OS
launches reuse the original receipt and preserve subsequent edits to the note.

Reviewed inbox payloads are bounded private installation files. The platform
inbox keeps its own byte and expiry limits. See [Receiving shares](share-in.md).

## Widgets

Open a saved active note, choose Show on widget, and confirm that its saved title
and text preview can be visible outside Notes. Add the Notes widget through the
home screen's widget picker. Every installed Notes widget uses this selection.

Later saves publish the selected note's latest saved content. Archiving or moving
that note to Trash through the mobile collection first clears the widget, then
changes the note after the device confirms the clear. A failed clear leaves the
note available and records a pending cleanup in Widget settings. Clear widget
removes the stored snapshot. Publishing reports stored content, while the OS
controls placement and refresh timing.

Only ordinary saved Notes records can be selected for publication. Widget text
is bounded to a 200-byte title and a 2,048-byte preview. See
[Home-screen widgets](widgets.md).

## Protected notes

Protected notes is a separate collection for encrypted text notes. Create a new
note there, then choose Save and lock. Opening or saving its title and body uses
a fresh strong-biometric operation against Android Keystore or iOS Keychain.
The complete encoded note must fit within the secure-storage limit of 16 KiB.
The collection supports up to 128 records. Protected notes contain text only.

The OS secret store holds the encrypted content. SQLite, feature metadata,
ordinary drafts, and widgets receive no protected title or body. The private
installation index stores opaque record identities, timestamps, and whether a
save was confirmed. An interrupted save keeps a recoverable identity so the user
can authenticate and inspect its actual encrypted content after restarting.

Unlocked content and unsaved edits remain in foreground memory. Going to the
background closes protected screens and revokes their foreground grants. The
native `privacy/v1` contract rejects durable drafts on protected forms and
fences late callbacks. Android uses a secure window and disables view-state
saving, autofill, and keyboard personalization for protected fields. UIKit
covers protected content while inactive and removes protected screens on an
actual background transition. Private routes are omitted from UIKit navigation
restoration. LocalAuthentication's temporary inactive state preserves the active
authentication flow.

Unsaved protected edits clear on background. Deletion asks for confirmation and
removes the secure record before its index entry. Missing or invalidated keys
leave the note locked and require explicit removal. Enrollment changes can make
previously protected content unrecoverable. See [Secure storage](secure-storage.md).

## Verification

Protocol regression tests cover import receipts across restart and failed
acknowledgement, immutable attachment identities at full quota, reviewed edits,
protected-save cancellation, canonical authenticated bytes with newer edits,
owner scoping, foreground grant revocation, plaintext-file exclusion, late
protected callbacks, explicit widget selection, saved-note publication, and
failed widget cleanup.

On November 6, 2026, the Android 14 x86-64 emulator in T3 Code's visible Device
panel accepted a real content-provider URI share, previewed its 22-byte text
attachment, saved the reviewed title, and retained one note and attachment
after restarting. A real AppWidgetHost rendered the selected note, opened its
editor on tap, displayed a later saved edit, and displayed the empty state
after clearing. Reopening the same editor reused its navigation entry.
An unenrolled biometric left a protected save unconfirmed; backgrounding
removed the protected editor and its unsaved text. No biometric was enrolled
or device lock changed for this check.

The final Go suite, vet, 143 Node tests, Android SDK compilation, APK build,
and v2/v3 signature verification passed. An earlier full run overlapped a Swift
source edit and reported an embedded-source mismatch; rebuilding the tests
resolved it. A WebView interaction timeout passed on rerun. The legacy Android
UIAutomator smoke runner could not dump the hierarchy while T3's helper was
active, so Device-panel interaction checks do not count as a pass of that runner.

Windows can compile and package the Android renderer and run the shared Go and
WebView suites. Physical Android enrollment/lockout, hardware input, and the iOS
app, Keychain, share extension, and WidgetKit runtime require their corresponding
devices and SDKs. Record the executed device checks separately from these gates.
