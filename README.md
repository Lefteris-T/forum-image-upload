# Forum

A full-stack web forum written in **Go** using the standard `net/http` package, **SQLite**, server-side HTML templates, and plain CSS.

The project uses a layered architecture and test-driven development, with emphasis on authentication, sessions, persistence, HTTP correctness, and clean separation between handlers, services, repositories, and the database.

## Features

- User registration
- Secure password hashing with bcrypt
- Login and logout
- GitHub OAuth login
- Google OAuth / OpenID Connect login
- UUID-based sessions
- One active session per user
- Public post listing and post detail pages
- Authenticated post creation with an optional JPEG, PNG, or GIF image
- Public post-image viewing for authenticated users and guests
- Content-based image validation with an exact 20 MiB limit
- Categories and category filtering
- Comments
- Like / dislike reactions
- Reaction toggle and switch behavior
- Filter posts created by the current user
- Filter posts liked by the current user
- Centralized HTTP routing and method handling
- Consistent HTTP error statuses
- Panic recovery middleware
- Request logging
- SQLite persistence
- Responsive HTML/CSS interface
- Docker and Docker Compose support
- Persistent Docker volumes for SQLite data and uploaded images
- Helper scripts for build, run, and stop

No JavaScript is required by the application.

---

## Tech Stack

- **Go**
- **net/http**
- **html/template**
- **SQLite**
- **bcrypt**
- **UUID sessions**
- **HTML**
- **CSS**
- **Docker**
- **Docker Compose**

---

## Architecture

```text
Browser
   |
   v
Recovery / Logging / Authentication middleware
   |
   v
Router -> HTTP handler -> validation -> service -> repository -> SQLite
   |                                                        |
   +---------------- template / redirect response <---------+

OAuth browser flow
   |
   v
OAuth start/callback -> GitHub or Google -> OAuth login service
   -> local user + OAuth account -> existing forum session

Image upload flow
   |
   v
Bounded multipart request -> verified JPEG/PNG/GIF -> UUID file on disk
   -> public image path in SQLite -> server-rendered post detail
```

Project structure:

```text
forum/
├── cmd/
│   └── forum/
│       └── main.go
├── internal/
│   ├── app/
│   ├── config/
│   ├── database/
│   ├── model/
│   ├── oauth/
│   ├── repository/
│   ├── service/
│   ├── session/
│   ├── upload/
│   ├── validation/
│   └── web/
│       ├── handler/
│       ├── middleware/
│       └── view/
├── migrations/
├── templates/
├── static/
│   └── uploads/
├── scripts/
├── docs/
├── data/
├── Dockerfile
├── compose.yml
├── go.mod
└── README.md
```

---

## Database

The application uses SQLite.

Main tables:

```text
users
sessions
posts
comments
categories
post_categories
post_reactions
comment_reactions
oauth_accounts
```

Important rules:

- normalized email and username are unique
- password values are stored only as bcrypt hashes
- OAuth-only users have no password hash
- provider identities are unique by provider and stable provider user ID
- UUID session identifiers
- one active session per user
- foreign keys enabled
- reaction values limited to `-1` and `1`
- one reaction per user per target
- unique post/category relations
- transactional multi-row writes
- nullable post image paths; image bytes remain on disk

Migrations live in:

```text
migrations/
```

and are applied automatically when the application starts.

The default local database path is:

```text
data/forum.db
```

Runtime database files are ignored by Git.

---

## Post Images

Authenticated users may attach one optional image when creating a post.
Text-only post creation continues to work.

Supported formats:

```text
JPEG
PNG
GIF
```

The exact maximum image size is 20 MiB (`20,971,520` bytes), displayed in the
form as 20 MB. The server checks the actual bytes rather than trusting the
browser filename, declared content type, or `Content-Length`. Empty,
unsupported, malformed, unreadable, and oversized images are rejected without
creating a post.

Validated images are written atomically under:

```text
static/uploads/{uuid}.jpg
static/uploads/{uuid}.png
static/uploads/{uuid}.gif
```

SQLite stores only the public path, such as
`/static/uploads/{uuid}.png`. Runtime uploads are ignored by Git and excluded
from Docker build contexts, while `static/uploads/.gitkeep` retains the
directory in clean checkouts. Uploading requires authentication, but post
images are intentionally public so guests can read image posts.

