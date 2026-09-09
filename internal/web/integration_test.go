package web

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forum/internal/database"
	"forum/internal/oauth"
	"forum/internal/repository"
	"forum/internal/service"
	sessionpkg "forum/internal/session"
	"forum/internal/upload"
	"forum/internal/web/handler"
	"forum/internal/web/middleware"
	"forum/internal/web/view"
)

func TestIntegrationRegistrationFlow(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	res, err := http.Get(
		server.URL + "/register",
	)
	if err != nil {
		t.Fatalf(
			"GET /register: %v",
			err,
		)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusOK,
		)
	}
}

type integrationEnv struct {
	Server          *httptest.Server
	DB              *sql.DB
	OAuthStateStore *oauth.OAuthStateStore
	UploadDir       string
}

type integrationOAuthProviders struct {
	GitHub oauth.Provider
	Google oauth.Provider
}

func newIntegrationEnv(t *testing.T) *integrationEnv {
	return newIntegrationEnvWithOAuth(t, integrationOAuthProviders{})
}

func newIntegrationEnvWithOAuth(
	t *testing.T,
	providers integrationOAuthProviders,
) *integrationEnv {
	t.Helper()

	// -------------------------------------------------
	// Database
	// -------------------------------------------------

	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf(
			"database.Open(): %v",
			err,
		)
	}

	t.Cleanup(func() {
		db.Close()
	})

	err = database.Migrate(
		db,
		filepath.Join(
			"..",
			"..",
			"migrations",
		),
	)
	if err != nil {
		t.Fatalf(
			"database.Migrate(): %v",
			err,
		)
	}

	// -------------------------------------------------
	// Repositories
	// -------------------------------------------------

	users := repository.NewUserRepository(db)

	sessions := repository.NewSessionRepository(db)

	categories := repository.NewCategoryRepository(db)

	posts := repository.NewPostRepository(db)

	comments := repository.NewCommentRepository(db)

	reactions := repository.NewReactionRepository(db)
	oauthAccounts := repository.NewOAuthAccountRepository(db)

	// -------------------------------------------------
	// Services
	// -------------------------------------------------

	passwords := service.NewPasswordManager()

	authService := service.NewAuthService(
		users,
		passwords,
	)

	sessionDuration := time.Hour

	loginService := service.NewLoginService(
		users,
		passwords,
		sessions,
		sessionDuration,
	)

	postService := service.NewPostService(
		posts,
	)

	commentService := service.NewCommentService(
		comments,
	)

	reactionService := service.NewReactionService(
		reactions,
	)

	// -------------------------------------------------
	// Cookie/session manager
	// -------------------------------------------------

	sessionManager := sessionpkg.NewManager(
		"forum_session",
		sessionDuration,
		false,
	)

	// -------------------------------------------------
	// Templates
	// -------------------------------------------------

	renderer, err := view.NewRenderer(
		filepath.Join(
			"..",
			"..",
			"templates",
		),
	)
	if err != nil {
		t.Fatalf(
			"view.NewRenderer(): %v",
			err,
		)
	}

	staticDir := t.TempDir()
	uploadDir := filepath.Join(staticDir, "uploads")
	imageStorage, err := upload.NewStorage(
		uploadDir,
	)
	if err != nil {
		t.Fatalf("upload.NewStorage(): %v", err)
	}

	// -------------------------------------------------
	// Handlers
	// -------------------------------------------------

	registerHandler := handler.NewRegisterHandler(
		authService,
		renderer,
		providers.GitHub != nil,
		providers.Google != nil,
	)

	loginHandler := handler.NewLoginHandler(
		loginService,
		sessionManager,
		renderer,
		providers.GitHub != nil,
		providers.Google != nil,
	)

	logoutHandler := handler.NewLogoutHandler(
		loginService,
		sessionManager,
	)

	homeHandler := handler.NewHomeHandler(
		posts,
		renderer,
	)

	postDetailHandler := handler.NewPostDetailHandler(
		posts,
		renderer,
	)

	postCreationHandler := handler.NewPostCreationHandler(
		postService,
		categories,
		renderer,
		imageStorage,
	)

	commentHandler := handler.NewCommentSubmissionHandler(
		commentService,
	)

	postReactionHandler := handler.NewPostReactionHandler(
		reactionService,
	)

	commentReactionHandler := handler.NewCommentReactionHandler(
		reactionService,
		comments,
	)

	oauthStateStore := oauth.NewOAuthStateStore()
	oauthLoginService := service.NewOAuthLoginService(oauthAccounts, users)
	oauthSuccessHandler := handler.NewOAuthSuccessHandler(
		oauthLoginService,
		sessionManager,
		loginService,
	)

	var githubOAuthHandler http.Handler
	var githubOAuthCallbackHandler http.Handler
	if providers.GitHub != nil {
		githubOAuthHandler = oauth.NewAuthorizationHandler(
			providers.GitHub,
			"github",
			oauthStateStore,
			"github_oauth_state",
			false,
		)
		githubOAuthCallbackHandler = oauth.NewCallbackHandler(
			providers.GitHub,
			"github",
			oauthStateStore,
			"github_oauth_state",
			false,
			oauthSuccessHandler.Handle,
		)
	}

	var googleOAuthHandler http.Handler
	var googleOAuthCallbackHandler http.Handler
	if providers.Google != nil {
		googleOAuthHandler = oauth.NewAuthorizationHandler(
			providers.Google,
			"google",
			oauthStateStore,
			"google_oauth_state",
			false,
		)
		googleOAuthCallbackHandler = oauth.NewCallbackHandler(
			providers.Google,
			"google",
			oauthStateStore,
			"google_oauth_state",
			false,
			oauthSuccessHandler.Handle,
		)
	}

	// -------------------------------------------------
	// Router
	// -------------------------------------------------

	router := NewForumRouter(
		Handlers{
			Home:                homeHandler,
			Register:            registerHandler,
			Login:               loginHandler,
			Logout:              logoutHandler,
			PostCreation:        postCreationHandler,
			PostDetail:          postDetailHandler,
			CommentCreate:       commentHandler,
			PostReaction:        postReactionHandler,
			CommentReaction:     commentReactionHandler,
			GitHubOAuth:         githubOAuthHandler,
			GitHubOAuthCallback: githubOAuthCallbackHandler,
			GoogleOAuth:         googleOAuthHandler,
			GoogleOAuthCallback: googleOAuthCallbackHandler,

			Static: http.FileServer(
				http.Dir(staticDir),
			),
		},
	)

	// -------------------------------------------------
	// Authentication middleware
	// -------------------------------------------------

	authenticate := middleware.NewAuthentication(
		sessionManager,
		sessions,
		users,
	)

	appHandler := authenticate(router)

	// -------------------------------------------------
	// Recovery + logging
	// -------------------------------------------------

	logger := log.New(
		io.Discard,
		"",
		0,
	)

	appHandler = WithMiddleware(
		logger,
		appHandler,
	)

	// -------------------------------------------------
	// Real HTTP test server
	// -------------------------------------------------

	return &integrationEnv{
		Server:          httptest.NewServer(appHandler),
		DB:              db,
		OAuthStateStore: oauthStateStore,
		UploadDir:       uploadDir,
	}
}
func newIntegrationServer(t *testing.T) *httptest.Server {
	t.Helper()

	env := newIntegrationEnv(t)

	return env.Server
}

