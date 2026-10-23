package wallets

import (
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

type Wallet struct {
	ID         uuid.UUID
	Namespace  chains.Namespace
	Address    string
	VerifiedAt time.Time
	CreatedAt  time.Time
}
