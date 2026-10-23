// Package xapi implements the slice of X's OAuth 2.0 (Authorization Code with
// PKCE) and users API that MAX needs to prove control of an X account.
// Docs: https://docs.x.com/fundamentals/authentication/oauth-2-0/user-access-token
package xapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultAuthorizeURL = "https://x.com/i/oauth2/authorize"
	DefaultAPIBase      = "https://api.x.com"
	// users/me requires both scopes; MAX asks for nothing that can post or read DMs.
	Scopes = "tweet.read users.read"
)

type Config struct {
	ClientID string
	// ClientSecret is set for confidential clients (Basic auth on token calls).
	ClientSecret string
	RedirectURI  string
	AuthorizeURL string
	APIBase      string
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	if cfg.AuthorizeURL == "" {
		cfg.AuthorizeURL = DefaultAuthorizeURL
	}
	if cfg.APIBase == "" {
		cfg.APIBase = DefaultAPIBase
	}
	cfg.APIBase = strings.TrimRight(cfg.APIBase, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Enabled() bool { return c.cfg.ClientID != "" && c.cfg.RedirectURI != "" }

type User struct {
	ID        string
	Username  string
	Name      string
	CreatedAt *time.Time
}

// NewPKCE returns a code verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func (c *Client) AuthorizeURL(state, challenge string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {c.cfg.ClientID},
		"redirect_uri":          {c.cfg.RedirectURI},
		"scope":                 {Scopes},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	// X expects %20 between scopes.
	return c.cfg.AuthorizeURL + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {c.cfg.RedirectURI},
		"code_verifier": {verifier},
	}
	var out struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := c.postForm(ctx, "/2/oauth2/token", form, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", errors.New("x: token response had no access_token")
	}
	return out.AccessToken, nil
}

// Revoke invalidates an access token; MAX never keeps tokens past verification.
func (c *Client) Revoke(ctx context.Context, token string) error {
	return c.postForm(ctx, "/2/oauth2/revoke", url.Values{"token": {token}}, nil)
}

func (c *Client) Me(ctx context.Context, token string) (User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.APIBase+"/2/users/me?user.fields=created_at", nil)
	if err != nil {
		return User{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	var out struct {
		Data struct {
			ID        string `json:"id"`
			Username  string `json:"username"`
			Name      string `json:"name"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	if err := c.do(req, &out); err != nil {
		return User{}, err
	}
	if out.Data.ID == "" || out.Data.Username == "" {
		return User{}, errors.New("x: users/me returned no id or username")
	}
	u := User{ID: out.Data.ID, Username: out.Data.Username, Name: out.Data.Name}
	if t, err := time.Parse(time.RFC3339, out.Data.CreatedAt); err == nil {
		u.CreatedAt = &t
	}
	return u, nil
}

func (c *Client) postForm(ctx context.Context, path string, form url.Values, out any) error {
	if c.cfg.ClientSecret == "" {
		form.Set("client_id", c.cfg.ClientID) // public client
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.cfg.ClientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(c.cfg.ClientID), url.QueryEscape(c.cfg.ClientSecret))
	}
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("x %s: %w", req.URL.Path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		// Error bodies can echo request data; keep only the status in errors.
		return fmt.Errorf("x %s: http %d", req.URL.Path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
