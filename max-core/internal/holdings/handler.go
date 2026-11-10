package holdings

import (
	"math/big"

	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type amount struct {
	Raw     string `json:"raw"`
	Display string `json:"display"`
}

type holdingResponse struct {
	Kind     string  `json:"kind"`
	Address  string  `json:"address"`
	Symbol   string  `json:"symbol"`
	Name     string  `json:"name"`
	Logo     string  `json:"logo"`
	Decimals int     `json:"decimals"`
	Balance  amount  `json:"balance"`
	PriceUSD *string `json:"priceUsd"`
	ValueUSD *string `json:"valueUsd"`
	Verified bool    `json:"verified"`
}

func toResponse(c chains.Chain, hs []Holding) []holdingResponse {
	out := make([]holdingResponse, 0, len(hs))
	for _, h := range hs {
		bal := h.Balance
		if bal == nil {
			bal = new(big.Int)
		}
		r := holdingResponse{
			Kind: h.Kind, Symbol: h.Symbol, Name: h.Name, Logo: h.Logo, Decimals: h.Decimals, Verified: h.Verified,
			Balance: amount{Raw: bal.String(), Display: payments.FormatAmount(bal, h.Decimals)},
		}
		if h.Address != "" {
			r.Address = chains.DisplayAddress(c.Namespace, h.Address)
		}
		if h.PriceUSD != "" {
			r.PriceUSD, r.ValueUSD = &h.PriceUSD, &h.ValueUSD
		}
		out = append(out, r)
	}
	return out
}

// List serves GET /v1/holdings?chain=&address=.
func (h *Handler) List(c *gin.Context) {
	out, err := h.svc.List(c.Request.Context(), c.Query("chain"), c.Query("address"))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	chain, _ := chains.Lookup(c.Query("chain"))
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toResponse(chain, out))
}
