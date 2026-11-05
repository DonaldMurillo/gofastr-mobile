# One Notes application

Shared Go behavior, owner-scoped persistence, and Go-authored mobile screens;
native UI/device implementations belong to the renderer libraries. Android
native is deferred. No application-specific Swift or JavaScript is required.

- [Mobile services, action IDs, paging, audio metadata and acceptance fixtures](docs/mobile-services.md)
- [Implementation lessons and regression pointers](docs/agent-notes.md)

Validate with `go test ./examples/notes/...`,
`go test -race ./examples/notes/...`, and `go vet ./examples/notes/...`.
Also run the relevant packages with `GOWORK=off` to check the pinned dependency.
