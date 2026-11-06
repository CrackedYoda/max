package market

import (
	"net/http"

	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

// Spec §9 errors. NAME_LOCKED, INSUFFICIENT_REGISTRATION_BALANCE and
// PRICE_UNAVAILABLE are owned by maxids and not redefined here.
var (
	ErrMarketDisabled    = shared.NewAppError("MARKET_DISABLED", http.StatusNotFound, "the marketplace is not enabled")
	ErrMarketPaused      = shared.NewAppError("MARKET_PAUSED", http.StatusServiceUnavailable, "marketplace trading is paused")
	ErrNotListable       = shared.NewAppError("NOT_LISTABLE", http.StatusUnprocessableEntity, "this MAX ID cannot be listed right now")
	ErrListingNotActive  = shared.NewAppError("LISTING_NOT_ACTIVE", http.StatusConflict, "this listing is not active")
	ErrNameLocked        = maxids.ErrNameLocked
	ErrSelfTrade         = shared.NewAppError("SELF_TRADE", http.StatusUnprocessableEntity, "you cannot trade with yourself or a related account")
	ErrMarketRestricted  = shared.NewAppError("MARKET_RESTRICTED", http.StatusForbidden, "this account is restricted from the marketplace")
	ErrAssetNotAccepted  = shared.NewAppError("ASSET_NOT_ACCEPTED", http.StatusUnprocessableEntity, "this chain and asset are not accepted on the marketplace")
	ErrInsufficientFunds = shared.NewAppError("INSUFFICIENT_FUNDS", http.StatusUnprocessableEntity, "the paying wallet does not hold enough funds")
	ErrOfferLimit        = shared.NewAppError("OFFER_LIMIT", http.StatusConflict, "offer limit reached")
	ErrOfferBelowMinimum = shared.NewAppError("OFFER_BELOW_MINIMUM", http.StatusUnprocessableEntity, "amount is below the marketplace minimum")
	ErrOfferUnfunded     = shared.NewAppError("OFFER_UNFUNDED", http.StatusUnprocessableEntity, "the buyer wallet does not currently cover this offer")
	ErrSaleExpired       = shared.NewAppError("SALE_EXPIRED", http.StatusConflict, "this sale has expired")
	ErrTxMismatch        = shared.NewAppError("TX_MISMATCH", http.StatusUnprocessableEntity, "the transaction does not match the sale terms")
	// ErrPendingApproval means an agent's action is held for its
	// administrator. Nothing happened; the handler answers 202 with
	// {status: "pending_approval", authorizationId}.
	ErrPendingApproval = shared.NewAppError("PENDING_APPROVAL", http.StatusAccepted, "this action is awaiting human approval")
)

// NotListable returns ErrNotListable carrying {reason: code}.
func NotListable(reason string) *shared.AppError {
	return ErrNotListable.WithDetails(map[string]any{"reason": reason})
}

// InsufficientFunds returns ErrInsufficientFunds with the shortfall details.
func InsufficientFunds(needAsset, haveAsset, needNative, haveNative string) *shared.AppError {
	return ErrInsufficientFunds.WithDetails(map[string]any{
		"needAsset": needAsset, "haveAsset": haveAsset,
		"needNative": needNative, "haveNative": haveNative,
	})
}
