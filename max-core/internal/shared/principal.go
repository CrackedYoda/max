package shared

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const principalKey = "principal"

// Principal is the authenticated caller, set by the auth middleware.
type Principal struct {
	AccountID   uuid.UUID
	AccountType string
	// SessionID is set for browser sessions (humans only).
	SessionID uuid.UUID
	// CredentialID and Scopes are set when an agent authenticates with an API key.
	CredentialID *uuid.UUID
	Scopes       []string
}

func (p Principal) IsAgentCredential() bool { return p.CredentialID != nil }

// HasScope reports whether the caller may perform an action. Sessions belong
// to humans acting on their own account and are not scope-limited.
func (p Principal) HasScope(scope string) bool {
	if p.CredentialID == nil {
		return true
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func SetPrincipal(c *gin.Context, p Principal) {
	c.Set(principalKey, p)
}

func GetPrincipal(c *gin.Context) (Principal, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return Principal{}, false
	}
	p, ok := v.(Principal)
	return p, ok
}

// MustPrincipal is for handlers mounted behind the auth middleware.
func MustPrincipal(c *gin.Context) Principal {
	p, ok := GetPrincipal(c)
	if !ok {
		panic("MustPrincipal called on a route without auth middleware")
	}
	return p
}
