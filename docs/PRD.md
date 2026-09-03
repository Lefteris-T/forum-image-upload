# Forum Image Upload Product Requirements

## Purpose

Extend the completed Go forum-authentication application so a registered user
can create a post containing text and an optional image. The image must remain
associated with the post and must be visible to both authenticated users and
guests whenever they view that post.

The completed authentication and forum behavior is the baseline and must not
regress.

## Release Scope

### Supported post images

The release supports these image formats:

- JPEG
- PNG
- GIF

Other formats, including SVG, are not supported by this release and must be
rejected safely. The browser filename and declared content type are not trusted.

An image is optional. Existing text-only posts and new text-only post creation
continue to work.

### Size limit

The maximum image size is exactly 20 MiB:

```text
20 * 1024 * 1024 = 20,971,520 bytes
```

The UI describes this as a 20 MB maximum. An image of exactly 20 MiB is
accepted when otherwise valid. An image of 20 MiB plus one byte is rejected
with a clear message that the image is too big.

The application does not trust `Content-Length`. It bounds the complete request
and independently measures the streamed image bytes, including for chunked or
otherwise unknown-length requests. Multipart parsing must not permit unbounded
memory or temporary-disk consumption.

### Image validity

The backend determines the image type from its bytes using
`http.DetectContentType`, maps only supported MIME types to safe extensions, and
confirms the content is a decodable JPEG, PNG, or GIF with standard-library
image decoders.

A missing file part means no image. A present zero-byte, truncated, malformed,
unreadable, or unsupported file is rejected. Failed validation creates neither
a post nor a stored image.

## User Experience

### Creating a post

Only an authenticated local forum user—whether signed in by password, GitHub,
or Google—can access the create-post form and submit a post.

The form continues to require the existing title, body, and category fields. It
adds an optional image input and visible guidance explaining:

- the image is optional
- JPEG, PNG, and GIF are supported
- the maximum size is 20 MB

The file input may use an HTML `accept` attribute, but backend validation is
always authoritative.

Successful creation redirects with `303 See Other` to `/posts/{id}`. A
text-only submission behaves as it did before this extension.

### Viewing a post

When a post has an image, its detail page renders that image responsively from a
public URL. Both authenticated users and guests can request the post page and
the image bytes. A post without an image renders no broken or empty image
element.

Showing a smaller image preview in post listings is optional. The post detail
image is mandatory.

## Architecture

The extension preserves the existing layers:

```text
POST /posts
→ authentication middleware
→ post-creation handler and bounded multipart parsing
→ image validator/storage
→ post validation service
→ post repository transaction
→ SQLite image path
→ 303 /posts/{id}

GET /posts/{id}
→ post repository detail
→ server-rendered template
→ public /static/uploads/{generated-name}
```

Responsibilities are separated as follows:

- the handler owns HTTP parsing, authentication context, safe status/message
  mapping, multipart cleanup, and compensating deletion after downstream
  failure
- the upload package owns exact byte limits, verified image type, UUID naming,
  atomic filesystem writes, public-path generation, and constrained deletion
- the validation/service layer owns the existing author, title, body, and
  category rules and passes through only a trusted internal image path
- the repository owns parameterized SQL and transactional post/category writes
- templates render the safe public path using `html/template`

## Storage

### Filesystem

Uploaded image bytes are stored on disk under:

```text
static/uploads/
```

Final public paths have this shape:

```text
/static/uploads/{uuid}.jpg
/static/uploads/{uuid}.png
/static/uploads/{uuid}.gif
```

The server generates the UUID and extension. Original filenames never become
filesystem paths. Files are written through a temporary file on the destination
filesystem, closed successfully, and atomically renamed to avoid exposing
partial images. Temporary or partial files are removed after failures.

Runtime images are ignored by Git. A placeholder such as
`static/uploads/.gitkeep` retains the directory in clean checkouts.

### Docker persistence

Docker Compose mounts persistent storage at `/app/static/uploads`, separately
from or alongside the existing SQLite data volume. The non-root application
user must be able to create and remove managed image files there.

Replacing the application container must not remove previously uploaded images.
The related SQLite post and its image must remain usable together.

### Database

A new numbered migration adds a nullable `posts.image_path` column. Applied
migrations are immutable, and existing posts must survive the upgrade.

