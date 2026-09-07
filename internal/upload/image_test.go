package upload

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

func TestValidateImageAcceptsSupportedImages(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		contentType string
		extension   string
	}{
		{
			name:        "jpeg",
			data:        mustJPEG(t),
			contentType: "image/jpeg",
			extension:   ".jpg",
		},
		{
			name:        "png",
			data:        mustPNG(t),
			contentType: "image/png",
			extension:   ".png",
		},
		{
			name:        "gif",
			data:        mustGIF(t),
			contentType: "image/gif",
			extension:   ".gif",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateImage(bytes.NewReader(tt.data))
			if err != nil {
				t.Fatalf("ValidateImage() error = %v, want nil", err)
			}

			if got.ContentType != tt.contentType {
				t.Fatalf("ContentType = %q, want %q", got.ContentType, tt.contentType)
			}

			if got.Extension != tt.extension {
				t.Fatalf("Extension = %q, want %q", got.Extension, tt.extension)
			}

			if !bytes.Equal(got.Bytes, tt.data) {
				t.Fatal("validated bytes were changed")
			}
		})
	}
}

func TestValidateImageRejectsInvalidImages(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{
			name: "empty image",
			data: nil,
			want: ErrEmptyImage,
		},
		{
			name: "unsupported content",
			data: []byte("plain text is not an image"),
			want: ErrUnsupportedImageType,
		},
		{
			name: "fake jpeg header",
			data: []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x01},
			want: ErrUnreadableImage,
		},
		{
			name: "truncated png",
			data: []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a},
			want: ErrUnreadableImage,
		},
		{
			name: "truncated gif",
			data: []byte("GIF89a"),
			want: ErrUnreadableImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateImage(bytes.NewReader(tt.data))
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateImage() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestValidateImageRejectsNilReader(t *testing.T) {
	_, err := ValidateImage(nil)
	if !errors.Is(err, ErrUnreadableImage) {
		t.Fatalf("ValidateImage() error = %v, want %v", err, ErrUnreadableImage)
	}
}

func TestValidateImageRejectsReaderError(t *testing.T) {
	_, err := ValidateImage(errorReader{})
	if !errors.Is(err, ErrUnreadableImage) {
		t.Fatalf("ValidateImage() error = %v, want %v", err, ErrUnreadableImage)
	}
}

func TestValidateImageSizeBoundary(t *testing.T) {
	base := mustPNG(t)
	if len(base) > MaxImageSize {
		t.Fatalf("test image size = %d, larger than MaxImageSize", len(base))
	}

	exactLimit := append([]byte{}, base...)
	exactLimit = append(exactLimit, bytes.Repeat([]byte{0}, MaxImageSize-len(exactLimit))...)

	got, err := ValidateImage(bytes.NewReader(exactLimit))
	if err != nil {
		t.Fatalf("ValidateImage() at exact limit error = %v, want nil", err)
	}

	if len(got.Bytes) != MaxImageSize {
		t.Fatalf("validated size = %d, want %d", len(got.Bytes), MaxImageSize)
	}

	tooLarge := append([]byte{}, exactLimit...)
	tooLarge = append(tooLarge, 0)

	_, err = ValidateImage(bytes.NewReader(tooLarge))
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("ValidateImage() over limit error = %v, want %v", err, ErrImageTooLarge)
	}
}

func TestValidateImageRejectsOversizedUnknownLengthInput(t *testing.T) {
	base := mustPNG(t)
	padding := bytes.NewReader(bytes.Repeat([]byte{0}, MaxImageSize+1-len(base)))
	reader := io.MultiReader(bytes.NewReader(base), padding)

	_, err := ValidateImage(reader)
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("ValidateImage() error = %v, want %v", err, ErrImageTooLarge)
	}
}

func TestValidateOptionalImageTreatsMissingUploadAsNoImage(t *testing.T) {
	got, err := ValidateOptionalImage(nil, false)
	if err != nil {
		t.Fatalf("ValidateOptionalImage() error = %v, want nil", err)
	}

	if got.ContentType != "" || got.Extension != "" || got.Bytes != nil {
		t.Fatalf("ValidateOptionalImage() = %#v, want zero value", got)
	}
}

func TestValidateOptionalImageRejectsPresentEmptyUpload(t *testing.T) {
	_, err := ValidateOptionalImage(bytes.NewReader(nil), true)
	if !errors.Is(err, ErrEmptyImage) {
		t.Fatalf("ValidateOptionalImage() error = %v, want %v", err, ErrEmptyImage)
	}
}

func TestValidateImagePreservesInputBytes(t *testing.T) {
	data := mustGIF(t)

	got, err := ValidateImage(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ValidateImage() error = %v, want nil", err)
	}

	if !bytes.Equal(got.Bytes, data) {
		t.Fatal("ValidateImage() did not preserve the original bytes")
	}
}

func mustPNG(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage()); err != nil {
		t.Fatalf("png.Encode(): %v", err)
	}

	return buf.Bytes()
}

func mustJPEG(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, testImage(), nil); err != nil {
		t.Fatalf("jpeg.Encode(): %v", err)
	}

	return buf.Bytes()
}

func mustGIF(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := gif.Encode(&buf, testImage(), nil); err != nil {
		t.Fatalf("gif.Encode(): %v", err)
	}

	return buf.Bytes()
}

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{G: 255, A: 255})
	img.Set(0, 1, color.RGBA{B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255})

	return img
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
