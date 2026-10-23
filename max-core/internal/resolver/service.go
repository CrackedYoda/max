package resolver

import (
	"context"
	"strings"
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
)

type service struct {
	repo Repository
	now  func() time.Time
}

var _ Service = (*service)(nil)

func NewService(repo Repository) Service {
	return &service{repo: repo, now: time.Now}
}

func (s *service) Profile(ctx context.Context, name string) (Profile, []ChainReach, error) {
	canonical, err := maxids.Normalize(name)
	if err != nil {
		return Profile{}, nil, ErrMaxIDNotFound
	}
	p, err := s.repo.GetProfile(ctx, canonical)
	if err != nil {
		return Profile{}, nil, err
	}
	reach := make([]ChainReach, 0, len(chains.Supported))
	for _, c := range chains.Supported {
		_, ok := p.Destinations[c.Namespace]
		reach = append(reach, ChainReach{Chain: c, Reachable: ok})
	}
	return p, reach, nil
}

func (s *service) Resolve(ctx context.Context, name, chainKey, assetParam string) (Resolution, error) {
	chain, ok := chains.Lookup(chainKey)
	if !ok {
		return Resolution{}, ErrUnsupportedChain
	}
	asset, err := parseAsset(chain.Namespace, assetParam)
	if err != nil {
		return Resolution{}, err
	}
	canonical, err := maxids.Normalize(name)
	if err != nil {
		return Resolution{}, ErrMaxIDNotFound
	}
	p, err := s.repo.GetProfile(ctx, canonical)
	if err != nil {
		return Resolution{}, err
	}

	// Never fall back to another namespace or guess an address: a payment to
	// the wrong chain is unrecoverable.
	addr, ok := p.Destinations[chain.Namespace]
	if !ok {
		return Resolution{}, ErrNoDestination
	}

	res := Resolution{
		MaxID: p.MaxID, Handle: maxids.Handle(p.CanonicalName), Type: p.Type,
		DisplayName: p.DisplayName, Chain: chain, Asset: asset,
		Address:           chains.DisplayAddress(chain.Namespace, addr),
		ResolutionVersion: p.ResolutionVersion, ResolvedAt: s.now().UTC(),
	}
	if chain.Namespace == chains.NamespaceSolana && asset.Kind == AssetToken {
		res.Notes = append(res.Notes, "SPL tokens are sent to the recipient's associated token account; the sender's transaction creates it if missing and pays its rent.")
	}
	if chain.Testnet {
		res.Notes = append(res.Notes, "Testnet: assets on this chain have no real value.")
	}
	return res, nil
}

func parseAsset(ns chains.Namespace, raw string) (Asset, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "native") {
		return Asset{Kind: AssetNative}, nil
	}
	addr, err := chains.NormalizeAddress(ns, raw)
	if err != nil {
		return Asset{}, ErrInvalidAsset
	}
	return Asset{Kind: AssetToken, Address: chains.DisplayAddress(ns, addr)}, nil
}

func (s *service) Settlement(ctx context.Context, name string) (*Settlement, error) {
	canonical, err := maxids.Normalize(name)
	if err != nil {
		return nil, ErrMaxIDNotFound
	}
	p, err := s.repo.GetProfile(ctx, canonical)
	if err != nil {
		return nil, err
	}
	return p.Settlement, nil
}
