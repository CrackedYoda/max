package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/max/max-core/internal/shared"
)

const SessionCookie = "max_session"

type CookieConfig struct {
	Secure bool
	TTL    time.Duration
}

type Handler struct {
	svc    Service
	cookie CookieConfig
}

func NewHandler(svc Service, cookie CookieConfig) *Handler {
	return &Handler{svc: svc, cookie: cookie}
}

func (h *Handler) Challenge(c *gin.Context) {
	var req challengeRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	ch, err := h.svc.CreateChallenge(c.Request.Context(), ChallengeInput{
		Purpose: Purpose(req.Purpose), Namespace: req.Namespace, Address: req.Address, Caller: callerID(c),
		Origin: c.GetHeader("Origin"),
	})
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.Created(c, challengeResponse{Nonce: ch.Nonce, Message: ch.Message, ExpiresAt: ch.ExpiresAt})
}

func (h *Handler) Verify(c *gin.Context) {
	var req verifyRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	res, err := h.svc.Verify(c.Request.Context(), VerifyInput{
		Nonce: req.Nonce, Signature: req.Signature, Caller: callerID(c),
		IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		shared.Fail(c, err)
		return
	}

	resp := verifyResponse{
		Purpose: string(res.Purpose), Account: toAccountResponse(res.Account),
		Wallet: toWalletResponse(res.Wallet), AccountCreated: res.AccountCreated,
	}
	if res.Session != nil {
		resp.SessionExpires = &res.Session.ExpiresAt
		if req.TokenDelivery == "body" {
			resp.SessionToken = res.SessionToken
			c.Header("Cache-Control", "no-store")
		} else {
			h.setCookie(c, res.SessionToken, int(h.cookie.TTL.Seconds()))
		}
	}
	shared.OK(c, resp)
}

func (h *Handler) Me(c *gin.Context) {
	p := shared.MustPrincipal(c)
	res, err := h.svc.Me(c.Request.Context(), p.AccountID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toMeResponse(res))
}

func (h *Handler) Logout(c *gin.Context) {
	p := shared.MustPrincipal(c)
	if err := h.svc.Logout(c.Request.Context(), p.SessionID); err != nil {
		shared.Fail(c, err)
		return
	}
	h.setCookie(c, "", -1)
	shared.NoContent(c)
}

func (h *Handler) setCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookie, value, maxAge, "/", "", h.cookie.Secure, true)
}

func callerID(c *gin.Context) *uuid.UUID {
	if p, ok := shared.GetPrincipal(c); ok {
		return &p.AccountID
	}
	return nil
}
