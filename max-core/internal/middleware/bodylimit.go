package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/shared"
)

// BodyLimit caps request bodies at max bytes before any handler decodes them.
// A declared Content-Length over the cap is refused up front; chunked or
// understated bodies fail when the reader passes the cap.
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > max {
			shared.Fail(c, shared.ErrBodyTooLarge)
			c.Abort()
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		}
		c.Next()
	}
}