---

## Filters

Public category filter:

```text
/?category=<id>
```

Authenticated filters:

```text
/?filter=created
/?filter=liked
```

---

## Routes

### Public

```text
GET  /
GET  /register
POST /register
GET  /login
POST /login
GET  /auth/github
GET  /auth/github/callback
GET  /auth/google
GET  /auth/google/callback
GET  /posts/{id}
GET  /static/*
```

### Authenticated

```text
POST /logout
GET  /posts/new
POST /posts
POST /posts/{id}/comments
POST /posts/{id}/react
POST /comments/{id}/react
```

---

## HTTP Behaviour

```text
200  successful page response
303  successful form submission redirect
400  malformed or invalid request
401  authentication required / invalid credentials
403  authenticated user lacks permission
404  resource or route not found
405  method not allowed
409  duplicate registration conflict
502  OAuth provider failure
500  unexpected internal error
```

Invalid image content and images larger than 20 MiB return `400 Bad Request`
with a safe user-facing message.

Successful state-changing form submissions use `303 See Other`.

Ordinary GET requests do not mutate forum content. OAuth callback GET requests
are protocol endpoints and may establish a session after validating the flow.

---

## Sessions

Authentication uses UUID-based session cookies.

Default configuration:

```text
Cookie name: forum_session
Duration:    24h
```

Creating a new session replaces the previous active session for the same user.

Session state is stored in SQLite.

---

## Configuration

Environment variables:

```text
FORUM_ADDRESS
FORUM_DATABASE_PATH
FORUM_SESSION_DURATION
FORUM_COOKIE_NAME
FORUM_SECURE_COOKIE
GITHUB_CLIENT_ID
GITHUB_CLIENT_SECRET
GITHUB_REDIRECT_URL
GOOGLE_CLIENT_ID
GOOGLE_CLIENT_SECRET
GOOGLE_REDIRECT_URL
```

Defaults:

```text
FORUM_ADDRESS=:8080
FORUM_DATABASE_PATH=data/forum.db
FORUM_SESSION_DURATION=24h
FORUM_COOKIE_NAME=forum_session
FORUM_SECURE_COOKIE=false
```

Each OAuth provider is enabled only when all three of its variables are set. A
provider with all three values empty is disabled; partial configuration stops
startup with an error. Disabled providers do not appear on the login and
registration pages.

Local callback URLs:

```text
http://localhost:8080/auth/github/callback
http://localhost:8080/auth/google/callback
```

See [docs/PRD.md](docs/PRD.md) for the current extension requirements and
architecture.

---

## Run Locally

Run without OAuth:

```bash
go run ./cmd/forum
```

Run with real GitHub and/or Google login:

```bash
cp .env.example .env
# Edit .env and fill all three variables for each provider you enable.

set -a
source .env
set +a
go run ./cmd/forum
```
``` or make run

`.env` is intentionally ignored by Git. Do not commit it.

Open:

```text
http://localhost:8080
```

The application creates the local SQLite data directory when needed.
Local uploaded images are created under `static/uploads/` and are ignored by
Git.

---

## Tests

Run the full suite:

```bash
go test ./...
```

Run with the race detector:

```bash
go test -race ./...
```

Format tracked Go files:

```bash
gofmt -w $(git ls-files '*.go')
```

Static analysis:

```bash
go vet ./...
```

Build:

```bash
go build ./...
```

The test suite covers configuration, server lifecycle, migrations,
repositories, validation, authentication, sessions, posts, optional image
uploads, exact size boundaries, cleanup behavior, comments, reactions,
filters, routing, middleware, template rendering, HTTP integration flows, and
real SQLite persistence.

HTTP integration tests use `httptest.Server` with a real temporary SQLite database.

---

## Docker

Build the image:

```bash
docker build -t forum .
```

Run directly:

```bash
docker run --rm \
  --name forum \
  --env-file .env \
  -p 8080:8080 \
  -v forum-data:/app/data \
  -v forum-uploads:/app/static/uploads \
  forum
