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
	// ErrDefaultNotTransferable backs the market and transfer paths: default IDs never move.
	ErrDefaultNotTransferable = shared.NewAppError("DEFAULT_ID_LOCKED", http.StatusConflict, "default MAX IDs cannot be transferred, sold or renamed")
	// ErrDefaultExists means the account already has a default ID.
	ErrDefaultExists  = shared.NewAppError("DEFAULT_ID_EXISTS", http.StatusConflict, "this account already has a default MAX ID")
	ErrMaxIDNotActive = shared.NewAppError("MAX_ID_NOT_ACTIVE", http.StatusConflict, "MAX ID is not active")
)

var (
	ErrInsufficientRegistrationBalance = shared.NewAppError("INSUFFICIENT_REGISTRATION_BALANCE", http.StatusForbidden, "linked wallets do not hold enough value to register a MAX ID")
	ErrPriceUnavailable                = shared.NewAppError("PRICE_UNAVAILABLE", http.StatusServiceUnavailable, "wallet balances could not be priced right now; try again shortly")
)

var (
	// ErrNameListed refuses renames while the name has a live listing.
	ErrNameListed = shared.NewAppError("NAME_LISTED", http.StatusConflict, "this MAX ID is listed for sale; delist it before renaming")
	// ErrNameLocked refuses edits while the name has an open sale.
	ErrNameLocked = shared.NewAppError("NAME_LOCKED", http.StatusConflict, "this MAX ID has a sale in progress and cannot be changed")
)

var (
	// ErrTransferOwnerMismatch means the ID is no longer held by the expected owner.
	ErrTransferOwnerMismatch = shared.NewAppError("OWNER_CHANGED", http.StatusConflict, "the MAX ID is no longer held by the expected owner")
	// ErrTransferSameAccount means a transfer named the same account on both sides.
	ErrTransferSameAccount = shared.NewAppError("TRANSFER_SAME_ACCOUNT", http.StatusBadRequest, "cannot transfer a MAX ID to its current owner")
	// ErrRenamePending refuses a transfer while a rename awaits payment. It
	// shares its code with renames.ErrRenamePending, so errors.Is matches both.
	ErrRenamePending = shared.NewAppError("RENAME_PENDING", http.StatusConflict, "a rename for this MAX ID is already awaiting payment")
)
