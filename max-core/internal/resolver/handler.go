package resolver

import (
	"github.com/gin-gonic/gin"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

type Handler struct {
	svc Service
}

func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Resolve(c *gin.Context) {
	res, err := h.svc.Resolve(c.Request.Context(), c.Param("maxId"), c.Query("chain"), c.Query("asset"))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	// Resolutions change when owners edit destinations; never let a stale one be cached.
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toResolveResponse(res))
}

func (h *Handler) Profile(c *gin.Context) {
	p, reach, err := h.svc.Profile(c.Request.Context(), c.Param("maxId"))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toProfileResponse(p, reach))
}

func (h *Handler) Chains(c *gin.Context) {
	shared.OK(c, chains.Supported)
}
