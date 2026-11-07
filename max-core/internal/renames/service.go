package renames

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
)

type service struct {
	repo     Repository
	payments Payments
	cfg      Config
	log      *zap.Logger
	now      func() time.Time
}

var _ Service = (*service)(nil)

func NewService(repo Repository, pay Payments, cfg Config, log *zap.Logger) Service {
	if cfg.Cooldown == 0 {
		cfg.Cooldown = Cooldown
	}
	return &service{repo: repo, payments: pay, cfg: cfg, log: log, now: time.Now}
}

func (s *service) Eligibility(ctx context.Context, owner, maxID uuid.UUID) (Eligibility, error) {
	m, err := s.repo.GetOwnedMaxID(ctx, maxID, owner)
	if err != nil {
		return Eligibility{}, err
	}
	e := Eligibility{MaxID: m, AvailableAt: m.RenameAvailableAt, Cooldown: s.cfg.Cooldown, Fees: s.cfg.Fees}
	if e.Pending, err = s.repo.GetPending(ctx, maxID); err != nil {
		return Eligibility{}, err
	}
	switch {
	case m.IsDefault:
		e.Reason = ErrDefaultNotRenameable.Message
	case !s.cfg.Enabled():
		e.Reason = "renames are not enabled"
	case m.Status != "active":
		e.Reason = "MAX ID is not active"
	case e.Pending != nil:
		e.Reason = "a rename is already awaiting payment"
	case s.now().Before(m.RenameAvailableAt):
		e.Reason = "cooldown: renamed within the last 72 hours"
	default:
		e.Eligible = true
	}
	return e, nil
}

func (s *service) Request(ctx context.Context, owner, maxID uuid.UUID, in RequestInput) (RequestView, error) {
	if !s.cfg.Enabled() {
		return RequestView{}, ErrRenamesDisabled
	}
	fee, err := s.feeOption(in.FeeChain, in.FeeAsset)
	if err != nil {
		return RequestView{}, err
	}
	newName, err := maxids.Normalize(in.NewName)
	if err != nil {
		return RequestView{}, maxids.ErrInvalidName.WithMessage(err.Error())
	}
	if maxids.IsDefaultShape(newName) {
		return RequestView{}, maxids.ErrInvalidName.WithMessage("this name uses a reserved format")
	}
	if reserved, err := s.repo.GetReserved(ctx, newName); err != nil {
		return RequestView{}, err
	} else if reserved != nil {
		if reserved.Policy == maxids.PolicyBlocked {
			return RequestView{}, maxids.ErrNameReserved
		}
		return RequestView{}, maxids.ErrNameNeedsVerification
	}

	m, err := s.repo.GetOwnedMaxID(ctx, maxID, owner)
	if err != nil {
		return RequestView{}, err
	}
	if m.IsDefault {
		return RequestView{}, ErrDefaultNotRenameable
	}
	if m.Name == newName {
		return RequestView{}, ErrSameName
	}
	if m.Status != "active" {
		return RequestView{}, maxids.ErrMaxIDNotActive
	}
	// Checked again under lock in Reserve; failing early here avoids creating
	// a fee intent that could never be used.
	if s.now().Before(m.RenameAvailableAt) {
		return RequestView{}, ErrCooldown.WithMessage("next rename available at " + m.RenameAvailableAt.UTC().Format(time.RFC3339))
	}

	prep, err := s.payments.PrepareFee(ctx, payments.FeeInput{
		Caller: &owner, Purpose: payments.PurposeRenameFee, Chain: fee.Chain, Asset: fee.Asset, Amount: fee.Amount,
		SenderAddress: in.SenderAddress, IdempotencyKey: in.IdempotencyKey,
		Treasury: s.cfg.Treasury[fee.Chain.Namespace],
		Label:    "MAX rename fee: " + maxids.Handle(m.Name) + " → " + maxids.Handle(newName),
		Hold:     true,
	})
	if err != nil {
		return RequestView{}, err
	}
	if prep.Replayed {
		return s.replay(ctx, maxID, prep.Payment)
	}

	req, err := s.repo.Reserve(ctx, ReserveParams{
		MaxID: maxID, Owner: owner, OldName: m.Name, NewName: newName, FeePaymentID: prep.Payment.ID,
	})
	if err != nil {
		// The held fee intent never received instructions; fail it so it can't be paid.
		if _, rerr := s.payments.Reject(ctx, prep.Payment.ID, "rename could not be reserved"); rerr != nil {
			s.log.Warn("reject orphaned rename fee", zap.String("payment_id", prep.Payment.ID.String()), zap.Error(rerr))
		}
		return RequestView{}, err
	}

	p, err := s.payments.Release(ctx, prep.Payment.ID, "rename reserved")
	if err != nil {
		if _, cerr := s.repo.Close(ctx, req.ID, StatusFailed, "fee intent could not be released"); cerr != nil {
			s.log.Warn("close rename after release failure", zap.Error(cerr))
		}
		return RequestView{}, err
	}
	ins := s.payments.Instructions(p)
	return RequestView{Request: req, Payment: p, Instructions: &ins, Warnings: prep.Warnings}, nil
}

