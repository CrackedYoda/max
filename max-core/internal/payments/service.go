package payments

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mr-tron/base58"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/resolver"
	"github.com/max/max-core/internal/routing"
	"github.com/max/max-core/internal/shared"
)

type Config struct {
	IntentTTL time.Duration
	// LateSubmitGrace is how long after expiry a signed transaction may still be attached.
	LateSubmitGrace time.Duration
	// SolanaNotFoundTimeout: after this, an unseen Solana tx can no longer land (blockhash expiry).
	SolanaNotFoundTimeout time.Duration
	// EVMNotFoundTimeout: after this, an unseen EVM tx is escalated to manual review,
	// since it could still be mined later with the same nonce.
	EVMNotFoundTimeout time.Duration

	// Cross-chain routing.
	RoutingEnabled     bool
	DefaultSlippageBps int
	MaxSlippageBps     int
	// QuoteDeadline is when the provider starts refunding an unexecuted swap.
	// It must comfortably exceed IntentTTL so a deposit sent near expiry still settles.
	QuoteDeadline time.Duration
	// MaxCrossChainUSD caps a single route's input value; 0 disables the cap.
	MaxCrossChainUSD float64
	// CrossChainStallTimeout escalates a route to manual review if the provider
	// has not settled or refunded this long after the quote deadline.
	CrossChainStallTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		IntentTTL:             15 * time.Minute,
		LateSubmitGrace:       time.Hour,
		SolanaNotFoundTimeout: 3 * time.Minute,
		EVMNotFoundTimeout:    30 * time.Minute,

		RoutingEnabled:         true,
		DefaultSlippageBps:     100,
		MaxSlippageBps:         300,
		QuoteDeadline:          45 * time.Minute,
		MaxCrossChainUSD:       1000,
		CrossChainStallTimeout: 2 * time.Hour,
	}
}

type Prepared struct {
	Payment      Payment
	Instructions Instructions
	Warnings     []string
	// Replayed is true when an earlier intent with the same idempotency key was returned.
	Replayed bool
}

type service struct {
	repo     Repository
	resolver Resolver
	gateway  Gateway
	provider routing.Provider
	cfg      Config
	log      *zap.Logger
	now      func() time.Time
}

var _ Service = (*service)(nil)

// NewService wires payments. provider may be nil, which disables cross-chain routes.
func NewService(repo Repository, resolver Resolver, gateway Gateway, provider routing.Provider, cfg Config, log *zap.Logger) Service {
	return &service{repo: repo, resolver: resolver, gateway: gateway, provider: provider, cfg: cfg, log: log, now: time.Now}
}

func (s *service) Prepare(ctx context.Context, in PrepareInput) (Prepared, error) {
	chain, ok := chains.Lookup(in.Chain)
	if !ok {
		return Prepared{}, shared.NewAppError("UNSUPPORTED_CHAIN", 400, "chain is not supported")
	}
	if chain.ReceiveOnly {
		return Prepared{}, receiveOnly(chain)
	}
	if !s.gateway.Configured(chain.Key) {
		return Prepared{}, ErrChainNotConfigured
	}
	sender, err := chains.NormalizeAddress(chain.Namespace, in.SenderAddress)
	if err != nil {
		return Prepared{}, ErrInvalidSender.WithMessage("sender address: " + err.Error())
	}
	if in.AmountType != "" && in.AmountType != "input" && in.AmountType != "output" {
		return Prepared{}, ErrInvalidAmount.WithMessage("amountType must be input or output")
	}

	if existing, err := s.repo.GetByIdempotencyKey(ctx, sender, in.IdempotencyKey); err == nil {
		return s.replay(existing, in, chain)
	} else if !errors.Is(err, ErrPaymentNotFound) {
		return Prepared{}, err
	}

	// Route selection: an explicit destination that differs from the source
	// asks for conversion; otherwise pay directly, falling back to the
	// recipient's settlement preference only when they have no wallet here.
	if dest := strings.TrimSpace(in.DestinationChain); dest != "" {
		destChain, ok := chains.Lookup(dest)
		if !ok {
			return Prepared{}, shared.NewAppError("UNSUPPORTED_CHAIN", 400, "destination chain is not supported")
		}
		if destChain.Key != chain.Key || !sameAssetParam(chain.Namespace, in.Asset, in.DestinationAsset) {
			return s.prepareCrossChain(ctx, in, chain, sender, destChain, in.DestinationAsset)
		}
	}

	res, err := s.resolver.Resolve(ctx, in.Recipient, chain.Key, in.Asset)
	if errors.Is(err, resolver.ErrNoDestination) {
		pref, perr := s.resolver.Settlement(ctx, in.Recipient)
		if perr != nil || pref == nil {
			return Prepared{}, err
		}
		destAsset := "native"
		if pref.Asset.Kind == resolver.AssetToken {
			destAsset = pref.Asset.Address
		}
		return s.prepareCrossChain(ctx, in, chain, sender, pref.Chain, destAsset)
	}
	if err != nil {
		return Prepared{}, err
	}
	if in.AmountType == "output" {
		return Prepared{}, ErrInvalidAmount.WithMessage("amountType output applies only to cross-chain payments")
	}
	return s.prepareDirect(ctx, in, chain, sender, res)
}

