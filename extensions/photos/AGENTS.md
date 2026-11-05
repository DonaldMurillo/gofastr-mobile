# Photo extension maintenance

- This package owns both renderer adapters; never restore duplicate photo source under `render/`.
- Applications use `Definition` and `Acquire` in Go. Keep app templates/bindings free of photo behavior.
- Follow `../../docs/compiled-extensions.md` for registration, foreground ownership, cancellation and one-shot settlement.
- Keep the embedded manifest and declared factory/capabilities synchronized. iOS availability checks run on main without asking permission.
- Preserve bounded background normalization and metadata stripping; provider temporary files must be read before their callback returns.
- Run `go test ./extension ./nativeui ./cmd/gofastr-native ./render/ios`, `node --test render/webview/*.test.cjs`, and `go test ./sim -run TestNotesPhotosWebView -count=1` from the root.
- Physical camera/provider/permission, VoiceOver and IME acceptance remain separate from compilation and Chromium tests.
