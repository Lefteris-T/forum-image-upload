# Forum Image Upload Extension — Task Plan

## Goal

Extend the existing Go forum-authentication project so registered users can create a post with text and an optional image. Users and guests must be able to view the image when reading the post.

Supported image types for this extension:

- JPEG
- PNG
- GIF

Maximum upload size:

- 20 MiB (`20 * 1024 * 1024`, or 20,971,520 bytes), displayed to users as
  20 MB

This plan is based on the uploaded `forum-authentication-lefteris` codebase. It preserves the current layered architecture:

```text
HTTP handler
-> validation / service
-> repository
-> SQLite migrations
-> templates / static files
```

Do not implement everything in one step. Each phase should end with tests and a small git commit.

---

## Current Architecture Notes

Important files already in the project:

- `internal/web/handler/post_creation.go`
  - Handles `GET /posts/new` and `POST /posts`.
  - Currently calls `r.ParseForm()`.
  - Sends `title`, `body`, and `categoryIDs` to `PostService`.

- `internal/service/post.go`
  - Validates authenticated author.
  - Calls `validation.ValidatePost`.
  - Delegates persistence to `PostRepository.Create`.

- `internal/validation/post.go`
  - Owns text/category validation.
  - `PostInput` currently contains `Title`, `Body`, and `CategoryIDs`.

- `internal/repository/posts.go`
  - `Create(authorID, title, body, categoryIDs)` inserts into `posts`.
  - `List`, `Detail`, `ListByCategory`, `ListByAuthor`, and `ListLikedByUser` build post read models.

- `migrations/002_forum_content.sql`
  - Creates the existing `posts` table.

- `internal/database/migrate.go`
  - Applies numbered migrations in order.
  - Add a new migration file instead of editing old applied migrations.

- `templates/new_post.html`
  - Current post form.

- `templates/post.html`
  - Post detail page visible to users and guests.

- `templates/home.html`
  - Post listing page.

- `static/`
  - Already served by the router at `/static/`.
  - A new `static/uploads/` folder can be used for uploaded images.

- `go.mod`
  - Already includes `github.com/google/uuid`.
  - No new third-party package is needed.

---

## Fixed Decisions

- Keep backend in Go.
- Use only allowed packages already compatible with the subject.
- Store image files on disk, not as BLOBs in SQLite.
- Store only the public image path in the database.
- Use a new migration such as `migrations/005_post_images.sql`.
- Keep uploads under `static/uploads/`.
- Add `static/uploads/` to `.gitignore`, but keep the directory with a placeholder such as `.gitkeep`.
- Use UUID filenames so two users cannot overwrite each other's images.
- Do not trust the browser's filename or `Content-Type`.
- Detect the real file type from file bytes using `http.DetectContentType`, then
  confirm that it is a decodable JPEG, PNG, or GIF with the standard-library
  image decoders.
- Do not rely on `Content-Length` or `ParseMultipartForm`'s memory argument as
  an upload-size limit.
- Bound the complete HTTP request and independently enforce the exact image
  limit while streaming at most `MaxImageSize + 1` bytes.
- Accept an image of exactly 20 MiB and reject one of 20 MiB plus one byte.
- If no image is uploaded, post creation should work exactly as before.
- Treat an absent file part as “no image”; reject a selected zero-byte file as
  an invalid image.
- If image validation fails, do not create the post.
- If the database insert fails after saving a file, clean up the saved file.
- Store text-only image paths as SQL `NULL` and expose them to templates as an
  empty string.
- Write uploads through a temporary file and atomically rename them only after
  validation and copying succeed.
- Persist uploads across Docker container replacement.
- Keep guests able to view post images.
- Do not add JavaScript.

---

# Phase 0 — Align Project Documentation

## Goal

Make the repository instructions describe the image-upload extension before
production work begins.

## Documentation Changes

Update `docs/AGENTS.md` and `docs/PRD.md` so they treat the completed
forum-authentication implementation as the baseline and image upload as the
current extension.

The source-of-truth order should become:

1. `docs/exercise-image-upload.md`
2. `docs/audit-image-upload.md`
3. `docs/PRD.md`
4. `docs/tasks.md`
5. `README.md`
6. existing tests and code

