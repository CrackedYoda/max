package routing

import (
	"errors"
	"fmt"
	"math/big"
	"time"
)

// ValidateQuote checks a provider quote against what was asked for before any
// sender is shown it. The provider is trusted to execute, not to define terms.
func ValidateQuote(req QuoteRequest, q Quote, now time.Time, mustLiveUntil time.Time) error {
	if q.DepositAddress == "" {
		return errors.New("quote has no deposit address")
	}
	if q.Signature == "" {
		return errors.New("quote is unsigned")
	}
	for name, v := range map[string]*big.Int{"amountIn": q.AmountIn, "amountOut": q.AmountOut, "minAmountOut": q.MinAmountOut} {
		if v == nil || v.Sign() <= 0 {
			return fmt.Errorf("quote %s must be positive", name)
		}
	}
	if q.MinAmountOut.Cmp(q.AmountOut) > 0 {
		return errors.New("quote minimum output exceeds expected output")
	}

	switch req.SwapType {
	case ExactInput:
		if q.AmountIn.Cmp(req.Amount) != 0 {
			return fmt.Errorf("quote input %s differs from requested %s", q.AmountIn, req.Amount)
		}
		// minAmountOut must respect the slippage the sender agreed to.
		floor := applyBps(q.AmountOut, 10_000-req.SlippageBps)
		if q.MinAmountOut.Cmp(floor) < 0 {
			return fmt.Errorf("quote minimum output %s is below the %d bps slippage floor %s", q.MinAmountOut, req.SlippageBps, floor)
		}
	case ExactOutput:
		if q.AmountOut.Cmp(req.Amount) < 0 {
			return fmt.Errorf("quote output %s is below requested %s", q.AmountOut, req.Amount)
		}
		if q.MinAmountOut.Cmp(req.Amount) < 0 {
			return fmt.Errorf("quote minimum output %s is below the exact amount requested %s", q.MinAmountOut, req.Amount)
		}
	default:
		return fmt.Errorf("unknown swap type %q", req.SwapType)
	}

	if q.Deadline.IsZero() || !q.Deadline.After(mustLiveUntil) || !q.Deadline.After(now) {
		return errors.New("quote deadline is too soon")
	}
	return nil
}

func applyBps(v *big.Int, bps int) *big.Int {
	out := new(big.Int).Mul(v, big.NewInt(int64(bps)))
	return out.Quo(out, big.NewInt(10_000))
}
