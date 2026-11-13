package maxids

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/pricing"
)

// WalletRef is a linked wallet the gate reads balances for.
type WalletRef struct {
	ID        uuid.UUID
	Namespace chains.Namespace
	Address   string
}

// WalletLister returns an account's active linked wallets.
type WalletLister interface {
	ListActiveWallets(ctx context.Context, owner uuid.UUID) ([]WalletRef, error)
}

// BalanceReader reads what an address holds on one chain: "native" plus each
// verified token keyed by its canonical address. It is satisfied by the
// settlement adapters and the EVM reader, without maxids importing them.
type BalanceReader interface {
	Balances(ctx context.Context, chain chains.Chain, owner string) (map[string]*big.Int, error)
}

// BalanceGate decides whether an account holds enough value to claim a MAX ID.
type BalanceGate interface {
	Check(ctx context.Context, owner uuid.UUID) error
}

type balanceGate struct {
	minUSD   float64
	wallets  WalletLister
	balances map[chains.Namespace]BalanceReader
	feed     pricing.Feed
	chains   []chains.Chain
}

// NewBalanceGate counts USDC and USDT at $1 and native ETH/SOL/POL at feed
// prices across the owner's active wallets, on cfgChains only (callers pass
// chains with an RPC URL, minus testnets in production). Nothing else counts.
// A minUSD of 0 disables the gate: it touches no wallet, RPC or feed.
func NewBalanceGate(minUSD float64, wallets WalletLister, balances map[chains.Namespace]BalanceReader, feed pricing.Feed, cfgChains []chains.Chain) BalanceGate {
	// Testnet coins are free, so a testnet balance never counts.
	var mainnets []chains.Chain
	for _, c := range cfgChains {
		if !c.Testnet {
			mainnets = append(mainnets, c)
		}
	}
	return &balanceGate{minUSD: minUSD, wallets: wallets, balances: balances, feed: feed, chains: mainnets}
}

func (g *balanceGate) Enabled() bool   { return g.minUSD > 0 }
func (g *balanceGate) MinUSD() float64 { return g.minUSD }

func (g *balanceGate) Check(ctx context.Context, owner uuid.UUID) error {
	if !g.Enabled() {
		return nil
	}
	refs, err := g.wallets.ListActiveWallets(ctx, owner)
	if err != nil {
		return err
	}

	stables := 0.0
	nativeUnits := map[string]float64{}
	var readErr error
	for _, w := range refs {
		reader := g.balances[w.Namespace]
		if reader == nil {
			continue
		}
		for _, ch := range g.chains {
			if ch.Namespace != w.Namespace {
				continue
			}
			held, err := reader.Balances(ctx, ch, w.Address)
			if err != nil {
				readErr = err
				continue
			}
			for _, t := range chains.VerifiedTokensFor(ch.Key) {
				if t.Symbol != "USDC" && t.Symbol != "USDT" {
					continue
				}
				stables += units(held[t.Address], t.Decimals)
			}
			if amt := held["native"]; amt != nil && amt.Sign() > 0 {
				nativeUnits[ch.NativeCoin] += units(amt, ch.NativeDecimals)
			}
		}
	}

	// Stablecoins alone can clear the bar; a partial RPC outage is then moot.
	if round(stables) >= g.minUSD {
		return nil
	}
	if readErr != nil {
		return ErrPriceUnavailable.WithError(fmt.Errorf("balance read failed: %w", readErr))
	}

	total := stables
	if len(nativeUnits) > 0 {
		symbols := make([]string, 0, len(nativeUnits))
		for s := range nativeUnits {
			symbols = append(symbols, s)
		}
		sort.Strings(symbols)
		prices, err := g.feed.USD(ctx, symbols)
		if err != nil {
			return ErrPriceUnavailable.WithError(err)
		}
		for _, s := range symbols {
			p := prices[s]
			if !(p > 0) || math.IsInf(p, 0) {
				return ErrPriceUnavailable.WithError(fmt.Errorf("no usable price for %s", s))
			}
			total += nativeUnits[s] * p
		}
	}
	total = round(total)
	if total >= g.minUSD {
		return nil
	}
	return ErrInsufficientRegistrationBalance.WithDetails(map[string]any{"requiredUsd": g.minUSD, "countedUsd": total})
}

// units converts a raw base-unit amount to whole units.
func units(raw *big.Int, decimals int) float64 {
	if raw == nil || raw.Sign() <= 0 {
		return 0
	}
	q := new(big.Float).Quo(new(big.Float).SetInt(raw), new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	f, _ := q.Float64()
	return f
}

// round trims float noise (0.2 * 150 = 30.000000000000004) to micro-dollars.
func round(v float64) float64 { return math.Round(v*1e6) / 1e6 }
