package shared

import (
	"errors"
	"net/http"
)

type AppError struct {
	Code    string
	Status  int
	Message string
	Err     error
	// Details is optional machine-readable context returned to the client.
	Details map[string]any
}

func NewAppError(code string, status int, message string) *AppError {
	return &AppError{Code: code, Status: status, Message: message}
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *AppError) Unwrap() error { return e.Err }

func (e *AppError) Is(target error) bool {
	var t *AppError
	if errors.As(target, &t) {
		return t.Code == e.Code
	}
	return false
}

// WithError clones the sentinel so the shared value is never mutated.
func (e *AppError) WithError(err error) *AppError {
	c := *e
	c.Err = err
	return &c
}

func (e *AppError) WithMessage(msg string) *AppError {
	c := *e
	c.Message = msg
	return &c
}

// WithDetails clones the sentinel and attaches response details.
func (e *AppError) WithDetails(d map[string]any) *AppError {
	c := *e
	c.Details = d
	return &c
}

var (
	ErrInternal     = NewAppError("INTERNAL_ERROR", http.StatusInternalServerError, "something went wrong")
	ErrBadRequest   = NewAppError("BAD_REQUEST", http.StatusBadRequest, "invalid request")
	ErrValidation   = NewAppError("VALIDATION_FAILED", http.StatusBadRequest, "request validation failed")
	ErrBodyTooLarge = NewAppError("BODY_TOO_LARGE", http.StatusRequestEntityTooLarge, "request body is too large")
	ErrUnauthorized = NewAppError("UNAUTHORIZED", http.StatusUnauthorized, "authentication required")
	ErrForbidden    = NewAppError("FORBIDDEN", http.StatusForbidden, "not allowed")
	ErrNotFound     = NewAppError("NOT_FOUND", http.StatusNotFound, "resource not found")
	ErrConflict     = NewAppError("CONFLICT", http.StatusConflict, "resource conflict")
	ErrRateLimited  = NewAppError("RATE_LIMITED", http.StatusTooManyRequests, "too many requests")
)
