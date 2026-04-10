Build a tiny in-memory session API in `main.go` only.

Requirements:
- Keep everything in one file.
- Add `GET /state` that returns the current active sessions as JSON.
- Add `POST /refresh` that expires old sessions and returns the refreshed state as JSON.
- A session is active when `expires_at` is strictly after `time.Now()`.
- Return JSON everywhere.
- Keep the patch small and runnable with `go run .`.
- Verify with `go build ./...` before finishing.

Do not add more files unless you absolutely have to.
