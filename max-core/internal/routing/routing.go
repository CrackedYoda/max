// Package routing defines the provider-neutral contract for cross-chain
// settlement. Providers (NEAR Intents today) plug in behind Provider so they
// can be replaced or run side by side without touching payments.
package routing

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/max/max-core/internal/chains"
)

var (
	// ErrRouteUnavailable means the provider cannot route this asset or pair.
	ErrRouteUnavailable = errors.New("route unavailable")
	// ErrDepositNotFound means the provider has no record of the deposit address yet.
	ErrDepositNotFound = errors.New("deposit not found")
)

// QuoteRejectedError carries a provider's client-safe reason (e.g. amount too low).
type QuoteRejectedError struct{ Reason string }

func (e *QuoteRejectedError) Error() string { return "quote rejected: " + e.Reason }

type AssetKind string

const (
	AssetNative AssetKind = "native"
	AssetToken  AssetKind = "token"
)

// AssetRef identifies an asset by chain and contract/mint (canonical form),
// never by symbol.
type AssetRef struct {
	Chain   chains.Chain
	Kind    AssetKind
	Address string
}

type ProviderAsset struct {
	ID       string
	Decimals int
	Symbol   string
}

// ListedAsset is something a provider can deliver on a MAX chain.
type ListedAsset struct {
	ChainKey string
	Kind     AssetKind
	Address  string // empty for the native coin
	Symbol   string
	Decimals int
	PriceUSD float64
}

// AssetLister is implemented by providers that can list what they deliver.
type AssetLister interface {
	Assets(ctx context.Context) ([]ListedAsset, error)
}

type SwapType string

const (
	ExactInput  SwapType = "EXACT_INPUT"
	ExactOutput SwapType = "EXACT_OUTPUT"
)

type QuoteRequest struct {
	Origin      AssetRef
	Destination AssetRef
	SwapType    SwapType
	// Amount is in base units of the origin asset (EXACT_INPUT) or the
	// destination asset (EXACT_OUTPUT).
	Amount      *big.Int
	Recipient   string // destination-chain address
	RefundTo    string // origin-chain address
	SlippageBps int
	Deadline    time.Time
}

type Quote struct {
	Provider           string
	OriginAssetID      string
	DestinationAssetID string
	DepositAddress     string
	AmountIn           *big.Int
	MinAmountIn        *big.Int
	AmountOut          *big.Int
	MinAmountOut       *big.Int
	AmountInUSD        string
	AmountOutUSD       string
	Deadline           time.Time
	TimeEstimate       time.Duration
	Signature          string
	CorrelationID      string
	Raw                json.RawMessage
}

type ExecutionState string

const (
	StatePendingDeposit    ExecutionState = "PENDING_DEPOSIT"
	StateKnownDepositTx    ExecutionState = "KNOWN_DEPOSIT_TX"
	StateIncompleteDeposit ExecutionState = "INCOMPLETE_DEPOSIT"
	StateProcessing        ExecutionState = "PROCESSING"
	StateSuccess           ExecutionState = "SUCCESS"
	StateRefunded          ExecutionState = "REFUNDED"
	StateFailed            ExecutionState = "FAILED"
)

type TxRef struct {
	Hash        string `json:"hash"`
	ExplorerURL string `json:"explorerUrl"`
}

type ExecutionStatus struct {
	State          ExecutionState
	UpdatedAt      time.Time
	AmountOut      *big.Int
	RefundedAmount *big.Int
	RefundReason   string
	OriginTxs      []TxRef
	DestinationTxs []TxRef
}

type Provider interface {
	Name() string
	// Asset maps a MAX asset to the provider's identifier; ErrRouteUnavailable if unsupported.
	Asset(ctx context.Context, ref AssetRef) (ProviderAsset, error)
	// Quote requests a live (non-dry) quote with a deposit address.
	Quote(ctx context.Context, req QuoteRequest) (Quote, error)
	// SubmitDeposit is an optional hint that speeds up processing.
	SubmitDeposit(ctx context.Context, depositAddress, txHash string) error
	Status(ctx context.Context, depositAddress string) (ExecutionStatus, error)
}
