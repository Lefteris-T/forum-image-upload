package handler

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forum/internal/model"
	"forum/internal/repository"
	"forum/internal/upload"
	"forum/internal/validation"
	"forum/internal/web/middleware"
	"forum/internal/web/view"
)

type fakeCategoryReader struct {
	categories []model.Category
	err        error
}

func (f *fakeCategoryReader) All() ([]model.Category, error) {
	return f.categories, f.err
}

type fakePostReader struct {
	posts         []repository.PostListItem
	detail        repository.PostDetail
	err           error
	categoryPosts []repository.PostListItem
	categoryErr   error
	categoryID    int64
	authorPosts   []repository.PostListItem
	authorErr     error
	authorID      int64
	likedPosts    []repository.PostListItem
	likedErr      error
	likedUserID   int64
}

func (f *fakePostReader) ListLikedByUser(
	userID int64,
) ([]repository.PostListItem, error) {
	f.likedUserID = userID

	return f.likedPosts, f.likedErr
}

func (f *fakePostReader) ListByAuthor(
	authorID int64,
) ([]repository.PostListItem, error) {
	f.authorID = authorID

	return f.authorPosts, f.authorErr
}

func (f *fakePostReader) List() ([]repository.PostListItem, error) {
	return f.posts, f.err
}

func (f *fakePostReader) Detail(
	id int64,
) (repository.PostDetail, error) {
	return f.detail, f.err
}

type fakePostCreationService struct {
	called   bool
	authorID int64
	input    validation.PostInput

	postID int64
	err    error
}

type fakePostImageStorage struct {
	saveCalled bool
	savedBytes []byte
	publicPath string
	saveErr    error

	deleteCalled bool
	deletedPath  string
	deleteErr    error
}

func (f *fakePostImageStorage) Save(r io.Reader) (string, error) {
	f.saveCalled = true

	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	f.savedBytes = data

	return f.publicPath, f.saveErr
}

func (f *fakePostImageStorage) Delete(publicPath string) error {
	f.deleteCalled = true
	f.deletedPath = publicPath

	return f.deleteErr
}

func (f *fakePostCreationService) Create(
	authorID int64,
	input validation.PostInput,
) (int64, error) {
	f.called = true
	f.authorID = authorID
	f.input = input

	return f.postID, f.err
}
func (f *fakePostReader) ListByCategory(
	categoryID int64,
) ([]repository.PostListItem, error) {
	f.categoryID = categoryID

	return f.categoryPosts, f.categoryErr
}

