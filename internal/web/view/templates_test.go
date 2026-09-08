package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealTemplatesLoad(t *testing.T) {
	renderer, err := NewRenderer(
		filepath.Join("..", "..", "..", "templates"),
	)
	if err != nil {
		t.Fatalf("NewRenderer() error: %v", err)
	}

	if renderer == nil {
		t.Fatal("renderer is nil")
	}
}

func TestNewPostTemplateExplainsImageUpload(t *testing.T) {
	templatePath := filepath.Join("..", "..", "..", "templates", "new_post.html")
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read new-post template: %v", err)
	}

	templateText := string(templateBytes)
	requiredText := []string{
		`enctype="multipart/form-data"`,
		`name="image"`,
		`type="file"`,
		`accept="image/jpeg,image/png,image/gif"`,
		"optional",
		"JPEG",
		"PNG",
		"GIF",
		"20 MB",
	}

	for _, required := range requiredText {
		if !strings.Contains(templateText, required) {
			t.Fatalf("new-post template does not contain %q", required)
		}
	}
}