func (s *service) prepareDirect(ctx context.Context, in PrepareInput, chain chains.Chain, sender string, res resolver.Resolution) (Prepared, error) {
	if in.ResolutionVersion != nil && *in.ResolutionVersion != res.ResolutionVersion {
		return Prepared{}, ErrResolutionStale
	}
	recipient, err := chains.NormalizeAddress(chain.Namespace, res.Address)
	if err != nil {
		return Prepared{}, shared.ErrInternal.WithError(err)
	}
	if recipient == sender {
		return Prepared{}, ErrSelfPayment
	}

	asset, warnings, err := s.assetFor(ctx, chain, string(res.Asset.Kind), res.Asset.Address)
	if err != nil {
		return Prepared{}, err
	}
	amount, err := ParseAmount(in.Amount, asset.Decimals)
	if err != nil {
		return Prepared{}, ErrInvalidAmount.WithMessage(err.Error())
	}

	p, err := s.repo.Create(ctx, Payment{
		SenderAccountID: in.Caller, SenderAddress: sender, IdempotencyKey: in.IdempotencyKey,
		MaxID: res.MaxID, RecipientHandle: res.Handle, RecipientAddress: recipient,
		ResolutionVersion: res.ResolutionVersion, Chain: chain, Asset: asset, Amount: amount,
		Route: RouteDirect, Status: holdOr(in, StatusAwaitingSenderSignature),
		RequiredConfirmations: requiredConfirmations(chain), ExpiresAt: s.now().Add(s.cfg.IntentTTL),
	})
	if errors.Is(err, errDuplicateIdempotency) {
		return s.replayAfterRace(ctx, in, chain, sender)
	}
	if err != nil {
		return Prepared{}, err
	}
	return s.finishPrepared(ctx, p, warnings), nil
}

// finishPrepared builds signing instructions and the warnings shared by both routes.
func (s *service) finishPrepared(ctx context.Context, p Payment, warnings []string) Prepared {
	ins := buildInstructions(p)
	if ins.Solana != nil && ins.Solana.Kind == SolanaSPLTransferChecked {
		exists, err := s.gateway.AccountExists(ctx, p.Chain, ins.Solana.DestinationTokenAccount)
		if err == nil {
			ins.Solana.DestinationTokenAccountExists = &exists
			if !exists {
				warnings = append(warnings, "The destination token account does not exist yet. Your transaction must create it (createAssociatedTokenAccountIdempotent) and you pay its rent (~0.002 SOL).")
			}
		}
	}
	if p.Chain.Testnet {
		warnings = append(warnings, "Testnet: assets on this chain have no real value.")
	}
	return Prepared{Payment: p, Instructions: ins, Warnings: warnings}
}