func TestHomeHandlerEmptyList(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "home.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				{{if .Posts}}
					{{range .Posts}}
						<h2>{{.Title}}</h2>
					{{end}}
				{{else}}
					<p>No posts yet</p>
				{{end}}
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{}

	h := NewHomeHandler(
		posts,
		renderer,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusOK,
		)
	}

	if !strings.Contains(
		rec.Body.String(),
		"No posts yet",
	) {
		t.Fatal("empty state was not rendered")
	}
}
func TestHomeHandlerRendersPostsAndEscapesUserContent(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "home.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				{{range .Posts}}
					<article>
						<h2>{{.Title}}</h2>
						<p>{{.Body}}</p>
						<span>{{.Author.Username}}</span>
					</article>
				{{end}}
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{
		posts: []repository.PostListItem{
			{
				ID:    1,
				Title: `<script>alert("xss")</script>`,
				Body:  `<img src=x onerror=alert(1)>`,
				Author: model.User{
					ID:       42,
					Username: "lefteris",
				},
			},
		},
	}

	h := NewHomeHandler(
		posts,
		renderer,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusOK,
		)
	}

	body := rec.Body.String()

	if !strings.Contains(body, "lefteris") {
		t.Fatal("post author was not rendered")
	}

	if strings.Contains(body, `<script>`) {
		t.Fatal("user script tag was not escaped")
	}

	if strings.Contains(body, `<img src=x`) {
		t.Fatal("user HTML was not escaped")
	}

	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("escaped title was not rendered")
	}
}
func TestHomeHandlerNavigationReflectsAuthentication(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "home.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				<nav>
					{{if .CurrentUser}}
						<span>{{.CurrentUser.Username}}</span>
						<a href="/posts/new">New Post</a>
						<form method="post" action="/logout">
							<button type="submit">Logout</button>
						</form>
					{{else}}
						<a href="/login">Login</a>
						<a href="/register">Register</a>
					{{end}}
				</nav>
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{}

	h := NewHomeHandler(
		posts,
		renderer,
	)

	t.Run("guest", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/",
			nil,
		)

		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		body := rec.Body.String()

		if !strings.Contains(body, "Login") {
			t.Fatal("guest navigation does not contain Login")
		}

		if !strings.Contains(body, "Register") {
			t.Fatal("guest navigation does not contain Register")
		}

		if strings.Contains(body, "New Post") {
			t.Fatal("guest navigation contains New Post")
		}
	})

	t.Run("authenticated user", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/",
			nil,
		)

		ctx := middleware.ContextWithUser(
			req.Context(),
			model.User{
				ID:       42,
				Username: "lefteris",
			},
		)

		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		body := rec.Body.String()

		if !strings.Contains(body, "lefteris") {
			t.Fatal("authenticated username was not rendered")
		}

		if !strings.Contains(body, "New Post") {
			t.Fatal("authenticated navigation does not contain New Post")
		}

		if !strings.Contains(body, "Logout") {
			t.Fatal("authenticated navigation does not contain Logout")
		}

		if strings.Contains(body, ">Login<") {
			t.Fatal("authenticated navigation contains Login")
		}
	})
}
func TestPostDetailHandlerReturnsPost(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "post.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				<h1>{{.Post.Title}}</h1>
				<p>{{.Post.Body}}</p>
				<span>{{.Post.Author.Username}}</span>
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{
		detail: repository.PostDetail{
			ID:    42,
			Title: "Post title",
			Body:  "Post body",
			Author: model.User{
				ID:       1,
				Username: "lefteris",
			},
		},
	}

	h := NewPostDetailHandler(
		posts,
		renderer,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/posts/42",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusOK,
		)
	}

	body := rec.Body.String()

	if !strings.Contains(body, "Post title") {
		t.Fatal("post title was not rendered")
	}

	if !strings.Contains(body, "lefteris") {
		t.Fatal("post author was not rendered")
	}
}
func TestPostDetailHandlerRejectsMalformedID(t *testing.T) {
	posts := &fakePostReader{}

	h := NewPostDetailHandler(
		posts,
		nil,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/posts/not-a-number",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusBadRequest,
		)
	}
}

