package renames

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Eligibility(c *gin.Context) {
	id, ok := param(c, "id", maxids.ErrMaxIDNotFound)
	if !ok {
		return
	}
	e, err := h.svc.Eligibility(c.Request.Context(), shared.MustPrincipal(c).AccountID, id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toEligibilityResponse(e))
}

func (h *Handler) Request(c *gin.Context) {
	id, ok := param(c, "id", maxids.ErrMaxIDNotFound)
	if !ok {
		return
	}
	var req renameRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	v, err := h.svc.Request(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, RequestInput{
		NewName: req.NewName, FeeChain: req.FeeChain, FeeAsset: req.FeeAsset,
		SenderAddress: req.SenderAddress, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.Created(c, toViewResponse(v))
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := param(c, "id", maxids.ErrMaxIDNotFound)
	if !ok {
		return
	}
	reqID, ok := param(c, "requestId", ErrRequestNotFound)
	if !ok {
		return
	}
	v, err := h.svc.Get(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, reqID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toViewResponse(v))
}

func (h *Handler) History(c *gin.Context) {
	id, ok := param(c, "id", maxids.ErrMaxIDNotFound)
	if !ok {
		return
	}
	hs, err := h.svc.History(c.Request.Context(), shared.MustPrincipal(c).AccountID, id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]historyResponse, 0, len(hs))
	for _, e := range hs {
		r := historyResponse{OldHandle: maxids.Handle(e.OldName), NewHandle: maxids.Handle(e.NewName), ChangedAt: e.ChangedAt}
		if e.FeePaymentID != nil {
			r.FeePaymentID = e.FeePaymentID.String()
		}
		out = append(out, r)
	}
	shared.OK(c, out)
}

func param(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		shared.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}
