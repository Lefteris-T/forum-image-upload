package repository

import (
	"database/sql"
	"errors"
	"forum/internal/model"
	"strings"
	"time"
)

// ErrPostNotFound lets HTTP handlers return 404 without inspecting SQL errors.
var ErrPostNotFound = errors.New("post not found")

// PostListItem is the read model needed by listing and filtering pages.
type PostListItem struct {
	ID         int64
	Title      string
	Body       string
	CreatedAt  time.Time
	Author     model.User
	Categories []model.Category
	Likes      int
	Dislikes   int
	ImagePath  string
}

// CommentView combines comment data with author and reaction totals.
type CommentView struct {
	ID        int64
	PostID    int64
	Body      string
	CreatedAt time.Time
	Author    model.User
	Likes     int
	Dislikes  int
}

// PostDetail is the complete read model for one post page.
type PostDetail struct {
	ID         int64
	Title      string
	Body       string
	CreatedAt  time.Time
	Author     model.User
	Categories []model.Category
	Likes      int
	Dislikes   int
	Comments   []CommentView
	ImagePath  string
}

// PostRepository owns post writes and the composed queries used by forum views.
type PostRepository struct {
	db *sql.DB
}

// NewPostRepository binds post operations to db.
func NewPostRepository(db *sql.DB) *PostRepository {
	return &PostRepository{
		db: db,
	}
}

// Create stores the post and all category links atomically, preventing a
// partially created post when one category insertion fails.
func (r *PostRepository) Create(
	authorID int64,
	title string,
	body string,
	categoryIDs []int64,
	imagePath string,
) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`
		INSERT INTO posts (
			author_id,
			title,
			body,
			created_at,
			image_path
		)
		VALUES (?, ?, ?, ?, NULLIF(?, ''))
	`,
		authorID,
		title,
		body,
		time.Now().UTC().Format(time.RFC3339),
		imagePath,
	)
	if err != nil {
		return 0, err
	}

	postID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	for _, categoryID := range categoryIDs {
		_, err := tx.Exec(
			`
				INSERT INTO post_categories (
					post_id,
					category_id
				)
				VALUES (?, ?)
			`,
			postID,
			categoryID,
		)
		if err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return postID, nil
}