func TestPostDetailHandlerReturnsNotFound(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "post.html"),
		[]byte(`<html><body>{{.Post.Title}}</body></html>`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{
		err: repository.ErrPostNotFound,
	}

	h := NewPostDetailHandler(
		posts,
		renderer,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/posts/999",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusNotFound,
		)
	}
}
func TestPublicPagesContainNoJavaScript(t *testing.T) {
	dir := t.TempDir()

	templates := map[string]string{
		"home.html": `
			<!doctype html>
			<html>
			<body>
				<h1>Forum</h1>
			</body>
			</html>
		`,
		"post.html": `
			<!doctype html>
			<html>
			<body>
				<h1>{{.Post.Title}}</h1>
				<p>{{.Post.Body}}</p>
			</body>
			</html>
		`,
	}

	for name, content := range templates {
		err := os.WriteFile(
			filepath.Join(dir, name),
			[]byte(content),
			0o644,
		)
		if err != nil {
			t.Fatalf("WriteFile(%s): %v", name, err)
		}
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{
		detail: repository.PostDetail{
			ID:    1,
			Title: "Post",
			Body:  "Body",
		},
	}

	tests := []struct {
		name    string
		handler http.Handler
		path    string
	}{
		{
			name:    "home",
			handler: NewHomeHandler(posts, renderer),
			path:    "/",
		},
		{
			name:    "post detail",
			handler: NewPostDetailHandler(posts, renderer),
			path:    "/posts/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet,
				tt.path,
				nil,
			)

			rec := httptest.NewRecorder()

			tt.handler.ServeHTTP(rec, req)

			body := strings.ToLower(
				rec.Body.String(),
			)

			forbidden := []string{
				"<script",
				"javascript:",
				"onclick=",
				"onerror=",
				"onload=",
			}

			for _, value := range forbidden {
				if strings.Contains(body, value) {
					t.Fatalf(
						"response contains forbidden JavaScript: %q",
						value,
					)
				}
			}
		})
	}
}
func TestNewPostHandlerShowsCategoriesToAuthenticatedUser(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "new_post.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				<form method="post" action="/posts">
					<input name="title">
					<textarea name="body"></textarea>

					{{range .Categories}}
						<label>
							<input
								type="checkbox"
								name="category"
								value="{{.ID}}"
							>
							{{.Name}}
						</label>
					{{end}}

					<button type="submit">Create</button>
				</form>
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	categories := &fakeCategoryReader{
		categories: []model.Category{
			{ID: 1, Name: "General"},
			{ID: 2, Name: "Go"},
		},
	}

	h := NewPostCreationHandler(
		nil,
		categories,
		renderer,
		nil,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/posts/new",
		nil,
	)

	ctx := middleware.ContextWithUser(
		req.Context(),
		model.User{
			ID:       42,
			Username: "lefteris",
		},
	)

	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusOK,
		)
	}

	body := rec.Body.String()

	if !strings.Contains(body, "General") {
		t.Fatal("General category was not rendered")
	}

	if !strings.Contains(body, "Go") {
		t.Fatal("Go category was not rendered")
	}
}
func TestNewPostHandlerRejectsGuest(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "new_post.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				<form method="post" action="/posts"></form>
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	categories := &fakeCategoryReader{}

	h := NewPostCreationHandler(
		nil,
		categories,
		renderer,
		nil,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/posts/new",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusUnauthorized,
		)
	}
}
func TestPostCreationHandlerPOSTAcceptsOneOrManyCategories(t *testing.T) {
	tests := []struct {
		name    string
		form    string
		wantIDs []int64
	}{
		{
			name:    "one category",
			form:    "title=Hello&body=World&category=1",
			wantIDs: []int64{1},
		},
		{
			name:    "many categories",
			form:    "title=Hello&body=World&category=1&category=2&category=4",
			wantIDs: []int64{1, 2, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakePostCreationService{
				postID: 99,
			}

			h := NewPostCreationHandler(
				service,
				nil,
				nil,
				nil,
			)

			req := httptest.NewRequest(
				http.MethodPost,
				"/posts",
				strings.NewReader(tt.form),
			)

			req.Header.Set(
				"Content-Type",
				"application/x-www-form-urlencoded",
			)

			ctx := middleware.ContextWithUser(
				req.Context(),
				model.User{
					ID:       42,
					Username: "lefteris",
				},
			)

			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf(
					"status = %d, want %d",
					rec.Code,
					http.StatusSeeOther,
				)
			}

			if !service.called {
				t.Fatal("post service Create() was not called")
			}

			if service.authorID != 42 {
				t.Fatalf(
					"authorID = %d, want 42",
					service.authorID,
				)
			}

			if len(service.input.CategoryIDs) != len(tt.wantIDs) {
				t.Fatalf(
					"category count = %d, want %d",
					len(service.input.CategoryIDs),
					len(tt.wantIDs),
				)
			}

			for i, wantID := range tt.wantIDs {
				if service.input.CategoryIDs[i] != wantID {
					t.Fatalf(
						"category[%d] = %d, want %d",
						i,
						service.input.CategoryIDs[i],
						wantID,
					)
				}
			}
		})
	}
}

func TestPostCreationHandlerPOSTAcceptsMultipartPostWithoutImage(t *testing.T) {
	service := &fakePostCreationService{postID: 99}
	h := NewPostCreationHandler(service, nil, nil, nil)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string][]string{
		"title":    {"Multipart title"},
		"body":     {"Multipart body"},
		"category": {"1", "2"},
	}
	for name, values := range fields {
		for _, value := range values {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatalf("WriteField(%q): %v", name, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/posts", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = req.WithContext(middleware.ContextWithUser(
		req.Context(),
		model.User{ID: 42, Username: "lefteris"},
	))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if !service.called {
		t.Fatal("post service Create() was not called")
	}
	if service.input.Title != "Multipart title" {
		t.Fatalf("title = %q, want %q", service.input.Title, "Multipart title")
	}
	if service.input.Body != "Multipart body" {
		t.Fatalf("body = %q, want %q", service.input.Body, "Multipart body")
	}
	if len(service.input.CategoryIDs) != 2 ||
		service.input.CategoryIDs[0] != 1 ||
		service.input.CategoryIDs[1] != 2 {
		t.Fatalf("category IDs = %v, want [1 2]", service.input.CategoryIDs)
	}
	if service.input.ImagePath != "" {
		t.Fatalf("image path = %q, want empty", service.input.ImagePath)
	}
}