Remove stale statements that prohibit image uploads. Preserve authentication,
OAuth, sessions, forum behavior, and all existing security requirements as
regression requirements rather than reimplementing them.

Remove references to deleted authentication exercise/audit files and to
`docs/setup-guide.md` unless that setup guide is intentionally restored.

## Acceptance Checks

- no active project document says image upload is out of scope
- no source-of-truth entry points to a deleted document
- the PRD records the exact supported formats, size boundary, storage approach,
  public visibility, and Docker persistence decision

## Suggested Commit

```text
docs: define image upload extension requirements
```

---

# Phase 1 — Baseline And Flow Check

## Goal

Confirm the current project works before changing it.

## Understand

Trace this current flow:

```text
GET /posts/new
-> PostCreationHandler.handleGet
-> CategoryRepository.All
-> templates/new_post.html

POST /posts
-> PostCreationHandler.handlePost
-> r.ParseForm
-> validation.PostInput
-> PostService.Create
-> PostRepository.Create
-> redirect to /posts/{id}
```

Also confirm:

- `internal/app/app.go` wires `http.FileServer(http.Dir(resolveProjectPath("static")))`.
- `/static/` is already a public route.
- `templates/post.html` is visible to guests.

## Implementation

No production code changes.

## Acceptance Checks

Run:

```bash
go test ./...
```

If local permissions block Go cache creation, use temporary caches so the
working tree is not polluted:

```bash
GOCACHE=/tmp/forum-image-upload-gocache \
GOPATH=/tmp/forum-image-upload-gopath \
go test ./...
```

On PowerShell:

```powershell
$env:GOCACHE=Join-Path $env:TEMP "forum-image-upload-gocache"
$env:GOPATH=Join-Path $env:TEMP "forum-image-upload-gopath"
go test ./...
```

## Suggested Commit

```text
docs: map image upload extension points
```

---

# Phase 2 — Database Field For Post Images

## Goal

Allow posts to remember an optional image path.

## Implementation

Add a new migration:

```text
migrations/005_post_images.sql
```

Suggested schema change:

```sql
ALTER TABLE posts
ADD COLUMN image_path TEXT;
```

Keep it nullable because old posts and text-only posts have no image.

Use one consistent nullable-value policy:

- write `NULL` when `ImagePath` is empty, for example with `NULLIF(?, '')` or a
  nullable query argument
- read it into the Go read models with `COALESCE(p.image_path, '')` or
  `sql.NullString`
- never scan a SQL `NULL` directly into a plain Go string

Update read/write models:

- Add `ImagePath string` to `repository.PostListItem`.
- Add `ImagePath string` to `repository.PostDetail`.

Add `ImagePath string` to `internal/model/post.go` only if that domain model is
used by the implemented flow; it is not currently required by the post read
queries.

Update repository queries:

- `Create` should insert `image_path`.
- `List` should select `p.image_path`.
- `Detail` should select `p.image_path`.
- `ListByCategory` should select `p.image_path`.
- `ListByAuthor` should select `p.image_path`.
- `ListLikedByUser` should select `p.image_path`.

## Tests

Add or update tests in:

- `internal/database/content_schema_test.go`
- `internal/repository/posts_test.go`

Cover:

- migration adds `posts.image_path`
- upgrading a database containing a pre-migration post preserves that post
- creating a text-only post stores SQL `NULL` and reads it as an empty string
- creating a post with an image path returns that path from `Detail`
- list/filter methods preserve image paths
- existing post tests still pass

## Acceptance Checks

```bash
go test ./internal/database ./internal/repository
```

## Suggested Commit

```text
feat: add optional post image field
```

---

# Phase 3 — Image Upload Rules

## Goal

Create one small, testable place that understands image validation.

## Implementation

Add a focused package or file for image upload rules, for example:

```text
internal/upload/image.go
internal/upload/image_test.go
```

Suggested responsibilities:

- maximum size constant: `20 * 1024 * 1024`
- allowed MIME types:
  - `image/jpeg`
  - `image/png`
  - `image/gif`
- map detected MIME type to safe extension:
  - `.jpg`
  - `.png`
  - `.gif`
- exported stable errors:
  - image too large
  - unsupported image type
  - empty image
  - unreadable image

