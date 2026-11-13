package payments

import (
	"math/big"
	"testing"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/routing"
)

func TestPaymentResponse_DestinationExplorerLink(t *testing.T) {
	sol, _ := chains.Lookup("solana")
	p := Payment{
		Chain: sol, Asset: Asset{Kind: AssetNative, Decimals: 9}, Amount: big.NewInt(1),
		CrossChain: &CrossChainRoute{
			DestinationChain: sol, DestinationAsset: Asset{Kind: AssetToken, Decimals: 6},
			AmountIn: big.NewInt(1), AmountOutExpected: big.NewInt(1), MinAmountOut: big.NewInt(1),
			DestinationTxs: []routing.TxRef{{Hash: "abc"}, {Hash: "def", ExplorerURL: "https://given/def"}},
		},
	}
	txs := toPaymentResponse(p).CrossChain.DestinationTxs
	if txs[0].ExplorerURL != sol.TxURL("abc") {
		t.Errorf("built link = %q", txs[0].ExplorerURL)
	}
	if txs[1].ExplorerURL != "https://given/def" {
		t.Errorf("provider link replaced: %q", txs[1].ExplorerURL)
	}
}
