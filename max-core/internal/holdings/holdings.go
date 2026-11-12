// Package holdings lists the tokens a wallet holds on one chain, with USD
// prices where known, so senders can pick what to send.
package holdings

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

const (
	KindNative = "native"
	KindToken  = "token"

	cacheTTL = 20 * time.Second
	// maxCacheEntries bounds memory for a public endpoint fed arbitrary addresses.
	maxCacheEntries = 10_000
	fetchTimeout    = 30 * time.Second
)

var (
	ErrUnsupportedChain = shared.NewAppError("UNSUPPORTED_CHAIN", http.StatusBadRequest, "holdings are available on chains MAX can send from")
	ErrInvalidAddress   = shared.NewAppError("INVALID_ADDRESS", http.StatusBadRequest, "address is not valid for this chain")
	ErrUnavailable      = shared.NewAppError("HOLDINGS_UNAVAILABLE", http.StatusServiceUnavailable, "wallet balances are unavailable right now")
)

// Holding is one asset in a wallet. PriceUSD and ValueUSD are empty when the
// asset has no known price.
type Holding struct {
	Kind     string
	Address  string // canonical token address; empty for the native coin
	Symbol   string
	Name     string
	Logo     string
	Decimals int
	Balance  *big.Int
	PriceUSD string
	ValueUSD string
	Verified bool
}

// Source lists a wallet's assets on a chain. It returns errNoSource for chains
// it does not cover, so the service can fall back.
type Source interface {
	Tokens(ctx context.Context, chain chains.Chain, owner string) ([]Holding, error)
}

var errNoSource = errors.New("chain not covered by source")

// NativeReader reads a wallet's native balance through the chain's RPC.
type NativeReader func(ctx context.Context, owner string) (*big.Int, error)

type cached struct {
	at  time.Time
	out []Holding
}

type Service struct {
	src     Source
	natives map[string]NativeReader
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cached
	// inflight collapses simultaneous misses for one wallet into one fetch.
	inflight singleflight.Group
}

// NewService builds the lister. src may be nil (no indexer configured);
// natives maps chain keys to RPC balance readers.
func NewService(src Source, natives map[string]NativeReader) *Service {
	return &Service{src: src, natives: natives, now: time.Now, cache: map[string]cached{}}
}

func (s *Service) List(ctx context.Context, chainKey, address string) ([]Holding, error) {
	chain, ok := chains.LookupSendable(chainKey)
	if !ok {
		return nil, ErrUnsupportedChain
	}
	owner, err := chains.NormalizeAddress(chain.Namespace, address)
	if err != nil {
		return nil, ErrInvalidAddress
	}
	key := chain.Key + "|" + owner
	s.mu.Lock()
	if c, ok := s.cache[key]; ok && s.now().Sub(c.at) < cacheTTL {
		s.mu.Unlock()
		return c.out, nil
	}
	s.mu.Unlock()

	v, err, _ := s.inflight.Do(key, func() (any, error) {
		// Shared by every waiter, so one caller hanging up must not fail the
		// rest; the timeout still bounds it.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
		defer cancel()
		out, err := s.fetch(fctx, chain, owner)
		if err != nil {
			return nil, err
		}
		out = finish(chain, out)
		s.store(key, out)
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]Holding), nil
}

// store caches a result, first dropping expired entries when the cache is
// full and then, if it is still full, arbitrary ones.
func (s *Service) store(key string, out []Holding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if len(s.cache) >= maxCacheEntries {
		for k, c := range s.cache {
			if now.Sub(c.at) >= cacheTTL {
				delete(s.cache, k)
			}
		}
	}
	for k := range s.cache {
		if len(s.cache) < maxCacheEntries {
			break
		}
		delete(s.cache, k)
	}
	s.cache[key] = cached{at: now, out: out}
}

func (s *Service) fetch(ctx context.Context, chain chains.Chain, owner string) ([]Holding, error) {
	if s.src != nil {
		out, err := s.src.Tokens(ctx, chain, owner)
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, errNoSource) {
			return nil, ErrUnavailable.WithError(err)
		}
	}
	read, ok := s.natives[chain.Key]
	if !ok {
		return nil, ErrUnavailable
	}
	bal, err := read(ctx, owner)
	if err != nil {
		return nil, ErrUnavailable.WithError(err)
	}
	return []Holding{{Kind: KindNative, Balance: bal}}, nil
}

// finish fills native metadata, marks verified tokens, drops empty tokens and
// sorts: native first, then verified tokens, then the rest; within each group
// priced by USD value, then unpriced by symbol.
func finish(chain chains.Chain, in []Holding) []Holding {
	native := Holding{Kind: KindNative, Balance: new(big.Int)}
	tokens := make([]Holding, 0, len(in))
	for _, h := range in {
		if h.Balance == nil {
			continue
		}
		if h.Kind == KindNative {
			native = h
			continue
		}
		if h.Balance.Sign() <= 0 {
			continue
		}
		_, h.Verified = chains.LookupVerifiedToken(chain.Key, h.Address)
		if strings.TrimSpace(h.Symbol) == "" {
			h.Symbol = shortAddress(chains.DisplayAddress(chain.Namespace, h.Address))
		}
		h.ValueUSD = valueUSD(h.Balance, h.Decimals, h.PriceUSD)
		tokens = append(tokens, h)
	}
	native.Address, native.Verified = "", true
	native.Symbol, native.Name, native.Decimals = chain.NativeCoin, chain.Name, chain.NativeDecimals
	native.ValueUSD = valueUSD(native.Balance, native.Decimals, native.PriceUSD)

	val := func(h Holding) float64 {
		f, _ := new(big.Float).SetString(h.ValueUSD)
		if f == nil {
			return -1
		}
		v, _ := f.Float64()
		return v
	}
	sort.SliceStable(tokens, func(i, j int) bool {
		// Verified tokens lead so a scam airdrop with a made-up price can't top the list.
		if tokens[i].Verified != tokens[j].Verified {
			return tokens[i].Verified
		}
		vi, vj := val(tokens[i]), val(tokens[j])
		if vi != vj {
			return vi > vj
		}
		return strings.ToLower(tokens[i].Symbol) < strings.ToLower(tokens[j].Symbol)
	})
	return append([]Holding{native}, tokens...)
}

// shortAddress labels a token that has no symbol, e.g. "DezX…B263".
func shortAddress(a string) string {
	if len(a) <= 10 {
		return a
	}
	return a[:4] + "…" + a[len(a)-4:]
}

// valueUSD returns balance × price to cents, or "" without a price.
func valueUSD(bal *big.Int, decimals int, price string) string {
	p, ok := new(big.Float).SetPrec(128).SetString(price)
	if price == "" || !ok || bal == nil {
		return ""
	}
	amt := new(big.Float).SetPrec(128).SetInt(bal)
	amt.Quo(amt, new(big.Float).SetPrec(128).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)))
	return amt.Mul(amt, p).Text('f', 2)
}