// replay returns the pending request whose fee intent was just replayed.
func (s *service) replay(ctx context.Context, maxID uuid.UUID, p payments.Payment) (RequestView, error) {
	pending, err := s.repo.GetPending(ctx, maxID)
	if err != nil {
		return RequestView{}, err
	}
	if pending == nil || pending.FeePaymentID != p.ID {
		return RequestView{}, payments.ErrIdempotencyConflict
	}
	return s.view(*pending, p), nil
}

func (s *service) feeOption(chainKey, asset string) (FeeOption, error) {
	chain, ok := chains.Lookup(chainKey)
	if !ok {
		return FeeOption{}, ErrFeeOption
	}
	want := ""
	if a := strings.TrimSpace(asset); a != "" && !strings.EqualFold(a, "native") {
		norm, err := chains.NormalizeAddress(chain.Namespace, a)
		if err != nil {
			return FeeOption{}, ErrFeeOption
		}
		want = norm
	}
	for _, f := range s.cfg.Fees {
		if f.Chain.Key == chain.Key && f.Asset == want {
			return f, nil
		}
	}
	return FeeOption{}, ErrFeeOption
}

func (s *service) Get(ctx context.Context, owner, maxID, requestID uuid.UUID) (RequestView, error) {
	req, err := s.repo.Get(ctx, requestID, owner)
	if err != nil || req.MaxID != maxID {
		return RequestView{}, ErrRequestNotFound
	}
	if req.Status == StatusAwaitingPayment {
		if req, err = s.reconcile(ctx, req); err != nil {
			return RequestView{}, err
		}
	}
	p, _, err := s.payments.Get(ctx, &owner, req.FeePaymentID)
	if err != nil {
		return RequestView{}, err
	}
	return s.view(req, p), nil
}

func (s *service) History(ctx context.Context, owner, maxID uuid.UUID) ([]HistoryEntry, error) {
	if _, err := s.repo.GetOwnedMaxID(ctx, maxID, owner); err != nil {
		return nil, err
	}
	return s.repo.History(ctx, maxID)
}

func (s *service) Sweep(ctx context.Context) (int, error) {
	pending, err := s.repo.ListPending(ctx, 100)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, req := range pending {
		out, err := s.reconcile(ctx, req)
		if err != nil {
			s.log.Warn("reconcile rename", zap.String("request_id", req.ID.String()), zap.Error(err))
			continue
		}
		if out.Status != StatusAwaitingPayment {
			changed++
		}
	}
	return changed, nil
}

// reconcile drives a pending request from its fee payment's state. Only a
// verified, completed fee finalizes the rename; a fee that can no longer
// complete releases the reservation without consuming the cooldown.
func (s *service) reconcile(ctx context.Context, req Request) (Request, error) {
	p, _, err := s.payments.Get(ctx, &req.OwnerID, req.FeePaymentID)
	if err != nil {
		return req, err
	}
	switch p.Status {
	case payments.StatusCompleted:
		return s.repo.Finalize(ctx, req.ID, s.cfg.Cooldown, s.cfg.Quarantine, s.cfg.HoldPeriod)
	case payments.StatusFailed, payments.StatusRefunded:
		return s.close(ctx, req, StatusFailed, "fee payment failed: "+p.StatusReason)
	case payments.StatusExpired:
		if s.now().After(p.ExpiresAt.Add(s.cfg.LateSubmitGrace)) {
			return s.close(ctx, req, StatusExpired, "fee was not paid in time")
		}
	}
	return req, nil
}

func (s *service) close(ctx context.Context, req Request, st Status, reason string) (Request, error) {
	out, err := s.repo.Close(ctx, req.ID, st, reason)
	if errors.Is(err, ErrRequestNotFound) {
		return req, nil // closed concurrently
	}
	return out, err
}

func (s *service) view(req Request, p payments.Payment) RequestView {
	v := RequestView{Request: req, Payment: p}
	if req.Status == StatusAwaitingPayment && p.Status == payments.StatusAwaitingSenderSignature {
		ins := s.payments.Instructions(p)
		v.Instructions = &ins
	}
	return v
}
