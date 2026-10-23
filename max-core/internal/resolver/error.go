package resolver

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	ErrMaxIDNotFound    = shared.NewAppError("MAX_ID_NOT_FOUND", http.StatusNotFound, "MAX ID not found")
	ErrUnsupportedChain = shared.NewAppError("UNSUPPORTED_CHAIN", http.StatusBadRequest, "chain is not supported")
	ErrInvalidAsset     = shared.NewAppError("INVALID_ASSET", http.StatusBadRequest, "asset must be 'native' or a valid token contract/mint address for the chain")
	ErrNoDestination    = shared.NewAppError("NO_COMPATIBLE_WALLET", http.StatusNotFound, "recipient has no verified wallet for this chain")
)
