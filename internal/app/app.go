// Package app wires infrastructure, services, handlers, and server lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"forum/internal/config"
	"forum/internal/database"
	"forum/internal/oauth"
	"forum/internal/repository"
	"forum/internal/service"
	sessionpkg "forum/internal/session"
	"forum/internal/upload"
	"forum/internal/web"
	"forum/internal/web/handler"
	"forum/internal/web/middleware"
	"forum/internal/web/view"
)

const shutdownTimeout = 5 * time.Second

// Run starts the configured server and shuts it down when ctx is cancelled.
// Startup and shutdown failures are returned to the command package.
func Run(ctx context.Context, cfg config.Config) error {
	appHandler, cleanup, err := buildHandler(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	server := &http.Server{
		Addr:    cfg.Address,
		Handler: appHandler,
	}

	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- normalizeServerError(
			server.ListenAndServe(),
		)
	}()

	select {
	case err := <-serverErrors:
		return err

	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			shutdownTimeout,
		)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf(
				"shutdown server: %w",
				err,
			)
		}

		err := <-serverErrors
		return err
	}
}

func buildHandler(
	cfg config.Config,
) (http.Handler, func(), error) {
	// Build dependencies from the outside in: repositories feed services,
	// services feed handlers, and handlers are mounted on the router.
	databaseDir := filepath.Dir(cfg.DatabasePath)

	if err := os.MkdirAll(databaseDir, 0755); err != nil {
		return nil, nil, fmt.Errorf(
			"create database directory: %w",
			err,
		)
	}
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"open database: %w",
			err,
		)
	}

	// The caller owns this cleanup function once construction succeeds.
	cleanup := func() {
		db.Close()
	}

	if err := database.Migrate(
		db,
		resolveProjectPath("migrations"),
	); err != nil {
		cleanup()

		return nil, nil, fmt.Errorf(
			"migrate database: %w",
			err,
		)
	}

	users := repository.NewUserRepository(db)
	sessions := repository.NewSessionRepository(db)
	categories := repository.NewCategoryRepository(db)
	posts := repository.NewPostRepository(db)
	comments := repository.NewCommentRepository(db)
	reactions := repository.NewReactionRepository(db)
	oauthAccounts := repository.NewOAuthAccountRepository(db)

	passwords := service.NewPasswordManager()

	authService := service.NewAuthService(
		users,
		passwords,
	)

	loginService := service.NewLoginService(
		users,
		passwords,
		sessions,
		cfg.SessionDuration,
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

	sessionManager := sessionpkg.NewManager(
		cfg.CookieName,
		cfg.SessionDuration,
		cfg.SecureCookie,
	)

	renderer, err := view.NewRenderer(
		resolveProjectPath("templates"),
	)
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf(
			"create renderer: %w",
			err,
		)
	}

	imageStorage, err := upload.NewStorage(
		resolveProjectPath("static/uploads"),
	)
	if err != nil {
		cleanup()

		return nil, nil, fmt.Errorf(
			"create image storage: %w",
			err,
		)
	}

	registerHandler := handler.NewRegisterHandler(
		authService,
		renderer,
		cfg.GitHub.Enabled,
		cfg.Google.Enabled,
	)

	loginHandler := handler.NewLoginHandler(
		loginService,
		sessionManager,
		renderer,
		cfg.GitHub.Enabled,
		cfg.Google.Enabled,
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

	oauthLoginService := service.NewOAuthLoginService(
		oauthAccounts,
		users,
	)

	oauthSuccessHandler := handler.NewOAuthSuccessHandler(
		oauthLoginService,
		sessionManager,
		loginService,
	)

	var githubOAuthHandler http.Handler
	var githubOAuthCallbackHandler http.Handler
	var googleOAuthHandler http.Handler
	var googleOAuthCallbackHandler http.Handler

	if cfg.GitHub.Enabled {
		githubProviderConfig := oauth.ProviderConfig{
			ClientID:              cfg.GitHub.ClientID,
			ClientSecret:          cfg.GitHub.ClientSecret,
			RedirectURL:           cfg.GitHub.RedirectURL,
			AuthorizationEndpoint: "https://github.com/login/oauth/authorize",
			TokenEndpoint:         "https://github.com/login/oauth/access_token",
			UserEndpoint:          "https://api.github.com/user",
			Client:                oauth.DefaultHTTPClient(),
		}

		githubProvider := oauth.NewGitHubProvider(
			githubProviderConfig,
		)

		githubOAuthHandler = oauth.NewGitHubAuthorizationHandler(
			githubProviderConfig,
			oauthStateStore,
			"github_oauth_state",
			cfg.SecureCookie,
		)

		githubOAuthCallbackHandler = oauth.NewGitHubCallbackHandler(
			githubProvider,
			oauthStateStore,
			"github_oauth_state",
			cfg.SecureCookie,
			oauthSuccessHandler.Handle,
		)
	}

	if cfg.Google.Enabled {
		googleProviderConfig := oauth.ProviderConfig{
			ClientID:              cfg.Google.ClientID,
			ClientSecret:          cfg.Google.ClientSecret,
			RedirectURL:           cfg.Google.RedirectURL,
			AuthorizationEndpoint: "https://accounts.google.com/o/oauth2/v2/auth",
			TokenEndpoint:         "https://oauth2.googleapis.com/token",
			UserEndpoint:          "https://openidconnect.googleapis.com/v1/userinfo",
			Client:                oauth.DefaultHTTPClient(),
		}

		googleProvider := oauth.NewGoogleProvider(
			googleProviderConfig,
		)

		googleOAuthHandler = oauth.NewAuthorizationHandler(
			googleProvider,
			"google",
			oauthStateStore,
			"google_oauth_state",
			cfg.SecureCookie,
		)

		googleOAuthCallbackHandler = oauth.NewCallbackHandler(
			googleProvider,
			"google",
			oauthStateStore,
			"google_oauth_state",
			cfg.SecureCookie,
			oauthSuccessHandler.Handle,
		)
	}
	// Routing describes HTTP shape only; business and persistence rules stay in
	// their respective service and repository layers.
	router := web.NewForumRouter(
		web.Handlers{
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
				http.Dir(resolveProjectPath("static")),
			),
		},
	)

	// Authentication runs before routing so all handlers receive the same
	// request-scoped current-user behavior.
	authenticate := middleware.NewAuthentication(
		sessionManager,
		sessions,
		users,
	)

	appHandler := authenticate(router)

	logger := log.New(
		os.Stdout,
		"",
		log.LstdFlags,
	)

	appHandler = web.WithMiddleware(
		logger,
		appHandler,
	)

	return appHandler, cleanup, nil
}

func normalizeServerError(err error) error {
	// ErrServerClosed is expected during graceful shutdown.
	if errors.Is(
		err,
		http.ErrServerClosed,
	) {
		return nil
	}

	return err
}

func resolveProjectPath(name string) string {
	// Package tests may run below the project root, unlike the built command.
	if _, err := os.Stat(name); err == nil {
		return name
	}

	return filepath.Join("..", "..", name)
}