func newIntegrationBrowser(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New(): %v", err)
	}

	return &http.Client{
		Jar: jar,
		CheckRedirect: func(
			req *http.Request,
			via []*http.Request,
		) error {
			return http.ErrUseLastResponse
		},
	}
}
func TestIntegrationRegistrationAndLogin(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	browser := newIntegrationBrowser(t)

	registerValues := url.Values{
		"email": {
			"alice@example.com",
		},
		"username": {
			"alice",
		},
		"password": {
			"strong-password-123",
		},
	}

	res, err := browser.PostForm(
		server.URL+"/register",
		registerValues,
	)
	if err != nil {
		t.Fatalf("POST /register: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"register status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	if got := res.Header.Get("Location"); got != "/login" {
		t.Fatalf(
			"register Location = %q, want /login",
			got,
		)
	}

	res.Body.Close()

	loginValues := url.Values{
		"email": {
			"alice@example.com",
		},
		"password": {
			"strong-password-123",
		},
	}

	res, err = browser.PostForm(
		server.URL+"/login",
		loginValues,
	)
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"login status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	if got := res.Header.Get("Location"); got != "/" {
		t.Fatalf(
			"login Location = %q, want /",
			got,
		)
	}

	res.Body.Close()
}
func registerAndLogin(
	t *testing.T,
	server *httptest.Server,
	browser *http.Client,
	email string,
	username string,
) {
	t.Helper()

	res, err := browser.PostForm(
		server.URL+"/register",
		url.Values{
			"email": {
				email,
			},
			"username": {
				username,
			},
			"password": {
				"strong-password-123",
			},
		},
	)
	if err != nil {
		t.Fatalf("POST /register: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"register status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	res.Body.Close()

	res, err = browser.PostForm(
		server.URL+"/login",
		url.Values{
			"email": {
				email,
			},
			"password": {
				"strong-password-123",
			},
		},
	)
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"login status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	res.Body.Close()
}
func TestIntegrationUserCreatesPostGuestCanReadIt(t *testing.T) {
	env := newIntegrationEnv(t)
	server := env.Server
	defer server.Close()

	userBrowser := newIntegrationBrowser(t)
	guestBrowser := newIntegrationBrowser(t)

	registerAndLogin(
		t,
		server,
		userBrowser,
		"alice@example.com",
		"alice",
	)

	res, err := userBrowser.PostForm(
		server.URL+"/posts",
		url.Values{
			"title": {
				"My Go post",
			},
			"body": {
				"Learning integration testing.",
			},
			"category": {
				"2",
			},
		},
	)
	if err != nil {
		t.Fatalf("POST /posts: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"create post status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	location := res.Header.Get("Location")
	res.Body.Close()

	if !strings.HasPrefix(
		location,
		"/posts/",
	) {
		t.Fatalf(
			"Location = %q, want /posts/<id>",
			location,
		)
	}

	res, err = guestBrowser.Get(
		server.URL + location,
	)
	if err != nil {
		t.Fatalf("guest GET post: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf(
			"guest status = %d, want %d",
			res.StatusCode,
			http.StatusOK,
		)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}
	res.Body.Close()

	body := string(bodyBytes)

	if !strings.Contains(
		body,
		"My Go post",
	) {
		t.Fatal("guest cannot see created post title")
	}

	if !strings.Contains(
		body,
		"Learning integration testing.",
	) {
		t.Fatal("guest cannot see created post body")
	}

	if strings.Contains(body, `class="post-image"`) {
		t.Fatal("text-only post rendered image markup")
	}

	var imagePath sql.NullString
	if err := env.DB.QueryRow(
		`SELECT image_path FROM posts WHERE title = ?`,
		"My Go post",
	).Scan(&imagePath); err != nil {
		t.Fatalf("query text-only image path: %v", err)
	}
	if imagePath.Valid {
		t.Fatalf("text-only image_path = %q, want SQL NULL", imagePath.String)
	}
}

func TestIntegrationUserUploadsImageGuestCanViewIt(t *testing.T) {
	env := newIntegrationEnv(t)
	defer env.Server.Close()

	browser := newIntegrationBrowser(t)
	registerAndLogin(
		t,
		env.Server,
		browser,
		"image-user@example.com",
		"image-user",
	)

	imageBytes := mustIntegrationPNG(t)
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	for name, value := range map[string]string{
		"title":    "Post with public image",
		"body":     "This image should remain visible after logout.",
		"category": "2",
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("WriteField(%q): %v", name, err)
		}
	}
	imagePart, err := writer.CreateFormFile("image", "browser-name.png")
	if err != nil {
		t.Fatalf("CreateFormFile(): %v", err)
	}
	if _, err := imagePart.Write(imageBytes); err != nil {
		t.Fatalf("write multipart image: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(
		http.MethodPost,
		env.Server.URL+"/posts",
		&requestBody,
	)
	if err != nil {
		t.Fatalf("create post request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := browser.Do(req)
	if err != nil {
		t.Fatalf("POST /posts: %v", err)
	}
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("create post status = %d, want %d", res.StatusCode, http.StatusSeeOther)
	}
	postLocation := res.Header.Get("Location")
	res.Body.Close()
	if !strings.HasPrefix(postLocation, "/posts/") {
		t.Fatalf("post Location = %q, want /posts/<id>", postLocation)
	}

	var imagePath string
	if err := env.DB.QueryRow(
		`SELECT image_path FROM posts WHERE title = ?`,
		"Post with public image",
	).Scan(&imagePath); err != nil {
		t.Fatalf("query stored image path: %v", err)
	}
	if !strings.HasPrefix(imagePath, "/static/uploads/") {
		t.Fatalf("stored image path = %q, want /static/uploads/ prefix", imagePath)
	}

	logoutResponse, err := browser.PostForm(env.Server.URL+"/logout", url.Values{})
	if err != nil {
		t.Fatalf("POST /logout: %v", err)
	}
	if logoutResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout status = %d, want %d", logoutResponse.StatusCode, http.StatusSeeOther)
	}
	logoutResponse.Body.Close()

	postResponse, err := browser.Get(env.Server.URL + postLocation)
	if err != nil {
		t.Fatalf("guest GET post: %v", err)
	}
	postBody, err := io.ReadAll(postResponse.Body)
	postResponse.Body.Close()
	if err != nil {
		t.Fatalf("read guest post response: %v", err)
	}
	if postResponse.StatusCode != http.StatusOK {
		t.Fatalf("guest post status = %d, want %d", postResponse.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(postBody), `src="`+imagePath+`"`) {
		t.Fatalf("guest post does not render image path %q", imagePath)
	}

	imageResponse, err := browser.Get(env.Server.URL + imagePath)
	if err != nil {
		t.Fatalf("guest GET image: %v", err)
	}
	servedBytes, err := io.ReadAll(imageResponse.Body)
	imageResponse.Body.Close()
	if err != nil {
		t.Fatalf("read guest image response: %v", err)
	}
	if imageResponse.StatusCode != http.StatusOK {
		t.Fatalf("guest image status = %d, want %d", imageResponse.StatusCode, http.StatusOK)
	}
	if contentType := imageResponse.Header.Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("guest image Content-Type = %q, want image/png", contentType)
	}
	if !bytes.Equal(servedBytes, imageBytes) {
		t.Fatal("guest received image bytes different from the upload")
	}
}

func TestIntegrationRejectedUploadCreatesNoPostOrImage(t *testing.T) {
	validPNG := mustIntegrationPNG(t)
	oversizedPNG := append([]byte{}, validPNG...)
	oversizedPNG = append(
		oversizedPNG,
		bytes.Repeat([]byte{0}, 25*1024*1024-len(oversizedPNG))...,
	)

	tests := []struct {
		name      string
		imageData []byte
		wantBody  string
	}{
		{
			name:      "unsupported content",
			imageData: []byte("plain text is not an image"),
			wantBody:  "Only JPEG, PNG, and GIF images are supported.",
		},
		{
			name:      "oversized image",
			imageData: oversizedPNG,
			wantBody:  "Image is too big. Maximum size is 20 MB.",
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newIntegrationEnv(t)
			defer env.Server.Close()

			browser := newIntegrationBrowser(t)
			registerAndLogin(
				t,
				env.Server,
				browser,
				fmt.Sprintf("rejected-%d@example.com", index),
				fmt.Sprintf("rejected-%d", index),
			)

			title := fmt.Sprintf("Rejected image post %d", index)
			req := newIntegrationImagePostRequest(
				t,
				env.Server.URL,
				title,
				tt.imageData,
			)
			res, err := browser.Do(req)
			if err != nil {
				t.Fatalf("POST /posts: %v", err)
			}
			responseBody, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatalf("read rejected-upload response: %v", err)
			}

			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusBadRequest)
			}
			if res.Close {
				t.Fatal("server requested connection close for rejected upload")
			}
			if !strings.Contains(string(responseBody), tt.wantBody) {
				t.Fatalf("response body = %q, want text %q", responseBody, tt.wantBody)
			}

			var postCount int
			if err := env.DB.QueryRow(
				`SELECT COUNT(*) FROM posts WHERE title = ?`,
				title,
			).Scan(&postCount); err != nil {
				t.Fatalf("count rejected posts: %v", err)
			}
			if postCount != 0 {
				t.Fatalf("rejected post count = %d, want 0", postCount)
			}

			entries, err := os.ReadDir(env.UploadDir)
			if err != nil {
				t.Fatalf("read upload directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("upload directory contains %d files after rejection", len(entries))
			}
		})
	}
}

func newIntegrationImagePostRequest(
	t *testing.T,
	serverURL string,
	title string,
	imageBytes []byte,
) *http.Request {
	t.Helper()

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	for name, value := range map[string]string{
		"title":    title,
		"body":     "Integration image upload body",
		"category": "2",
	} {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("WriteField(%q): %v", name, err)
		}
	}

	imagePart, err := writer.CreateFormFile("image", "browser-name.png")
	if err != nil {
		t.Fatalf("CreateFormFile(): %v", err)
	}
	if _, err := imagePart.Write(imageBytes); err != nil {
		t.Fatalf("write multipart image: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/posts", &requestBody)
	if err != nil {
		t.Fatalf("create image-post request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req
}

func mustIntegrationPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255})

	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}

	return data.Bytes()
}
func createPost(
	t *testing.T,
	server *httptest.Server,
	browser *http.Client,
	title string,
) string {
	t.Helper()

	res, err := browser.PostForm(
		server.URL+"/posts",
		url.Values{
			"title": {
				title,
			},
			"body": {
				"Integration test body",
			},
			"category": {
				"2",
			},
		},
	)
	if err != nil {
		t.Fatalf("POST /posts: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"create post status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	location := res.Header.Get("Location")
	res.Body.Close()

	if !strings.HasPrefix(
		location,
		"/posts/",
	) {
		t.Fatalf(
			"Location = %q, want /posts/<id>",
			location,
		)
	}

	return location
}
func TestIntegrationUserCreatesComment(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	browser := newIntegrationBrowser(t)

	registerAndLogin(
		t,
		server,
		browser,
		"alice@example.com",
		"alice",
	)

	postLocation := createPost(
		t,
		server,
		browser,
		"Post with comment",
	)

	postID := strings.TrimPrefix(
		postLocation,
		"/posts/",
	)

	res, err := browser.PostForm(
		server.URL+"/posts/"+postID+"/comments",
		url.Values{
			"body": {
				"My integration comment",
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"POST comment: %v",
			err,
		)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"comment status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	if got := res.Header.Get("Location"); got != postLocation {
		t.Fatalf(
			"Location = %q, want %q",
			got,
			postLocation,
		)
	}

	res.Body.Close()

	res, err = browser.Get(
		server.URL + postLocation,
	)
	if err != nil {
		t.Fatalf(
			"GET post detail: %v",
			err,
		)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusOK,
		)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf(
			"ReadAll(): %v",
			err,
		)
	}
	res.Body.Close()

	body := string(bodyBytes)

	if !strings.Contains(
		body,
		"My integration comment",
	) {
		t.Fatal("created comment is not visible")
	}
}
func TestIntegrationPostReactionUpdatesCount(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	browser := newIntegrationBrowser(t)

	registerAndLogin(
		t,
		server,
		browser,
		"alice@example.com",
		"alice",
	)

	postLocation := createPost(
		t,
		server,
		browser,
		"Post with reaction",
	)

	postID := strings.TrimPrefix(
		postLocation,
		"/posts/",
	)

	// First like.
	res, err := browser.PostForm(
		server.URL+"/posts/"+postID+"/react",
		url.Values{
			"value": {"1"},
		},
	)
	if err != nil {
		t.Fatalf("POST reaction: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"reaction status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	if got := res.Header.Get("Location"); got != postLocation {
		t.Fatalf(
			"Location = %q, want %q",
			got,
			postLocation,
		)
	}

	res.Body.Close()

	res, err = browser.Get(
		server.URL + postLocation,
	)
	if err != nil {
		t.Fatalf("GET post detail: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusOK,
		)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}
	res.Body.Close()

	body := string(bodyBytes)

	if !strings.Contains(
		body,
		"👍 1",
	) {
		t.Fatal("post like count was not updated")
	}

	// Same like again = toggle off.
	res, err = browser.PostForm(
		server.URL+"/posts/"+postID+"/react",
		url.Values{
			"value": {"1"},
		},
	)
	if err != nil {
		t.Fatalf("POST toggle reaction: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"toggle status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	res.Body.Close()

	res, err = browser.Get(
		server.URL + postLocation,
	)
	if err != nil {
		t.Fatalf("GET post after toggle: %v", err)
	}

	bodyBytes, err = io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}
	res.Body.Close()

	body = string(bodyBytes)

	if !strings.Contains(
		body,
		"👍 0",
	) {
		t.Fatal("post like was not toggled off")
	}

	// Now dislike.
	res, err = browser.PostForm(
		server.URL+"/posts/"+postID+"/react",
		url.Values{
			"value": {"-1"},
		},
	)
	if err != nil {
		t.Fatalf("POST dislike: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"dislike status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	res.Body.Close()

	res, err = browser.Get(
		server.URL + postLocation,
	)
	if err != nil {
		t.Fatalf("GET post after dislike: %v", err)
	}

	bodyBytes, err = io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}
	res.Body.Close()

	body = string(bodyBytes)

	if !strings.Contains(
		body,
		"👍 0",
	) {
		t.Fatal("like count should remain 0 after dislike")
	}

	if !strings.Contains(
		body,
		"👎 1",
	) {
		t.Fatal("post dislike count was not updated")
	}
}
func TestIntegrationCreatedFilterShowsOnlyCurrentUserPosts(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	aliceBrowser := newIntegrationBrowser(t)
	bobBrowser := newIntegrationBrowser(t)

	registerAndLogin(
		t,
		server,
		aliceBrowser,
		"alice@example.com",
		"alice",
	)

	registerAndLogin(
		t,
		server,
		bobBrowser,
		"bob@example.com",
		"bob",
	)

	createPost(
		t,
		server,
		aliceBrowser,
		"Alice post",
	)

	createPost(
		t,
		server,
		bobBrowser,
		"Bob post",
	)

	res, err := aliceBrowser.Get(
		server.URL + "/?filter=created",
	)
	if err != nil {
		t.Fatalf("GET created filter: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusOK,
		)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}
	res.Body.Close()

	body := string(bodyBytes)

	if !strings.Contains(
		body,
		"Alice post",
	) {
		t.Fatal("current user's post is missing")
	}

	if strings.Contains(
		body,
		"Bob post",
	) {
		t.Fatal("another user's post appeared in created filter")
	}
}
func TestIntegrationGuestCannotUseCreatedFilter(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	guest := newIntegrationBrowser(t)

	res, err := guest.Get(
		server.URL + "/?filter=created",
	)
	if err != nil {
		t.Fatalf("GET created filter: %v", err)
	}

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusUnauthorized,
		)
	}

	res.Body.Close()
}
func TestIntegrationLikedFilterShowsOnlyCurrentUserLikes(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	aliceBrowser := newIntegrationBrowser(t)
	bobBrowser := newIntegrationBrowser(t)

	registerAndLogin(
		t,
		server,
		aliceBrowser,
		"alice@example.com",
		"alice",
	)

	registerAndLogin(
		t,
		server,
		bobBrowser,
		"bob@example.com",
		"bob",
	)

	alicePostLocation := createPost(
		t,
		server,
		aliceBrowser,
		"Alice post",
	)

	bobPostLocation := createPost(
		t,
		server,
		bobBrowser,
		"Bob post",
	)

	bobPostID := strings.TrimPrefix(
		bobPostLocation,
		"/posts/",
	)

	res, err := aliceBrowser.PostForm(
		server.URL+"/posts/"+bobPostID+"/react",
		url.Values{
			"value": {"1"},
		},
	)
	if err != nil {
		t.Fatalf("POST like: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"like status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	res.Body.Close()

	res, err = aliceBrowser.Get(
		server.URL + "/?filter=liked",
	)
	if err != nil {
		t.Fatalf("GET liked filter: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusOK,
		)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}
	res.Body.Close()

	body := string(bodyBytes)

	if !strings.Contains(
		body,
		"Bob post",
	) {
		t.Fatal("liked post is missing")
	}

	if strings.Contains(
		body,
		"Alice post",
	) {
		t.Fatal("unliked post appeared in liked filter")
	}

	_ = alicePostLocation
}
func TestIntegrationGuestCannotUseLikedFilter(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	guest := newIntegrationBrowser(t)

	res, err := guest.Get(
		server.URL + "/?filter=liked",
	)
	if err != nil {
		t.Fatalf("GET liked filter: %v", err)
	}

	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusUnauthorized,
		)
	}

	res.Body.Close()
}
func createPostWithCategory(
	t *testing.T,
	server *httptest.Server,
	browser *http.Client,
	title string,
	category string,
) string {
	t.Helper()

	res, err := browser.PostForm(
		server.URL+"/posts",
		url.Values{
			"title":    {title},
			"body":     {"Integration test body"},
			"category": {category},
		},
	)
	if err != nil {
		t.Fatalf("POST /posts: %v", err)
	}

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"create post status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	location := res.Header.Get("Location")
	res.Body.Close()

	if !strings.HasPrefix(location, "/posts/") {
		t.Fatalf(
			"Location = %q, want /posts/<id>",
			location,
		)
	}

	return location
}

func TestIntegrationCategoryFilterShowsOnlyMatchingPosts(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	browser := newIntegrationBrowser(t)

	registerAndLogin(
		t,
		server,
		browser,
		"alice@example.com",
		"alice",
	)

	createPostWithCategory(
		t,
		server,
		browser,
		"Go category post",
		"2",
	)

	createPostWithCategory(
		t,
		server,
		browser,
		"DevOps category post",
		"4",
	)

	res, err := browser.Get(
		server.URL + "/?category=2",
	)
	if err != nil {
		t.Fatalf("GET category filter: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusOK,
		)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(): %v", err)
	}
	res.Body.Close()

	body := string(bodyBytes)

	if !strings.Contains(
		body,
		"Go category post",
	) {
		t.Fatal("matching category post is missing")
	}

	if strings.Contains(
		body,
		"DevOps category post",
	) {
		t.Fatal("post from another category appeared")
	}
}
func TestIntegrationImportantHTTPStatuses(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	guest := newIntegrationBrowser(t)

	tests := []struct {
		name   string
		method string
		path   string
		form   url.Values
		want   int
	}{
		{
			name:   "guest cannot create post",
			method: http.MethodPost,
			path:   "/posts",
			form: url.Values{
				"title":    {"Guest post"},
				"body":     {"Should fail"},
				"category": {"2"},
			},
			want: http.StatusUnauthorized,
		},
		{
			name:   "unknown route",
			method: http.MethodGet,
			path:   "/does-not-exist",
			want:   http.StatusNotFound,
		},
		{
			name:   "wrong method logout",
			method: http.MethodGet,
			path:   "/logout",
			want:   http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				res *http.Response
				err error
			)

			if tt.method == http.MethodPost {
				res, err = guest.PostForm(
					server.URL+tt.path,
					tt.form,
				)
			} else {
				req, reqErr := http.NewRequest(
					tt.method,
					server.URL+tt.path,
					nil,
				)
				if reqErr != nil {
					t.Fatalf("NewRequest(): %v", reqErr)
				}

				res, err = guest.Do(req)
			}

			if err != nil {
				t.Fatalf("%s %s: %v", tt.method, tt.path, err)
			}
			defer res.Body.Close()

			if res.StatusCode != tt.want {
				t.Fatalf(
					"status = %d, want %d",
					res.StatusCode,
					tt.want,
				)
			}
		})
	}
}
func TestIntegrationInvalidRegistrationReturns400(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	browser := newIntegrationBrowser(t)

	res, err := browser.PostForm(
		server.URL+"/register",
		url.Values{
			"email":    {"bad-email"},
			"username": {"alice"},
			"password": {"strong-password-123"},
		},
	)
	if err != nil {
		t.Fatalf("POST /register: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusBadRequest,
		)
	}
}

func TestIntegrationAuthFormsDelegateValidationToServer(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	for _, path := range []string{"/login", "/register"} {
		res, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}

		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatalf("read GET %s response: %v", path, err)
		}

		if res.StatusCode != http.StatusOK {
			t.Fatalf(
				"GET %s status = %d, want %d",
				path,
				res.StatusCode,
				http.StatusOK,
			)
		}

		if !strings.Contains(string(body), "novalidate") {
			t.Fatalf("GET %s form does not delegate validation to server", path)
		}
	}

	tests := []struct {
		name        string
		form        url.Values
		wantMessage string
	}{
		{
			name: "missing email",
			form: url.Values{
				"password": {"strong-password-123"},
			},
			wantMessage: "email is required",
		},
		{
			name: "missing password",
			form: url.Values{
				"email": {"alice@example.com"},
			},
			wantMessage: "password is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := http.PostForm(server.URL+"/login", tt.form)
			if err != nil {
				t.Fatalf("POST /login: %v", err)
			}

			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatalf("read POST /login response: %v", err)
			}

			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf(
					"status = %d, want %d",
					res.StatusCode,
					http.StatusBadRequest,
				)
			}

			if !strings.Contains(string(body), tt.wantMessage) {
				t.Fatalf(
					"body does not contain %q: %s",
					tt.wantMessage,
					body,
				)
			}
		})
	}
}

