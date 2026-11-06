# Compiled UIKit rendering components

Rendering components are separate from file acquisition factories. Core-only builds
embed `GofastrComponentExtensions.swift`, advertise no component capabilities, and
never reference or synthesize a media factory. An unavailable required component
must fail Go capability negotiation before `open`; an unexpected unsupported or
malformed component received later shows `native.component.error`, not a fallback.

## Same-module factory catalog interface

The CLI compiles selected library sources into the **same Swift module** as the
renderer. These interfaces are intentionally internal, like the file factory seam:

```swift
protocol MobileComponentExtensionFactory {
    static var name: String { get }
    static func capabilities() -> [String: String]
    static func make(payload: String, compact: Bool,
                     emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView
}

protocol MobileComponentExtensionView: AnyObject {
    var view: UIView { get }
    func update(payload: String, emit: @escaping (String) -> Void) throws
    func dispose()
}

MobileComponentExtensions.install([GofastrMediaComponent.self])
```

Install the catalog on the main thread **before starting the renderer**, hence
before its `hello` and first `open`. The catalog only needs `name`, `capabilities()`
and `MobileComponentExtensions.install`; it must not implement rendering. Each
factory's capability dictionary has exactly one key equal to its name. The media
factory name/capability is `gofastr.media/v1`, returns `available`, and comes from
`extensions/media/GofastrMedia.swift`. Do not put it in `MobileFileExtensions`.
An empty catalog is valid. No application imports or Go bindings belong in source.

`make` and `update` run on the main thread. A same-ID/same-name component is updated
in place, not recreated when its payload changes. `update` receives a fresh emitter;
rebind child actions to that emitter and never keep using an old queued closure.
`compact` selects the existing document-form image layout. `dispose` must be safe
to call again, stop playback/release resources, and revoke callbacks. Factories
emit opaque event keys only, on the main thread after creation/update, never URLs, routes,
editor values, actions, or service writes. Synchronous creation/update-time events are
ignored because no retained entry exists yet. No renderer command grants arbitrary
event writes in this contract.

## Wire, reconciliation, and navigation

`MobileScreen.components` is optional. `MobileComponent` decodes `id`, `name`, a
JSON **string** `payload` bounded to 2 MiB of UTF-8, and optional `destinations`.
The DTO and `MobileComponentReconciler` are Foundation-only. Standalone wire tests
extract the wire section of `GofastrNative.swift`; explicitly include the new
`GofastrComponentExtensions.swift` in any source list used by such tests.

The reconciler keys by ID and retains the exact resource while the adapter name
is unchanged. Reorder makes no update call; payload or destination-map changes
invoke `update` with a new callback generation. Changed names replace the resource;
removed IDs are revoked before disposal. An unrecoverable update error disposes
only its failed resource and shows a diagnostic; unchanged siblings keep playing.
The next corrected snapshot can recreate that failed entry. Duplicate IDs fail
rather than trap. Stale/reentrant callbacks are generation-fenced; pending
navigation also checks the full current descriptor and resource generation after
checkpoint. An old callback cannot revive when identical content reappears.

The host resolves only the **current** `destinations[eventKey]`, then uses the
existing attachment navigation path. Dirty forms checkpoint before navigating;
pending/unknown saves, inactive hosts, replaced components and hidden controllers
cannot navigate. The Go runtime remains authoritative for registered route and
payload validation. Payload route fields are never used for navigation.

Component stacks appear in list headers, forms, text screens and component-only
screens. Refresh does not reconstruct editor controls or sessions. Disappearance,
session retirement and background suspension dispose resources; late refresh
replies cannot recreate a suspended stack. Reappearance/resume reactivates from
the latest retained DTO. Legacy images, attachments and file previews remain.

## Media ownership and accessibility

The media library owns composition of header, gallery, attachment rows, playback
and optional text preview. It reuses renderer primitives, not Notes templates.
Those primitives use `MobileKeyedResources` for stable image/attachment IDs and
`MobileRetainedResource` for the exact audio document. An unrelated item change,
row rename, reorder, or index-key rebinding does not recreate unchanged players.
Only changed/removed audio stops that child's player. Existing image controls
and decoded image data survive metadata-only updates. Actions capture the new
emitter, and the media view separately revokes its prior generation on update.
Image/file callbacks are rewritten to `image.<index>` / `file.<index>`;
preview taps or its accessibility custom action emit `preview`.
The library receives neither the destination allowlist nor the host. Existing
`native.image.<id>`, `native.attachment.<id>`, `native.audio.*`, `native.file.name`
and `native.file.preview` IDs remain on their original primitives.

## Verification and remaining acceptance

```sh
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./render/ios -count=1
```

On macOS/Xcode this executes Foundation regressions for actual wire extraction,
UTF-8 bounds, opaque event rewriting, keyed retention, destination changes,
retained payload updates, keyed media children, unchanged/changed audio resources,
update failures, replace/remove/disappear/retirement and old queued callbacks. The existing optimized
core-only UIKit test and the media-enabled optimized compile/link test exercise
both source catalogs for the iOS 16 simulator target. Foundation probes model
playback identity; they do not prove actual AVAudioSession behavior. Real simulator
UI acceptance, VoiceOver, photo/file providers and device audio/OS service flows
remain required and are owned by the parent task. No UI acceptance is claimed.
