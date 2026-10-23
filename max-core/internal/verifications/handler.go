package verifications

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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

func (h *Handler) StartX(c *gin.Context) {
	p := shared.MustPrincipal(c)
	u, exp, err := h.svc.StartX(c.Request.Context(), p.AccountID, p.SessionID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.Created(c, gin.H{"authorizeUrl": u, "expiresAt": exp})
}

// CallbackX is where X redirects the browser. It always ends in a redirect to
// the configured frontend URL (never one taken from the request), carrying
// only a status and a machine-readable code.
func (h *Handler) CallbackX(c *gin.Context) {
	var account, session *uuid.UUID
	if p, ok := shared.GetPrincipal(c); ok && !p.IsAgentCredential() {
		account, session = &p.AccountID, &p.SessionID
	}
	v, renewed, err := h.svc.CompleteX(c.Request.Context(), account, session, c.Query("state"), c.Query("code"), c.Query("error"))

	target := h.svc.ResultRedirect()
	if target == "" {
		if err != nil {
			shared.Fail(c, err)
			return
		}
		shared.OK(c, gin.H{"verification": toVerificationResponse(v), "renewedClaims": len(renewed)})
		return
	}
	q := url.Values{}
	if err != nil {
		var appErr *shared.AppError
		code := "X_PROVIDER_ERROR"
		if errors.As(err, &appErr) {
			code = appErr.Code
		}
		shared.GetLogger(c).Warn("x verification failed")
		q.Set("x", "error")
		q.Set("code", code)
	} else {
		q.Set("x", "verified")
		q.Set("username", v.Username)
		q.Set("renewed", strconv.Itoa(len(renewed)))
	}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, target+"?"+q.Encode())
}

func (h *Handler) ListVerifications(c *gin.Context) {
	vs, err := h.svc.ListVerifications(c.Request.Context(), shared.MustPrincipal(c).AccountID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]verificationResponse, 0, len(vs))
	for _, v := range vs {
		out = append(out, toVerificationResponse(v))
	}
	shared.OK(c, out)
}

func (h *Handler) RevokeVerification(c *gin.Context) {
	id, ok := param(c, "id", ErrVerificationNotFound)
	if !ok {
		return
	}
	if err := h.svc.RevokeMine(c.Request.Context(), shared.MustPrincipal(c).AccountID, id); err != nil {
		shared.Fail(c, err)
		return
	}
	shared.NoContent(c)
}

func (h *Handler) Claim(c *gin.Context) {
	var req claimRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	res, err := h.svc.ClaimReserved(c.Request.Context(), shared.MustPrincipal(c).AccountID, req.Name, req.DisplayName)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	if res.Pending {
		c.JSON(http.StatusAccepted, gin.H{"data": toClaimResponse(res.Claim)})
		return
	}
	shared.Created(c, toClaimResponse(res.Claim))
}

func (h *Handler) ListClaims(c *gin.Context) {
	cs, err := h.svc.ListClaims(c.Request.Context(), shared.MustPrincipal(c).AccountID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toClaimResponses(cs))
}

// ---- Administrator ----

func (h *Handler) AdminListClaims(c *gin.Context) {
	cs, err := h.svc.AdminListClaims(c.Request.Context(), ClaimStatus(c.Query("status")))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toClaimResponses(cs))
}

func (h *Handler) AdminApprove(c *gin.Context) { h.adminDecide(c, h.svc.AdminApprove) }
func (h *Handler) AdminDeny(c *gin.Context)    { h.adminDecide(c, h.svc.AdminDeny) }
func (h *Handler) AdminRevokeClaim(c *gin.Context) {
	h.adminDecide(c, h.svc.AdminRevokeClaim)
}

func (h *Handler) adminDecide(c *gin.Context, fn func(ctx context.Context, admin, id uuid.UUID, note string) (Claim, error)) {
	id, ok := param(c, "id", ErrClaimNotFound)
	if !ok {
		return
	}
	var req noteRequest
	_ = c.ShouldBindJSON(&req)
	cl, err := fn(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, req.Note)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toClaimResponse(cl))
}

func (h *Handler) AdminRevokeVerification(c *gin.Context) {
	id, ok := param(c, "id", ErrVerificationNotFound)
	if !ok {
		return
	}
	var req noteRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.svc.AdminRevokeVerification(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, req.Note); err != nil {
		shared.Fail(c, err)
		return
	}
	shared.NoContent(c)
}

func param(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		shared.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}
