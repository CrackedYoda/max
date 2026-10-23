package wallets

import (
	"time"

	"github.com/max/max-core/internal/chains"
)

type walletResponse struct {
	ID         string    `json:"id"`
	Namespace  string    `json:"namespace"`
	Address    string    `json:"address"`
	VerifiedAt time.Time `json:"verifiedAt"`
	CreatedAt  time.Time `json:"createdAt"`
}

func toWalletResponse(w Wallet) walletResponse {
	return walletResponse{
		ID: w.ID.String(), Namespace: string(w.Namespace),
		Address:    chains.DisplayAddress(w.Namespace, w.Address),
		VerifiedAt: w.VerifiedAt, CreatedAt: w.CreatedAt,
	}
}
