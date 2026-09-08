package upload

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
)

const MaxImageSize = 20 * 1024 * 1024

var (
	ErrImageTooLarge        = errors.New("image is too large")
	ErrUnsupportedImageType = errors.New("unsupported image type")
	ErrEmptyImage           = errors.New("image is empty")
	ErrUnreadableImage      = errors.New("image could not be read")
)

type ValidatedImage struct {
	ContentType string
	Extension   string
	Bytes       []byte
}

func ValidateOptionalImage(r io.Reader, present bool) (ValidatedImage, error) {
	if !present {
		return ValidatedImage{}, nil
	}

	return ValidateImage(r)
}

func ValidateImage(r io.Reader) (ValidatedImage, error) {
	if r == nil {
		return ValidatedImage{}, ErrUnreadableImage
	}

	data, err := io.ReadAll(io.LimitReader(r, MaxImageSize+1))
	if err != nil {
		return ValidatedImage{}, ErrUnreadableImage
	}

	contentType, extension, err := validateImageContent(
		bytes.NewReader(data),
		int64(len(data)),
	)
	if err != nil {
		return ValidatedImage{}, err
	}

	return ValidatedImage{
		ContentType: contentType,
		Extension:   extension,
		Bytes:       data,
	}, nil
}

func validateImageContent(r io.ReadSeeker, size int64) (string, string, error) {
	if size > MaxImageSize {
		return "", "", ErrImageTooLarge
	}
	if size == 0 {
		return "", "", ErrEmptyImage
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", "", ErrUnreadableImage
	}

	headerSize := min(size, 512)
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", "", ErrUnreadableImage
	}

	contentType := http.DetectContentType(header)
	extension, ok := extensionForContentType(contentType)
	if !ok {
		return "", "", ErrUnsupportedImageType
	}

	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", "", ErrUnreadableImage
	}

	_, format, err := image.DecodeConfig(r)
	if err != nil {
		return "", "", ErrUnreadableImage
	}
	if !contentTypeMatchesFormat(contentType, format) {
		return "", "", ErrUnsupportedImageType
	}

	return contentType, extension, nil
}

func extensionForContentType(contentType string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	default:
		return "", false
	}
}

func contentTypeMatchesFormat(contentType string, format string) bool {
	switch contentType {
	case "image/jpeg":
		return format == "jpeg"
	case "image/png":
		return format == "png"
	case "image/gif":
		return format == "gif"
	default:
		return false
	}
}
