package payments

import (
	"encoding/json"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/routing"
)

type Status string

const (
	StatusCreated                 Status = "CREATED"
	StatusQuoted                  Status = "QUOTED"
	StatusAwaitingSenderSignature Status = "AWAITING_SENDER_SIGNATURE"
	StatusSubmitted               Status = "SUBMITTED"
	StatusConfirming              Status = "CONFIRMING"
	StatusCompleted               Status = "COMPLETED"
	StatusExpired                 Status = "EXPIRED"
	StatusFailed                  Status = "FAILED"
	StatusRefundPending           Status = "REFUND_PENDING"
	StatusRefunded                Status = "REFUNDED"
	StatusManualReview            Status = "MANUAL_REVIEW"
)

type AssetKind string

const (
	AssetNative AssetKind = "native"
	AssetToken  AssetKind = "token"
)

type Purpose string

const (
	PurposeTransfer  Purpose = "transfer"
	PurposeRenameFee Purpose = "rename_fee"
)

type RouteType string

const (
	RouteDirect     RouteType = "direct"
	RouteCrossChain RouteType = "cross_chain"
)

// ReceivableAsset is an asset a cross-chain payment can deliver.
type ReceivableAsset struct {
	Chain    chains.Chain
	Kind     AssetKind
	Address  string // display form; empty for the native coin
	Symbol   string
	Decimals int
	PriceUSD float64
	// Verified is true for native coins and tokens on MAX's verified list.
	Verified bool
}

type Asset struct {
	Kind         AssetKind
	Address      string // canonical contract/mint; empty for native
	Decimals     int
	Symbol       string
	Verified     bool
	TokenProgram string // Solana SPL token program owning the mint
}

// Payment is an immutable intent plus its mutable lifecycle state. Everything
// above Status is the authorization snapshot and never changes after creation.
type Payment struct {
	ID              uuid.UUID
	Purpose         Purpose
	SenderAccountID *uuid.UUID
	SenderAddress   string
	IdempotencyKey  string
	// MaxID is uuid.Nil for fee payments, which pay MAX's treasury.
	MaxID             uuid.UUID
	RecipientHandle   string
	RecipientAddress  string
	ResolutionVersion int64
	Chain             chains.Chain
	Asset             Asset
	Amount            *big.Int
	Route             RouteType
	// CrossChain is set for RouteCrossChain. RecipientAddress then lives on
	// CrossChain.DestinationChain, and the sender pays CrossChain.DepositAddress.
	CrossChain *CrossChainRoute

	Status                Status
	StatusReason          string
	TxHash                string
	Confirmations         int64
	RequiredConfirmations int64
	ExpiresAt             time.Time
	SubmittedAt           *time.Time
	CompletedAt           *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// PayTo is the address the sender's transaction must pay on the origin chain.
func (p Payment) PayTo() string {
	if p.CrossChain != nil {
		return p.CrossChain.DepositAddress
	}
	return p.RecipientAddress
}

// CrossChainRoute is the locked provider quote. Fields above DepositConfirmedAt never change.
type CrossChainRoute struct {
	Provider           string
	SwapType           routing.SwapType
	RequestedAmount    *big.Int
	OriginAssetID      string
	DestinationAssetID string
	DestinationChain   chains.Chain
	DestinationAsset   Asset
	DepositAddress     string
	RefundTo           string
	AmountIn           *big.Int
	AmountOutExpected  *big.Int
	MinAmountOut       *big.Int
	AmountInUSD        string
	AmountOutUSD       string
	SlippageBps        int
	TimeEstimate       time.Duration
	QuoteDeadline      time.Time
	QuoteSignature     string
	CorrelationID      string
	QuoteRaw           json.RawMessage

	DepositConfirmedAt *time.Time
	ProviderStatus     string
	ProviderUpdatedAt  *time.Time
	AmountOutActual    *big.Int
	RefundedAmount     *big.Int
	RefundReason       string
	DestinationTxs     []routing.TxRef
}

// RouteProgress is what the provider reports while executing a route.
type RouteProgress struct {
	ProviderStatus    string
	ProviderUpdatedAt *time.Time
	AmountOutActual   *big.Int
	RefundedAmount    *big.Int
	RefundReason      string
	DestinationTxs    []routing.TxRef
}

type Event struct {
	Type       string
	FromStatus Status
	ToStatus   Status
	Detail     map[string]any
	CreatedAt  time.Time
}

type PrepareInput struct {
	Caller         *uuid.UUID
	Recipient      string
	Chain          string
	Asset          string
	Amount         string
	SenderAddress  string
	IdempotencyKey string
	// ResolutionVersion, when set, must equal the recipient's current version
	// so the sender signs exactly what they were shown.
	ResolutionVersion *int64

	// DestinationChain/DestinationAsset request a cross-chain conversion. When
	// empty, a route is used only if the recipient has no wallet on Chain and
	// has opted into a settlement preference.
	DestinationChain string
	DestinationAsset string
	// AmountType is "input" (default: Amount is what the sender pays) or
	// "output" (Amount is what the recipient receives; cross-chain only).
	AmountType  string
	SlippageBps *int
	// HoldForApproval creates the intent in CREATED. It receives no signing
	// instructions until Release is called (used for agent proposals).
	HoldForApproval bool
}

// FeeInput describes a payment to MAX itself (e.g. a rename fee). The
// recipient is a fixed treasury address, not a resolved MAX ID.
type FeeInput struct {
	Caller         *uuid.UUID
	Purpose        Purpose
	Chain          chains.Chain
	Asset          string // canonical; "" = native
	Amount         *big.Int
	SenderAddress  string
	IdempotencyKey string
	Treasury       string // canonical address on Chain
	Label          string
	// Hold creates the intent in CREATED so the caller can release it only
	// once whatever the fee pays for has been secured.
	Hold bool
}

type SubmitInput struct {
	Caller    *uuid.UUID
	PaymentID uuid.UUID
	TxHash    string
}

// Transition describes a state change plus the fields it updates.
type Transition struct {
	From          Status
	To            Status
	Reason        string
	Event         string
	Detail        map[string]any
	TxHash        *string
	Confirmations *int64
	SubmittedAt   *time.Time
	CompletedAt   *time.Time
	// NextCheckIn schedules the next on-chain check relative to the DB clock;
	// nil means the payment is no longer polled.
	NextCheckIn *time.Duration
}

// Observation is what the chain says about a submitted transaction.
type Observation struct {
	// Found is false while the transaction is not yet visible on-chain.
	Found bool
	// Reverted is true when the transaction executed but failed.
	Reverted bool
	// Mismatch explains why a landed transaction does not satisfy the intent.
	Mismatch      string
	Confirmations int64
	Final         bool
}