func (s *service) replayAfterRace(ctx context.Context, in PrepareInput, chain chains.Chain, sender string) (Prepared, error) {
	existing, err := s.repo.GetByIdempotencyKey(ctx, sender, in.IdempotencyKey)
	if err != nil {
		return Prepared{}, err
	}
	return s.replay(existing, in, chain)
}

func requiredConfirmations(c chains.Chain) int64 {
	if c.Namespace == chains.NamespaceSolana {
		return 1 // "finalized" commitment
	}
	return c.Confirmations
}

// replay returns an existing intent only if the retried request asks for the
// same payment; reusing a key for different terms is rejected.
func (s *service) replay(existing Payment, in PrepareInput, chain chains.Chain) (Prepared, error) {
	// Replay follows the same ownership rule as GET; anyone else sees a
	// conflict, never the other account's intent.
	if !sameCaller(existing.SenderAccountID, in.Caller) {
		return Prepared{}, ErrIdempotencyConflict
	}
	same := purposeOr(existing.Purpose) == PurposeTransfer && existing.Chain.Key == chain.Key
	if canonical, err := maxids.Normalize(in.Recipient); err != nil || maxids.Handle(canonical) != existing.RecipientHandle {
		same = false
	}
	wantAsset, ok := canonicalAssetParam(chain.Namespace, in.Asset)
	if !ok || wantAsset != existing.Asset.Address {
		same = false
	}

	amountDecimals, compareTo := existing.Asset.Decimals, existing.Amount
	if r := existing.CrossChain; r != nil {
		if in.DestinationChain != "" && in.DestinationChain != r.DestinationChain.Key {
			same = false
		}
		if (in.AmountType == "output") != (r.SwapType == routing.ExactOutput) {
			same = false
		}
		if in.DestinationChain != "" && !sameAssetParam(r.DestinationChain.Namespace, in.DestinationAsset, r.DestinationAsset.Address) {
			same = false
		}
		slippage := s.cfg.DefaultSlippageBps
		if in.SlippageBps != nil {
			slippage = *in.SlippageBps
		}
		if slippage != r.SlippageBps {
			same = false
		}
		compareTo = r.RequestedAmount
		if r.SwapType == routing.ExactOutput {
			amountDecimals = r.DestinationAsset.Decimals
		}
	} else if in.DestinationChain != "" && in.DestinationChain != chain.Key {
		same = false
	}
	if amt, err := ParseAmount(in.Amount, amountDecimals); err != nil || amt.Cmp(compareTo) != 0 {
		same = false
	}
	if !same {
		return Prepared{}, ErrIdempotencyConflict
	}
	if in.ResolutionVersion != nil && *in.ResolutionVersion != existing.ResolutionVersion {
		return Prepared{}, ErrResolutionStale
	}
	return Prepared{Payment: existing, Instructions: buildInstructions(existing), Replayed: true}, nil
}

// sameCaller reports whether a retry comes from the principal that created
// the intent: the same account, or anonymous for an anonymous intent.
func sameCaller(owner, caller *uuid.UUID) bool {
	if owner == nil || caller == nil {
		return owner == nil && caller == nil
	}
	return *owner == *caller
}

// canonicalAssetParam normalizes "native"/"" or a contract/mint address.
func canonicalAssetParam(ns chains.Namespace, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "native") {
		return "", true
	}
	norm, err := chains.NormalizeAddress(ns, raw)
	return norm, err == nil
}

func sameAssetParam(ns chains.Namespace, a, b string) bool {
	ca, okA := canonicalAssetParam(ns, a)
	cb, okB := canonicalAssetParam(ns, b)
	return okA && okB && ca == cb
}