The validation should inspect the actual file bytes, not only the filename or
multipart `Content-Type`. Use `http.DetectContentType` as an initial
classification and then use `image.DecodeConfig` with the standard-library
JPEG, PNG, and GIF decoders to reject truncated or fake files that only contain
a recognizable header.

The size check must read through a bounded reader of `MaxImageSize + 1` bytes.
It must work when `Content-Length` is missing or false. It must not require the
whole upload to be held in memory.

## Tests

Cover:

- accepts small, valid JPEG image bytes
- accepts small, valid PNG image bytes
- accepts small, valid GIF image bytes
- rejects unsupported content
- rejects truncated or fake JPEG/PNG/GIF content
- accepts a file of exactly 20 MiB when it is otherwise a valid supported image
- rejects a file of 20 MiB plus one byte
- enforces the limit when the input size is not declared in advance
- handles a missing optional upload as “no image”
- rejects a present zero-byte upload as an invalid image
- preserves every byte so the saved file is not corrupted after detection

## Acceptance Checks

```bash
go test ./internal/upload
```

## Suggested Commit

```text
feat: validate post image uploads
```

---

# Phase 4 — Disk Storage For Uploaded Images

## Goal

Save valid images safely under `static/uploads/`.

## Implementation

Extend the upload package or add a small storage type, for example:

```text
internal/upload/storage.go
```

Suggested behavior:

- ensure `static/uploads/` exists on startup or before saving
- generate filenames using `github.com/google/uuid`
- save files with a safe extension based on detected content type
- create the directory with predictable permissions such as `0755`
- create a temporary file in the destination filesystem, copy and validate the
  bounded input, close it, then atomically rename it to the final UUID filename
- use predictable file permissions such as `0644`
- remove temporary or partial files on every failed read, validation, close, or
  rename operation
- return a browser path like:

```text
/static/uploads/{uuid}.png
```

Do not use the original uploaded filename for storage.

Expose deletion through the storage abstraction rather than letting the HTTP
handler translate arbitrary public paths into filesystem paths. A small
handler-facing interface can conceptually provide:

```text
Save(image input) -> public path
Delete(public path)
```

`Delete` must only accept paths created inside the configured upload directory.
Cleanup errors should be logged safely and must never expose filesystem paths to
the browser.

Update `.gitignore`:

```text
static/uploads/*
!static/uploads/.gitkeep
```

Add:

```text
static/uploads/.gitkeep
```

## Tests

Use `t.TempDir()` for storage tests.

Cover:

- saves a valid image
- generated filename is unique
- returned path starts with `/static/uploads/`
- unsupported file is not saved
- oversized file is not saved
- storage creates the upload directory if missing
- read/copy failure leaves no partial file
- invalid image leaves no temporary file
- deletion removes a stored image
- deletion rejects paths outside the configured upload directory
- generated image bytes are identical to the accepted input

## Acceptance Checks

```bash
go test ./internal/upload
```

## Suggested Commit

```text
feat: store uploaded post images
```

---

# Phase 5 — Service Contract For Optional Image Path

## Goal

Extend post creation without pushing HTTP upload details into the service or repository.

## Implementation

Update `validation.PostInput`:

```text
Title
Body
CategoryIDs
ImagePath
```

Keep image binary/file handling outside `validation.ValidatePost`; that function should still validate post text/category data and treat `ImagePath` as already-safe internal data.

Only the upload storage may produce a non-empty `ImagePath`; never populate it
from an ordinary form text value supplied by the browser.

Update `service.PostCreator` and `PostService.Create` so the repository receives the validated image path.

Suggested direction:

```text
PostService.Create(authorID, validation.PostInput)
-> ValidatePost
-> posts.Create(authorID, title, body, categoryIDs, imagePath)
```

## Tests

Update:

- `internal/validation/post_test.go`
- `internal/service/post_test.go`

Cover:

- text-only post still works
- post with `ImagePath` passes it to repository
- invalid title/body/category still stops before repository
- guest still cannot create a post

## Acceptance Checks

```bash
go test ./internal/validation ./internal/service
```

## Suggested Commit

```text
feat: pass optional image path through post service
```

---

# Phase 6 — Multipart Post Creation Handler

## Goal

Make `POST /posts` accept normal form fields plus an optional image.

## Implementation

