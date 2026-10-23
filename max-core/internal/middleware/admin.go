package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/max/max-core/internal/shared"
)

// Admin allows only signed-in sessions whose account is on the configured
// administrator list. Non-admins get 404 so the admin surface isn't advertised.
func Admin(isAdmin func(uuid.UUID) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := shared.GetPrincipal(c)
		if !ok || p.IsAgentCredential() || isAdmin == nil || !isAdmin(p.AccountID) {
			shared.Fail(c, shared.ErrNotFound)
			return
		}
		c.Next()
	}
}