func (s *service) assetFor(ctx context.Context, chain chains.Chain, kind, displayAddr string) (Asset, []string, error) {
	if kind == "native" {
		return Asset{Kind: AssetNative, Decimals: chain.NativeDecimals, Symbol: chain.NativeCoin, Verified: true}, nil, nil
	}
	addr, err := chains.NormalizeAddress(chain.Namespace, displayAddr)
	if err != nil {
		return Asset{}, nil, ErrAssetUnavailable
	}
	info, err := s.gateway.TokenInfo(ctx, chain, addr)
	if err != nil {
		return Asset{}, nil, ErrAssetUnavailable.WithError(err)
	}
	asset := Asset{Kind: AssetToken, Address: addr, Decimals: info.Decimals, TokenProgram: info.TokenProgram}

	var warnings []string
	if known, ok := chains.LookupVerifiedToken(chain.Key, addr); ok {
		if known.Decimals != info.Decimals {
			return Asset{}, nil, ErrAssetUnavailable.WithMessage("token metadata on-chain does not match MAX's verified record")
		}
		asset.Symbol, asset.Verified = known.Symbol, true
	} else {
		warnings = append(warnings, "This token is not on MAX's verified list. Confirm the contract/mint address with the recipient — names and symbols can be faked.")
	}
	return asset, warnings, nil
}

func (s *service) Submit(ctx context.Context, in SubmitInput) (Payment, error) {
	p, err := s.repo.Get(ctx, in.PaymentID)
	if err != nil {
		return Payment{}, err
	}
	if !canAccess(p, in.Caller) {
		return Payment{}, ErrPaymentNotFound
	}
	hash, err := normalizeTxHash(p.Chain.Namespace, in.TxHash)
	if err != nil {
		return Payment{}, err
	}

	if p.TxHash != "" {
		if p.TxHash == hash {
			return p, nil
		}
		return Payment{}, ErrTxHashMismatch
	}

	now := s.now()
	lateUntil := p.ExpiresAt.Add(s.cfg.LateSubmitGrace)
	if p.CrossChain != nil && p.CrossChain.QuoteDeadline.Before(lateUntil) {
		// After the quote deadline the provider refunds instead of swapping.
		lateUntil = p.CrossChain.QuoteDeadline
	}
	// The deadline is absolute: an intent the sweeper hasn't expired yet is
	// still past it.
	switch p.Status {
	case StatusAwaitingSenderSignature, StatusQuoted, StatusExpired:
	default:
		return Payment{}, ErrInvalidState
	}
	if !now.Before(lateUntil) {
		return Payment{}, ErrInvalidState
	}

	updated, err := s.repo.Apply(ctx, p.ID, Transition{
		From: p.Status, To: StatusSubmitted, Event: "submitted",
		Detail: map[string]any{"tx_hash": hash}, TxHash: &hash, SubmittedAt: &now, NextCheckIn: ptr(time.Duration(0)),
	})
	if errors.Is(err, ErrStaleTransition) {
		// A concurrent submit won; succeed only if it attached the same hash.
		if cur, gerr := s.repo.Get(ctx, p.ID); gerr == nil && cur.TxHash == hash {
			return cur, nil
		}
		return Payment{}, ErrTxHashMismatch
	}
	if err == nil && updated.CrossChain != nil && s.provider != nil {
		// Best-effort hint; settlement is tracked by polling regardless.
		hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if herr := s.provider.SubmitDeposit(hctx, updated.CrossChain.DepositAddress, hash); herr != nil {
			s.log.Warn("deposit hint to routing provider failed", zap.String("payment_id", updated.ID.String()), zap.Error(herr))
		}
	}
	return updated, err
}

func (s *service) Get(ctx context.Context, caller *uuid.UUID, id uuid.UUID) (Payment, []Event, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return Payment{}, nil, err
	}
	if !canAccess(p, caller) {
		return Payment{}, nil, ErrPaymentNotFound
	}
	events, err := s.repo.Events(ctx, id)
	if err != nil {
		return Payment{}, nil, err
	}
	return p, events, nil
}

func (s *service) List(ctx context.Context, caller uuid.UUID, direction string, limit int32) ([]Payment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if direction == "received" {
		return s.repo.ListReceived(ctx, caller, limit)
	}
	return s.repo.ListBySender(ctx, caller, limit)
}