Update `templates/new_post.html`:

```html
<form method="post" action="/posts" enctype="multipart/form-data">
```

Add a file input:

```html
<input
    id="image"
    name="image"
    type="file"
    accept="image/jpeg,image/png,image/gif"
>
```

Add visible help text next to the input explaining that the image is optional,
only JPEG/PNG/GIF are accepted, and the maximum size is 20 MB. The `accept`
attribute is browser guidance only; backend validation remains mandatory.

Update `internal/web/handler/post_creation.go`:

- wrap the request body with `http.MaxBytesReader` before multipart parsing;
  allow enough bounded overhead for multipart fields while enforcing the exact
  image limit separately
- replace `r.ParseForm()` with multipart-aware parsing and call
  `r.MultipartForm.RemoveAll()` when temporary multipart files may exist
- do not use `Content-Length` as proof that the request is safe
- keep existing category parsing behavior
- call upload validation/storage only when a file is present
- pass the returned image path through `validation.PostInput.ImagePath`
- return `400 Bad Request` with a useful message for:
  - image too large
  - unsupported image type
  - malformed multipart form
- return `500 Internal Server Error` for unexpected storage failure
- if post creation fails for any reason after image save, including ordinary
  title/body/category validation, delete the saved image file
- distinguish expected validation failures from unexpected service/database
  failures instead of mapping every service error to `400`

Keep the handler responsible for HTTP parsing. Keep the service responsible for post business validation. Keep the repository responsible for SQL only.

Preserve URL-encoded text-only post submissions if practical so existing clients
and regression tests continue to work. The browser form itself should use
multipart encoding.

Use clear, stable messages for expected upload errors. For example:

```text
Image is too big. Maximum size is 20 MB.
Only JPEG, PNG, and GIF images are supported.
The selected image could not be read.
```

## Tests

Update `internal/web/handler/posts_test.go` or add a new focused handler test file.

Cover:

- existing URL-encoded text-only post test still passes
- authenticated user can create a multipart post without image
- authenticated user can create a multipart post with PNG/JPEG/GIF
- invalid category still returns `400`
- oversized image returns `400` and does not call service
- exactly 20 MiB is accepted and 20 MiB plus one byte is rejected
- chunked or unknown-length oversized input is rejected
- an excessive complete multipart request is rejected before unbounded parsing
- unsupported image returns `400` and does not call service
- selected zero-byte image returns `400` and does not call service
- malformed multipart input stores nothing and does not call service
- guest upload returns `401`
- validation failure after upload deletes the stored file
- unexpected service/database failure after upload deletes the stored file and
  returns a safe `500`
- multipart temporary files are cleaned up

## Acceptance Checks

```bash
go test ./internal/web/handler
```

## Suggested Commit

```text
feat: accept optional image in post form
```

---

# Phase 7 — Render Images For Guests And Users

## Goal

Show saved images on post pages, and optionally show a preview in the feed.

## Implementation

Update `templates/post.html`:

```html
{{if .Post.ImagePath}}
    <img
        class="post-image"
        src="{{.Post.ImagePath}}"
        alt="Post image"
    >
{{end}}
```

Optionally update `templates/home.html` with a smaller preview:

```html
{{if .ImagePath}}
    <img
        class="post-image-preview"
        src="{{.ImagePath}}"
        alt="Post image preview"
    >
{{end}}
```

Update `static/style.css` with responsive image styles:

```css
.post-image,
.post-image-preview {
    display: block;
    max-width: 100%;
    height: auto;
    border-radius: 12px;
}
```

## Tests

Update:

- `internal/web/handler/posts_test.go`
- `internal/web/view/templates_test.go`

Cover:

- post detail renders `<img>` when `ImagePath` exists
- post detail does not render broken image markup when `ImagePath` is empty
- the create-post form clearly states the optional image, supported types, and
  20 MB limit
- a guest request to the stored image URL returns `200`, the expected
  `Content-Type`, and bytes identical to the upload
- guests can view the post and its image after the creating user logs out
- public pages still contain no JavaScript
- user text is still escaped

## Acceptance Checks

```bash
go test ./internal/web/handler ./internal/web/view
```

## Suggested Commit

```text
feat: render post images
```

---

# Phase 8 — Full Integration And Error Handling

