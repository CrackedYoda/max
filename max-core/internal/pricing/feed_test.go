package pricing

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeSource struct {
	calls int
	resp  map[string]float64
	err   error
}

func (f *fakeSource) Fetch(_ context.Context, _ []string) (map[string]float64, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]float64, len(f.resp))
	for k, v := range f.resp {
		out[k] = v
	}
	return out, nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newClock() *clock { return &clock{t: time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)} }

func TestCached_ServesWithinTTL(t *testing.T) {
	src := &fakeSource{resp: map[string]float64{"SOL": 150}}
	c := newClock()
	f := NewCached(src, time.Minute, 5*time.Minute, c.now)
	for i := 0; i < 2; i++ {
		got, err := f.USD(context.Background(), []string{"SOL"})
		if err != nil || got["SOL"] != 150 {
			t.Fatalf("got %v, %v", got, err)
		}
		c.t = c.t.Add(30 * time.Second)
	}
	if src.calls != 1 {
		t.Fatalf("calls = %d, want 1", src.calls)
	}
}

func TestCached_StaleAfterMaxAgeFailsClosed(t *testing.T) {
	src := &fakeSource{resp: map[string]float64{"SOL": 150}}
	c := newClock()
	f := NewCached(src, time.Minute, 5*time.Minute, c.now)
	if _, err := f.USD(context.Background(), []string{"SOL"}); err != nil {
		t.Fatal(err)
	}
	src.err = errors.New("boom")
	c.t = c.t.Add(6 * time.Minute)
	if _, err := f.USD(context.Background(), []string{"SOL"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestCached_UsesStaleWithinMaxAgeOnError(t *testing.T) {
	src := &fakeSource{resp: map[string]float64{"SOL": 150}}
	c := newClock()
	f := NewCached(src, time.Minute, 5*time.Minute, c.now)
	if _, err := f.USD(context.Background(), []string{"SOL"}); err != nil {
		t.Fatal(err)
	}
	src.err = errors.New("boom")
	c.t = c.t.Add(2 * time.Minute)
	got, err := f.USD(context.Background(), []string{"SOL"})
	if err != nil || got["SOL"] != 150 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestCached_RejectsBadValues(t *testing.T) {
	cases := map[string]map[string]float64{
		"zero":    {"SOL": 0, "ETH": 3000},
		"neg":     {"SOL": -1, "ETH": 3000},
		"nan":     {"SOL": math.NaN(), "ETH": 3000},
		"inf":     {"SOL": math.Inf(1), "ETH": 3000},
		"missing": {"SOL": 150},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			f := NewCached(&fakeSource{resp: resp}, time.Minute, 5*time.Minute, newClock().now)
			if _, err := f.USD(context.Background(), []string{"SOL", "ETH"}); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestCached_BadValueDoesNotPoisonCache(t *testing.T) {
	src := &fakeSource{resp: map[string]float64{"SOL": 150}}
	c := newClock()
	f := NewCached(src, time.Minute, 5*time.Minute, c.now)
	if _, err := f.USD(context.Background(), []string{"SOL"}); err != nil {
		t.Fatal(err)
	}
	src.resp = map[string]float64{"SOL": 0}
	c.t = c.t.Add(2 * time.Minute)
	got, err := f.USD(context.Background(), []string{"SOL"})
	if err != nil || got["SOL"] != 150 {
		t.Fatalf("got %v, %v; want stale 150", got, err)
	}
}

func TestCoinGecko_ParsesSimplePrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("vs_currencies") != "usd" {
			t.Errorf("vs_currencies = %q", r.URL.Query().Get("vs_currencies"))
		}
		_, _ = w.Write([]byte(`{"solana":{"usd":150.5},"ethereum":{"usd":3000},"polygon-ecosystem-token":{"usd":0.4}}`))
	}))
	defer srv.Close()
	got, err := NewCoinGecko(srv.URL, srv.Client()).Fetch(context.Background(), []string{"SOL", "ETH", "POL"})
	if err != nil {
		t.Fatal(err)
	}
	if got["SOL"] != 150.5 || got["ETH"] != 3000 || got["POL"] != 0.4 {
		t.Fatalf("got %v", got)
	}
}

func TestCoinGecko_NonOKStatusAndBadSymbol(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("secret-body"))
	}))
	defer srv.Close()
	cg := NewCoinGecko(srv.URL, srv.Client())
	_, err := cg.Fetch(context.Background(), []string{"SOL"})
	if err == nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret-body") {
		t.Fatal("error leaks body")
	}
	if _, err := cg.Fetch(context.Background(), []string{"DOGE"}); err == nil {
		t.Fatal("expected error for unknown symbol")
	}
}
