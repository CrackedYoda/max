package settle

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/max/max-core/internal/chains"
)

type fakeEVM struct {
	native *big.Int
	tokens map[string]*big.Int
	err    error
}

func (f *fakeEVM) Balance(context.Context, string) (*big.Int, error) { return f.native, f.err }
func (f *fakeEVM) ERC20Balance(_ context.Context, token, _ string) (*big.Int, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tokens[token], nil
}

func TestEVMBalances(t *testing.T) {
	ch, _ := chains.Lookup("base")
	usdc := chains.VerifiedTokensFor("base")[0].Address
	r := NewEVMBalances(map[string]EVMRPC{"base": &fakeEVM{native: big.NewInt(7), tokens: map[string]*big.Int{usdc: big.NewInt(5_000_000)}}})
	m, err := r.Balances(context.Background(), ch, "0xabc")
	if err != nil {
		t.Fatal(err)
	}
	if m["native"].Int64() != 7 || m[usdc].Int64() != 5_000_000 || len(m) != 2 {
		t.Fatalf("got %v", m)
	}
}

func TestEVMBalances_Errors(t *testing.T) {
	ch, _ := chains.Lookup("base")
	if _, err := NewEVMBalances(nil).Balances(context.Background(), ch, "0xabc"); err == nil {
		t.Fatal("missing client must error")
	}
	boom := errors.New("boom")
	if _, err := NewEVMBalances(map[string]EVMRPC{"base": &fakeEVM{err: boom}}).Balances(context.Background(), ch, "0xabc"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
