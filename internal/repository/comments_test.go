package repository

import (
	"errors"
	"path/filepath"
	"testing"

	"forum/internal/database"
)

func TestCommentRepositoryCreateStoresAuthor(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "forum.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("database.Open(): %v", err)
	}
	defer db.Close()

	err = database.Migrate(
		db,
		filepath.Join("..", "..", "migrations"),
	)
	if err != nil {
		t.Fatalf("database.Migrate(): %v", err)
	}

	users := NewUserRepository(db)

	authorID, err := users.Create(
		"author@example.com",
		"author",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	posts := NewPostRepository(db)

	postID, err := posts.Create(
		authorID,
		"Post title",
		"Post body",
		[]int64{1},
		"",
	)
	if err != nil {
		t.Fatalf("posts.Create(): %v", err)
	}

	comments := NewCommentRepository(db)

	commentID, err := comments.Create(
		postID,
		authorID,
		"Hello comment",
	)
	if err != nil {
		t.Fatalf("comments.Create(): %v", err)
	}

	if commentID == 0 {
		t.Fatal("commentID = 0, want non-zero")
	}

	var gotPostID int64
	var gotAuthorID int64
	var gotBody string

	err = db.QueryRow(`
		SELECT post_id, author_id, body
		FROM comments
		WHERE id = ?
	`, commentID).Scan(
		&gotPostID,
		&gotAuthorID,
		&gotBody,
	)
	if err != nil {
		t.Fatalf("query comment: %v", err)
	}

	if gotPostID != postID {
		t.Fatalf(
			"postID = %d, want %d",
			gotPostID,
			postID,
		)
	}

	if gotAuthorID != authorID {
		t.Fatalf(
			"authorID = %d, want %d",
			gotAuthorID,
			authorID,
		)
	}

	if gotBody != "Hello comment" {
		t.Fatalf(
			"body = %q, want %q",
			gotBody,
			"Hello comment",
		)
	}
}

func TestCommentRepositoryCreateReturnsUnknownPost(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "forum.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("database.Open(): %v", err)
	}
	defer db.Close()

	err = database.Migrate(
		db,
		filepath.Join("..", "..", "migrations"),
	)
	if err != nil {
		t.Fatalf("database.Migrate(): %v", err)
	}

	users := NewUserRepository(db)

	authorID, err := users.Create(
		"author@example.com",
		"author",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create author: %v", err)
	}

	comments := NewCommentRepository(db)

	_, err = comments.Create(
		999,
		authorID,
		"Should fail",
	)

	if !errors.Is(err, ErrPostNotFound) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			ErrPostNotFound,
		)
	}
}
