package shared

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// BindJSON binds and validates a request body, converting failures into a
// client-safe validation error that names the offending fields.
func BindJSON(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return ErrBodyTooLarge.WithError(err)
		}
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			parts := make([]string, 0, len(verrs))
			for _, fe := range verrs {
				parts = append(parts, fmt.Sprintf("%s failed %s", fe.Field(), fe.Tag()))
			}
			return ErrValidation.WithMessage(strings.Join(parts, "; ")).WithError(err)
		}
		return ErrBadRequest.WithMessage("malformed JSON body").WithError(err)
	}
	return nil
}
