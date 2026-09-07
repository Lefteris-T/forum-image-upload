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

	if len(data) > MaxImageSize {
		return ValidatedImage{}, ErrImageTooLarge
	}

	if len(data) == 0 {
		return ValidatedImage{}, ErrEmptyImage
	}

	contentType := http.DetectContentType(data)
	extension, ok := extensionForContentType(contentType)
	if !ok {
		return ValidatedImage{}, ErrUnsupportedImageType
	}

	format, err := decodedImageFormat(data)
	if err != nil {
		return ValidatedImage{}, ErrUnreadableImage
	}

	if !contentTypeMatchesFormat(contentType, format) {
		return ValidatedImage{}, ErrUnsupportedImageType
	}

	return ValidatedImage{
		ContentType: contentType,
		Extension:   extension,
		Bytes:       data,
	}, nil
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

func decodedImageFormat(data []byte) (string, error) {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}

	return format, nil
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
 