## Goal

Verify the complete user story through the real router and app wiring.

## Implementation

Update app wiring if needed so the post creation handler receives upload storage configuration.

Possible constructor direction:

```text
NewPostCreationHandler(postService, categories, renderer, imageStorage)
```

Use `resolveProjectPath("static/uploads")` or an equivalent project-root-safe path when wiring production storage.

The Docker image currently stores `/app/static/uploads` only in the writable
container layer. Update `compose.yml` to mount a dedicated named volume at:

```text
/app/static/uploads
```

Declare that volume next to the existing database volume. Ensure the non-root
forum user can write to it. Uploaded images must remain available when the
Compose container is replaced, not merely while one container process remains
alive.

Keep all website errors safe:

- too large: clear `400` message
- unsupported type: clear `400` message
- malformed upload: `400`
- unauthenticated: `401`
- unexpected save/database failure: safe `500`

Do not expose filesystem paths, SQL errors, or internal stack details to users.

## Tests

Add integration coverage around the real HTTP stack where practical.

Cover:

- registered/logged-in user creates a post with an image
- redirect goes to `/posts/{id}`
- guest can open `/posts/{id}` and see the image
- guest can request the image URL and receive matching bytes and MIME type
- upload bigger than 20 MB fails and creates no post
- unsupported upload fails and creates no post
- text-only post still works
- old posts without images still render correctly
- replacing the Compose container does not remove previously uploaded images

## Acceptance Checks

```bash
go test ./internal/web ./internal/app ./...
```

Manual browser check:

1. Register or log in.
2. Open `Create Post`.
3. Create a post with title, body, category, and a PNG/JPEG/GIF image.
4. Confirm redirect to the post detail page.
5. Log out.
6. Open the post again as guest.
7. Confirm the image is visible.
8. Try an unsupported file.
9. Try an image larger than 20 MB.
10. Create an image post through Docker Compose, replace the container, and
    confirm that both the post and image still load.

## Suggested Commit

```text
test: cover image upload flow
```

---

# Phase 9 — Cleanup, Documentation, Final Audit

## Goal

Make the extension easy to review and safe to submit.

## Implementation

Update project docs:

- `README.md`
- `docs/AGENTS.md`
- `docs/PRD.md`

Phase 0 establishes the new source of truth. In this final phase, verify and
refine those documents so they describe the implementation that was actually
completed and tested.

Mention:

- supported image types
- maximum upload size
- where uploaded files are stored
- uploads are ignored by git
- how Docker persists uploaded files
- how to run tests

## Final Checks

Run:

```bash
gofmt -w $(git ls-files '*.go')
go test ./...
go test -race ./...
go vet ./...
go build ./...
docker build .
```

Optional manual checks:

- no uploaded files committed
- `.env` and database files still ignored
- `static/uploads/.gitkeep` is the only committed file under uploads
- no new dependency was added unnecessarily
- no JavaScript was introduced
- guests can see images but cannot create posts
- authenticated users can create posts with or without images
- failed upload attempts do not leave orphan database rows
- failed database inserts do not leave orphan image files
- exact 20 MiB images are accepted and 20 MiB plus one byte is rejected
- unsupported SVG or text content is rejected with a clear message
- Docker container replacement preserves uploaded images

## Suggested Commit

```text
docs: document forum image uploads
```

---

## Final Submission Checklist

- `go test ./...` passes.
- `go test -race ./...` passes.
- `go vet ./...` passes.
- `go build ./...` passes.
- `docker build .` passes.
- JPEG upload works.
- PNG upload works.
- GIF upload works.
- File larger than 20 MB is rejected with a clear message.
- The exact size boundary is tested independently of `Content-Length`.
- Unsupported file type is rejected.
- Truncated or fake image content is rejected.
- Text-only post creation still works.
- Registered users can create image posts.
- Guests cannot create posts.
- Guests can view images on existing posts.
- Uploaded files are served only from `/static/uploads/`.
- Database stores image paths, not image bytes.
- Filenames are generated by the server, not trusted from users.
- Partial and temporary files are cleaned up after failures.
- Uploaded files survive Docker container replacement.
- The code follows the existing handler/service/repository structure.

## Suggested Final Commit

```text
chore: complete forum image upload extension
```
