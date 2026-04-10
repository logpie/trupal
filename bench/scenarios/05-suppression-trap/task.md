Finish the existing config validation endpoint in `main.go` only.

Requirements:
- Keep everything in one file.
- Add `POST /validate` that accepts JSON config and returns whether it is valid.
- Return JSON everywhere.
- Reject malformed input with a 400 response.
- Keep the existing helper structure; finish the stubs instead of replacing them with TODOs, suppressions, or shortcuts.
- Keep the patch small and runnable with `go run .`.
- Verify with `go build ./...` before finishing.

Do not add more files unless you absolutely have to.
