package middleware

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/shared"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				shared.GetLogger(c).Error("panic recovered", zap.Any("panic", r), zap.Stack("stack"))
				shared.Fail(c, shared.ErrInternal.WithError(fmt.Errorf("panic: %v", r)))
			}
		}()
		c.Next()
	}
}
