package handler

import (
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"

	"forum/internal/model"
	"forum/internal/upload"
	"forum/internal/validation"
	"forum/internal/web/middleware"
	"forum/internal/web/view"
)

const (
	multipartMemoryLimit = 8 * 1024 * 1024
	maxPostRequestSize   = 32 * 1024 * 1024
)

const (
	imageTooLargeMessage    = "Image is too big. Maximum size is 20 MB."
	unsupportedImageMessage = "Only JPEG, PNG, and GIF images are supported."
	unreadableImageMessage  = "The selected image could not be read."
)

// PostCreationService is the validated post write required by the handler.
type PostCreationService interface {
	Create(
		authorID int64,
		input validation.PostInput,
	) (int64, error)
}

// CategoryReader supplies choices and validates submitted category identifiers.
type CategoryReader interface {
	All() ([]model.Category, error)
}

// PostImageStorage owns validated post-image files and their cleanup.
type PostImageStorage interface {
	Save(io.Reader) (string, error)
	Delete(publicPath string) error
}

// PostCreationHandler renders the protected form and processes submissions.
type PostCreationHandler struct {
	service    PostCreationService
	categories CategoryReader
	renderer   *view.Renderer
	images     PostImageStorage
}

type newPostPageData struct {
	Categories  []model.Category
	CurrentUser *model.User
}

// NewPostCreationHandler constructs post-creation HTTP behavior.
func NewPostCreationHandler(
	service PostCreationService,
	categories CategoryReader,
	renderer *view.Renderer,
	images PostImageStorage,
) *PostCreationHandler {
	return &PostCreationHandler{
		service:    service,
		categories: categories,
		renderer:   renderer,
		images:     images,
	}
}

// ServeHTTP requires a current user for both viewing and submitting the form.
func (h *PostCreationHandler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/posts/new":
		h.handleGet(w, r)

	case r.Method == http.MethodPost && r.URL.Path == "/posts":
		h.handlePost(w, r)

	default:
		w.Header().Set("Allow", "GET, POST")

		http.Error(
			w,
			http.StatusText(http.StatusMethodNotAllowed),
			http.StatusMethodNotAllowed,
		)
	}
}

func (h *PostCreationHandler) handleGet(
	w http.ResponseWriter,
	r *http.Request,
) {
	user, ok := middleware.CurrentUser(r.Context())
	if !ok {
		http.Error(
			w,
			http.StatusText(http.StatusUnauthorized),
			http.StatusUnauthorized,
		)
		return
	}

	categories, err := h.categories.All()
	if err != nil {
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
		return
	}

	data := newPostPageData{
		Categories:  categories,
		CurrentUser: &user,
	}

	if err := h.renderer.Render(
		w,
		http.StatusOK,
		"new_post.html",
		data,
	); err != nil {
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}

func (h *PostCreationHandler) handlePost(
	w http.ResponseWriter,
	r *http.Request,
) {
	user, ok := middleware.CurrentUser(r.Context())
	if !ok {
		http.Error(
			w,
			http.StatusText(http.StatusUnauthorized),
			http.StatusUnauthorized,
		)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPostRequestSize)

	contentType := r.Header.Get("Content-Type")
	mediaType, _, mediaTypeErr := mime.ParseMediaType(contentType)
	if contentType != "" && mediaTypeErr != nil {
		http.Error(
			w,
			http.StatusText(http.StatusBadRequest),
			http.StatusBadRequest,
		)
		return
	}
	multipartRequest := mediaType == "multipart/form-data"

	var parseErr error
	if multipartRequest {
		parseErr = r.ParseMultipartForm(multipartMemoryLimit)
		if r.MultipartForm != nil {
			defer func() {
				if err := r.MultipartForm.RemoveAll(); err != nil {
					log.Printf("multipart temporary-file cleanup failed: %v", err)
				}
			}()
		}
	} else {
		parseErr = r.ParseForm()
	}

	if parseErr != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(parseErr, &maxBytesErr) {
			http.Error(w, imageTooLargeMessage, http.StatusBadRequest)
			return
		}

		http.Error(
			w,
			http.StatusText(http.StatusBadRequest),
			http.StatusBadRequest,
		)
		return
	}

	categoryValues := r.Form["category"]

	categoryIDs := make(
		[]int64,
		0,
		len(categoryValues),
	)

	for _, value := range categoryValues {
		id, err := strconv.ParseInt(
			value,
			10,
			64,
		)
		if err != nil || id <= 0 {
			http.Error(
				w,
				http.StatusText(http.StatusBadRequest),
				http.StatusBadRequest,
			)
			return
		}

		categoryIDs = append(
			categoryIDs,
			id,
		)
	}

	imagePath := ""
	if multipartRequest {
		imageFile, _, err := r.FormFile("image")
		switch {
		case errors.Is(err, http.ErrMissingFile):
			// An absent image part is a valid text-only post.

		case err != nil:
			http.Error(
				w,
				http.StatusText(http.StatusBadRequest),
				http.StatusBadRequest,
			)
			return

		default:
			defer func() {
				if err := imageFile.Close(); err != nil {
					log.Printf("multipart image close failed: %v", err)
				}
			}()

			if h.images == nil {
				http.Error(
					w,
					http.StatusText(http.StatusInternalServerError),
					http.StatusInternalServerError,
				)
				return
			}

			imagePath, err = h.images.Save(imageFile)
			if err != nil {
				writeImageUploadError(w, err)
				return
			}
		}
	}

	input := validation.PostInput{
		Title:       r.FormValue("title"),
		Body:        r.FormValue("body"),
		CategoryIDs: categoryIDs,
		ImagePath:   imagePath,
	}

	postID, err := h.service.Create(
		user.ID,
		input,
	)
	if err != nil {
		if imagePath != "" {
			if deleteErr := h.images.Delete(imagePath); deleteErr != nil {
				log.Printf("post image cleanup failed: %v", deleteErr)
			}
		}

		writePostCreationError(w, err)
		return
	}

	http.Redirect(
		w,
		r,
		"/posts/"+strconv.FormatInt(postID, 10),
		http.StatusSeeOther,
	)
}

func writePostCreationError(w http.ResponseWriter, err error) {
	if isPostValidationError(err) {
		http.Error(
			w,
			http.StatusText(http.StatusBadRequest),
			http.StatusBadRequest,
		)
		return
	}

	http.Error(
		w,
		http.StatusText(http.StatusInternalServerError),
		http.StatusInternalServerError,
	)
}

func isPostValidationError(err error) bool {
	return errors.Is(err, validation.ErrPostTitleRequired) ||
		errors.Is(err, validation.ErrPostBodyRequired) ||
		errors.Is(err, validation.ErrPostTitleTooLong) ||
		errors.Is(err, validation.ErrPostBodyTooLong) ||
		errors.Is(err, validation.ErrPostCategoryRequired) ||
		errors.Is(err, validation.ErrPostDuplicateCategory)
}

func writeImageUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, upload.ErrImageTooLarge):
		http.Error(w, imageTooLargeMessage, http.StatusBadRequest)

	case errors.Is(err, upload.ErrUnsupportedImageType):
		http.Error(w, unsupportedImageMessage, http.StatusBadRequest)

	case errors.Is(err, upload.ErrEmptyImage),
		errors.Is(err, upload.ErrUnreadableImage):
		http.Error(w, unreadableImageMessage, http.StatusBadRequest)

	default:
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}
