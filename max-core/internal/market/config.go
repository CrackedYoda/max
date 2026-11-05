package market

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/config"
	"github.com/max/max-core/internal/payments"
)

// Asset is one accepted settlement asset (spec §10 MARKET_ASSETS).
type Asset struct {
	Chain    chains.Chain
	Address  string // canonical mint / contract address
	Symbol   string
	Decimals int
}

// Config is the validated marketplace configuration.
type Config struct {
	Enabled, TradingPaused bool
	Assets                 []Asset
	FeeBPS                 int
	HoldPeriod             time.Duration
	BuyLock                time.Duration
	OfferPayWindow         time.Duration
	// ReviewThreshold is a decimal amount; sales priced above it need admin
	// review. Empty disables review.
	ReviewThreshold string
	// MinPrice is MARKET_MIN_OFFER: the minimum offer and listing price, as a
	// decimal amount of the asset.
	MinPrice string
	Treasury map[chains.Namespace]string
}

// NewConfig validates the environment's market settings. Assets are always
// parsed so a typo fails at boot; treasury and RPC presence are required only
// when the market is enabled (spec §10).
func NewConfig(mc config.MarketConfig, rpcURLs map[string]string) (Config, error) {
	c := Config{
		Enabled: mc.Enabled, TradingPaused: mc.TradingPaused, FeeBPS: mc.FeeBPS,
		HoldPeriod: mc.HoldPeriod, BuyLock: mc.BuyLock, OfferPayWindow: mc.OfferPayWindow,
		ReviewThreshold: strings.TrimSpace(mc.ReviewThreshold), MinPrice: strings.TrimSpace(mc.MinOffer),
		Treasury: map[chains.Namespace]string{},
	}
	if c.FeeBPS < 0 || c.FeeBPS > bpsDenominator {
		return Config{}, fmt.Errorf("MARKET_FEE_BPS %d outside 0..%d", c.FeeBPS, bpsDenominator)
	}
	assets, err := ParseAssets(mc.Assets)
	if err != nil {
		return Config{}, err
	}
	c.Assets = assets
	for _, a := range assets {
		if _, err := payments.ParseAmount(c.MinPrice, a.Decimals); err != nil {
			return Config{}, fmt.Errorf("MARKET_MIN_OFFER %q for %s: %w", c.MinPrice, a.Chain.Key, err)
		}
		if c.ReviewThreshold != "" {
			if _, err := payments.ParseAmount(c.ReviewThreshold, a.Decimals); err != nil {
				return Config{}, fmt.Errorf("MARKET_REVIEW_THRESHOLD %q for %s: %w", c.ReviewThreshold, a.Chain.Key, err)
			}
		}
	}
	for ns, raw := range map[chains.Namespace]string{chains.NamespaceSolana: mc.TreasurySolana, chains.NamespaceEVM: mc.TreasuryEVM} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		addr, err := chains.NormalizeAddress(ns, raw)
		if err != nil {
			return Config{}, fmt.Errorf("market treasury for %s: %w", ns, err)
		}
		c.Treasury[ns] = addr
	}
	if !c.Enabled {
		return c, nil
	}
	if len(c.Assets) == 0 {
		return Config{}, errors.New("MARKET_ENABLED=true requires MARKET_ASSETS")
	}
	if c.BuyLock <= 0 || c.OfferPayWindow <= 0 || c.HoldPeriod < 0 {
		return Config{}, errors.New("MARKET_BUY_LOCK and MARKET_OFFER_PAY_WINDOW must be positive and MARKET_HOLD_PERIOD not negative")
	}
	for _, a := range c.Assets {
		if c.Treasury[a.Chain.Namespace] == "" {
			return Config{}, fmt.Errorf("MARKET_ASSETS has %s but no treasury is set for namespace %s (TREASURY_%s)",
				a.Chain.Key, a.Chain.Namespace, strings.ToUpper(string(a.Chain.Namespace)))
		}
		if rpcURLs[a.Chain.Key] == "" {
			return Config{}, fmt.Errorf("MARKET_ASSETS has %s but no RPC URL is configured for it", a.Chain.Key)
		}
	}
	return c, nil
}

// ParseAssets parses "chain:asset,..." into verified settlement assets.
// Settlement v1 is Solana only.
func ParseAssets(s string) ([]Asset, error) {
	var out []Asset
	seen := map[string]bool{}
	for _, raw := range strings.Split(s, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		key, addr, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, fmt.Errorf("MARKET_ASSETS entry %q must be chain:asset", raw)
		}
		chain, ok := chains.Lookup(key)
		if !ok {
			return nil, fmt.Errorf("MARKET_ASSETS entry %q: unknown chain", raw)
		}
		if chain.Namespace != chains.NamespaceSolana {
			return nil, fmt.Errorf("MARKET_ASSETS entry %q: settlement supports Solana chains only", raw)
		}
		canon, err := chains.NormalizeAddress(chain.Namespace, addr)
		if err != nil {
			return nil, fmt.Errorf("MARKET_ASSETS entry %q: %w", raw, err)
		}
		tok, ok := chains.LookupVerifiedToken(chain.Key, canon)
		if !ok {
			return nil, fmt.Errorf("MARKET_ASSETS entry %q is not a verified token", raw)
		}
		if seen[chain.Key+":"+canon] {
			continue
		}
		seen[chain.Key+":"+canon] = true
		out = append(out, Asset{Chain: chain, Address: canon, Symbol: tok.Symbol, Decimals: tok.Decimals})
	}
	return out, nil
}

// asset returns the accepted asset for a chain key and address, if any.
func (c Config) asset(chainKey, addr string) (Asset, bool) {
	chain, ok := chains.Lookup(chainKey)
	if !ok {
		return Asset{}, false
	}
	canon, err := chains.NormalizeAddress(chain.Namespace, addr)
	if err != nil {
		return Asset{}, false
	}
	for _, a := range c.Assets {
		if a.Chain.Key == chain.Key && a.Address == canon {
			return a, true
		}
	}
	return Asset{}, false
}

// minBase is the minimum price in the asset's base units.
func (c Config) minBase(a Asset) *big.Int {
	v, err := payments.ParseAmount(c.MinPrice, a.Decimals)
	if err != nil { // validated in NewConfig; a zero-value Config means "no minimum"
		return big.NewInt(1)
	}
	return v
}

// thresholdBase is the review threshold in base units, or nil when off.
func (c Config) thresholdBase(a Asset) *big.Int {
	if c.ReviewThreshold == "" {
		return nil
	}
	v, err := payments.ParseAmount(c.ReviewThreshold, a.Decimals)
	if err != nil {
		return nil
	}
	return v
}
