package maxids

import (
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

func (h *Handler) Availability(c *gin.Context) {
	a, err := h.svc.CheckAvailability(c.Request.Context(), c.Query("name"))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toAvailabilityResponse(a))
}

func (h *Handler) Claim(c *gin.Context) {
	var req claimRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	p := shared.MustPrincipal(c)
	typ := Type(req.Type)
	if p.AccountType == "agent" {
		typ = TypeAgent // an agent can only ever hold agent IDs
	}
	m, err := h.svc.Claim(c.Request.Context(), p.AccountID, ClaimInput{
		Name: req.Name, Type: typ, DisplayName: req.DisplayName,
	})
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.Created(c, toMaxIDResponse(m))
}

func (h *Handler) List(c *gin.Context) {
	ids, err := h.svc.ListMine(c.Request.Context(), shared.MustPrincipal(c).AccountID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]maxIDResponse, 0, len(ids))
	for _, m := range ids {
		out = append(out, toMaxIDResponse(m))
	}
	shared.OK(c, out)
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	m, err := h.svc.Get(c.Request.Context(), shared.MustPrincipal(c).AccountID, id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toMaxIDResponse(m))
}

func (h *Handler) UpdateProfile(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req profileRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	m, err := h.svc.UpdateProfile(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, ProfileInput{
		DisplayName: req.DisplayName, Description: req.Description, AvatarURL: req.AvatarURL,
	})
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toMaxIDResponse(m))
}

func (h *Handler) SetDestination(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req destinationRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	m, err := h.svc.SetDestination(c.Request.Context(), shared.MustPrincipal(c).AccountID, id,
		c.Param("namespace"), uuid.MustParse(req.WalletID))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toMaxIDResponse(m))
}

func (h *Handler) RemoveDestination(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	m, err := h.svc.RemoveDestination(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, c.Param("namespace"))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toMaxIDResponse(m))
}

func (h *Handler) SetSettlement(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req settlementRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	enabled := req.Enabled == nil || *req.Enabled
	m, err := h.svc.SetSettlement(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, req.Chain, req.Asset, enabled)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toMaxIDResponse(m))
}

func (h *Handler) RemoveSettlement(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	m, err := h.svc.RemoveSettlement(c.Request.Context(), shared.MustPrincipal(c).AccountID, id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toMaxIDResponse(m))
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.Fail(c, ErrMaxIDNotFound)
		return uuid.Nil, false
	}
	return id, true
}
