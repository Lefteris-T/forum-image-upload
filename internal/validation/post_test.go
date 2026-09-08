package validation

import (
	"strings"
	"testing"
)

func TestValidatePostAcceptsValidInput(t *testing.T) {
	input := PostInput{
		Title:       "  My first post  ",
		Body:        "  This is the body.  ",
		CategoryIDs: []int64{1, 2},
		ImagePath:   "/static/uploads/550e8400-e29b-41d4-a716-446655440000.png",
	}

	got, err := ValidatePost(input)
	if err != nil {
		t.Fatalf("ValidatePost() error = %v, want nil", err)
	}

	if got.Title != "My first post" {
		t.Fatalf(
			"Title = %q, want %q",
			got.Title,
			"My first post",
		)
	}

	if got.Body != "This is the body." {
		t.Fatalf(
			"Body = %q, want %q",
			got.Body,
			"This is the body.",
		)
	}

	if len(got.CategoryIDs) != 2 {
		t.Fatalf(
			"len(CategoryIDs) = %d, want 2",
			len(got.CategoryIDs),
		)
	}

	if got.ImagePath != input.ImagePath {
		t.Fatalf("ImagePath = %q, want %q", got.ImagePath, input.ImagePath)
	}
}

func TestValidatePostRejectsWhitespaceTitleOrBody(t *testing.T) {
	tests := []struct {
		name  string
		input PostInput
	}{
		{
			name: "blank title",
			input: PostInput{
				Title:       "   ",
				Body:        "valid body",
				CategoryIDs: []int64{1},
			},
		},
		{
			name: "blank body",
			input: PostInput{
				Title:       "valid title",
				Body:        "   ",
				CategoryIDs: []int64{1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePost(tt.input)
			if err == nil {
				t.Fatal("ValidatePost() error = nil, want error")
			}
		})
	}
}
func TestValidatePostRejectsMissingCategories(t *testing.T) {
	input := PostInput{
		Title:       "valid title",
		Body:        "valid body",
		CategoryIDs: nil,
	}

	_, err := ValidatePost(input)
	if err == nil {
		t.Fatal("ValidatePost() error = nil, want error")
	}
}

func TestValidatePostRejectsDuplicateCategories(t *testing.T) {
	input := PostInput{
		Title:       "valid title",
		Body:        "valid body",
		CategoryIDs: []int64{1, 1},
	}

	_, err := ValidatePost(input)
	if err == nil {
		t.Fatal("ValidatePost() error = nil, want error")
	}
}

func TestValidatePostAcceptsOneOrManyCategories(t *testing.T) {
	tests := []struct {
		name string
		ids  []int64
	}{
		{
			name: "one category",
			ids:  []int64{1},
		},
		{
			name: "many categories",
			ids:  []int64{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := PostInput{
				Title:       "valid title",
				Body:        "valid body",
				CategoryIDs: tt.ids,
			}

			_, err := ValidatePost(input)
			if err != nil {
				t.Fatalf(
					"ValidatePost() error = %v, want nil",
					err,
				)
			}
		})
	}
}
func TestValidatePostRejectsTooLongTitleOrBody(t *testing.T) {
	tests := []struct {
		name  string
		input PostInput
	}{
		{
			name: "title too long",
			input: PostInput{
				Title:       strings.Repeat("a", MaxPostTitleLength+1),
				Body:        "valid body",
				CategoryIDs: []int64{1},
			},
		},
		{
			name: "body too long",
			input: PostInput{
				Title:       "valid title",
				Body:        strings.Repeat("a", MaxPostBodyLength+1),
				CategoryIDs: []int64{1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidatePost(tt.input)
			if err == nil {
				t.Fatal("ValidatePost() error = nil, want error")
			}
		})
	}
}
