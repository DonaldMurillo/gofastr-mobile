# Media rendering extension

- Own the typed Go constructor, embedded manifest, and both compiled renderer adapters here; application screens must stay Go-only.
- Generic lifecycle and navigation live in renderers. Emit only `image.<index>`, `file.<index>` and `preview` keys; never navigate directly from payload URLs or send Go writes.
- Payloads use `nativeui.Media`; validate bounded private bytes and metadata in Go. Keep plain text plain, including preview bodies and labels.
- Reconcile by component ID; update media without replacing live text controls. Release playback/resources on removal or screen retirement.
- Read `../../docs/rendering-extensions.md`. Run media/nativeui tests, renderer tests and `go test ./sim -run 'TestNotes(Photos|Files|DeviceServices)' -count=1` after behavioral changes.
- Real provider, VoiceOver/IME and hardware capture acceptance is separate from compile and browser evidence.
