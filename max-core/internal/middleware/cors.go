package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/shared"
)

// CORS allows credentialed requests from an explicit origin list. It also
// rejects state-changing requests whose Origin is not on the list, which —
// together with SameSite=Lax session cookies — is the CSRF defence.
func CORS(origins []string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowed[strings.TrimRight(o, "/")] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		ok := origin != "" && allowed[origin]

		if ok {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			if ok {
				h := c.Writer.Header()
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				// Solana-Client is sent by @solana/web3.js on every call through the /v1/rpc proxy.
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Solana-Client")
				h.Set("Access-Control-Max-Age", "600")
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		if origin != "" && !ok && !isSafeMethod(c.Request.Method) {
			shared.Fail(c, shared.ErrForbidden.WithMessage("origin not allowed"))
			return
		}
		c.Next()
	}
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}
