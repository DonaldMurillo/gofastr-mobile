# WebView rendering components

Rendering components are selected compiled libraries, separate from file
acquisition and built-in form/list controls. They never own a Go session, command
endpoint, router, or editor model. Legacy `Screen.Images`, `Screen.Attachments`,
and `Screen.FilePreview` remain supported independently.

## Registration and negotiation

An extension library registers exactly one descriptor per selected module:

```js
(window.__gofastr_component_extension_definitions ||= []).push({
  name: 'example.library/v1',
  capabilities: () => ({ 'example.library/v1': 'available' }),
  mount(container, component, { open }) {
    return { update(component) {}, dispose() {} };
  },
});
```

`extension/webassets` wraps compiled registration. The renderer serializes
selected content-addressed module names under `extensions` and selected
component names under `componentExtensions` in `data-gfw-config`. All selected
modules finish loading before `hello`. The generic host validates unique
descriptors, callable `capabilities`/`mount`, and exactly one valid support fact
matching each selected name. A globally registered, unselected library does not
advertise support for that app.

Go automatically requires available component support. Both Go startup and the
generic mount host reject missing, unavailable, or fallback support: required
content cannot silently disappear. Startup failures appear in `[data-gfw-status]`;
instance failures show `role="alert"` at `[data-gfw-component-error]`. Without
JavaScript, SSR shows generic loading placeholders, not a bespoke legacy media
fallback decoded from component payloads.

## Snapshot and lifecycle

`Screen.Components` becomes JSON `components` in full-page and partial navigation
config and subsequent command/open responses. Each snapshot contains `id`,
`name`, `payload` (a JSON string, maximum 2 MiB UTF-8), and optional `destinations`
mapping opaque event keys to validated local Go routes.

`mount` and every `update` receive the whole snapshot, not just its payload.
Libraries parse their own schema. The generic host keys instances by ID, retains
them while name/ID match, forwards refreshed snapshots, and disposes removed or
replaced instances and retired screens. Inline collection refresh does not
replace form/query controls. Same-route refresh retaining the root and form
transfers its keyed host to the fresh Go screen generation; route/form retirement
disposes it. Active IME stays guarded until the original `compositionend`.

Adapters call only `open(key)`. The host checks own-property membership in the
latest snapshot's destination map, ignores disposed/stale callbacks, and uses
existing leave/checkpoint, composition, pending/unknown, foreground-modal,
local-route, and GoFastr navigation guards. A URL in a payload is never a
navigation instruction. Compiled libraries are trusted code, not sandboxed
arbitrary user plugins.

## Media pilot and selectors

`extensions/media/media.js` registers `gofastr.media/v1`. It parses `nativeui.Media`
(`images`, `attachments`, `preview`) and owns themed DOM/styles, plain-text output,
bounded JPEG data previews, file metadata, text/file detail composition, and
private audio controls. Only `image.<index>`, `file.<index>`, and `preview` events
are emitted. Payload route strings are never assigned to `href`. Link-like
`<a role="link" tabindex="0">` controls retain click/Enter behavior without exposing
navigable payload URLs.

- `[data-gfw-components]` contains keyed `[data-gfw-component="<id>"]` hosts.
- `[data-gfw-media]` scopes library-owned styling.
- `[data-gfw-image="<image-id>"] img` contains each bounded photo preview.
- `[data-gfw-attachments]` and `[data-gfw-attachment="<file-id>"]` retain file rows.
- `[data-gfw-file-preview]` contains **the detail gallery, attachments, and text**;
  `[data-gfw-file-preview] img` remains the photo-detail browser selector.
- `[data-gfw-preview]` emits `preview`; `pre` contains plain preview text.
- `[data-gfw-audio-playback="<file-id>"]` exposes private native playback controls.

Unchanged keyed images/audio retain DOM nodes and sources across updates, even
when another part of the payload changes. Audio has no autoplay or acquisition
side effects: signature-checked bounded bytes become private Blob URLs. Removal,
disposal, pagehide, and background visibility revoke sources; foreground return
recreates a source but never plays automatically. Bytes are not uploaded,
downloaded, fetched by URL, or persisted in browser storage.

## Verification

```sh
node --test render/webview/*.test.cjs
GOCACHE=/tmp/gofastr-native-build-cache.IUXdIL go test ./render/webview ./extensions/media ./nativeui
```

`rendering.test.cjs` executes the real bundled descriptor through the generic
host: mount/update/dispose, payload/route security, audio cleanup, required/omitted
support, dirty navigation, IME, and inline refresh. `rendering_test.go` exercises
full/partial config, no legacy SSR fallback, selected module metadata, and
required support at the HTTP command boundary. Dependency-free Node DOM tests
are not Chromium, WKWebView, VoiceOver, real-device audio, or hardware IME
acceptance. The parent owns Notes Chromium flows and their unchanged selectors.