func TestPostCreationHandlerPOSTSavesSupportedImage(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		extension  string
		imageBytes func(*testing.T) []byte
	}{
		{name: "jpeg", filename: "photo.jpeg", extension: ".jpg", imageBytes: mustPostJPEG},
		{name: "png", filename: "photo.png", extension: ".png", imageBytes: mustPostPNG},
		{name: "gif", filename: "photo.gif", extension: ".gif", imageBytes: mustPostGIF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageBytes := tt.imageBytes(t)
			publicPath := "/static/uploads/550e8400-e29b-41d4-a716-446655440000" + tt.extension

			images := &fakePostImageStorage{publicPath: publicPath}
			service := &fakePostCreationService{postID: 99}
			h := NewPostCreationHandler(service, nil, nil, images)

			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			for name, value := range map[string]string{
				"title":    "Image post",
				"body":     "Post with an image",
				"category": "1",
			} {
				if err := writer.WriteField(name, value); err != nil {
					t.Fatalf("WriteField(%q): %v", name, err)
				}
			}

			filePart, err := writer.CreateFormFile("image", tt.filename)
			if err != nil {
				t.Fatalf("CreateFormFile(): %v", err)
			}
			if _, err := filePart.Write(imageBytes); err != nil {
				t.Fatalf("write multipart image: %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close multipart writer: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/posts", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req = req.WithContext(middleware.ContextWithUser(
				req.Context(),
				model.User{ID: 42, Username: "lefteris"},
			))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
			}
			if !images.saveCalled {
				t.Fatal("image storage Save() was not called")
			}
			if !bytes.Equal(images.savedBytes, imageBytes) {
				t.Fatal("image storage received different bytes")
			}
			if service.input.ImagePath != publicPath {
				t.Fatalf("ImagePath = %q, want %q", service.input.ImagePath, publicPath)
			}
		})
	}
}

func TestPostCreationHandlerPOSTMapsImageStorageErrors(t *testing.T) {
	unexpectedErr := errors.New("disk path /secret/uploads is unavailable")
	tests := []struct {
		name       string
		storageErr error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "oversized image",
			storageErr: upload.ErrImageTooLarge,
			wantStatus: http.StatusBadRequest,
			wantBody:   "Image is too big. Maximum size is 20 MB.",
		},
		{
			name:       "unsupported image",
			storageErr: upload.ErrUnsupportedImageType,
			wantStatus: http.StatusBadRequest,
			wantBody:   "Only JPEG, PNG, and GIF images are supported.",
		},
		{
			name:       "empty image",
			storageErr: upload.ErrEmptyImage,
			wantStatus: http.StatusBadRequest,
			wantBody:   "The selected image could not be read.",
		},
		{
			name:       "unreadable image",
			storageErr: upload.ErrUnreadableImage,
			wantStatus: http.StatusBadRequest,
			wantBody:   "The selected image could not be read.",
		},
		{
			name:       "unexpected storage failure",
			storageErr: unexpectedErr,
			wantStatus: http.StatusInternalServerError,
			wantBody:   http.StatusText(http.StatusInternalServerError),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			images := &fakePostImageStorage{saveErr: tt.storageErr}
			service := &fakePostCreationService{postID: 99}
			h := NewPostCreationHandler(service, nil, nil, images)

			req := newMultipartImagePostRequest(t, mustPostPNG(t))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want text %q", rec.Body.String(), tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), unexpectedErr.Error()) {
				t.Fatal("response leaked internal storage error")
			}
			if service.called {
				t.Fatal("post service Create() was called after image failure")
			}
		})
	}
}

