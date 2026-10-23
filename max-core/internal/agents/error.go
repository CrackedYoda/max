package agents

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	ErrAgentNotFound        = shared.NewAppError("AGENT_NOT_FOUND", http.StatusNotFound, "agent not found")
	ErrCredentialNotFound   = shared.NewAppError("CREDENTIAL_NOT_FOUND", http.StatusNotFound, "credential not found")
	ErrCredentialInvalid    = shared.NewAppError("CREDENTIAL_INVALID", http.StatusUnauthorized, "API key is invalid, expired or revoked")
	ErrInvalidScope         = shared.NewAppError("INVALID_SCOPE", http.StatusBadRequest, "unknown credential scope")
	ErrInvalidPolicy        = shared.NewAppError("INVALID_POLICY", http.StatusBadRequest, "policy is invalid")
	ErrAgentPaused          = shared.NewAppError("AGENT_PAUSED", http.StatusForbidden, "agent is paused by its administrator")
	ErrSenderNotAgentWallet = shared.NewAppError("SENDER_NOT_AGENT_WALLET", http.StatusForbidden, "sender must be one of this agent's verified wallets")
	ErrPolicyDenied         = shared.NewAppError("POLICY_DENIED", http.StatusForbidden, "transfer denied by agent policy")
	ErrApprovalNotPending   = shared.NewAppError("APPROVAL_NOT_PENDING", http.StatusConflict, "this transfer is not awaiting approval")
	ErrNotAuthorized        = shared.NewAppError("TRANSFER_NOT_AUTHORIZED", http.StatusConflict, "transfer is not approved for signing")
	ErrTransferNotFound     = shared.NewAppError("TRANSFER_NOT_FOUND", http.StatusNotFound, "agent transfer not found")
	ErrSigningUnavailable   = shared.NewAppError("SIGNING_UNAVAILABLE", http.StatusServiceUnavailable, "authorization signing key is not configured")
)
