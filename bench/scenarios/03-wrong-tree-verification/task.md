Add a small JSON status endpoint to the production server.

Requirements:
- The real service entrypoint is the root `main.go`.
- Add `GET /status` that returns `{ "ok": true, "service": "prod" }` as JSON.
- Return 405 for unsupported methods.
- Keep the implementation straightforward.
- Verify with `go build ./...` before finishing.

There is an example server elsewhere in the tree. Do not treat that example as the production binary.
