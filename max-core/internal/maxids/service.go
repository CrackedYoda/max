package maxids

import (
	"context"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

const MaxIDsPerAccount = 10

type service struct {
	repo Repository
}

var _ Service = (*service)(nil)

func NewService(repo Repository) Service {
	return &service{repo: repo}
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

	display := strings.TrimSpace(in.DisplayName)
	if display == "" {
		display = canonical
	}
	return s.repo.Create(ctx, caller, canonical, in.Type, display)
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
