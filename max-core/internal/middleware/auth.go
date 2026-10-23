package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/auth"
	"github.com/max/max-core/internal/shared"
)

const apiKeyPrefix = "maxk_"

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (auth.Session, error)
}

// CredentialAuthenticator verifies agent API keys.
type CredentialAuthenticator interface {
	Authenticate(ctx context.Context, apiKey string) (shared.Principal, error)
}

type Auth struct {
	Sessions    Authenticator
	Credentials CredentialAuthenticator
}

func (a Auth) principal(c *gin.Context) (shared.Principal, bool, error) {
	token := bearer(c)
	if token == "" {
		return shared.Principal{}, false, nil
	}
	if strings.HasPrefix(token, apiKeyPrefix) {
		p, err := a.Credentials.Authenticate(c.Request.Context(), token)
		return p, err == nil, err
	}
	s, err := a.Sessions.Authenticate(c.Request.Context(), token)
	if err != nil {
		return shared.Principal{}, false, err
	}
	return shared.Principal{AccountID: s.AccountID, AccountType: s.AccountType, SessionID: s.ID}, true, nil
}

// Optional attaches the principal when valid credentials are presented and
// otherwise lets the request through anonymously.
func (a Auth) Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p, ok, _ := a.principal(c); ok {
			shared.SetPrincipal(c, p)
		}
		c.Next()
	}
}

func (a Auth) Required() gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok, err := a.principal(c)
		if err != nil {
			shared.Fail(c, err)
			return
		}
		if !ok {
			shared.Fail(c, shared.ErrUnauthorized)
			return
		}
		shared.SetPrincipal(c, p)
		c.Next()
	}
}

// SessionOnly restricts a route to signed-in humans; agent API keys are refused.
func SessionOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p, ok := shared.GetPrincipal(c); ok && p.IsAgentCredential() {
			shared.Fail(c, shared.ErrForbidden.WithMessage("this endpoint requires a signed-in administrator, not an agent key"))
			return
		}
		c.Next()
	}
}

// AgentOnly restricts a route to agent API keys.
func AgentOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p, ok := shared.GetPrincipal(c); !ok || !p.IsAgentCredential() {
			shared.Fail(c, shared.ErrForbidden.WithMessage("this endpoint requires an agent API key"))
			return
		}
		c.Next()
	}
}

// Scope enforces an agent credential scope. Human sessions are not scoped;
// anonymous requests pass through for routes that allow them.
func Scope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p, ok := shared.GetPrincipal(c); ok && !p.HasScope(scope) {
			shared.Fail(c, shared.ErrForbidden.WithMessage("API key lacks scope "+scope))
			return
		}
		c.Next()
	}
}

func bearer(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if k := c.GetHeader("X-API-Key"); k != "" {
		return strings.TrimSpace(k)
	}
	if v, err := c.Cookie(auth.SessionCookie); err == nil {
		return v
	}
	return ""
}
