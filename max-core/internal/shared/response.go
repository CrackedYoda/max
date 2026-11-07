package shared

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Fail is the single place errors become wire responses. Unknown errors are
// logged in full and returned as a generic 500.
func Fail(c *gin.Context, err error) {
	var appErr *AppError
	if !errors.As(err, &appErr) {
		appErr = ErrInternal.WithError(err)
	}

	log := GetLogger(c)
	if appErr.Status >= 500 {
		log.Error("request failed", zap.String("code", appErr.Code), zap.Error(appErr))
	} else {
		log.Warn("request rejected", zap.String("code", appErr.Code), zap.Error(appErr))
	}

	c.AbortWithStatusJSON(appErr.Status, gin.H{"error": errorBody{Code: appErr.Code, Message: appErr.Message, Details: appErr.Details}})
}