func TestPostCreationHandlerPOSTDeletesImageAfterServiceFailure(t *testing.T) {
	unexpectedErr := errors.New("database /secret/forum.db is unavailable")
	tests := []struct {
		name       string
		serviceErr error
		deleteErr  error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "post validation failure",
			serviceErr: validation.ErrPostTitleRequired,
			wantStatus: http.StatusBadRequest,
			wantBody:   http.StatusText(http.StatusBadRequest),
		},
		{
			name:       "unexpected persistence failure",
			serviceErr: unexpectedErr,
			wantStatus: http.StatusInternalServerError,
			wantBody:   http.StatusText(http.StatusInternalServerError),
		},
		{
			name:       "cleanup failure preserves validation response",
			serviceErr: validation.ErrPostBodyRequired,
			deleteErr:  errors.New("cleanup failed"),
			wantStatus: http.StatusBadRequest,
			wantBody:   http.StatusText(http.StatusBadRequest),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const publicPath = "/static/uploads/550e8400-e29b-41d4-a716-446655440000.png"
			images := &fakePostImageStorage{
				publicPath: publicPath,
				deleteErr:  tt.deleteErr,
			}
			service := &fakePostCreationService{err: tt.serviceErr}
			h := NewPostCreationHandler(service, nil, nil, images)

			req := newMultipartImagePostRequest(t, mustPostPNG(t))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want text %q", rec.Body.String(), tt.wantBody)
			}
			if strings.Contains(rec.Body.String(), unexpectedErr.Error()) {
				t.Fatal("response leaked internal service error")
			}
			if !images.deleteCalled {
				t.Fatal("image storage Delete() was not called")
			}
			if images.deletedPath != publicPath {
				t.Fatalf("deleted path = %q, want %q", images.deletedPath, publicPath)
			}
		})
	}
}

