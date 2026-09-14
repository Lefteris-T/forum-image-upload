// Package validation normalizes untrusted input and enforces size/content rules.
package validation

import (
	"fmt"
	"net/mail"
	"strings"
)

const (
	minUsernameLength = 3
	maxUsernameLength = 32
	minPasswordLength = 8
	maxPasswordLength = 72
)

// RegistrationInput is raw account-creation form data.
type RegistrationInput struct {
	Email    string
	Username string
	Password string
}

// LoginInput is raw credential form data.
type LoginInput struct {
	Email    string
	Password string
}

// ValidateRegistration returns normalized email and username values while
// preserving the password exactly as submitted for bcrypt comparison.
func ValidateRegistration(input RegistrationInput) (RegistrationInput, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	username := strings.TrimSpace(input.Username)

	if email == "" {
		return RegistrationInput{}, fmt.Errorf("email is required")
	}

	if _, err := mail.ParseAddress(email); err != nil {
		return RegistrationInput{}, fmt.Errorf("invalid email")
	}

	at := strings.LastIndexByte(email, '@')
	if at < 1 || !strings.Contains(email[at+1:], ".") {
		return RegistrationInput{}, fmt.Errorf("email domain must contain a dot")
	}

	if username == "" {
		return RegistrationInput{}, fmt.Errorf("username is required")
	}

	if len(username) < minUsernameLength {
		return RegistrationInput{}, fmt.Errorf(
			"username must be at least %d characters",
			minUsernameLength,
		)
	}

	if len(username) > maxUsernameLength {
		return RegistrationInput{}, fmt.Errorf(
			"username must be at most %d characters",
			maxUsernameLength,
		)
	}

	if input.Password == "" {
		return RegistrationInput{}, fmt.Errorf("password is required")
	}

	if len(input.Password) < minPasswordLength {
		return RegistrationInput{}, fmt.Errorf(
			"password must be at least %d characters",
			minPasswordLength,
		)
	}

	if len(input.Password) > maxPasswordLength {
		return RegistrationInput{}, fmt.Errorf(
			"password must be at most %d characters",
			maxPasswordLength,
		)
	}

	return RegistrationInput{
		Email:    email,
		Username: username,
		Password: input.Password,
	}, nil
}

// ValidateLogin normalizes the email and applies safe input-size limits.
func ValidateLogin(input LoginInput) (LoginInput, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))

	if email == "" {
		return LoginInput{}, fmt.Errorf("email is required")
	}

	if len(email) > 254 {
		return LoginInput{}, fmt.Errorf("email is too long")
	}

	if _, err := mail.ParseAddress(email); err != nil {
		return LoginInput{}, fmt.Errorf("invalid email")
	}

	at := strings.LastIndexByte(email, '@')
	if at < 1 || !strings.Contains(email[at+1:], ".") {
		return LoginInput{}, fmt.Errorf("email domain must contain a dot")
	}

	if input.Password == "" {
		return LoginInput{}, fmt.Errorf("password is required")
	}

	if len(input.Password) > maxPasswordLength {
		return LoginInput{}, fmt.Errorf(
			"password must be at most %d characters",
			maxPasswordLength,
		)
	}

	return LoginInput{
		Email:    email,
		Password: input.Password,
	}, nil
}
