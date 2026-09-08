package validation

import (
	"errors"
	"strings"
)

const (
	MaxPostTitleLength = 120
	MaxPostBodyLength  = 5000
)

var (
	ErrPostTitleRequired     = errors.New("post title is required")
	ErrPostBodyRequired      = errors.New("post body is required")
	ErrPostTitleTooLong      = errors.New("post title is too long")
	ErrPostBodyTooLong       = errors.New("post body is too long")
	ErrPostCategoryRequired  = errors.New("at least one category is required")
	ErrPostDuplicateCategory = errors.New("duplicate category")
)

// PostInput contains post creation values. ImagePath must come from upload
// storage rather than directly from an untrusted form field.
type PostInput struct {
	Title       string
	Body        string
	CategoryIDs []int64
	ImagePath   string
}

// ValidatePost trims text and requires at least one unique category.
func ValidatePost(input PostInput) (PostInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)

	if input.Title == "" {
		return PostInput{}, ErrPostTitleRequired
	}

	if len(input.Title) > MaxPostTitleLength {
		return PostInput{}, ErrPostTitleTooLong
	}

	if input.Body == "" {
		return PostInput{}, ErrPostBodyRequired
	}

	if len(input.Body) > MaxPostBodyLength {
		return PostInput{}, ErrPostBodyTooLong
	}

	if len(input.CategoryIDs) == 0 {
		return PostInput{}, ErrPostCategoryRequired
	}

	seen := make(map[int64]struct{})

	for _, id := range input.CategoryIDs {
		if _, exists := seen[id]; exists {
			return PostInput{}, ErrPostDuplicateCategory
		}

		seen[id] = struct{}{}
	}

	return input, nil
}
