package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type GoogleProvider struct {
	cfg ProviderConfig
}

func NewGoogleProvider(cfg ProviderConfig) *GoogleProvider {
	return &GoogleProvider{cfg: cfg}
}

func (p *GoogleProvider) AuthorizationURL(
	state string,
	challenge string,
) (string, error) {
	u, err := url.Parse(p.cfg.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse google authorization endpoint: %w", err)
	}

	query := u.Query()

	query.Set("client_id", p.cfg.ClientID)
	query.Set("redirect_uri", p.cfg.RedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", "openid email profile")
	query.Set("prompt", "select_account")
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")

	u.RawQuery = query.Encode()

	return u.String(), nil
}

func (p *GoogleProvider) ExchangeCode(
	ctx context.Context,
	code string,
	verifier string,
) (string, error) {
	form := url.Values{}

	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.cfg.TokenEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("create google token request: %w", err)
	}

	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	req.Header.Set("Accept", "application/json")

	res, err := p.cfg.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("google token request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"google token request returned status %d",
			res.StatusCode,
		)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}

	if err := decodeProviderJSON(res.Body, &payload); err != nil {
		return "", fmt.Errorf("decode google token response: %w", err)
	}

	if payload.AccessToken == "" {
		return "", fmt.Errorf("google token response missing access token")
	}

	return payload.AccessToken, nil
}

func (p *GoogleProvider) FetchUser(
	ctx context.Context,
	accessToken string,
) (User, error) {
	var profile struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}

	if err := getProviderJSON(
		ctx,
		p.cfg.Client,
		p.cfg.UserEndpoint,
		accessToken,
		"application/json",
		&profile,
	); err != nil {
		return User{}, fmt.Errorf("fetch google user: %w", err)
	}

	providerUserID := strings.TrimSpace(profile.Sub)
	if providerUserID == "" {
		return User{}, fmt.Errorf("google user response missing sub")
	}

	verifiedEmail := strings.TrimSpace(profile.Email)
	if verifiedEmail == "" || !profile.EmailVerified {
		return User{}, fmt.Errorf("google account has no verified email")
	}

	return User{
		Provider:          "google",
		ProviderUserID:    providerUserID,
		VerifiedEmail:     verifiedEmail,
		SuggestedUsername: profile.Name,
	}, nil
}
