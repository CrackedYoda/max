package pricing

import (
	"testing"

	"github.com/max/max-core/internal/chains"
)

func TestCoinGeckoIDs_CoverLinkableNativeCoins(t *testing.T) {
	for _, c := range chains.Supported {
		if c.Testnet || !chains.IsLinkable(c.Namespace) {
			continue
		}
		if _, ok := coinGeckoIDs[c.NativeCoin]; !ok {
			t.Errorf("%s: no CoinGecko id for %s", c.Key, c.NativeCoin)
		}
	}
}
