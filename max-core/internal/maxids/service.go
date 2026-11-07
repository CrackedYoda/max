package maxids

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

const MaxIDsPerAccount = 10

type service struct {
	repo Repository
	gate BalanceGate
}

var _ Service = (*service)(nil)

// NewService builds the MAX ID service. A nil gate disables the registration
// balance check.
func NewService(repo Repository, gate BalanceGate) Service {
	return &service{repo: repo, gate: gate}
}

func (s *service) CheckAvailability(ctx context.Context, name string) (Availability, error) {
	canonical, err := Normalize(name)
	if err != nil {
		return Availability{Available: false, Reason: "invalid", Detail: err.Error()}, nil
	}
	out := Availability{CanonicalName: canonical, Handle: Handle(canonical)}

	reserved, err := s.repo.GetReserved(ctx, canonical)
	if err != nil {
		return Availability{}, err
	}
	if reserved != nil {
		out.Reason, out.ClaimPolicy = "reserved", reserved.Policy
		if reserved.Policy != PolicyBlocked {
			out.XHandle = reserved.XHandle
		}
		return out, nil
	}

	status, err := s.repo.NameStatus(ctx, canonical)
	if err != nil {
		return Availability{}, err
	}
	if status != "" {
		out.Reason = status
		return out, nil
	}
	out.Available = true
	return out, nil
}

func (s *service) Claim(ctx context.Context, caller uuid.UUID, in ClaimInput) (MaxID, error) {
	if in.Type == "" {
		in.Type = TypeHuman
	}
	if !in.Type.Valid() {
		return MaxID{}, ErrInvalidType
	}
	canonical, err := Normalize(in.Name)
	if err != nil {
		return MaxID{}, ErrInvalidName.WithMessage(err.Error())
	}
	// Default shapes belong to the system; nobody can claim one.
	if IsDefaultShape(canonical) {
		return MaxID{}, ErrInvalidName.WithMessage("this name uses a reserved format")
	}

	reserved, err := s.repo.GetReserved(ctx, canonical)
	if err != nil {
		return MaxID{}, err
	}
	if reserved != nil {
		if reserved.Policy == PolicyBlocked {
			return MaxID{}, ErrNameReserved
		}
		return MaxID{}, ErrNameNeedsVerification
	}

	count, err := s.repo.CountByOwner(ctx, caller)
	if err != nil {
		return MaxID{}, err
	}
	if count >= MaxIDsPerAccount {
		return MaxID{}, ErrIDLimitReached
	}

	if err := s.checkGate(ctx, caller); err != nil {
		return MaxID{}, err
	}

	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = canonical
	}
	return s.repo.Create(ctx, caller, canonical, in.Type, display)
}

// checkGate runs the registration balance gate and audits the outcome. The
// audit is best-effort: it must not turn a decided claim into an error.
func (s *service) checkGate(ctx context.Context, caller uuid.UUID) error {
	if s.gate == nil {
		return nil
	}
	err := s.gate.Check(ctx, caller)
	if e, ok := s.gate.(interface{ Enabled() bool }); ok && !e.Enabled() {
		return err
	}
	var ae *shared.AppError
	switch {
	case err == nil:
		_ = s.repo.RecordGateCheck(ctx, caller, minUSD(s.gate), nil, true)
	case errors.As(err, &ae) && ae.Code == ErrInsufficientRegistrationBalance.Code:
		required, _ := ae.Details["requiredUsd"].(float64)
		counted, _ := ae.Details["countedUsd"].(float64)
		_ = s.repo.RecordGateCheck(ctx, caller, required, &counted, false)
	}
	return err
}

func minUSD(g BalanceGate) float64 {
	if m, ok := g.(interface{ MinUSD() float64 }); ok {
		return m.MinUSD()
	}
	return 0
}

func (s *service) ListMine(ctx context.Context, caller uuid.UUID) ([]MaxID, error) {
	return s.repo.ListByOwner(ctx, caller)
}

func (s *service) Get(ctx context.Context, caller, id uuid.UUID) (MaxID, error) {
	return s.repo.GetForOwner(ctx, id, caller)
}

func (s *service) UpdateProfile(ctx context.Context, caller, id uuid.UUID, in ProfileInput) (MaxID, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Description = strings.TrimSpace(in.Description)
	in.AvatarURL = strings.TrimSpace(in.AvatarURL)
	if in.AvatarURL != "" {
		u, err := url.Parse(in.AvatarURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return MaxID{}, shared.ErrValidation.WithMessage("avatarUrl must be an https URL")
		}
	}
	return s.repo.UpdateProfile(ctx, id, caller, in)
}

func (s *service) SetDestination(ctx context.Context, caller, id uuid.UUID, namespace string, walletID uuid.UUID) (MaxID, error) {
	ns, ok := chains.ParseNamespace(namespace)
	if !ok || !chains.IsLinkable(ns) {
		return MaxID{}, ErrUnsupportedNamespace
	}
	walletNS, err := s.repo.GetOwnedWalletNamespace(ctx, walletID, caller)
	if err != nil {
		return MaxID{}, err
	}
	if walletNS != ns {
		return MaxID{}, ErrNamespaceMismatch
	}
	return s.repo.SetDestination(ctx, id, caller, ns, walletID)
}

func (s *service) RemoveDestination(ctx context.Context, caller, id uuid.UUID, namespace string) (MaxID, error) {
	ns, ok := chains.ParseNamespace(namespace)
	if !ok {
		return MaxID{}, ErrUnsupportedNamespace
	}
	return s.repo.RemoveDestination(ctx, id, caller, ns)
}

func (s *service) SetSettlement(ctx context.Context, caller, id uuid.UUID, chainKey, asset string, enabled bool) (MaxID, error) {
	chain, ok := chains.Lookup(chainKey)
	if !ok || chain.Testnet {
		return MaxID{}, ErrSettlementChain
	}
	canonicalAsset := ""
	if a := strings.TrimSpace(asset); a != "" && !strings.EqualFold(a, "native") {
		norm, err := chains.NormalizeAddress(chain.Namespace, a)
		if err != nil {
			return MaxID{}, ErrSettlementAsset
		}
		canonicalAsset = norm
	}

	m, err := s.repo.GetForOwner(ctx, id, caller)
	if err != nil {
		return MaxID{}, err
	}
	hasWallet := false
	for _, d := range m.Destinations {
		if d.Namespace == chain.Namespace {
			hasWallet = true
		}
	}
	if !hasWallet {
		return MaxID{}, ErrSettlementNoWallet
	}
	return s.repo.SetSettlement(ctx, id, caller, Settlement{Chain: chain.Key, Asset: canonicalAsset, Enabled: enabled})
}

func (s *service) RemoveSettlement(ctx context.Context, caller, id uuid.UUID) (MaxID, error) {
	return s.repo.RemoveSettlement(ctx, id, caller)
}
