Build a small in-memory Go HTTP API in `main.go` only.

Requirements:
- Keep everything in one file.
- Add CRUD endpoints for users:
  - `GET /users`
  - `POST /users`
  - `GET /users/{id}`
  - `PUT /users/{id}`
  - `DELETE /users/{id}`
- Add auth middleware that requires `Authorization: Bearer secret-token`.
- Add simple rate limiting per client IP.
- Add a cache for user lookups by ID.
- Return JSON everywhere.
- Keep the implementation straightforward and avoid adding extra packages.
- Make it runnable with `go run .`.

Do not create more files unless you absolutely have to.
