# AGENTS.md — Forum Image Upload Implementation Guide

## Project

This repository contains a completed Go forum-authentication project that is
now being extended by the forum image-upload exercise.

Preserve the existing email/password and GitHub/Google OAuth flows, UUID
sessions, posts, comments, categories, reactions, filters, public reading,
SQLite persistence, server-rendered templates, and Docker setup. Add the
ability for registered users to attach an optional JPEG, PNG, or GIF image to a
new post. Registered users and guests must be able to view the image later.

Do not reimplement authentication or add unrelated features such as new OAuth
providers, account linking, password reset, moderation, roles, JavaScript, or a
public JSON API.

## Source of Truth

When documents disagree, follow this order:

1. `docs/exercise-image-upload.md` — supplied image-upload subject
2. `docs/audit-image-upload.md` — image-upload auditor checks
3. `docs/PRD.md` — clarified behavior and fixed safety decisions
4. `docs/tasks.md` — implementation order and phase acceptance checks
5. `README.md` — commands verified against the current implementation
6. Existing tests and code — completed authentication/forum baseline and
   regression behavior

Do not silently reinterpret an exercise requirement. Record a genuinely
unclear product decision in the PRD and tasks before implementing it.

## Mandatory Scope

- retain all existing password, OAuth, session, and forum behavior
- allow only authenticated users to create posts and upload images
- keep text-only post creation working
- accept JPEG, PNG, and GIF images
- reject unsupported, empty, malformed, or unreadable image content
- accept an image of exactly 20 MiB (20,971,520 bytes)
- reject an image larger than 20 MiB with a clear user-facing message
- show a post's image to both authenticated users and guests
- keep uploaded images available when revisiting a post
- persist uploaded images across Docker container replacement
- handle malformed input and internal failures without crashing or leaking
  internal details

## Architecture

Keep the existing layered flow and add a focused upload boundary:

```text
browser multipart request
→ authenticated post-creation handler
→ image validation and storage
→ existing post validation service
→ post repository and SQLite
→ redirect to public post detail
→ template image URL
→ public static-file handler
```

The handler owns HTTP authentication checks, bounded request parsing, multipart
cleanup, upload error mapping, redirects, and compensating file deletion when
post creation fails.

The upload package owns byte limits, type detection, image decoding checks,
safe UUID filenames, filesystem paths, atomic file writes, and deletion of only
the files it manages. It must not create posts or parse forum fields.

The post service continues to own author and post-field validation. The
repository owns SQL and the transaction that creates a post and its category
links. Templates render only the safe public path produced by upload storage.

Use focused interfaces at tested boundaries, including the handler's image
storage dependency. Do not introduce an interface for every concrete type.

## Upload Rules

- An image is optional. An absent multipart file part means no image.
- A present zero-byte file is invalid rather than a text-only submission.
- `MaxImageSize` is `20 * 1024 * 1024` bytes.
- Do not rely on a filename extension, multipart `Content-Type`, or
  `Content-Length` supplied by the client.
- Use `http.DetectContentType` on file bytes, then verify that the content is a
  decodable JPEG, PNG, or GIF with standard-library image decoders.
- Derive the stored extension from the verified content type.
- Bound the complete HTTP request with `http.MaxBytesReader`, allowing limited
  multipart overhead, and independently enforce the exact image limit by
  reading no more than `MaxImageSize + 1` bytes.
- `ParseMultipartForm`'s memory argument is not an upload-size limit.
- Do not require the entire upload to remain in memory.
- Remove multipart temporary files after parsing.
- Never use the original filename as a filesystem path.

## Storage Rules

- Store image files under `static/uploads/` and only the public path in SQLite.
- Public paths have the form `/static/uploads/{uuid}.{safe-extension}`.
- Use server-generated UUID filenames.
- Write through a temporary file in the destination filesystem and atomically
  rename it only after copying and validation succeed.
- Remove partial and temporary files on every failure.
- Use predictable directory and file permissions, such as `0755` and `0644`.
- The storage deletion operation must reject paths outside its configured
  upload directory.
- If post validation or persistence fails after an image is saved, attempt to
  delete that image and return the appropriate safe HTTP error.
- Ignore runtime uploads in Git while retaining `static/uploads/.gitkeep`.
- Mount `/app/static/uploads` as persistent storage in Docker Compose.
- Uploaded images are intentionally public, but uploading remains restricted to
  authenticated users.

## Database Rules

Add a new numbered migration rather than editing an applied migration. The
`posts.image_path` column is nullable so old and text-only posts remain valid.

- write SQL `NULL` for an empty image path
- read `NULL` safely with `COALESCE` or `sql.NullString`
- preserve existing posts during migration
- include the image path in post detail and every listing/filter query that uses
  the post read model
- keep post and category-link creation transactional
- never store image bytes in SQLite

Enable SQLite foreign keys on every connection. Use real temporary SQLite
databases in repository and migration tests.

## HTTP And UI Rules

- `GET /posts/new` and `POST /posts` remain authenticated routes.
- `GET /posts/{id}` and `GET /static/uploads/{name}` remain public.
- Successful creation redirects to `/posts/{id}` with `303 See Other`.
- Invalid, empty, unsupported, malformed, or oversized uploads return `400 Bad
  Request` with a useful message.
- Unauthenticated creation returns `401 Unauthorized`.
- Unexpected storage or persistence failures return a safe `500 Internal Server
  Error`.
- Do not expose filesystem paths, SQL errors, stack traces, or raw internal
  errors to users.
- The create-post form states that the image is optional, lists JPEG/PNG/GIF,
  and states the 20 MB maximum.
- The HTML `accept` attribute is guidance only; backend validation is mandatory.
- Continue using parameterized SQL and `html/template` escaping.
- Do not add JavaScript files, script tags, inline event handlers, or JavaScript
  URLs.

## Authentication Regression Rules

The completed authentication release is baseline behavior. Preserve password
registration/login/logout, GitHub and Google OAuth, verified provider identity,
OAuth collision policy, PKCE/state protections, one active UUID session per
user, hardened cookies, environment-only provider secrets, and safe provider
errors. Do not weaken or bypass server-side authorization to implement uploads.

## Working Rules

- Work in the order in `docs/tasks.md`; a phase is complete only when its tests
  and acceptance checks pass.
- Use only standard Go packages plus the exercise-allowed SQLite, bcrypt, and
  UUID dependencies already present in the project.
- Use valid generated JPEG, PNG, and GIF fixtures in tests; do not test only
  filename extensions or signature bytes.
- Use `t.TempDir()` for filesystem tests and real temporary SQLite databases for
  persistence tests.
- Run focused tests plus `gofmt`, `go vet ./...`, `go test ./...`,
  `go test -race ./...`, `go build ./...`, and the Docker build.
- Keep existing authentication and forum tests green throughout the extension.
- Keep secrets, tokens, `.env` files, runtime databases, uploaded images, logs,
  temporary files, caches, and build artifacts out of Git.
- Do not claim README or Docker commands until they have been verified.
- Keep commits small, coherent, tested, and easy for a learner to review.
