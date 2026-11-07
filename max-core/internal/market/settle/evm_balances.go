package settle

import (
	"context"
	"fmt"
	"math/big"

	"github.com/max/max-core/internal/chains"
)

// BalanceReader reads what an owner holds on one chain: "native" plus each
// verified token by canonical address.
type BalanceReader interface {
	Balances(ctx context.Context, chain chains.Chain, owner string) (map[string]*big.Int, error)
}

// EVMRPC is the subset of evmrpc.Client the balance reader uses.
type EVMRPC interface {
	Balance(ctx context.Context, addr string) (*big.Int, error)
	ERC20Balance(ctx context.Context, token, owner string) (*big.Int, error)
}

type evmBalances struct {
	clients map[string]EVMRPC
}

// NewEVMBalances returns a read-only balance reader for EVM chains, keyed by
// chain key. It never builds or submits transactions.
func NewEVMBalances(clients map[string]EVMRPC) BalanceReader {
	return &evmBalances{clients: clients}
}

func (e *evmBalances) Balances(ctx context.Context, chain chains.Chain, owner string) (map[string]*big.Int, error) {
	c, ok := e.clients[chain.Key]
	if !ok || c == nil {
		return nil, fmt.Errorf("settle: no evm rpc client for chain %q", chain.Key)
	}
	native, err := c.Balance(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := map[string]*big.Int{nativeKey: native}
	for _, t := range chains.VerifiedTokensFor(chain.Key) {
		v, err := c.ERC20Balance(ctx, t.Address, owner)
		if err != nil {
			return nil, err
		}
		out[t.Address] = v
	}
	return out, nil
}