// List returns every post with author, category, and reaction information.
func (r *PostRepository) List() ([]PostListItem, error) {
	rows, err := r.db.Query(`
		SELECT
			p.id,
			p.title,
			p.body,
			p.created_at,
			COALESCE(p.image_path, ''),
			u.id,
			u.email,
			u.username,
			u.created_at,
			COALESCE(SUM(
				CASE WHEN pr.value = 1 THEN 1 ELSE 0 END
			), 0) AS likes,
			COALESCE(SUM(
				CASE WHEN pr.value = -1 THEN 1 ELSE 0 END
			), 0) AS dislikes
		FROM posts p
		JOIN users u
			ON u.id = p.author_id
		LEFT JOIN post_reactions pr
			ON pr.post_id = p.id
		GROUP BY p.id
		ORDER BY p.created_at DESC, p.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []PostListItem

	for rows.Next() {
		var post PostListItem
		var postCreatedAt string
		var userCreatedAt string

		err := rows.Scan(
			&post.ID,
			&post.Title,
			&post.Body,
			&postCreatedAt,
			&post.ImagePath,
			&post.Author.ID,
			&post.Author.Email,
			&post.Author.Username,
			&userCreatedAt,
			&post.Likes,
			&post.Dislikes,
		)
		if err != nil {
			return nil, err
		}

		post.CreatedAt, err = time.Parse(
			time.RFC3339,
			postCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		post.Author.CreatedAt, err = time.Parse(
			time.RFC3339,
			userCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		posts = append(
			posts,
			post,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	categoriesByPost, err := r.categoriesForPosts(posts)
	if err != nil {
		return nil, err
	}

	for i := range posts {
		posts[i].Categories = categoriesByPost[posts[i].ID]
	}

	return posts, nil
}
func (r *PostRepository) categoriesForPost(
	postID int64,
) ([]model.Category, error) {
	rows, err := r.db.Query(`
		SELECT
			c.id,
			c.name
		FROM categories c
		JOIN post_categories pc
			ON pc.category_id = c.id
		WHERE pc.post_id = ?
		ORDER BY c.id
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []model.Category

	for rows.Next() {
		var category model.Category

		if err := rows.Scan(
			&category.ID,
			&category.Name,
		); err != nil {
			return nil, err
		}

		categories = append(
			categories,
			category,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return categories, nil
}
func (r *PostRepository) categoriesForPosts(
	posts []PostListItem,
) (map[int64][]model.Category, error) {
	result := make(map[int64][]model.Category)

	if len(posts) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(posts))
	args := make([]any, len(posts))

	for i, post := range posts {
		placeholders[i] = "?"
		args[i] = post.ID
	}

	query := `
		SELECT
			pc.post_id,
			c.id,
			c.name
		FROM post_categories pc
		JOIN categories c
			ON c.id = pc.category_id
		WHERE pc.post_id IN (` +
		strings.Join(placeholders, ",") +
		`)
		ORDER BY pc.post_id, c.id
	`

	rows, err := r.db.Query(
		query,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var postID int64
		var category model.Category

		if err := rows.Scan(
			&postID,
			&category.ID,
			&category.Name,
		); err != nil {
			return nil, err
		}

		result[postID] = append(
			result[postID],
			category,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// Detail returns one post and its public comments, or ErrPostNotFound.
func (r *PostRepository) Detail(
	postID int64,
) (PostDetail, error) {
	var post PostDetail
	var postCreatedAt string
	var userCreatedAt string

	err := r.db.QueryRow(`
		SELECT
			p.id,
			p.title,
			p.body,
			p.created_at,
			COALESCE(p.image_path, ''),
			u.id,
			u.email,
			u.username,
			u.created_at,
			COALESCE(SUM(
				CASE WHEN pr.value = 1 THEN 1 ELSE 0 END
			), 0) AS likes,
			COALESCE(SUM(
				CASE WHEN pr.value = -1 THEN 1 ELSE 0 END
			), 0) AS dislikes
		FROM posts p
		JOIN users u
			ON u.id = p.author_id
		LEFT JOIN post_reactions pr
			ON pr.post_id = p.id
		WHERE p.id = ?
		GROUP BY p.id
	`, postID).Scan(
		&post.ID,
		&post.Title,
		&post.Body,
		&postCreatedAt,
		&post.ImagePath,
		&post.Author.ID,
		&post.Author.Email,
		&post.Author.Username,
		&userCreatedAt,
		&post.Likes,
		&post.Dislikes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PostDetail{}, ErrPostNotFound
	}
	if err != nil {
		return PostDetail{}, err
	}

	post.CreatedAt, err = time.Parse(
		time.RFC3339,
		postCreatedAt,
	)
	if err != nil {
		return PostDetail{}, err
	}

	post.Author.CreatedAt, err = time.Parse(
		time.RFC3339,
		userCreatedAt,
	)
	if err != nil {
		return PostDetail{}, err
	}

	post.Categories, err = r.categoriesForPost(post.ID)
	if err != nil {
		return PostDetail{}, err
	}

	post.Comments, err = r.commentsForPost(post.ID)
	if err != nil {
		return PostDetail{}, err
	}

	return post, nil
}
func (r *PostRepository) commentsForPost(
	postID int64,
) ([]CommentView, error) {
	rows, err := r.db.Query(`
		SELECT
			c.id,
			c.post_id,
			c.body,
			c.created_at,
			u.id,
			u.email,
			u.username,
			u.created_at,
			COALESCE(SUM(
				CASE WHEN cr.value = 1 THEN 1 ELSE 0 END
			), 0) AS likes,
			COALESCE(SUM(
				CASE WHEN cr.value = -1 THEN 1 ELSE 0 END
			), 0) AS dislikes
		FROM comments c
		JOIN users u
			ON u.id = c.author_id
		LEFT JOIN comment_reactions cr
			ON cr.comment_id = c.id
		WHERE c.post_id = ?
		GROUP BY c.id
		ORDER BY c.created_at ASC, c.id ASC
	`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []CommentView

	for rows.Next() {
		var comment CommentView
		var commentCreatedAt string
		var userCreatedAt string

		err := rows.Scan(
			&comment.ID,
			&comment.PostID,
			&comment.Body,
			&commentCreatedAt,
			&comment.Author.ID,
			&comment.Author.Email,
			&comment.Author.Username,
			&userCreatedAt,
			&comment.Likes,
			&comment.Dislikes,
		)
		if err != nil {
			return nil, err
		}

		comment.CreatedAt, err = time.Parse(
			time.RFC3339,
			commentCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		comment.Author.CreatedAt, err = time.Parse(
			time.RFC3339,
			userCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		comments = append(
			comments,
			comment,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return comments, nil
}

// ListByCategory returns posts carrying one validated category.
func (r *PostRepository) ListByCategory(
	categoryID int64,
) ([]PostListItem, error) {
	var exists int

	err := r.db.QueryRow(`
		SELECT 1
		FROM categories
		WHERE id = ?
	`, categoryID).Scan(&exists)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCategoryNotFound
	}

	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(`
		SELECT
			p.id,
			p.title,
			p.body,
			p.created_at,
			COALESCE(p.image_path, ''),
			u.id,
			u.email,
			u.username,
			u.created_at,
			COALESCE(SUM(
				CASE WHEN pr.value = 1 THEN 1 ELSE 0 END
			), 0) AS likes,
			COALESCE(SUM(
				CASE WHEN pr.value = -1 THEN 1 ELSE 0 END
			), 0) AS dislikes
		FROM posts p
		JOIN users u
			ON u.id = p.author_id
		JOIN post_categories pc
			ON pc.post_id = p.id
		LEFT JOIN post_reactions pr
			ON pr.post_id = p.id
		WHERE pc.category_id = ?
		GROUP BY p.id
		ORDER BY p.created_at DESC, p.id DESC
	`, categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []PostListItem

	for rows.Next() {
		var post PostListItem
		var postCreatedAt string
		var userCreatedAt string

		err := rows.Scan(
			&post.ID,
			&post.Title,
			&post.Body,
			&postCreatedAt,
			&post.ImagePath,
			&post.Author.ID,
			&post.Author.Email,
			&post.Author.Username,
			&userCreatedAt,
			&post.Likes,
			&post.Dislikes,
		)
		if err != nil {
			return nil, err
		}

		post.CreatedAt, err = time.Parse(
			time.RFC3339,
			postCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		post.Author.CreatedAt, err = time.Parse(
			time.RFC3339,
			userCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	categoriesByPost, err := r.categoriesForPosts(posts)
	if err != nil {
		return nil, err
	}

	for i := range posts {
		posts[i].Categories = categoriesByPost[posts[i].ID]
	}

	return posts, nil
}

// ListByAuthor powers the current user's "created posts" filter.
func (r *PostRepository) ListByAuthor(
	authorID int64,
) ([]PostListItem, error) {
	rows, err := r.db.Query(`
		SELECT
			p.id,
			p.title,
			p.body,
			p.created_at,
			COALESCE(p.image_path, ''),
			u.id,
			u.email,
			u.username,
			u.created_at,
			COALESCE(SUM(
				CASE WHEN pr.value = 1 THEN 1 ELSE 0 END
			), 0) AS likes,
			COALESCE(SUM(
				CASE WHEN pr.value = -1 THEN 1 ELSE 0 END
			), 0) AS dislikes
		FROM posts p
		JOIN users u
			ON u.id = p.author_id
		LEFT JOIN post_reactions pr
			ON pr.post_id = p.id
		WHERE p.author_id = ?
		GROUP BY p.id
		ORDER BY p.created_at DESC, p.id DESC
	`, authorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []PostListItem

	for rows.Next() {
		var post PostListItem
		var postCreatedAt string
		var userCreatedAt string

		err := rows.Scan(
			&post.ID,
			&post.Title,
			&post.Body,
			&postCreatedAt,
			&post.ImagePath,
			&post.Author.ID,
			&post.Author.Email,
			&post.Author.Username,
			&userCreatedAt,
			&post.Likes,
			&post.Dislikes,
		)
		if err != nil {
			return nil, err
		}

		post.CreatedAt, err = time.Parse(
			time.RFC3339,
			postCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		post.Author.CreatedAt, err = time.Parse(
			time.RFC3339,
			userCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	categoriesByPost, err := r.categoriesForPosts(posts)
	if err != nil {
		return nil, err
	}

	for i := range posts {
		posts[i].Categories = categoriesByPost[posts[i].ID]
	}

	return posts, nil
}

// ListLikedByUser powers the current user's "liked posts" filter. The mine
// join selects liked posts while all_pr independently counts all reactions.
func (r *PostRepository) ListLikedByUser(
	userID int64,
) ([]PostListItem, error) {
	rows, err := r.db.Query(`
		SELECT
			p.id,
			p.title,
			p.body,
			p.created_at,
			COALESCE(p.image_path, ''),
			u.id,
			u.email,
			u.username,
			u.created_at,
			COALESCE(SUM(
				CASE WHEN all_pr.value = 1 THEN 1 ELSE 0 END
			), 0) AS likes,
			COALESCE(SUM(
				CASE WHEN all_pr.value = -1 THEN 1 ELSE 0 END
			), 0) AS dislikes
		FROM posts p
		JOIN users u
			ON u.id = p.author_id

		JOIN post_reactions mine
			ON mine.post_id = p.id
			AND mine.user_id = ?
			AND mine.value = 1

		LEFT JOIN post_reactions all_pr
			ON all_pr.post_id = p.id

		GROUP BY p.id
		ORDER BY p.created_at DESC, p.id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []PostListItem

	for rows.Next() {
		var post PostListItem
		var postCreatedAt string
		var userCreatedAt string

		err := rows.Scan(
			&post.ID,
			&post.Title,
			&post.Body,
			&postCreatedAt,
			&post.ImagePath,
			&post.Author.ID,
			&post.Author.Email,
			&post.Author.Username,
			&userCreatedAt,
			&post.Likes,
			&post.Dislikes,
		)
		if err != nil {
			return nil, err
		}

		post.CreatedAt, err = time.Parse(
			time.RFC3339,
			postCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		post.Author.CreatedAt, err = time.Parse(
			time.RFC3339,
			userCreatedAt,
		)
		if err != nil {
			return nil, err
		}

		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	categoriesByPost, err := r.categoriesForPosts(posts)
	if err != nil {
		return nil, err
	}

	for i := range posts {
		posts[i].Categories = categoriesByPost[posts[i].ID]
	}

	return posts, nil
}
