package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/shared"
)

func RequestLogger(base *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		log := base.With(zap.String("request_id", c.GetString("request_id")))
		shared.SetLogger(c, log)

		c.Next()

		log.Info("http",
			zap.String("method", c.Request.Method),
			zap.String("route", c.FullPath()),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		)
	}
}
