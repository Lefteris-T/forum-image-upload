package service

import (
	"errors"
	"testing"

	"forum/internal/validation"
)

type fakePostCreator struct {
	called      bool
	authorID    int64
	title       string
	body        string
	categoryIDs []int64
	imagePath   string

	postID int64
	err    error
}

func (f *fakePostCreator) Create(
	authorID int64,
	title string,
	body string,
	categoryIDs []int64,
	imagePath string,
) (int64, error) {
	f.called = true
	f.authorID = authorID
	f.title = title
	f.body = body
	f.categoryIDs = categoryIDs
	f.imagePath = imagePath

	return f.postID, f.err
}

func TestPostServiceRejectsGuest(t *testing.T) {
	repo := &fakePostCreator{}

	service := NewPostService(repo)

	input := validation.PostInput{
		Title:       "valid title",
		Body:        "valid body",
		CategoryIDs: []int64{1},
	}

	_, err := service.Create(0, input)

	if !errors.Is(err, ErrAuthenticationRequired) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			ErrAuthenticationRequired,
		)
	}

	if repo.called {
		t.Fatal("repository Create() was called for guest")
	}
}

func TestPostServiceInvalidInputStopsBeforeRepository(t *testing.T) {
	repo := &fakePostCreator{}

	service := NewPostService(repo)

	input := validation.PostInput{
		Title:       "   ",
		Body:        "valid body",
		CategoryIDs: []int64{1},
	}

	_, err := service.Create(42, input)

	if err == nil {
		t.Fatal("Create() error = nil, want validation error")
	}

	if repo.called {
		t.Fatal("repository Create() was called for invalid input")
	}
}

func TestPostServiceUsesAuthenticatedUserAsAuthor(t *testing.T) {
	repo := &fakePostCreator{
		postID: 100,
	}

	service := NewPostService(repo)

	input := validation.PostInput{
		Title:       "  My post  ",
		Body:        "  My body  ",
		CategoryIDs: []int64{1, 2},
	}

	postID, err := service.Create(42, input)
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}

	if postID != 100 {
		t.Fatalf(
			"postID = %d, want 100",
			postID,
		)
	}

	if !repo.called {
		t.Fatal("repository Create() was not called")
	}

	if repo.authorID != 42 {
		t.Fatalf(
			"authorID = %d, want 42",
			repo.authorID,
		)
	}

	if repo.title != "My post" {
		t.Fatalf(
			"title = %q, want %q",
			repo.title,
			"My post",
		)
	}

	if repo.body != "My body" {
		t.Fatalf(
			"body = %q, want %q",
			repo.body,
			"My body",
		)
	}
}
