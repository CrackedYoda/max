package payments

import (
	"errors"
	"math/big"
	"strings"
)

var (
	errAmountFormat    = errors.New("amount must be a positive decimal number, e.g. 0.25")
	errAmountPrecision = errors.New("amount has more decimal places than the asset supports")
	errAmountZero      = errors.New("amount must be greater than zero")
	errAmountTooLarge  = errors.New("amount is too large")
)

var maxBaseUnits = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// ParseAmount converts a human decimal string into integer base units without
// ever passing through floating point.
func ParseAmount(s string, decimals int) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.Count(s, ".") > 1 {
		return nil, errAmountFormat
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	for _, part := range []string{whole, frac} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return nil, errAmountFormat
			}
		}
	}
	frac = strings.TrimRight(frac, "0")
	if len(frac) > decimals {
		return nil, errAmountPrecision
	}
	digits := whole + frac + strings.Repeat("0", decimals-len(frac))
	v, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, errAmountFormat
	}
	if v.Sign() <= 0 {
		return nil, errAmountZero
	}
	if v.Cmp(maxBaseUnits) > 0 {
		return nil, errAmountTooLarge
	}
	return v, nil
}

// FormatAmount renders base units as a trimmed decimal string.
func FormatAmount(v *big.Int, decimals int) string {
	s := v.String()
	if decimals == 0 {
		return s
	}
	if len(s) <= decimals {
		s = strings.Repeat("0", decimals-len(s)+1) + s
	}
	whole, frac := s[:len(s)-decimals], strings.TrimRight(s[len(s)-decimals:], "0")
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}
