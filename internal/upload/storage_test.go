package upload

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewStorageCreatesUploadDirectory(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "static", "uploads")

	storage, err := NewStorage(uploadDir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	if storage == nil {
		t.Fatal("NewStorage() returned a nil storage")
	}

	info, err := os.Stat(uploadDir)
	if err != nil {
		t.Fatalf("stat upload directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("upload path is not a directory")
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o755); got != want {
		t.Fatalf("upload directory permissions = %o, want %o", got, want)
	}
}

func TestNewStorageRejectsEmptyUploadDirectory(t *testing.T) {
	storage, err := NewStorage("")
	if err == nil {
		t.Fatal("NewStorage() error = nil, want an error")
	}
	if storage != nil {
		t.Fatalf("NewStorage() = %#v, want nil", storage)
	}
}

func TestStorageSaveWritesValidImage(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	storage, err := NewStorage(uploadDir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	imageBytes := mustPNG(t)
	publicPath, err := storage.Save(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !strings.HasPrefix(publicPath, "/static/uploads/") {
		t.Fatalf("Save() public path = %q, want /static/uploads/ prefix", publicPath)
	}

	filename := path.Base(publicPath)
	if extension := path.Ext(filename); extension != ".png" {
		t.Fatalf("saved extension = %q, want .png", extension)
	}
	if _, err := uuid.Parse(strings.TrimSuffix(filename, ".png")); err != nil {
		t.Fatalf("saved filename %q does not contain a valid UUID: %v", filename, err)
	}

	savedPath := filepath.Join(uploadDir, filename)
	savedBytes, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read saved image: %v", err)
	}
	if !bytes.Equal(savedBytes, imageBytes) {
		t.Fatal("saved image bytes differ from the uploaded bytes")
	}

	info, err := os.Stat(savedPath)
	if err != nil {
		t.Fatalf("stat saved image: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Fatalf("saved image permissions = %o, want %o", got, want)
	}
}

func TestStorageSaveGeneratesUniqueFilenames(t *testing.T) {
	storage, err := NewStorage(filepath.Join(t.TempDir(), "uploads"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	imageBytes := mustJPEG(t)
	firstPath, err := storage.Save(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	secondPath, err := storage.Save(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	if firstPath == secondPath {
		t.Fatalf("Save() generated duplicate public path %q", firstPath)
	}
}

func TestStorageSaveFailureLeavesNoFiles(t *testing.T) {
	pngBytes := mustPNG(t)
	oversizedImage := append([]byte{}, pngBytes...)
	oversizedImage = append(
		oversizedImage,
		bytes.Repeat([]byte{0}, MaxImageSize+1-len(oversizedImage))...,
	)

	tests := []struct {
		name   string
		reader io.Reader
		want   error
	}{
		{
			name:   "unsupported content",
			reader: bytes.NewReader([]byte("not an image")),
			want:   ErrUnsupportedImageType,
		},
		{
			name:   "malformed image",
			reader: bytes.NewReader([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}),
			want:   ErrUnreadableImage,
		},
		{
			name:   "oversized image",
			reader: bytes.NewReader(oversizedImage),
			want:   ErrImageTooLarge,
		},
		{
			name:   "interrupted read",
			reader: io.MultiReader(bytes.NewReader(pngBytes), errorReader{}),
			want:   ErrUnreadableImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploadDir := filepath.Join(t.TempDir(), "uploads")
			storage, err := NewStorage(uploadDir)
			if err != nil {
				t.Fatalf("NewStorage() error = %v", err)
			}

			_, err = storage.Save(tt.reader)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Save() error = %v, want %v", err, tt.want)
			}

			assertDirectoryEmpty(t, uploadDir)
		})
	}
}

func TestStorageSaveRenameFailureRemovesTemporaryFile(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	storage, err := NewStorage(uploadDir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	const blockedFilename = "blocked.png"
	storage.newFilename = func(string) string {
		return blockedFilename
	}

	if err := os.Mkdir(filepath.Join(uploadDir, blockedFilename), 0o755); err != nil {
		t.Fatalf("create path that blocks rename: %v", err)
	}

	if _, err := storage.Save(bytes.NewReader(mustPNG(t))); err == nil {
		t.Fatal("Save() error = nil, want rename failure")
	}

	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatalf("read upload directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != blockedFilename || !entries[0].IsDir() {
		t.Fatalf("upload directory entries = %#v, want only blocking directory", entries)
	}
}

func TestStorageDeleteRemovesSavedImage(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	storage, err := NewStorage(uploadDir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	publicPath, err := storage.Save(bytes.NewReader(mustGIF(t)))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	savedPath := filepath.Join(uploadDir, path.Base(publicPath))
	if err := storage.Delete(publicPath); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(savedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat deleted image error = %v, want os.ErrNotExist", err)
	}
}

func TestStorageDeleteRejectsUnmanagedPaths(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	storage, err := NewStorage(uploadDir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	validName := uuid.NewString() + ".png"
	tests := []struct {
		name       string
		publicPath string
	}{
		{name: "empty", publicPath: ""},
		{name: "filesystem path", publicPath: filepath.Join(uploadDir, validName)},
		{name: "different public directory", publicPath: "/static/other/" + validName},
		{name: "parent traversal", publicPath: "/static/uploads/../" + validName},
		{name: "nested path", publicPath: "/static/uploads/nested/" + validName},
		{name: "non UUID filename", publicPath: "/static/uploads/avatar.png"},
		{name: "unsupported extension", publicPath: "/static/uploads/" + uuid.NewString() + ".svg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.Delete(tt.publicPath)
			if !errors.Is(err, ErrInvalidUploadPath) {
				t.Fatalf("Delete(%q) error = %v, want %v", tt.publicPath, err, ErrInvalidUploadPath)
			}
		})
	}
}

func TestStorageDeleteDoesNotRemoveOutsideFile(t *testing.T) {
	rootDir := t.TempDir()
	uploadDir := filepath.Join(rootDir, "uploads")
	storage, err := NewStorage(uploadDir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	outsidePath := filepath.Join(rootDir, "outside.png")
	if err := os.WriteFile(outsidePath, mustPNG(t), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	traversalPath := "/static/uploads/../outside.png"
	if err := storage.Delete(traversalPath); !errors.Is(err, ErrInvalidUploadPath) {
		t.Fatalf("Delete(%q) error = %v, want %v", traversalPath, err, ErrInvalidUploadPath)
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("outside file was affected: %v", err)
	}
}

func assertDirectoryEmpty(t *testing.T, directory string) {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read upload directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("upload directory contains %d entries after failed save", len(entries))
	}
}