func TestPostCreationHandlerPOSTDoesNotDeleteForTextOnlyFailure(t *testing.T) {
	images := &fakePostImageStorage{}
	service := &fakePostCreationService{err: validation.ErrPostTitleRequired}
	h := NewPostCreationHandler(service, nil, nil, images)

	req := httptest.NewRequest(
		http.MethodPost,
		"/posts",
		strings.NewReader("title=&body=World&category=1"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(middleware.ContextWithUser(
		req.Context(),
		model.User{ID: 42, Username: "lefteris"},
	))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if images.deleteCalled {
		t.Fatal("image storage Delete() was called for a text-only post")
	}
}

func TestPostCreationHandlerPOSTRejectsMalformedMultipart(t *testing.T) {
	images := &fakePostImageStorage{}
	service := &fakePostCreationService{postID: 99}
	h := NewPostCreationHandler(service, nil, nil, images)

	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader("broken multipart body"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
	req = req.WithContext(middleware.ContextWithUser(
		req.Context(),
		model.User{ID: 42, Username: "lefteris"},
	))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if images.saveCalled {
		t.Fatal("image storage Save() was called for malformed multipart data")
	}
	if service.called {
		t.Fatal("post service Create() was called for malformed multipart data")
	}
}

func TestPostCreationHandlerPOSTRejectsExcessiveMultipartRequest(t *testing.T) {
	images := &fakePostImageStorage{}
	service := &fakePostCreationService{postID: 99}
	h := NewPostCreationHandler(service, nil, nil, images)

	req := newMultipartImagePostRequest(
		t,
		bytes.Repeat([]byte{0}, int(maxPostRequestSize)+1),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if images.saveCalled {
		t.Fatal("image storage Save() was called for excessive request")
	}
	if service.called {
		t.Fatal("post service Create() was called for excessive request")
	}
}

func TestPostCreationHandlerPOSTEnforcesExactImageSizeBoundary(t *testing.T) {
	baseImage := mustPostPNG(t)
	if len(baseImage) > upload.MaxImageSize {
		t.Fatalf("base image size = %d, exceeds upload limit", len(baseImage))
	}

	tests := []struct {
		name          string
		size          int
		contentLength int64
		wantStatus    int
		wantService   bool
	}{
		{
			name:          "exactly 20 MiB",
			size:          upload.MaxImageSize,
			contentLength: 0,
			wantStatus:    http.StatusSeeOther,
			wantService:   true,
		},
		{
			name:          "20 MiB plus one with unknown length",
			size:          upload.MaxImageSize + 1,
			contentLength: -1,
			wantStatus:    http.StatusBadRequest,
			wantService:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageBytes := append([]byte{}, baseImage...)
			imageBytes = append(
				imageBytes,
				bytes.Repeat([]byte{0}, tt.size-len(imageBytes))...,
			)

			images, err := upload.NewStorage(filepath.Join(t.TempDir(), "uploads"))
			if err != nil {
				t.Fatalf("upload.NewStorage(): %v", err)
			}
			service := &fakePostCreationService{postID: 99}
			h := NewPostCreationHandler(service, nil, nil, images)

			req := newMultipartImagePostRequest(t, imageBytes)
			if tt.contentLength == -1 {
				req.ContentLength = -1
			}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if service.called != tt.wantService {
				t.Fatalf("service called = %t, want %t", service.called, tt.wantService)
			}
			if tt.wantService && service.input.ImagePath == "" {
				t.Fatal("accepted image post has an empty ImagePath")
			}
		})
	}
}

func TestPostCreationHandlerPOSTRejectsGuestUploadBeforeStorage(t *testing.T) {
	images := &fakePostImageStorage{}
	service := &fakePostCreationService{postID: 99}
	h := NewPostCreationHandler(service, nil, nil, images)

	req := newMultipartImagePostRequest(t, mustPostPNG(t))
	req = req.WithContext(context.Background())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if images.saveCalled {
		t.Fatal("image storage Save() was called for guest upload")
	}
	if service.called {
		t.Fatal("post service Create() was called for guest upload")
	}
}

func TestPostCreationHandlerPOSTRejectsInvalidCategoryBeforeStorage(t *testing.T) {
	images := &fakePostImageStorage{}
	service := &fakePostCreationService{postID: 99}
	h := NewPostCreationHandler(service, nil, nil, images)

	req := newMultipartImagePostRequestWithCategory(t, mustPostPNG(t), "invalid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if images.saveCalled {
		t.Fatal("image storage Save() was called for invalid category")
	}
	if service.called {
		t.Fatal("post service Create() was called for invalid category")
	}
}

func TestPostCreationHandlerPOSTRemovesMultipartTemporaryFiles(t *testing.T) {
	temporaryDir := filepath.Join(t.TempDir(), "multipart-temp")
	if err := os.Mkdir(temporaryDir, 0o755); err != nil {
		t.Fatalf("create multipart temporary directory: %v", err)
	}
	t.Setenv("TMPDIR", temporaryDir)

	imageBytes := mustPostPNG(t)
	imageBytes = append(
		imageBytes,
		bytes.Repeat([]byte{0}, multipartMemoryLimit+1-len(imageBytes))...,
	)

	images := &fakePostImageStorage{
		publicPath: "/static/uploads/550e8400-e29b-41d4-a716-446655440000.png",
	}
	service := &fakePostCreationService{postID: 99}
	h := NewPostCreationHandler(service, nil, nil, images)

	req := newMultipartImagePostRequest(t, imageBytes)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	entries, err := os.ReadDir(temporaryDir)
	if err != nil {
		t.Fatalf("read multipart temporary directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("multipart temporary directory contains %d files after request", len(entries))
	}
}

func newMultipartImagePostRequest(t *testing.T, imageBytes []byte) *http.Request {
	return newMultipartImagePostRequestWithCategory(t, imageBytes, "1")
}

func newMultipartImagePostRequestWithCategory(
	t *testing.T,
	imageBytes []byte,
	category string,
) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title":    "Image post",
		"body":     "Post with an image",
		"category": category,
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("WriteField(%q): %v", name, err)
		}
	}

	filePart, err := writer.CreateFormFile("image", "image.png")
	if err != nil {
		t.Fatalf("CreateFormFile(): %v", err)
	}
	if _, err := filePart.Write(imageBytes); err != nil {
		t.Fatalf("write multipart image: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/posts", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req.WithContext(middleware.ContextWithUser(
		req.Context(),
		model.User{ID: 42, Username: "lefteris"},
	))
}

func mustPostPNG(t *testing.T) []byte {
	t.Helper()

	var data bytes.Buffer
	if err := png.Encode(&data, postTestImage()); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}

	return data.Bytes()
}

func mustPostJPEG(t *testing.T) []byte {
	t.Helper()

	var data bytes.Buffer
	if err := jpeg.Encode(&data, postTestImage(), nil); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}

	return data.Bytes()
}

func mustPostGIF(t *testing.T) []byte {
	t.Helper()

	var data bytes.Buffer
	if err := gif.Encode(&data, postTestImage(), nil); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}

	return data.Bytes()
}

func postTestImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255})

	return img
}

