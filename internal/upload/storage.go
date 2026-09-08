package upload

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const publicUploadPath = "/static/uploads"

var (
	ErrUploadDirectoryRequired = errors.New("upload directory is required")
	ErrInvalidUploadPath       = errors.New("invalid upload path")
)

// Storage manages post images inside one configured upload directory.
type Storage struct {
	uploadDir   string
	newFilename func(extension string) string
}

// NewStorage prepares the directory used for uploaded post images.
func NewStorage(uploadDir string) (*Storage, error) {
	if strings.TrimSpace(uploadDir) == "" {
		return nil, ErrUploadDirectoryRequired
	}

	uploadDir = filepath.Clean(uploadDir)

	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload directory: %w", err)
	}

	if err := os.Chmod(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("set upload directory permissions: %w", err)
	}

	return &Storage{
		uploadDir: uploadDir,
		newFilename: func(extension string) string {
			return uuid.NewString() + extension
		},
	}, nil
}

// Save validates an image and atomically publishes it under a generated name.
func (s *Storage) Save(r io.Reader) (string, error) {
	temporaryFile, err := os.CreateTemp(s.uploadDir, ".upload-*")
	if err != nil {
		return "", fmt.Errorf("create temporary upload: %w", err)
	}

	temporaryPath := temporaryFile.Name()
	defer func() {
		temporaryFile.Close()
		os.Remove(temporaryPath)
	}()

	written, err := io.Copy(
		temporaryFile,
		io.LimitReader(r, int64(MaxImageSize)+1),
	)
	if err != nil {
		return "", fmt.Errorf("%w: interrupted upload read", ErrUnreadableImage)
	}

	_, extension, err := validateImageContent(temporaryFile, written)
	if err != nil {
		return "", err
	}

	if err := temporaryFile.Chmod(0o644); err != nil {
		return "", fmt.Errorf("set upload permissions: %w", err)
	}

	if err := temporaryFile.Close(); err != nil {
		return "", fmt.Errorf("close temporary upload: %w", err)
	}

	filename := s.newFilename(extension)
	finalPath := filepath.Join(s.uploadDir, filename)

	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", fmt.Errorf("publish upload: %w", err)
	}

	return path.Join(publicUploadPath, filename), nil
}

// Delete removes a regular image file previously published by this storage.
func (s *Storage) Delete(publicPath string) error {
	filename, err := managedFilename(publicPath)
	if err != nil {
		return err
	}

	storedPath := filepath.Join(s.uploadDir, filename)
	info, err := os.Lstat(storedPath)
	if err != nil {
		return fmt.Errorf("inspect upload for deletion: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrInvalidUploadPath
	}

	if err := os.Remove(storedPath); err != nil {
		return fmt.Errorf("delete upload: %w", err)
	}

	return nil
}

func managedFilename(publicPath string) (string, error) {
	if publicPath == "" || path.Clean(publicPath) != publicPath {
		return "", ErrInvalidUploadPath
	}
	if path.Dir(publicPath) != publicUploadPath {
		return "", ErrInvalidUploadPath
	}

	filename := path.Base(publicPath)
	extension := path.Ext(filename)
	if !isSupportedExtension(extension) {
		return "", ErrInvalidUploadPath
	}

	idText := strings.TrimSuffix(filename, extension)
	id, err := uuid.Parse(idText)
	if err != nil || id.String() != idText {
		return "", ErrInvalidUploadPath
	}

	return filename, nil
}

func isSupportedExtension(extension string) bool {
	switch extension {
	case ".jpg", ".png", ".gif":
		return true
	default:
		return false
	}
}
