package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type GitHubProvider struct {
	cfg           ProviderConfig
	emailEndpoint string
}

func NewGitHubProvider(cfg ProviderConfig) *GitHubProvider {
	return &GitHubProvider{
		cfg:           cfg,
		emailEndpoint: "https://api.github.com/user/emails",
	}
}

func (p *GitHubProvider) AuthorizationURL(
	state string,
	challenge string,
) (string, error) {
	u, err := url.Parse(p.cfg.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse github authorization endpoint: %w", err)
	}

	q := u.Query()

	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("scope", "user:email")
	q.Set("prompt", "select_account")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")

	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (p *GitHubProvider) ExchangeCode(
	ctx context.Context,
	code string,
	verifier string,
) (string, error) {
	form := url.Values{}

	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.cfg.TokenEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("create github token request: %w", err)
	}

	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)
	req.Header.Set("Accept", "application/json")

	res, err := p.cfg.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github token request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"github token request returned status %d",
			res.StatusCode,
		)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
	}

	if err := decodeProviderJSON(res.Body, &payload); err != nil {
		return "", fmt.Errorf("decode github token response: %w", err)
	}

	if payload.AccessToken == "" {
		return "", fmt.Errorf("github token response missing access token")
	}

	return payload.AccessToken, nil
}

func (p *GitHubProvider) FetchUser(
	ctx context.Context,
	accessToken string,
) (User, error) {
	var githubUser struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}

	if err := p.getJSON(
		ctx,
		p.cfg.UserEndpoint,
		accessToken,
		&githubUser,
	); err != nil {
		return User{}, fmt.Errorf("fetch github user: %w", err)
	}

	if githubUser.ID == 0 {
		return User{}, fmt.Errorf("github user response missing id")
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := p.getJSON(
		ctx,
		p.emailEndpoint,
		accessToken,
		&emails,
	); err != nil {
		return User{}, fmt.Errorf("fetch github emails: %w", err)
	}

	var verifiedEmail string

	for _, email := range emails {
		if email.Primary && email.Verified {
			verifiedEmail = strings.TrimSpace(email.Email)
			break
		}
	}

	if verifiedEmail == "" {
		return User{}, fmt.Errorf(
			"github account has no primary verified email",
		)
	}

	return User{
		Provider:          "github",
		ProviderUserID:    strconv.FormatInt(githubUser.ID, 10),
		VerifiedEmail:     verifiedEmail,
		SuggestedUsername: githubUser.Login,
	}, nil
}

func (p *GitHubProvider) getJSON(
	ctx context.Context,
	endpoint string,
	accessToken string,
	dst any,
) error {
	if err := getProviderJSON(
		ctx,
		p.cfg.Client,
		endpoint,
		accessToken,
		"application/vnd.github+json",
		dst,
	); err != nil {
		return fmt.Errorf("github api request: %w", err)
	}

	return nil
}
