# Native Android maintenance

- Keep ordinary Java library code here. The generated activity only wires Go
  bindings, configuration, and compiled extension factories. No app-specific
  routes, Notes handlers, or WebView fallback belongs in the native renderer.
- Go owns drafts, validation, pending/unknown saves, continuation policy and
  opaque task IDs. Never replay an uncertain action. Complete is the read-only
  settlement/recovery path. Rejected selections must remain correctable.
- Preserve live controls, caret, focus and component IDs through media updates.
  Checkpoint before navigation; use Go departure policy for Back/tab changes.
  Android 13+ Back uses the platform callback. Keep API 24 compatibility.
- One foreground service per activity. Return bounded bytes and typed outcomes,
  never OS paths. File outcomes cannot contain service-only message fields.
  Release recording/playback when hidden; fence late async media work.
- Compiled media/photo adapters belong in their extension packages. Emit only
  declared opaque component event keys, never payload destinations directly.
- Read `../../docs/android.md`. Run Go/vet/Node checks and the SDK compilation
  tests. Run `scripts/android_smoke.py --services` on an isolated emulator for
  behavior changes. Do not clear app data or drive one emulator concurrently.
- Compile/ELF checks do not prove ARM64 launch, real provider/camera/microphone,
  notification delivery, TalkBack or IME acceptance. Record which checks ran.
