package service

import (
	"errors"

	"forum/internal/validation"
)

// ErrAuthenticationRequired is shared by protected content operations.
var ErrAuthenticationRequired = errors.New("authentication required")

type PostCreator interface {
	Create(
		authorID int64,
		title string,
		body string,
		categoryIDs []int64,
		imagePath string,
	) (int64, error)
}

// PostService validates post creation before delegating persistence.
type PostService struct {
	posts PostCreator
}

func NewPostService(
	posts PostCreator,
) *PostService {
	return &PostService{
		posts: posts,
	}
}

// Create requires an authenticated author and validated post input.
func (s *PostService) Create(
	authorID int64,
	input validation.PostInput,
) (int64, error) {
	if authorID <= 0 {
		return 0, ErrAuthenticationRequired
	}

	validated, err := validation.ValidatePost(input)
	if err != nil {
		return 0, err
	}

	return s.posts.Create(
		authorID,
		validated.Title,
		validated.Body,
		validated.CategoryIDs,
		"",
	)
}
