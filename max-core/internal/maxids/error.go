package maxids

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	ErrInvalidName           = shared.NewAppError("INVALID_MAX_ID", http.StatusBadRequest, "MAX ID is not valid")
	ErrInvalidType           = shared.NewAppError("INVALID_MAX_ID_TYPE", http.StatusBadRequest, "type must be human or agent")
	ErrNameTaken             = shared.NewAppError("MAX_ID_TAKEN", http.StatusConflict, "this MAX ID is already taken")
	ErrNameQuarantined       = shared.NewAppError("MAX_ID_QUARANTINED", http.StatusConflict, "this MAX ID was recently released and is temporarily unavailable")
	ErrNameReserved          = shared.NewAppError("MAX_ID_RESERVED", http.StatusForbidden, "this MAX ID is reserved")
	ErrNameNeedsVerification = shared.NewAppError("MAX_ID_VERIFICATION_REQUIRED", http.StatusForbidden, "this MAX ID is reserved; verify the matching X account, then claim it via POST /v1/ids/reserved")
	ErrIDLimitReached        = shared.NewAppError("MAX_ID_LIMIT_REACHED", http.StatusForbidden, "account has reached the MAX ID limit")
	ErrMaxIDNotFound         = shared.NewAppError("MAX_ID_NOT_FOUND", http.StatusNotFound, "MAX ID not found")
	ErrWalletNotFound        = shared.NewAppError("WALLET_NOT_FOUND", http.StatusNotFound, "wallet not found")
	ErrNamespaceMismatch     = shared.NewAppError("NAMESPACE_MISMATCH", http.StatusBadRequest, "wallet does not belong to the requested chain namespace")
	ErrUnsupportedNamespace  = shared.NewAppError("UNSUPPORTED_CHAIN", http.StatusBadRequest, "unsupported chain namespace")
	ErrDestinationNotFound   = shared.NewAppError("DESTINATION_NOT_FOUND", http.StatusNotFound, "no destination configured for this chain")
	ErrSettlementChain       = shared.NewAppError("INVALID_SETTLEMENT_CHAIN", http.StatusBadRequest, "settlement chain must be a supported mainnet chain")
	ErrSettlementAsset       = shared.NewAppError("INVALID_SETTLEMENT_ASSET", http.StatusBadRequest, "settlement asset must be 'native' or a valid token address for the chain")
	ErrSettlementNoWallet    = shared.NewAppError("SETTLEMENT_NO_WALLET", http.StatusBadRequest, "set a destination wallet on the settlement chain first")
	ErrSettlementNotFound    = shared.NewAppError("SETTLEMENT_NOT_FOUND", http.StatusNotFound, "no settlement preference configured")
	ErrMaxIDNotActive        = shared.NewAppError("MAX_ID_NOT_ACTIVE", http.StatusConflict, "MAX ID is not active")
)

var (
	ErrInsufficientRegistrationBalance = shared.NewAppError("INSUFFICIENT_REGISTRATION_BALANCE", http.StatusForbidden, "linked wallets do not hold enough value to register a MAX ID")
	ErrPriceUnavailable                = shared.NewAppError("PRICE_UNAVAILABLE", http.StatusServiceUnavailable, "wallet balances could not be priced right now; try again shortly")
)
