package payments

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/max/max-core/internal/shared"
)

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Prepare(c *gin.Context) {
	if p, ok := shared.GetPrincipal(c); ok && p.CredentialID != nil {
		shared.Fail(c, shared.ErrForbidden.WithMessage("agents must propose transfers via /v1/agent/transfers"))
		return
	}
	var req prepareRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	res, err := h.svc.Prepare(c.Request.Context(), PrepareInput{
		Caller: callerID(c), Recipient: req.Recipient, Chain: req.Chain, Asset: req.Asset,
		Amount: req.Amount, SenderAddress: req.SenderAddress, IdempotencyKey: req.IdempotencyKey,
		ResolutionVersion: req.ResolutionVersion,
		DestinationChain:  req.DestinationChain, DestinationAsset: req.DestinationAsset,
		AmountType: req.AmountType, SlippageBps: req.SlippageBps,
	})
	if err != nil {
		shared.Fail(c, err)
		return
	}
	resp := toPaymentResponse(res.Payment)
	resp.Instructions = toInstructionsResponse(res.Instructions)
	resp.Warnings = res.Warnings
	resp.Replayed = res.Replayed
	if res.Replayed {
		shared.OK(c, resp)
		return
	}
	shared.Created(c, resp)
}

func (h *Handler) Submit(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req submitRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	p, err := h.svc.Submit(c.Request.Context(), SubmitInput{Caller: callerID(c), PaymentID: id, TxHash: req.TxHash})
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toPaymentResponse(p))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, events, err := h.svc.Get(c.Request.Context(), callerID(c), id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	resp := toPaymentResponse(p)
	resp.Events = toEventResponses(events)
	if p.Status == StatusAwaitingSenderSignature || p.Status == StatusQuoted {
		resp.Instructions = toInstructionsResponse(h.svc.Instructions(p))
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, resp)
}

func (h *Handler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	ps, err := h.svc.List(c.Request.Context(), shared.MustPrincipal(c).AccountID, c.DefaultQuery("direction", "sent"), int32(limit))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]paymentResponse, 0, len(ps))
	for _, p := range ps {
		out = append(out, toPaymentResponse(p))
	}
	shared.OK(c, out)
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.Fail(c, ErrPaymentNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func callerID(c *gin.Context) *uuid.UUID {
	if p, ok := shared.GetPrincipal(c); ok {
		return &p.AccountID
	}
	return nil
}

// ReceivableAssets lists what a recipient can be paid in through a
// cross-chain route, grouped by chain.
func (h *Handler) ReceivableAssets(c *gin.Context) {
	assets, err := h.svc.ReceivableAssets(c.Request.Context())
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]receivableAssetResponse, 0, len(assets))
	for _, a := range assets {
		out = append(out, receivableAssetResponse{
			Chain: a.Chain.Key, Kind: string(a.Kind), Address: a.Address, Symbol: a.Symbol,
			Decimals: a.Decimals, PriceUSD: a.PriceUSD, Verified: a.Verified,
		})
	}
	shared.OK(c, out)
}