func TestIntegrationDuplicateRegistrationReturns409(t *testing.T) {
	server := newIntegrationServer(t)
	defer server.Close()

	browser := newIntegrationBrowser(t)

	form := url.Values{
		"email":    {"alice@example.com"},
		"username": {"alice"},
		"password": {"strong-password-123"},
	}

	res, err := browser.PostForm(
		server.URL+"/register",
		form,
	)
	if err != nil {
		t.Fatalf("first POST /register: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"first registration status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	res, err = browser.PostForm(
		server.URL+"/register",
		form,
	)
	if err != nil {
		t.Fatalf("duplicate POST /register: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusConflict {
		t.Fatalf(
			"duplicate registration status = %d, want %d",
			res.StatusCode,
			http.StatusConflict,
		)
	}
}
func TestIntegrationSQLiteStoresRegisteredUserWithBcryptHash(t *testing.T) {
	env := newIntegrationEnv(t)
	defer env.Server.Close()

	browser := newIntegrationBrowser(t)

	res, err := browser.PostForm(
		env.Server.URL+"/register",
		url.Values{
			"email": {
				"alice@example.com",
			},
			"username": {
				"alice",
			},
			"password": {
				"strong-password-123",
			},
		},
	)
	if err != nil {
		t.Fatalf("POST /register: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	var (
		email        string
		username     string
		passwordHash string
	)

	err = env.DB.QueryRow(`
		SELECT email, username, password_hash
		FROM users
		WHERE email = ?
	`,
		"alice@example.com",
	).Scan(
		&email,
		&username,
		&passwordHash,
	)
	if err != nil {
		t.Fatalf("query registered user: %v", err)
	}

	if email != "alice@example.com" {
		t.Fatalf(
			"email = %q, want %q",
			email,
			"alice@example.com",
		)
	}

	if username != "alice" {
		t.Fatalf(
			"username = %q, want %q",
			username,
			"alice",
		)
	}

	if passwordHash == "strong-password-123" {
		t.Fatal("password was stored as plaintext")
	}

	if !strings.HasPrefix(passwordHash, "$2") {
		t.Fatalf(
			"password hash = %q, want bcrypt hash",
			passwordHash,
		)
	}
}
func TestIntegrationSQLiteStoresPostAndComment(t *testing.T) {
	env := newIntegrationEnv(t)
	defer env.Server.Close()

	browser := newIntegrationBrowser(t)

	registerAndLogin(
		t,
		env.Server,
		browser,
		"alice@example.com",
		"alice",
	)

	postLocation := createPost(
		t,
		env.Server,
		browser,
		"SQLite audit post",
	)

	postID := strings.TrimPrefix(
		postLocation,
		"/posts/",
	)

	res, err := browser.PostForm(
		env.Server.URL+"/posts/"+postID+"/comments",
		url.Values{
			"body": {
				"SQLite audit comment",
			},
		},
	)
	if err != nil {
		t.Fatalf("POST comment: %v", err)
	}
	res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf(
			"comment status = %d, want %d",
			res.StatusCode,
			http.StatusSeeOther,
		)
	}

	var (
		title    string
		body     string
		authorID int64
	)

	err = env.DB.QueryRow(`
		SELECT title, body, author_id
		FROM posts
		WHERE id = ?
	`, postID).Scan(
		&title,
		&body,
		&authorID,
	)
	if err != nil {
		t.Fatalf("query post: %v", err)
	}

	if title != "SQLite audit post" {
		t.Fatalf(
			"title = %q, want %q",
			title,
			"SQLite audit post",
		)
	}

	if body != "Integration test body" {
		t.Fatalf(
			"body = %q, want %q",
			body,
			"Integration test body",
		)
	}

	if authorID == 0 {
		t.Fatal("post author_id was not stored")
	}

	var (
		commentBody     string
		commentAuthorID int64
		commentPostID   int64
	)

	err = env.DB.QueryRow(`
		SELECT body, author_id, post_id
		FROM comments
		WHERE body = ?
	`, "SQLite audit comment").Scan(
		&commentBody,
		&commentAuthorID,
		&commentPostID,
	)
	if err != nil {
		t.Fatalf("query comment: %v", err)
	}

	if commentBody != "SQLite audit comment" {
		t.Fatalf(
			"comment body = %q, want %q",
			commentBody,
			"SQLite audit comment",
		)
	}

	if commentAuthorID != authorID {
		t.Fatalf(
			"comment author_id = %d, want %d",
			commentAuthorID,
			authorID,
		)
	}

	if fmt.Sprint(commentPostID) != postID {
		t.Fatalf(
			"comment post_id = %d, want %s",
			commentPostID,
			postID,
		)
	}
}
func TestIntegrationSQLiteSchemaWasCreated(t *testing.T) {
	env := newIntegrationEnv(t)
	defer env.Server.Close()

	tables := []string{
		"users",
		"sessions",
		"categories",
		"posts",
		"comments",
		"post_categories",
		"post_reactions",
		"comment_reactions",
	}

	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var sqlStatement string

			err := env.DB.QueryRow(`
				SELECT sql
				FROM sqlite_master
				WHERE type = 'table'
				AND name = ?
			`, table).Scan(&sqlStatement)
			if err != nil {
				t.Fatalf(
					"table %q not found: %v",
					table,
					err,
				)
			}

			if !strings.Contains(
				strings.ToUpper(sqlStatement),
				"CREATE TABLE",
			) {
				t.Fatalf(
					"schema for %q does not contain CREATE TABLE: %q",
					table,
					sqlStatement,
				)
			}
		})
	}
}
