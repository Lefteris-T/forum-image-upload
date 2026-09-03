package repository

import (
	"errors"
	"path/filepath"
	"testing"

	"forum/internal/database"
	"forum/internal/model"
)

func TestPostReactionRepositoryInsertAndToggleOff(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

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

	reactorID, err := users.Create(
		"reactor@example.com",
		"reactor",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create reactor: %v", err)
	}

	posts := NewPostRepository(db)

	postID, err := posts.Create(
		authorID,
		"Post",
		"Body",
		[]int64{1},
		"",
	)
	if err != nil {
		t.Fatalf("posts.Create(): %v", err)
	}

	reactions := NewReactionRepository(db)

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionLike,
	)
	if err != nil {
		t.Fatalf("first SetPostReaction(): %v", err)
	}

	var value int

	err = db.QueryRow(`
		SELECT value
		FROM post_reactions
		WHERE user_id = ?
		  AND post_id = ?
	`,
		reactorID,
		postID,
	).Scan(&value)
	if err != nil {
		t.Fatalf("query reaction: %v", err)
	}

	if value != int(model.ReactionLike) {
		t.Fatalf(
			"value = %d, want %d",
			value,
			model.ReactionLike,
		)
	}

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionLike,
	)
	if err != nil {
		t.Fatalf("second SetPostReaction(): %v", err)
	}

	var count int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM post_reactions
		WHERE user_id = ?
		  AND post_id = ?
	`,
		reactorID,
		postID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count reaction: %v", err)
	}

	if count != 0 {
		t.Fatalf(
			"reaction count = %d, want 0 after toggle",
			count,
		)
	}
}
func TestPostReactionRepositorySwitchesReaction(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

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

	reactorID, err := users.Create(
		"reactor@example.com",
		"reactor",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create reactor: %v", err)
	}

	posts := NewPostRepository(db)

	postID, err := posts.Create(
		authorID,
		"Post",
		"Body",
		[]int64{1},
		"",
	)
	if err != nil {
		t.Fatalf("posts.Create(): %v", err)
	}

	reactions := NewReactionRepository(db)

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionLike,
	)
	if err != nil {
		t.Fatalf("set like: %v", err)
	}

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionDislike,
	)
	if err != nil {
		t.Fatalf("switch to dislike: %v", err)
	}

	var value int

	err = db.QueryRow(`
		SELECT value
		FROM post_reactions
		WHERE user_id = ?
		  AND post_id = ?
	`,
		reactorID,
		postID,
	).Scan(&value)
	if err != nil {
		t.Fatalf("query dislike: %v", err)
	}

	if value != int(model.ReactionDislike) {
		t.Fatalf(
			"value = %d, want %d",
			value,
			model.ReactionDislike,
		)
	}

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionLike,
	)
	if err != nil {
		t.Fatalf("switch back to like: %v", err)
	}

	err = db.QueryRow(`
		SELECT value
		FROM post_reactions
		WHERE user_id = ?
		  AND post_id = ?
	`,
		reactorID,
		postID,
	).Scan(&value)
	if err != nil {
		t.Fatalf("query like: %v", err)
	}

	if value != int(model.ReactionLike) {
		t.Fatalf(
			"value = %d, want %d",
			value,
			model.ReactionLike,
		)
	}
}
func TestCommentReactionRepositoryTransitions(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

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

	reactorID, err := users.Create(
		"reactor@example.com",
		"reactor",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create reactor: %v", err)
	}

	posts := NewPostRepository(db)

	postID, err := posts.Create(
		authorID,
		"Post",
		"Body",
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
		"Comment",
	)
	if err != nil {
		t.Fatalf("comments.Create(): %v", err)
	}

	reactions := NewReactionRepository(db)

	err = reactions.SetCommentReaction(
		reactorID,
		commentID,
		model.ReactionLike,
	)
	if err != nil {
		t.Fatalf("set comment like: %v", err)
	}

	var value int

	err = db.QueryRow(`
		SELECT value
		FROM comment_reactions
		WHERE user_id = ?
		  AND comment_id = ?
	`,
		reactorID,
		commentID,
	).Scan(&value)
	if err != nil {
		t.Fatalf("query comment like: %v", err)
	}

	if value != int(model.ReactionLike) {
		t.Fatalf(
			"value = %d, want %d",
			value,
			model.ReactionLike,
		)
	}

	err = reactions.SetCommentReaction(
		reactorID,
		commentID,
		model.ReactionDislike,
	)
	if err != nil {
		t.Fatalf("switch comment reaction: %v", err)
	}

	err = db.QueryRow(`
		SELECT value
		FROM comment_reactions
		WHERE user_id = ?
		  AND comment_id = ?
	`,
		reactorID,
		commentID,
	).Scan(&value)
	if err != nil {
		t.Fatalf("query comment dislike: %v", err)
	}

	if value != int(model.ReactionDislike) {
		t.Fatalf(
			"value = %d, want %d",
			value,
			model.ReactionDislike,
		)
	}

	err = reactions.SetCommentReaction(
		reactorID,
		commentID,
		model.ReactionDislike,
	)
	if err != nil {
		t.Fatalf("toggle comment dislike off: %v", err)
	}

	var count int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM comment_reactions
		WHERE user_id = ?
		  AND comment_id = ?
	`,
		reactorID,
		commentID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count comment reaction: %v", err)
	}

	if count != 0 {
		t.Fatalf(
			"comment reaction count = %d, want 0",
			count,
		)
	}
}
func TestReactionRepositoryReturnsNotFoundForMissingTargets(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

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

	userID, err := users.Create(
		"user@example.com",
		"user",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("users.Create(): %v", err)
	}

	reactions := NewReactionRepository(db)

	t.Run("missing post", func(t *testing.T) {
		err := reactions.SetPostReaction(
			userID,
			999,
			model.ReactionLike,
		)

		if !errors.Is(err, ErrPostNotFound) {
			t.Fatalf(
				"error = %v, want %v",
				err,
				ErrPostNotFound,
			)
		}
	})

	t.Run("missing comment", func(t *testing.T) {
		err := reactions.SetCommentReaction(
			userID,
			999,
			model.ReactionLike,
		)

		if !errors.Is(err, ErrCommentNotFound) {
			t.Fatalf(
				"error = %v, want %v",
				err,
				ErrCommentNotFound,
			)
		}
	})
}
func TestPostReactionRepositoryKeepsOneReactionPerUser(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

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

	reactorID, err := users.Create(
		"reactor@example.com",
		"reactor",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create reactor: %v", err)
	}

	posts := NewPostRepository(db)

	postID, err := posts.Create(
		authorID,
		"Post",
		"Body",
		[]int64{1},
		"",
	)
	if err != nil {
		t.Fatalf("posts.Create(): %v", err)
	}

	reactions := NewReactionRepository(db)

	if err := reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionLike,
	); err != nil {
		t.Fatalf("set like: %v", err)
	}

	if err := reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionDislike,
	); err != nil {
		t.Fatalf("switch to dislike: %v", err)
	}

	var count int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM post_reactions
		WHERE user_id = ?
		  AND post_id = ?
	`,
		reactorID,
		postID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count reactions: %v", err)
	}

	if count != 1 {
		t.Fatalf(
			"reaction count = %d, want 1",
			count,
		)
	}
}
func TestPostReactionCountsFollowTransitions(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

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

	reactorID, err := users.Create(
		"reactor@example.com",
		"reactor",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create reactor: %v", err)
	}

	posts := NewPostRepository(db)

	postID, err := posts.Create(
		authorID,
		"Post",
		"Body",
		[]int64{1},
		"",
	)
	if err != nil {
		t.Fatalf("posts.Create(): %v", err)
	}

	reactions := NewReactionRepository(db)

	checkCounts := func(
		wantLikes int,
		wantDislikes int,
	) {
		t.Helper()

		detail, err := posts.Detail(postID)
		if err != nil {
			t.Fatalf("posts.Detail(): %v", err)
		}

		if detail.Likes != wantLikes {
			t.Fatalf(
				"likes = %d, want %d",
				detail.Likes,
				wantLikes,
			)
		}

		if detail.Dislikes != wantDislikes {
			t.Fatalf(
				"dislikes = %d, want %d",
				detail.Dislikes,
				wantDislikes,
			)
		}
	}

	checkCounts(0, 0)

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionLike,
	)
	if err != nil {
		t.Fatalf("set like: %v", err)
	}

	checkCounts(1, 0)

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionDislike,
	)
	if err != nil {
		t.Fatalf("switch to dislike: %v", err)
	}

	checkCounts(0, 1)

	err = reactions.SetPostReaction(
		reactorID,
		postID,
		model.ReactionDislike,
	)
	if err != nil {
		t.Fatalf("toggle dislike off: %v", err)
	}

	checkCounts(0, 0)
}
func TestCommentReactionCountsFollowTransitions(t *testing.T) {
	dbPath := filepath.Join(
		t.TempDir(),
		"forum.db",
	)

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

	reactorID, err := users.Create(
		"reactor@example.com",
		"reactor",
		"password-hash",
	)
	if err != nil {
		t.Fatalf("create reactor: %v", err)
	}

	posts := NewPostRepository(db)

	postID, err := posts.Create(
		authorID,
		"Post",
		"Body",
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
		"Comment",
	)
	if err != nil {
		t.Fatalf("comments.Create(): %v", err)
	}

	reactions := NewReactionRepository(db)

	checkCounts := func(
		wantLikes int,
		wantDislikes int,
	) {
		t.Helper()

		detail, err := posts.Detail(postID)
		if err != nil {
			t.Fatalf("posts.Detail(): %v", err)
		}

		if len(detail.Comments) != 1 {
			t.Fatalf(
				"comment count = %d, want 1",
				len(detail.Comments),
			)
		}

		comment := detail.Comments[0]

		if comment.Likes != wantLikes {
			t.Fatalf(
				"likes = %d, want %d",
				comment.Likes,
				wantLikes,
			)
		}

		if comment.Dislikes != wantDislikes {
			t.Fatalf(
				"dislikes = %d, want %d",
				comment.Dislikes,
				wantDislikes,
			)
		}
	}

	checkCounts(0, 0)

	err = reactions.SetCommentReaction(
		reactorID,
		commentID,
		model.ReactionLike,
	)
	if err != nil {
		t.Fatalf("set like: %v", err)
	}

	checkCounts(1, 0)

	err = reactions.SetCommentReaction(
		reactorID,
		commentID,
		model.ReactionDislike,
	)
	if err != nil {
		t.Fatalf("switch to dislike: %v", err)
	}

	checkCounts(0, 1)

	err = reactions.SetCommentReaction(
		reactorID,
		commentID,
		model.ReactionDislike,
	)
	if err != nil {
		t.Fatalf("toggle dislike off: %v", err)
	}

	checkCounts(0, 0)
}
