package agents

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/auth"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/shared"
	"github.com/max/max-core/internal/wallets"
)

// SpendFunc reports committed spend in base units over the rolling 24h window.
type SpendFunc func(chain, asset string) *big.Int

type Repository interface {
	CreateAgent(ctx context.Context, admin uuid.UUID, name, description string) (Agent, Policy, error)
	GetForAdministrator(ctx context.Context, agentID, admin uuid.UUID) (Agent, error)
	Get(ctx context.Context, agentID uuid.UUID) (Agent, error)
	ListForAdministrator(ctx context.Context, admin uuid.UUID) ([]Agent, error)
	Update(ctx context.Context, a Agent, actor uuid.UUID) (Agent, error)

	GetPolicy(ctx context.Context, agentID uuid.UUID) (Policy, error)
	UpdatePolicy(ctx context.Context, agentID uuid.UUID, p Policy, actor uuid.UUID) (Policy, error)

	CreateCredential(ctx context.Context, c Credential, secretHash []byte) (Credential, error)
	// RotateCredential atomically issues next and revokes old (old.replaced_by = next).
	RotateCredential(ctx context.Context, agentID, oldID uuid.UUID, next Credential, secretHash []byte) (Credential, error)
	GetCredentialByPrefix(ctx context.Context, prefix string) (Credential, []byte, Status, error)
	ListCredentials(ctx context.Context, agentID uuid.UUID) ([]Credential, error)
	RevokeCredential(ctx context.Context, agentID, credID, actor uuid.UUID) error
	TouchCredential(ctx context.Context, id uuid.UUID) error

	// DecideUnderLock serializes per agent: it reads rolling spend, lets decide
	// build the authorization, and stores it, all in one transaction.
	DecideUnderLock(ctx context.Context, agentID uuid.UUID, decide func(SpendFunc) TransferAuthorization) (TransferAuthorization, error)
	GetAuthorization(ctx context.Context, paymentID uuid.UUID) (TransferAuthorization, error)
	// DecideApproval moves a pending authorization to approved/denied (compare-and-swap).
	DecideApproval(ctx context.Context, paymentID uuid.UUID, d DecisionOutcome, by uuid.UUID) (TransferAuthorization, error)
	ListAuthorizations(ctx context.Context, agentID uuid.UUID, decision *DecisionOutcome, limit int32) ([]TransferAuthorization, error)
}

type Payments interface {
	Prepare(ctx context.Context, in payments.PrepareInput) (payments.Prepared, error)
	Get(ctx context.Context, caller *uuid.UUID, id uuid.UUID) (payments.Payment, []payments.Event, error)
	Release(ctx context.Context, id uuid.UUID, reason string) (payments.Payment, error)
	Reject(ctx context.Context, id uuid.UUID, reason string) (payments.Payment, error)
	Instructions(p payments.Payment) payments.Instructions
}

type Wallets interface {
	List(ctx context.Context, caller uuid.UUID) ([]wallets.Wallet, error)
}

type IDs interface {
	Claim(ctx context.Context, caller uuid.UUID, in maxids.ClaimInput) (maxids.MaxID, error)
	ListMine(ctx context.Context, caller uuid.UUID) ([]maxids.MaxID, error)
}

type WalletProofs interface {
	CreateChallenge(ctx context.Context, in auth.ChallengeInput) (auth.Challenge, error)
	Verify(ctx context.Context, in auth.VerifyInput) (auth.VerifyResult, error)
}

type LimitInput struct {
	Chain          string
	Asset          string
	MaxPerTransfer string // human decimal, e.g. "0.2"
	MaxPer24h      string
}

type PolicyInput struct {
	Limits            []LimitInput
	AllowedRecipients []string
	AllowAnyRecipient bool
}

type AgentDetail struct {
	Agent       Agent
	Policy      Policy
	Credentials []Credential
	Wallets     []wallets.Wallet
	IDs         []maxids.MaxID
}

type ProposeInput struct {
	Recipient        string
	Chain            string
	Asset            string
	Amount           string
	SenderAddress    string
	IdempotencyKey   string
	DestinationChain string
	DestinationAsset string
	AmountType       string
	// Instruction is the natural-language request, stored for audit only.
	Instruction string
}

// TransferView is an agent transfer with its policy decision and, once
// approved and signable, MAX's signed authorization for the agent's signer.
type TransferView struct {
	Payment       payments.Payment
	Authorization TransferAuthorization
	Instructions  *payments.Instructions
	Signed        *SignedAuthorization
	Warnings      []string
	Replayed      bool
}

type Service interface {
	// Administrator (human session) operations.
	CreateAgent(ctx context.Context, admin uuid.UUID, name, description string) (AgentDetail, error)
	ListAgents(ctx context.Context, admin uuid.UUID) ([]Agent, error)
	GetAgent(ctx context.Context, admin, agentID uuid.UUID) (AgentDetail, error)
	UpdateAgent(ctx context.Context, admin, agentID uuid.UUID, name, description string, status Status) (Agent, error)
	SetPolicy(ctx context.Context, admin, agentID uuid.UUID, in PolicyInput) (Policy, error)
	IssueCredential(ctx context.Context, admin, agentID uuid.UUID, name string, scopes []string, ttl time.Duration) (IssuedCredential, error)
	RotateCredential(ctx context.Context, admin, agentID, credID uuid.UUID) (IssuedCredential, error)
	RevokeCredential(ctx context.Context, admin, agentID, credID uuid.UUID) error
	ClaimID(ctx context.Context, admin, agentID uuid.UUID, name, displayName string) (maxids.MaxID, error)
	WalletChallenge(ctx context.Context, admin, agentID uuid.UUID, namespace, address string) (auth.Challenge, error)
	WalletVerify(ctx context.Context, admin, agentID uuid.UUID, nonce, signature string) (auth.Wallet, error)
	ListTransfers(ctx context.Context, admin, agentID uuid.UUID, decision *DecisionOutcome) ([]TransferView, error)
	Approve(ctx context.Context, admin, paymentID uuid.UUID) (TransferView, error)
	Deny(ctx context.Context, admin, paymentID uuid.UUID, reason string) (TransferView, error)

	// Agent (API key) operations.
	Authenticate(ctx context.Context, apiKey string) (shared.Principal, error)
	Propose(ctx context.Context, p shared.Principal, in ProposeInput) (TransferView, error)
	GetTransfer(ctx context.Context, p shared.Principal, paymentID uuid.UUID) (TransferView, error)
	SignedAuthorization(ctx context.Context, p shared.Principal, paymentID uuid.UUID) (SignedAuthorization, error)
	PublicKey() (keyID string, pub []byte, ok bool)
}
