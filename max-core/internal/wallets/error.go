package wallets

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	ErrWalletNotFound = shared.NewAppError("WALLET_NOT_FOUND", http.StatusNotFound, "wallet not found")
	ErrLastWallet     = shared.NewAppError("LAST_WALLET", http.StatusConflict, "cannot remove the only wallet on the account; link another wallet first")
)
