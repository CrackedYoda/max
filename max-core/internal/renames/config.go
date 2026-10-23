package renames

import (
	"fmt"
	"strings"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/payments"
)

// ParseFees parses "chain:asset:amount" entries separated by commas, e.g.
// "solana:native:0.05,base:0x8335…2913:5". Only native coins and verified
// tokens are accepted so decimals can never be spoofed.
func ParseFees(spec string) ([]FeeOption, error) {
	var out []FeeOption
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		if len(parts) != 3 {
			return nil, fmt.Errorf("rename fee %q must be chain:asset:amount", entry)
		}
		chain, ok := chains.Lookup(parts[0])
		if !ok {
			return nil, fmt.Errorf("rename fee: unknown chain %q", parts[0])
		}
		opt := FeeOption{Chain: chain, Symbol: chain.NativeCoin, Decimals: chain.NativeDecimals}
		if a := strings.TrimSpace(parts[1]); !strings.EqualFold(a, "native") {
			norm, err := chains.NormalizeAddress(chain.Namespace, a)
			if err != nil {
				return nil, fmt.Errorf("rename fee: invalid asset %q", a)
			}
			tok, ok := chains.LookupVerifiedToken(chain.Key, norm)
			if !ok {
				return nil, fmt.Errorf("rename fee: %s on %s is not a verified token", a, chain.Key)
			}
			opt.Asset, opt.Symbol, opt.Decimals = norm, tok.Symbol, tok.Decimals
		}
		amt, err := payments.ParseAmount(parts[2], opt.Decimals)
		if err != nil {
			return nil, fmt.Errorf("rename fee %q: %w", entry, err)
		}
		opt.Amount = amt
		out = append(out, opt)
	}
	return out, nil
}

// ParseTreasury validates treasury addresses and checks every fee option has one.
func ParseTreasury(evm, solana string, fees []FeeOption) (map[chains.Namespace]string, error) {
	t := map[chains.Namespace]string{}
	for ns, raw := range map[chains.Namespace]string{chains.NamespaceEVM: evm, chains.NamespaceSolana: solana} {
		if raw == "" {
			continue
		}
		norm, err := chains.NormalizeAddress(ns, raw)
		if err != nil {
			return nil, fmt.Errorf("rename treasury for %s: %w", ns, err)
		}
		t[ns] = norm
	}
	for _, f := range fees {
		if _, ok := t[f.Chain.Namespace]; !ok {
			return nil, fmt.Errorf("rename fee on %s has no treasury address for %s", f.Chain.Key, f.Chain.Namespace)
		}
	}
	return t, nil
}