func TestPostCreationHandlerPOSTInvalidInputReturnsBadRequest(t *testing.T) {
	tests := []struct {
		name string
		form string
	}{
		{
			name: "empty title",
			form: "title=&body=World&category=1",
		},
		{
			name: "empty body",
			form: "title=Hello&body=&category=1",
		},
		{
			name: "missing category",
			form: "title=Hello&body=World",
		},
		{
			name: "invalid category id",
			form: "title=Hello&body=World&category=abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakePostCreationService{
				err: validation.ErrPostTitleRequired,
			}

			h := NewPostCreationHandler(
				service,
				nil,
				nil,
				nil,
			)

			req := httptest.NewRequest(
				http.MethodPost,
				"/posts",
				strings.NewReader(tt.form),
			)

			req.Header.Set(
				"Content-Type",
				"application/x-www-form-urlencoded",
			)

			ctx := middleware.ContextWithUser(
				req.Context(),
				model.User{
					ID:       42,
					Username: "lefteris",
				},
			)

			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf(
					"status = %d, want %d",
					rec.Code,
					http.StatusBadRequest,
				)
			}
		})
	}
}
func TestHomeHandlerFiltersByCategory(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "home.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				{{range .Posts}}
					<h2>{{.Title}}</h2>
				{{end}}
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{
		categoryPosts: []repository.PostListItem{
			{
				ID:    10,
				Title: "Go post",
			},
		},
	}

	h := NewHomeHandler(
		posts,
		renderer,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/?category=2",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusOK,
		)
	}

	if posts.categoryID != 2 {
		t.Fatalf(
			"categoryID = %d, want 2",
			posts.categoryID,
		)
	}

	if !strings.Contains(
		rec.Body.String(),
		"Go post",
	) {
		t.Fatal("filtered post was not rendered")
	}
}
func TestHomeHandlerRejectsMalformedCategory(t *testing.T) {
	posts := &fakePostReader{}

	h := NewHomeHandler(
		posts,
		nil,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/?category=abc",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusBadRequest,
		)
	}
}

func TestHomeHandlerReturnsNotFoundForUnknownCategory(t *testing.T) {
	posts := &fakePostReader{
		categoryErr: repository.ErrCategoryNotFound,
	}

	h := NewHomeHandler(
		posts,
		nil,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/?category=999",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusNotFound,
		)
	}
}
func TestHomeHandlerFiltersCreatedPostsForCurrentUser(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "home.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				{{range .Posts}}
					<h2>{{.Title}}</h2>
				{{end}}
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{
		authorPosts: []repository.PostListItem{
			{
				ID:    10,
				Title: "My post",
			},
		},
	}

	h := NewHomeHandler(
		posts,
		renderer,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/?filter=created",
		nil,
	)

	ctx := middleware.ContextWithUser(
		req.Context(),
		model.User{
			ID:       42,
			Username: "lefteris",
		},
	)

	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusOK,
		)
	}

	if posts.authorID != 42 {
		t.Fatalf(
			"authorID = %d, want 42",
			posts.authorID,
		)
	}

	if !strings.Contains(
		rec.Body.String(),
		"My post",
	) {
		t.Fatal("created post was not rendered")
	}
}
func TestHomeHandlerRejectsGuestCreatedFilter(t *testing.T) {
	posts := &fakePostReader{}

	h := NewHomeHandler(
		posts,
		nil,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/?filter=created",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusUnauthorized,
		)
	}
}
func TestHomeHandlerFiltersLikedPostsForCurrentUser(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(
		filepath.Join(dir, "home.html"),
		[]byte(`
			<!doctype html>
			<html>
			<body>
				{{range .Posts}}
					<h2>{{.Title}}</h2>
				{{end}}
			</body>
			</html>
		`),
		0o644,
	)
	if err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	renderer, err := view.NewRenderer(dir)
	if err != nil {
		t.Fatalf("NewRenderer(): %v", err)
	}

	posts := &fakePostReader{
		likedPosts: []repository.PostListItem{
			{
				ID:    10,
				Title: "Liked post",
			},
		},
	}

	h := NewHomeHandler(
		posts,
		renderer,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/?filter=liked",
		nil,
	)

	ctx := middleware.ContextWithUser(
		req.Context(),
		model.User{
			ID:       42,
			Username: "lefteris",
		},
	)

	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusOK,
		)
	}

	if posts.likedUserID != 42 {
		t.Fatalf(
			"liked user ID = %d, want 42",
			posts.likedUserID,
		)
	}

	if !strings.Contains(
		rec.Body.String(),
		"Liked post",
	) {
		t.Fatal("liked post was not rendered")
	}
}
func TestHomeHandlerRejectsGuestLikedFilter(t *testing.T) {
	posts := &fakePostReader{}

	h := NewHomeHandler(
		posts,
		nil,
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/?filter=liked",
		nil,
	)

	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			rec.Code,
			http.StatusUnauthorized,
		)
	}
}