- image bytes are never stored in SQLite
- image posts store the generated public path
- text-only posts store SQL `NULL`
- repository reads map `NULL` to an empty Go string safely
- post detail and all list/filter read models preserve the image path
- post creation and category links remain one database transaction

If image saving succeeds but post validation or persistence fails, the handler
attempts to delete the saved image. Cleanup accepts only paths managed by the
configured upload storage.

## HTTP Behavior And Errors

Existing route authorization remains unchanged:

```text
GET  /posts/new                 authenticated
POST /posts                     authenticated
GET  /posts/{id}                public
GET  /static/uploads/{filename} public
```

Expected outcomes:

- successful post creation: `303 See Other`
- malformed multipart form: `400 Bad Request`
- empty, unsupported, malformed, or unreadable image: `400 Bad Request`
- image larger than 20 MiB: `400 Bad Request` with a clear size message
- unauthenticated post creation: `401 Unauthorized`
- unexpected filesystem or database failure: safe `500 Internal Server Error`

Errors must not reveal local filesystem paths, SQL errors, stack traces, or
other internal implementation details. Failed requests must not leave a post
row, category links, multipart temporary files, partial upload files, or a saved
image that should have been rolled back.

URL-encoded text-only submissions should remain supported so existing behavior
and clients do not regress. The browser create-post form uses
`multipart/form-data`.

## Security And Safety

- Upload authorization is enforced server-side before storage work begins.
- Request size is bounded with `http.MaxBytesReader`, with limited multipart
  overhead above the exact image limit.
- The image stream is independently limited to `MaxImageSize + 1` bytes.
- `ParseMultipartForm` memory settings are not treated as security limits.
- Multipart temporary files are explicitly removed.
- Client filenames, extensions, MIME declarations, and content lengths are
  untrusted.
- Only verified JPEG, PNG, and GIF content receives a final stored filename.
- Storage deletion cannot escape the configured upload directory.
- Public image URLs use only server-generated names.
- User text remains escaped by `html/template` and SQL remains parameterized.
- No JavaScript is introduced.

Uploaded images are public by design because guests must see them. Public read
access does not grant upload or filesystem-management permission.

## Baseline Regression Requirements

The completed forum-authentication application remains fully supported:

- email/password registration, login, and logout
- GitHub and Google OAuth login
- verified provider identity and existing collision policy
- OAuth state, PKCE, provider timeout, and secret-handling protections
- UUID-backed sessions, hardened cookies, and one active session per user
- public post/category reading
- authenticated post, comment, and reaction behavior
- category, created-post, and liked-post filters
- SQLite migrations, foreign keys, and transactional writes
- server-rendered HTML/CSS without JavaScript

Image upload must use the current authenticated local user and must not create a
new authentication or authorization path.

## Technology Constraints

- Go backend using the standard library for upload and image handling
- SQLite through the existing exercise-allowed driver
- existing bcrypt and UUID dependencies
- server-rendered HTML and CSS
- Docker-compatible build and persistent runtime storage
- no additional third-party image/upload dependency

## Quality And Acceptance

The release is ready only when:

- valid JPEG, PNG, and GIF uploads create posts successfully
- unsupported and fake image content is rejected
- exactly 20 MiB is accepted and 20 MiB plus one byte is rejected
- unknown-length oversized input is rejected safely
- text-only post creation and old posts still work
- guests can reopen an image post and retrieve identical image bytes with the
  correct content type
- failed validation, storage, or database operations leave no inappropriate
  post row or image file
- Docker container replacement preserves both database content and uploads
- the create-post page clearly explains supported formats and size
- all existing authentication and forum regression tests remain green
- `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`,
  `go build ./...`, and `docker build .` pass
- runtime uploads, secrets, databases, logs, caches, and build artifacts are not
  committed
- the manual checks in `docs/audit-image-upload.md` have been exercised
- README instructions work from a clean checkout

## Out Of Scope

- image editing, resizing, cropping, recompression, or thumbnail generation
- formats other than JPEG, PNG, and GIF
- multiple images per post
- image upload on comments or user profiles
- remote/object/cloud image storage
- image deletion or replacement UI
- new OAuth providers, account linking, or password reset
- moderation, roles, JavaScript, SPA frameworks, or a public JSON API
- HTTPS termination, rate limiting, quotas, and deployment infrastructure
