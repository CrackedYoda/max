package renames

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

// Cooldown is the product rule: an ID can be renamed at most once per 72 hours.
const Cooldown = 72 * time.Hour

type Status string

const (
	StatusAwaitingPayment Status = "awaiting_payment"
	StatusCompleted       Status = "completed"
	StatusFailed          Status = "failed"
	StatusExpired         Status = "expired"
)

type Request struct {
	ID            uuid.UUID
	MaxID         uuid.UUID
	OwnerID       uuid.UUID
	OldName       string
	NewName       string
	Status        Status
	FeePaymentID  uuid.UUID
	FailureReason string
	CreatedAt     time.Time
	CompletedAt   *time.Time
}

// FeeOption is one accepted way to pay the rename fee.
type FeeOption struct {
	Chain    chains.Chain
	Asset    string // canonical; "" = native
	Symbol   string
	Decimals int
	Amount   *big.Int
}

type Config struct {
	Cooldown   time.Duration
	Quarantine time.Duration
	Fees       []FeeOption
	// Treasury is MAX's fee-receiving address per namespace (canonical form).
	Treasury map[chains.Namespace]string
	// LateSubmitGrace mirrors payments: an expired fee intent can still be
	// submitted this long after expiry, so its reservation must outlive it.
	LateSubmitGrace time.Duration
}

func (c Config) Enabled() bool { return len(c.Fees) > 0 }

type MaxIDInfo struct {
	ID                uuid.UUID
	Name              string
	Status            string
	RenameAvailableAt time.Time
}

type Eligibility struct {
	MaxID       MaxIDInfo
	Eligible    bool
	Reason      string
	AvailableAt time.Time
	Cooldown    time.Duration
	Fees        []FeeOption
	Pending     *Request
}

type HistoryEntry struct {
	OldName      string
	NewName      string
	FeePaymentID *uuid.UUID
	ChangedAt    time.Time
}

type RequestInput struct {
	NewName        string
	FeeChain       string
	FeeAsset       string
	SenderAddress  string
	IdempotencyKey string
}