func (s *service) PrepareFee(ctx context.Context, in FeeInput) (Prepared, error) {
	chain := in.Chain
	if !s.gateway.Configured(chain.Key) {
		return Prepared{}, ErrChainNotConfigured
	}
	sender, err := chains.NormalizeAddress(chain.Namespace, in.SenderAddress)
	if err != nil {
		return Prepared{}, ErrInvalidSender.WithMessage("sender address: " + err.Error())
	}
	if sender == in.Treasury {
		return Prepared{}, ErrSelfPayment
	}

	if existing, err := s.repo.GetByIdempotencyKey(ctx, sender, in.IdempotencyKey); err == nil {
		if !sameCaller(existing.SenderAccountID, in.Caller) || existing.Purpose != in.Purpose || existing.Chain.Key != chain.Key || existing.Asset.Address != in.Asset ||
			existing.Amount.Cmp(in.Amount) != 0 || existing.RecipientAddress != in.Treasury {
			return Prepared{}, ErrIdempotencyConflict
		}
		return Prepared{Payment: existing, Instructions: buildInstructions(existing), Replayed: true}, nil
	} else if !errors.Is(err, ErrPaymentNotFound) {
		return Prepared{}, err
	}

	kind := "native"
	if in.Asset != "" {
		kind = "token"
	}
	asset, warnings, err := s.assetFor(ctx, chain, kind, in.Asset)
	if err != nil {
		return Prepared{}, err
	}
	p, err := s.repo.Create(ctx, Payment{
		Purpose: in.Purpose, SenderAccountID: in.Caller, SenderAddress: sender, IdempotencyKey: in.IdempotencyKey,
		RecipientHandle: in.Label, RecipientAddress: in.Treasury, Chain: chain, Asset: asset, Amount: in.Amount,
		Route: RouteDirect, Status: holdOr(PrepareInput{HoldForApproval: in.Hold}, StatusAwaitingSenderSignature),
		RequiredConfirmations: requiredConfirmations(chain), ExpiresAt: s.now().Add(s.cfg.IntentTTL),
	})
	if errors.Is(err, errDuplicateIdempotency) {
		return Prepared{}, ErrIdempotencyConflict
	}
	if err != nil {
		return Prepared{}, err
	}
	return s.finishPrepared(ctx, p, warnings), nil
}

func holdOr(in PrepareInput, s Status) Status {
	if in.HoldForApproval {
		return StatusCreated
	}
	return s
}

func (s *service) Release(ctx context.Context, id uuid.UUID, reason string) (Payment, error) {
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return Payment{}, err
	}
	if p.Status != StatusCreated || !s.now().Before(p.ExpiresAt) {
		return Payment{}, ErrInvalidState.WithMessage("payment is no longer awaiting approval")
	}
	to := StatusAwaitingSenderSignature
	if p.CrossChain != nil {
		to = StatusQuoted
	}
	return s.repo.Apply(ctx, id, Transition{From: StatusCreated, To: to, Event: "released", Reason: reason})
}

func (s *service) Reject(ctx context.Context, id uuid.UUID, reason string) (Payment, error) {
	return s.repo.Apply(ctx, id, Transition{From: StatusCreated, To: StatusFailed, Event: "rejected", Reason: reason})
}

func (s *service) Instructions(p Payment) Instructions {
	return buildInstructions(p)
}

func (s *service) ExpireStale(ctx context.Context) (int, error) {
	ids, err := s.repo.ExpireAwaiting(ctx)
	return len(ids), err
}

