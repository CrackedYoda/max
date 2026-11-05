// Package settle builds and verifies marketplace settlement transactions.
// MAX never signs or submits: it describes the transaction for the buyer's
// wallet and later checks, exactly, what the chain recorded.
package settle

import (
	"context"
	"math/big"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

// Order is the snapshot a sale settles against.
type Order struct {
	SaleID                   uuid.UUID
	Chain                    chains.Chain
	Mint                     string
	Decimals                 int
	Buyer, Seller, Treasury  string
	Price, Fee, SellerAmount *big.Int
}

// Quote is the network cost of settling an Order.
type Quote struct {
	NetworkFee                         *big.Int // lamports
	CreateSellerATA, CreateTreasuryATA bool
	RentPerATA                         *big.Int
}

// FundsShortfall reports the buyer cannot cover the order.
type FundsShortfall struct {
	NeedAsset, HaveAsset, NeedNative, HaveNative *big.Int
}

func (e *FundsShortfall) Error() string { return "insufficient funds for settlement" }

type SolanaIx struct {
	Kind     string // create_ata_idempotent | transfer_checked | memo
	Accounts map[string]string
	Amount   string
	Decimals int
	Memo     string
}

type Instructions struct {
	Solana []SolanaIx
}

// Observation statuses.
const (
	StatusNotFound  = "not_found"
	StatusPending   = "pending"
	StatusConfirmed = "confirmed"
	StatusFailed    = "failed"
)

type Observation struct {
	Status        string
	Matches       bool
	Reason        string
	Confirmations int64
}

type Adapter interface {
	Quote(ctx context.Context, o Order) (Quote, error)
	CheckFunds(ctx context.Context, o Order, q Quote) error
	BuildTx(o Order, q Quote) (Instructions, error)
	Observe(ctx context.Context, o Order, txHash string) (Observation, error)
	// Balances returns "native" and each verified mint held by owner.
	Balances(ctx context.Context, chain chains.Chain, owner string) (map[string]*big.Int, error)
}
