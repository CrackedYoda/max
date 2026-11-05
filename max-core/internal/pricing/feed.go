// Package pricing provides cached USD prices for native coins.
package pricing

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// ErrUnavailable means a price could not be obtained or is too stale to trust.
var ErrUnavailable = errors.New("price unavailable")

// Feed returns USD prices keyed by symbol (e.g. ETH, SOL, POL).
type Feed interface {
	USD(ctx context.Context, symbols []string) (map[string]float64, error)
}

// Source fetches fresh prices from an upstream provider.
type Source interface {
	Fetch(ctx context.Context, symbols []string) (map[string]float64, error)
}

type entry struct {
	usd float64
	at  time.Time
}

type cached struct {
	src    Source
	ttl    time.Duration
	maxAge time.Duration
	now    func() time.Time

	mu    sync.Mutex
	items map[string]entry
}

// NewCached wraps src with a per-symbol cache. Entries younger than ttl are
// served without calling src. If a refresh fails, entries no older than maxAge
// are served; anything older fails closed with ErrUnavailable.
func NewCached(src Source, ttl, maxAge time.Duration, now func() time.Time) Feed {
	if now == nil {
		now = time.Now
	}
	return &cached{src: src, ttl: ttl, maxAge: maxAge, now: now, items: map[string]entry{}}
}

func (c *cached) USD(ctx context.Context, symbols []string) (map[string]float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if got, ok := c.lookup(symbols, func(age time.Duration) bool { return age < c.ttl }); ok {
		return got, nil
	}
	fresh, err := c.src.Fetch(ctx, symbols)
	if err == nil && validAll(fresh, symbols) {
		at := c.now()
		out := make(map[string]float64, len(symbols))
		for _, s := range symbols {
			c.items[s] = entry{usd: fresh[s], at: at}
			out[s] = fresh[s]
		}
		return out, nil
	}
	if got, ok := c.lookup(symbols, func(age time.Duration) bool { return age <= c.maxAge }); ok {
		return got, nil
	}
	return nil, ErrUnavailable
}

// lookup returns all symbols if every one is cached and ok(age) holds.
func (c *cached) lookup(symbols []string, ok func(time.Duration) bool) (map[string]float64, bool) {
	if len(symbols) == 0 {
		return nil, false
	}
	now := c.now()
	out := make(map[string]float64, len(symbols))
	for _, s := range symbols {
		e, found := c.items[s]
		if !found || !ok(now.Sub(e.at)) {
			return nil, false
		}
		out[s] = e.usd
	}
	return out, true
}

func validAll(m map[string]float64, symbols []string) bool {
	if len(symbols) == 0 {
		return false
	}
	for _, s := range symbols {
		v, ok := m[s]
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			return false
		}
	}
	return true
}
