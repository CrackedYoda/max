package verifications

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	ErrXDisabled               = shared.NewAppError("X_VERIFICATION_DISABLED", http.StatusServiceUnavailable, "X verification is not configured on this server")
	ErrSessionRequired         = shared.NewAppError("SESSION_REQUIRED", http.StatusForbidden, "X verification requires a signed-in browser session")
	ErrOAuthStateInvalid       = shared.NewAppError("OAUTH_STATE_INVALID", http.StatusBadRequest, "verification link is invalid, expired or already used")
	ErrOAuthSessionMismatch    = shared.NewAppError("OAUTH_SESSION_MISMATCH", http.StatusForbidden, "verification must be completed in the same browser session that started it")
	ErrOAuthDenied             = shared.NewAppError("OAUTH_DENIED", http.StatusBadRequest, "X authorization was declined")
	ErrXProvider               = shared.NewAppError("X_PROVIDER_ERROR", http.StatusBadGateway, "could not complete verification with X; try again")
	ErrXAccountLinkedElsewhere = shared.NewAppError("X_ACCOUNT_LINKED_ELSEWHERE", http.StatusConflict, "this X account already verifies another MAX account; open a dispute if this is wrong")
	ErrNotReserved             = shared.NewAppError("NAME_NOT_RESERVED", http.StatusBadRequest, "this name is not reserved; claim it with POST /v1/ids")
	ErrVerificationRequired    = shared.NewAppError("X_VERIFICATION_REQUIRED", http.StatusForbidden, "verify the matching X account first")
	ErrVerificationStale       = shared.NewAppError("X_VERIFICATION_STALE", http.StatusForbidden, "your X verification is too old to claim with; verify again")
	ErrXHandleMismatch         = shared.NewAppError("X_HANDLE_MISMATCH", http.StatusForbidden, "your verified X account does not match this reserved name")
	ErrClaimInProgress         = shared.NewAppError("RESERVED_CLAIM_EXISTS", http.StatusConflict, "this reserved name already has a claim in progress")
	ErrClaimNotFound           = shared.NewAppError("CLAIM_NOT_FOUND", http.StatusNotFound, "claim not found")
	ErrClaimState              = shared.NewAppError("CLAIM_STATE_CONFLICT", http.StatusConflict, "claim is not in a state that allows this action")
	ErrVerificationNotFound    = shared.NewAppError("VERIFICATION_NOT_FOUND", http.StatusNotFound, "verification not found")
)