func (s *service) Process(ctx context.Context, p Payment) error {
	if p.CrossChain != nil {
		return s.processCrossChain(ctx, p)
	}
	now := s.now()
	obs, err := s.gateway.Observe(ctx, p)
	if err != nil {
		s.log.Warn("payment observation failed", zap.String("payment_id", p.ID.String()), zap.Error(err))
		return s.repo.Reschedule(ctx, p.ID, p.Status, 15*time.Second, p.Confirmations)
	}

	next := pollInterval(p.Chain)
	switch {
	case !obs.Found:
		if p.Status == StatusConfirming {
			return s.apply(ctx, p, Transition{To: StatusSubmitted, Event: "reorged",
				Reason: "transaction is no longer visible on-chain (possible reorg); re-checking", NextCheckIn: &next})
		}
		if p.SubmittedAt != nil && now.Sub(*p.SubmittedAt) > s.notFoundTimeout(p.Chain) {
			if p.Chain.Namespace == chains.NamespaceSolana {
				return s.apply(ctx, p, Transition{To: StatusFailed, Event: "not_found",
					Reason: "transaction never landed on-chain; it expired before confirmation"})
			}
			return s.apply(ctx, p, Transition{To: StatusManualReview, Event: "not_found",
				Reason: "transaction not seen on-chain after timeout; it may still be pending in the mempool"})
		}
		return s.repo.Reschedule(ctx, p.ID, p.Status, next, 0)

	case obs.Reverted:
		return s.apply(ctx, p, Transition{To: StatusFailed, Event: "reverted", Reason: "transaction failed on-chain"})

	case obs.Mismatch != "":
		return s.apply(ctx, p, Transition{To: StatusFailed, Event: "mismatch",
			Reason: "transaction does not match the payment intent: " + obs.Mismatch,
			Detail: map[string]any{"mismatch": obs.Mismatch}})

	case obs.Final:
		return s.apply(ctx, p, Transition{To: StatusCompleted, Event: "completed",
			Confirmations: &obs.Confirmations, CompletedAt: &now,
			Detail: map[string]any{"confirmations": obs.Confirmations}})

	case p.Status == StatusSubmitted:
		return s.apply(ctx, p, Transition{To: StatusConfirming, Event: "confirming",
			Confirmations: &obs.Confirmations, NextCheckIn: &next,
			Detail: map[string]any{"confirmations": obs.Confirmations}})

	default:
		return s.repo.Reschedule(ctx, p.ID, p.Status, next, obs.Confirmations)
	}
}

func (s *service) apply(ctx context.Context, p Payment, t Transition) error {
	t.From = p.Status
	if !CanTransition(t.From, t.To) {
		return ErrInvalidState.WithMessage(string(t.From) + " -> " + string(t.To))
	}
	_, err := s.repo.Apply(ctx, p.ID, t)
	if errors.Is(err, ErrStaleTransition) {
		return nil // another worker already moved it
	}
	return err
}

func (s *service) notFoundTimeout(c chains.Chain) time.Duration {
	if c.Namespace == chains.NamespaceSolana {
		return s.cfg.SolanaNotFoundTimeout
	}
	return s.cfg.EVMNotFoundTimeout
}

func ptr[T any](v T) *T { return &v }

func pollInterval(c chains.Chain) time.Duration {
	if c.Namespace == chains.NamespaceSolana {
		return 2 * time.Second
	}
	return 4 * time.Second
}

// canAccess: intents created by a signed-in sender are visible only to that
// account; anonymous intents are reachable by their unguessable id.
func canAccess(p Payment, caller *uuid.UUID) bool {
	if p.SenderAccountID == nil {
		return true
	}
	return caller != nil && *caller == *p.SenderAccountID
}

func normalizeTxHash(ns chains.Namespace, h string) (string, error) {
	h = strings.TrimSpace(h)
	switch ns {
	case chains.NamespaceEVM:
		body := strings.TrimPrefix(strings.ToLower(h), "0x")
		if b, err := hex.DecodeString(body); err != nil || len(b) != 32 {
			return "", ErrInvalidTxHash.WithMessage("EVM transaction hash must be 0x followed by 64 hex characters")
		}
		return "0x" + body, nil
	case chains.NamespaceSolana:
		if b, err := base58.Decode(h); err != nil || len(b) != 64 {
			return "", ErrInvalidTxHash.WithMessage("Solana transaction signature must be base58-encoded 64 bytes")
		}
		return h, nil
	}
	return "", ErrInvalidTxHash
}

func receiveOnly(c chains.Chain) error {
	return ErrReceiveOnlyChain.WithMessage("MAX can't send on " + c.Name + " yet.")
}
