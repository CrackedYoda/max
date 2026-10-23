package auth

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	ErrUnsupportedChain      = shared.NewAppError("UNSUPPORTED_CHAIN", http.StatusBadRequest, "wallet linking is not supported for this chain")
	ErrInvalidAddress        = shared.NewAppError("INVALID_ADDRESS", http.StatusBadRequest, "wallet address is invalid")
	ErrInvalidPurpose        = shared.NewAppError("INVALID_PURPOSE", http.StatusBadRequest, "purpose must be login or link")
	ErrChallengeInvalid      = shared.NewAppError("CHALLENGE_INVALID", http.StatusUnauthorized, "challenge is invalid, expired or already used")
	ErrSignatureInvalid      = shared.NewAppError("SIGNATURE_INVALID", http.StatusUnauthorized, "signature verification failed")
	ErrWalletLinkedElsewhere = shared.NewAppError("WALLET_ALREADY_LINKED", http.StatusConflict, "this wallet is linked to another MAX account")
	ErrAccountSuspended      = shared.NewAppError("ACCOUNT_SUSPENDED", http.StatusForbidden, "account is suspended")
	ErrAgentLogin            = shared.NewAppError("AGENT_LOGIN_NOT_ALLOWED", http.StatusForbidden, "this wallet belongs to an agent; agents authenticate with API keys, not sessions")
	ErrSessionInvalid        = shared.NewAppError("SESSION_INVALID", http.StatusUnauthorized, "session is invalid or expired")
)
