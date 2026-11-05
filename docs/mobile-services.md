# Mobile device services and live collections

Implementation and acceptance guide, November 5, 2026. Application authors still
write Go screens/actions. Ordinary library Swift and JavaScript adapt them; no
application-owned native screens, JavaScript, or behavior templates are required.
GoFastr core remains unchanged. Native Android is deferred.

## Four additions in the same Notes application

| Capability | Go contract | Notes workflow |
| --- | --- | --- |
| Local reminders | `device/notifications`, `notifications/v1` | Set date/time on a saved note; explicitly schedule, cancel, or check status |
| Rich share and clipboard | `device/sharing`, `sharing/v1`, `clipboard/v1` | Share current note text plus Markdown, share original attachment bytes, or explicitly copy text |
| Voice notes | `device/audio`, `audio/v1`, `Attachment.Audio` | Record on a saved note, accept/cancel, persist privately, reopen/play, and share/export |
| Repository paging/live reads | `Screen.Live`, `Route.Watch`, `behavior.Task`, `live/v1` | Load beyond 200 notes, retain stable selection, watch visible collections, cancel/retry reads |

Reminder fields do not schedule anything implicitly. Device-local wall time is
interpreted by the device, not Go's timezone. Scheduling can fail or be denied.
A share-sheet completion does not establish recipient delivery. Clipboard reads
are not implemented. No background recording or remote sync is promised.

Recording defaults to at most 60 seconds/1 MiB in the service contract. Notes
uses 60 seconds/**512 KiB** to fit its versioned duration/content envelope in
the existing private attachment store. Audio signature checks are not full
server-side media decoding. See [Notes persistence](../examples/notes/docs/mobile-services.md).

## Runtime and host boundary

`Route.Actions` requests a typed `ServiceRequest`; matching `Route.Services`
continuations receive observed outcomes. `nativeui.Runtime` issues the opaque
service identity. The adapter executes once, sends `service-result`, then reads
`complete`. Missing responses never authorize another OS action or mutation.
Unknown writes retain the existing nonretryable recovery policy. Inputs, caret,
IME, scroll, and native controls remain renderer-owned.

UIKit uses reusable `GofastrServiceRunner` sources. The default iOS WebView host
uses `mobilehost/ios.GofastrWebViewServices`, restricted to the exact trusted
origin and main frame/current document. Native and WebView hosts package the
same service runner; custom hosts must explicitly integrate it. Optional
`--microphone-usage` adds the purpose string. An omitted purpose string leaves
native recording unavailable. See [UIKit details](../render/ios/docs/services.md).

Plain-browser notifications are unsupported, not in-page timer imitations.
Sharing/download and clipboard fallbacks are explicit. MediaRecorder requires
browser support, a secure context, permission, and compatible response policy.
Notes opts into same-origin microphone access and private Blob media only;
legacy web/native compositions retain their security defaults.

## Live data and honest limits

Notes queries filter/order/page inside its authorized repository. Each window
displays at most 100 rows; `Paging.Next` is opaque, not an integer to calculate.
The process-local owner/epoch hub changes only after confirmed writes through
the shared service. Separate service instances, direct SQL, other processes,
and remote changes do not automatically emit invalidations.

Only visible lists watch; they do not replace dirty editor buffers. Read
generation/cancellation and retry are separate from write outcomes. Filters
retire old queries; selected IDs survive refresh only while still valid/visible.
Selection-toolbar actions and opaque hidden values are reconciled with those
rows, so archive/Undo does not retain a missing or obsolete receipt. Host Stop
cancels its bridge polls before draining HTTP; direct callers retain their own
contexts. See [lifecycle ownership](mobile-lifecycle.md).

## Repeatable acceptance

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./sim -run 'TestNotes(AudioRecordingBrowser|DeviceServicesProtocolWebView|BrowserReminderUnsupported|LivePagingWebView)$' -count=3
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./nativeui ./behavior ./device/... ./examples/notes/... ./cmd/gofastr-native ./mobilehost/ios ./render/ios ./render/webview
node --test render/webview/*.test.cjs
```

The protocol browser test uses an explicitly injected fake device adapter for
OS outcomes. A separate test uses Chromium's synthetic microphone with the real
MediaRecorder, bridge, storage and playback loading. Neither is physical
microphone, OS reminder delivery, share-provider completion, or VoiceOver/IME
acceptance. Both complete iOS hosts have been bound/compiled/linked; actual
hosted-WebView service UI acceptance remains a separate check. The existing
native app was reinstalled/launched without resetting its bundle identity/data.

### Verified in this slice

| Check | Observed result |
| --- | --- |
| Workspace `go test ./...` | Pass, including the complete browser suite |
| Public pin `GOWORK=off go test ./...` | Pass without the local GoFastr workspace |
| Targeted Go race suites and `go vet ./...` | Pass |
| Adapter JavaScript syntax and Node checks | 103 tests pass |
| Device-service/live, controls/Undo, photo and draft/share browser flows | Three consecutive passes in Chromium; iOS/Android presentation styles, not native Android |
| Synthetic microphone | Real MediaRecorder → bridge → private persistence → playback loading passes |
| Native UIKit and hosted-WebView iOS simulator bundles | Fresh gomobile bind, optimized Swift compile/link and signing pass |

The already-running native app was left open after inspection rather than
interrupting an active editor for another rebuild. Updated bundles are ready at
`dist/mobile/ios-services-qa/Notesnative.app` and
`dist/mobile/ios-services-web-qa/Notesmobile.app`. Build success does not replace
the outstanding OS permission, reminder delivery, hosted-WebView service UI,
share-provider, accessibility, and physical-device checks above.

See [typed API and runtime safety](device-services.md),
[frozen coordination contract](mobile-services-plan.md), and
[roadmap](mobile-roadmap.md) for the remaining gates.
