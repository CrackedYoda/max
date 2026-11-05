package market

import (
	"fmt"
	"math/big"
)

const bpsDenominator = 10000

// SplitFee returns fee = floor(price * feeBPS / 10000) and seller = price - fee.
// It panics on a nil or negative price or a feeBPS outside 0..10000: those are
// programmer or config errors (config is validated at startup), never user input.
// Inputs are not mutated.
func SplitFee(price *big.Int, feeBPS int) (fee, seller *big.Int) {
	if price == nil || price.Sign() < 0 {
		panic("market.SplitFee: price must be a non-negative integer")
	}
	if feeBPS < 0 || feeBPS > bpsDenominator {
		panic(fmt.Sprintf("market.SplitFee: feeBPS %d outside 0..%d", feeBPS, bpsDenominator))
	}
	fee = new(big.Int).Mul(price, big.NewInt(int64(feeBPS)))
	fee.Quo(fee, big.NewInt(bpsDenominator)) // non-negative, so Quo == floor
	seller = new(big.Int).Sub(price, fee)
	return fee, seller
}
