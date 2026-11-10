package holdings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/max/max-core/internal/chains"
)

const (
	defaultAlchemyURL = "https://api.g.alchemy.com"
	maxPages          = 10
)

// alchemyNetworks maps MAX's paying mainnet chains to Alchemy network slugs.
var alchemyNetworks = map[string]string{
	"ethereum":  "eth-mainnet",
	"base":      "base-mainnet",
	"arbitrum":  "arb-mainnet",
	"optimism":  "opt-mainnet",
	"polygon":   "matic-mainnet",
	"bnb":       "bnb-mainnet",
	"avalanche": "avax-mainnet",
	"solana":    "solana-mainnet",
}

type AlchemyConfig struct {
	APIKey  string
	BaseURL string
}

// Alchemy reads balances, token metadata and USD prices from the Alchemy
// Portfolio API ("tokens by wallet").
type Alchemy struct {
	cfg  AlchemyConfig
	http *http.Client
}

func NewAlchemy(cfg AlchemyConfig) *Alchemy {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultAlchemyURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Alchemy{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

type alchemyRequest struct {
	Addresses           []alchemyAddress `json:"addresses"`
	WithMetadata        bool             `json:"withMetadata"`
	WithPrices          bool             `json:"withPrices"`
	IncludeNativeTokens bool             `json:"includeNativeTokens"`
	PageKey             string           `json:"pageKey,omitempty"`
}

type alchemyAddress struct {
	Address  string   `json:"address"`
	Networks []string `json:"networks"`
}

type alchemyResponse struct {
	Data struct {
		Tokens []struct {
			TokenAddress  *string `json:"tokenAddress"`
			TokenBalance  *string `json:"tokenBalance"`
			TokenMetadata struct {
				Decimals *int    `json:"decimals"`
				Logo     *string `json:"logo"`
				Name     *string `json:"name"`
				Symbol   *string `json:"symbol"`
			} `json:"tokenMetadata"`
			TokenPrices []struct {
				Currency string `json:"currency"`
				Value    string `json:"value"`
			} `json:"tokenPrices"`
			Error *string `json:"error"`
		} `json:"tokens"`
		PageKey *string `json:"pageKey"`
	} `json:"data"`
}

func (a *Alchemy) Tokens(ctx context.Context, chain chains.Chain, owner string) ([]Holding, error) {
	network, ok := alchemyNetworks[chain.Key]
	if !ok || a.cfg.APIKey == "" {
		return nil, errNoSource
	}
	req := alchemyRequest{
		Addresses:    []alchemyAddress{{Address: chains.DisplayAddress(chain.Namespace, owner), Networks: []string{network}}},
		WithMetadata: true, WithPrices: true, IncludeNativeTokens: true,
	}
	var out []Holding
	for page := 0; page < maxPages; page++ {
		resp, err := a.post(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, t := range resp.Data.Tokens {
			if t.Error != nil || t.TokenBalance == nil {
				continue
			}
			bal, ok := parseBalance(*t.TokenBalance)
			if !ok {
				continue
			}
			h := Holding{Kind: KindNative, Balance: bal, Symbol: str(t.TokenMetadata.Symbol), Name: str(t.TokenMetadata.Name), Logo: str(t.TokenMetadata.Logo)}
			for _, p := range t.TokenPrices {
				if strings.EqualFold(p.Currency, "usd") {
					h.PriceUSD = p.Value
				}
			}
			if t.TokenAddress != nil && *t.TokenAddress != "" {
				addr, err := chains.NormalizeAddress(chain.Namespace, *t.TokenAddress)
				if err != nil || t.TokenMetadata.Decimals == nil {
					continue // an amount can't be shown without decimals
				}
				if chain.Namespace == chains.NamespaceSolana && *t.TokenMetadata.Decimals == 0 {
					continue // Solana NFTs are zero-decimal mints; NFTs are out of scope
				}
				h.Kind, h.Address, h.Decimals = KindToken, addr, *t.TokenMetadata.Decimals
			}
			out = append(out, h)
		}
		if resp.Data.PageKey == nil || *resp.Data.PageKey == "" {
			break
		}
		req.PageKey = *resp.Data.PageKey
	}
	return out, nil
}

func (a *Alchemy) post(ctx context.Context, body alchemyRequest) (*alchemyResponse, error) {
	b, _ := json.Marshal(body)
	url := a.cfg.BaseURL + "/data/v1/" + a.cfg.APIKey + "/assets/tokens/by-address"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.http.Do(req)
	if err != nil {
		// The URL carries the API key; never surface it in errors.
		return nil, fmt.Errorf("alchemy tokens request failed")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alchemy tokens: HTTP %d", res.StatusCode)
	}
	var out alchemyResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("alchemy tokens: bad response")
	}
	return &out, nil
}

// parseBalance accepts hex ("0x…") or decimal strings.
func parseBalance(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if h, ok := strings.CutPrefix(strings.ToLower(s), "0x"); ok {
		if h == "" {
			return new(big.Int), true
		}
		return new(big.Int).SetString(h, 16)
	}
	return new(big.Int).SetString(s, 10)
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
