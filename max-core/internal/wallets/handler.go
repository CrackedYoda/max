package wallets

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

func (h *Handler) List(c *gin.Context) {
	ws, err := h.svc.List(c.Request.Context(), shared.MustPrincipal(c).AccountID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]walletResponse, 0, len(ws))
	for _, w := range ws {
		out = append(out, toWalletResponse(w))
	}
	shared.OK(c, out)
}

func (h *Handler) Remove(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		shared.Fail(c, ErrWalletNotFound)
		return
	}
	if err := h.svc.Remove(c.Request.Context(), shared.MustPrincipal(c).AccountID, id); err != nil {
		shared.Fail(c, err)
		return
	}
	shared.NoContent(c)
}
