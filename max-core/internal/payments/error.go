package payments

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	ErrPaymentNotFound     = shared.NewAppError("PAYMENT_NOT_FOUND", http.StatusNotFound, "payment not found")
	ErrInvalidAmount       = shared.NewAppError("INVALID_AMOUNT", http.StatusBadRequest, "amount is invalid")
	ErrInvalidSender       = shared.NewAppError("INVALID_SENDER_ADDRESS", http.StatusBadRequest, "sender address is invalid for this chain")
	ErrInvalidTxHash       = shared.NewAppError("INVALID_TX_HASH", http.StatusBadRequest, "transaction hash is invalid for this chain")
	ErrChainNotConfigured  = shared.NewAppError("CHAIN_NOT_CONFIGURED", http.StatusServiceUnavailable, "payments on this chain are not enabled on this server")
	ErrAssetUnavailable    = shared.NewAppError("ASSET_UNAVAILABLE", http.StatusBadRequest, "token could not be verified on this chain")
	ErrResolutionStale     = shared.NewAppError("RESOLUTION_STALE", http.StatusConflict, "recipient's wallet configuration changed; review the new destination before sending")
	ErrIdempotencyConflict = shared.NewAppError("IDEMPOTENCY_CONFLICT", http.StatusConflict, "idempotency key was already used for a different payment")
	ErrInvalidState        = shared.NewAppError("INVALID_PAYMENT_STATE", http.StatusConflict, "payment is not in a state that allows this action")
	ErrTxAlreadyUsed       = shared.NewAppError("TX_ALREADY_USED", http.StatusConflict, "this transaction is already attached to another payment")
	ErrTxHashMismatch      = shared.NewAppError("TX_HASH_MISMATCH", http.StatusConflict, "a different transaction was already submitted for this payment")
	ErrSelfPayment         = shared.NewAppError("SELF_PAYMENT", http.StatusBadRequest, "sender and recipient addresses are the same")
	ErrRoutingDisabled     = shared.NewAppError("CROSS_CHAIN_DISABLED", http.StatusServiceUnavailable, "cross-chain payments are temporarily disabled")
	ErrRouteUnavailable    = shared.NewAppError("ROUTE_UNAVAILABLE", http.StatusUnprocessableEntity, "no supported cross-chain route for this asset pair")
	ErrQuoteRejected       = shared.NewAppError("QUOTE_REJECTED", http.StatusUnprocessableEntity, "the routing provider declined to quote this payment")
	ErrQuoteInvalid        = shared.NewAppError("QUOTE_INVALID", http.StatusBadGateway, "the routing provider returned a quote that failed MAX's safety checks")
	ErrRoutingProvider     = shared.NewAppError("ROUTING_PROVIDER_ERROR", http.StatusBadGateway, "the routing provider is unavailable; try again shortly")
	ErrRouteLimit          = shared.NewAppError("ROUTE_LIMIT_EXCEEDED", http.StatusUnprocessableEntity, "amount exceeds the cross-chain limit")
	ErrInvalidSlippage     = shared.NewAppError("INVALID_SLIPPAGE", http.StatusBadRequest, "slippage tolerance is out of range")
	ErrStaleTransition     = shared.NewAppError("STALE_TRANSITION", http.StatusConflict, "payment state changed concurrently")
)