```

Omit `--env-file .env` when running the container without OAuth or other
environment overrides.

The named volumes:

```text
forum-data
forum-uploads
```

store the SQLite database and uploaded images outside the container so users,
posts, comments, reactions, and post images survive container recreation.

---

## Docker Compose

Start without OAuth:

```bash
docker compose up --build
```

Start with real OAuth credentials from `.env`:

```bash
docker compose --env-file .env up --build
```

Stop:

```bash
docker compose down
```

The Compose configuration uses `forum-data` for SQLite and `forum-uploads` for
`/app/static/uploads`.

Avoid:

```bash
docker compose down -v
```

unless you intentionally want to delete both the persistent database and
uploaded-image volumes.

---

## Helper Scripts

```text
scripts/build.sh
scripts/run.sh
scripts/stop.sh
```

Build:

```bash
./scripts/build.sh
```

Run:

```bash
./scripts/run.sh
```

Stop:

```bash
./scripts/stop.sh
```

The scripts use `set -eu` so failures are not silently ignored.

---

## Logging

Requests are logged with useful HTTP information such as:

```text
method
path
status
```

The logging middleware avoids exposing request bodies, passwords, cookies, or other sensitive values.

Unexpected panics are recovered and converted into safe `500 Internal Server Error` responses so the server can continue handling later requests.

---

## Security

Implemented security-related decisions include:

- bcrypt password hashing
- parameterized SQL queries
- server-side sessions
- UUID session IDs
- unique active session per user
- foreign key enforcement
- protected authenticated routes
- OAuth state bound to the initiating browser with a short-lived cookie
- PKCE S256 for GitHub and Google
- stable provider identifiers and verified provider emails
- transactional creation of local users and OAuth identities
- provider access tokens discarded after identity lookup
- centralized method enforcement
- safe internal error responses
- automatic HTML escaping through `html/template`
- bounded multipart requests and an independently enforced image-byte limit
- content detection plus JPEG/PNG/GIF decoding before image publication
- server-generated UUID image filenames and constrained cleanup paths
- no JavaScript dependency
- runtime databases, uploaded images, and environment files excluded from Git

OAuth client secrets belong only in environment variables or a deployment
secret manager. Never place real credentials in `.env.example`, Compose,
Dockerfiles, source code, documentation, logs, or commits. Rotate a credential
immediately if it is exposed.

For HTTPS deployments:

```text
FORUM_SECURE_COOKIE=true
```

---

## UI

The frontend uses server-rendered HTML and CSS only.

It includes:

- responsive navigation
- post cards
- category badges
- login and registration forms
- post creation form
- optional responsive post images
- comment cards
- like / dislike controls
- developer-themed background artwork
- responsive mobile layout

Static assets are served from:

```text
/static/
```

---

## Useful SQLite Commands

```bash
sqlite3 data/forum.db
```

Inside SQLite:

```sql
.tables
SELECT * FROM users;
SELECT * FROM posts;
SELECT id, title, image_path FROM posts;
SELECT * FROM comments;
```

Exit:

```sql
.quit
```

---

## Development Workflow

```text
write test
→ observe expected failure
→ implement the smallest change
→ run focused tests
→ run full test suite
→ format
→ commit
```

---

## Final Verification

```bash
gofmt -w $(git ls-files '*.go')
go vet ./...
go test ./...
go test -race ./...
go build ./...
docker build -t forum .
```

Also verify manually that:

- registration and login work
- real GitHub and Google login work with exact configured callback URLs
- session replacement behaves correctly
- guests cannot access protected actions
- posts and comments persist
- authenticated users can create text-only and image posts
- JPEG, PNG, and GIF uploads work and remain visible to guests
- unsupported and larger-than-20-MiB images are rejected
- uploaded images survive Compose container replacement
- reactions toggle and switch correctly
- category, created, and liked filters work
- unknown routes return `404`
- invalid methods return `405`
- SQLite data survives container recreation
- no JavaScript exists in the repository
- no database, uploaded image, secret, log, or build artifact is committed

---

## Project Goal

The goal is not only to build a working forum, but to practice the structure and behaviour of a real web application:

- HTTP request lifecycle
- authentication
- session management
- database design
- SQL migrations
- transactional persistence
- middleware
- routing
- server-side rendering
- bounded image validation and filesystem storage
- testing
- containerization
- application configuration

It provides a solid foundation for further work in backend engineering, DevOps, and application security.
