package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const defaultCoinGeckoURL = "https://api.coingecko.com/api/v3/simple/price"

var coinGeckoIDs = map[string]string{
	"SOL": "solana",
	"ETH": "ethereum",
	"POL": "polygon-ecosystem-token",
}

type coinGecko struct {
	url    string
	apiKey string
	hc     *http.Client
}

// NewCoinGecko returns a Source backed by CoinGecko's simple/price endpoint.
// An empty url uses the public default; a nil client uses http.DefaultClient.
// A non-empty apiKey is sent as the pro header for pro-api.coingecko.com and
// as the demo header otherwise.
func NewCoinGecko(rawURL, apiKey string, hc *http.Client) Source {
	if rawURL == "" {
		rawURL = defaultCoinGeckoURL
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	return &coinGecko{url: rawURL, apiKey: strings.TrimSpace(apiKey), hc: hc}
}

func (c *coinGecko) Fetch(ctx context.Context, symbols []string) (map[string]float64, error) {
	ids := make([]string, 0, len(symbols))
	for _, s := range symbols {
		id, ok := coinGeckoIDs[s]
		if !ok {
			return nil, fmt.Errorf("%w: unsupported symbol %q", ErrUnavailable, s)
		}
		ids = append(ids, id)
	}
	u, err := url.Parse(c.url)
	if err != nil {
		return nil, fmt.Errorf("%w: bad feed url", ErrUnavailable)
	}
	q := u.Query()
	q.Set("ids", strings.Join(ids, ","))
	q.Set("vs_currencies", "usd")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request", ErrUnavailable)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		header := "x-cg-demo-api-key"
		if strings.HasPrefix(u.Host, "pro-api.") {
			header = "x-cg-pro-api-key"
		}
		req.Header.Set(header, c.apiKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request failed", ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	var body map[string]struct {
		USD *float64 `json:"usd"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: decode failed", ErrUnavailable)
	}
	out := make(map[string]float64, len(symbols))
	for _, s := range symbols {
		if v, ok := body[coinGeckoIDs[s]]; ok && v.USD != nil {
			out[s] = *v.USD
		}
	}
	return out, nil
}
