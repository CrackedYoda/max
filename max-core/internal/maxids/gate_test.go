package maxids

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

const (
	usdcMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	bonkMint = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
)

type fakeWallets struct {
	refs []WalletRef
	err  error
}

func (f fakeWallets) ListActiveWallets(context.Context, uuid.UUID) ([]WalletRef, error) {
	return f.refs, f.err
}

type fakeBalances struct {
	byChain map[string]map[string]*big.Int
	err     error
}

func (f fakeBalances) Balances(_ context.Context, c chains.Chain, _ string) (map[string]*big.Int, error) {
	return f.byChain[c.Key], f.err
}

type fakeFeed struct {
	prices map[string]float64
	err    error
	calls  int
	panics bool
}

func (f *fakeFeed) USD(_ context.Context, syms []string) (map[string]float64, error) {
	if f.panics {
		panic("price feed must not be called")
	}
	f.calls++
	return f.prices, f.err
}

func solChain() []chains.Chain {
	c, _ := chains.Lookup("solana")
	return []chains.Chain{c}
}

func solGate(min float64, held map[string]*big.Int, feed *fakeFeed) BalanceGate {
	return NewBalanceGate(min,
		fakeWallets{refs: []WalletRef{{Namespace: chains.NamespaceSolana, Address: "W1"}}},
		map[chains.Namespace]BalanceReader{chains.NamespaceSolana: fakeBalances{byChain: map[string]map[string]*big.Int{"solana": held}}},
		feed, solChain())
}

func usdc(n int64) *big.Int { return big.NewInt(n * 1_000_000) }

func counted(t *testing.T, err error) float64 {
	t.Helper()
	var ae *shared.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("not an AppError: %v", err)
	}
	if ae.Details["requiredUsd"] != float64(100) {
		t.Errorf("requiredUsd = %v", ae.Details["requiredUsd"])
	}
	return ae.Details["countedUsd"].(float64)
}

func TestGate_DisabledAtZero(t *testing.T) {
	g := NewBalanceGate(0, fakeWallets{err: errors.New("wallets must not be read")}, nil, &fakeFeed{panics: true}, solChain())
	if err := g.Check(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
}

func TestGate_BelowMinimum(t *testing.T) {
	held := map[string]*big.Int{usdcMint: usdc(60), "native": big.NewInt(200_000_000)}
	err := solGate(100, held, &fakeFeed{prices: map[string]float64{"SOL": 150}}).Check(context.Background(), uuid.New())
	if !errors.Is(err, ErrInsufficientRegistrationBalance) {
		t.Fatalf("err = %v", err)
	}
	if got := counted(t, err); got != 90 {
		t.Errorf("countedUsd = %v, want 90", got)
	}
}

func TestGate_AtOrAboveMinimum(t *testing.T) {
	held := map[string]*big.Int{usdcMint: usdc(70), "native": big.NewInt(200_000_000)}
	if err := solGate(100, held, &fakeFeed{prices: map[string]float64{"SOL": 150}}).Check(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
}

func TestGate_IgnoresBONK(t *testing.T) {
	held := map[string]*big.Int{bonkMint: new(big.Int).Exp(big.NewInt(10), big.NewInt(12), nil)}
	err := solGate(100, held, &fakeFeed{panics: true}).Check(context.Background(), uuid.New())
	if !errors.Is(err, ErrInsufficientRegistrationBalance) {
		t.Fatalf("err = %v", err)
	}
	if got := counted(t, err); got != 0 {
		t.Errorf("countedUsd = %v", got)
	}
}

func TestGate_FeedDown(t *testing.T) {
	held := map[string]*big.Int{"native": big.NewInt(200_000_000)}
	err := solGate(100, held, &fakeFeed{err: errors.New("down")}).Check(context.Background(), uuid.New())
	if !errors.Is(err, ErrPriceUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestGate_StablesOnlySkipsFeed(t *testing.T) {
	held := map[string]*big.Int{usdcMint: usdc(150), "native": big.NewInt(200_000_000)}
	if err := solGate(100, held, &fakeFeed{panics: true}).Check(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
}

func TestGate_NoNativeHeldSkipsFeed(t *testing.T) {
	held := map[string]*big.Int{usdcMint: usdc(5), "native": big.NewInt(0)}
	err := solGate(100, held, &fakeFeed{panics: true}).Check(context.Background(), uuid.New())
	if !errors.Is(err, ErrInsufficientRegistrationBalance) {
		t.Fatalf("err = %v", err)
	}
}

func TestGate_MissingPriceFailsClosed(t *testing.T) {
	held := map[string]*big.Int{"native": big.NewInt(200_000_000)}
	for _, prices := range []map[string]float64{{}, {"SOL": 0}} {
		err := solGate(100, held, &fakeFeed{prices: prices}).Check(context.Background(), uuid.New())
		if !errors.Is(err, ErrPriceUnavailable) {
			t.Fatalf("prices %v: err = %v", prices, err)
		}
	}
}

func TestGate_RPCErrorFailsClosed(t *testing.T) {
	g := NewBalanceGate(100,
		fakeWallets{refs: []WalletRef{{Namespace: chains.NamespaceSolana, Address: "W1"}}},
		map[chains.Namespace]BalanceReader{chains.NamespaceSolana: fakeBalances{err: errors.New("rpc down")}},
		&fakeFeed{panics: true}, solChain())
	if err := g.Check(context.Background(), uuid.New()); !errors.Is(err, ErrPriceUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestGate_WalletListErrorPropagates(t *testing.T) {
	boom := errors.New("db down")
	g := NewBalanceGate(100, fakeWallets{err: boom}, nil, &fakeFeed{panics: true}, solChain())
	if err := g.Check(context.Background(), uuid.New()); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestGate_SumsAcrossNamespacesAndChains(t *testing.T) {
	base, _ := chains.Lookup("base")
	poly, _ := chains.Lookup("polygon")
	baseUSDC := chains.VerifiedTokensFor("base")[0].Address
	g := NewBalanceGate(100,
		fakeWallets{refs: []WalletRef{
			{Namespace: chains.NamespaceSolana, Address: "W1"},
			{Namespace: chains.NamespaceEVM, Address: "0xabc"},
		}},
		map[chains.Namespace]BalanceReader{
			chains.NamespaceSolana: fakeBalances{byChain: map[string]map[string]*big.Int{"solana": {usdcMint: usdc(30)}}},
			chains.NamespaceEVM: fakeBalances{byChain: map[string]map[string]*big.Int{
				"base":    {baseUSDC: usdc(30)},
				"polygon": {"native": new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18))}, // 100 POL
			}},
		},
		&fakeFeed{prices: map[string]float64{"POL": 0.4}}, append(solChain(), base, poly))
	if err := g.Check(context.Background(), uuid.New()); err != nil { // 30+30+40
		t.Fatal(err)
	}
}

func TestGate_IgnoresUnconfiguredChain(t *testing.T) {
	// Only solana is configured; an EVM wallet's balances are never read.
	g := NewBalanceGate(100,
		fakeWallets{refs: []WalletRef{{Namespace: chains.NamespaceEVM, Address: "0xabc"}}},
		map[chains.Namespace]BalanceReader{chains.NamespaceEVM: fakeBalances{err: errors.New("must not be read")}},
		&fakeFeed{panics: true}, solChain())
	if err := g.Check(context.Background(), uuid.New()); !errors.Is(err, ErrInsufficientRegistrationBalance) {
		t.Fatalf("err = %v", err)
	}
}
