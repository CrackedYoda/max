package agents

import (
	"time"

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

// ---- Administrator (session) ----

func (h *Handler) Create(c *gin.Context) {
	var req createAgentRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	d, err := h.svc.CreateAgent(c.Request.Context(), admin(c), req.Name, req.Description)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.Created(c, toAgentDetailResponse(d))
}

func (h *Handler) List(c *gin.Context) {
	as, err := h.svc.ListAgents(c.Request.Context(), admin(c))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]agentResponse, 0, len(as))
	for _, a := range as {
		out = append(out, toAgentResponse(a))
	}
	shared.OK(c, out)
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	d, err := h.svc.GetAgent(c.Request.Context(), admin(c), id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toAgentDetailResponse(d))
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	var req updateAgentRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	a, err := h.svc.UpdateAgent(c.Request.Context(), admin(c), id, req.Name, req.Description, Status(req.Status))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toAgentResponse(a))
}

func (h *Handler) SetPolicy(c *gin.Context) {
	id, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	var req policyRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	in := PolicyInput{AllowedRecipients: req.AllowedRecipients, AllowAnyRecipient: req.AllowAnyRecipient}
	if req.Market != nil {
		in.Market = &MarketPolicyInput{CanSell: req.Market.CanSell}
		for _, sm := range req.Market.MinSalePrices {
			in.Market.MinSalePrices = append(in.Market.MinSalePrices, SaleMinimumInput{Chain: sm.Chain, Asset: sm.Asset, Amount: sm.Amount})
		}
	}
	for _, l := range req.Limits {
		in.Limits = append(in.Limits, LimitInput{Chain: l.Chain, Asset: l.Asset, MaxPerTransfer: l.MaxPerTransfer, MaxPer24h: l.MaxPer24h})
	}
	p, err := h.svc.SetPolicy(c.Request.Context(), admin(c), id, in)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toPolicyResponse(p))
}

func (h *Handler) IssueCredential(c *gin.Context) {
	id, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	var req credentialRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	cred, err := h.svc.IssueCredential(c.Request.Context(), admin(c), id, req.Name, req.Scopes, time.Duration(req.ExpiresInDays)*24*time.Hour)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	resp := toCredentialResponse(cred.Credential)
	resp.Secret = cred.Secret
	c.Header("Cache-Control", "no-store")
	shared.Created(c, resp)
}

func (h *Handler) RotateCredential(c *gin.Context) {
	agentID, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	credID, ok := param(c, "credentialId", ErrCredentialNotFound)
	if !ok {
		return
	}
	cred, err := h.svc.RotateCredential(c.Request.Context(), admin(c), agentID, credID)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	resp := toCredentialResponse(cred.Credential)
	resp.Secret = cred.Secret
	c.Header("Cache-Control", "no-store")
	shared.Created(c, resp)
}

func (h *Handler) RevokeCredential(c *gin.Context) {
	agentID, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	credID, ok := param(c, "credentialId", ErrCredentialNotFound)
	if !ok {
		return
	}
	if err := h.svc.RevokeCredential(c.Request.Context(), admin(c), agentID, credID); err != nil {
		shared.Fail(c, err)
		return
	}
	shared.NoContent(c)
}

func (h *Handler) ClaimID(c *gin.Context) {
	agentID, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	var req claimIDRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	m, err := h.svc.ClaimID(c.Request.Context(), admin(c), agentID, req.Name, req.DisplayName)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.Created(c, gin.H{"id": m.ID.String(), "name": m.CanonicalName, "handle": m.Handle(), "type": string(m.Type)})
}

func (h *Handler) WalletChallenge(c *gin.Context) {
	agentID, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	var req walletChallengeRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	ch, err := h.svc.WalletChallenge(c.Request.Context(), admin(c), agentID, req.Namespace, req.Address)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.Created(c, gin.H{"nonce": ch.Nonce, "message": ch.Message, "expiresAt": ch.ExpiresAt})
}

func (h *Handler) WalletVerify(c *gin.Context) {
	agentID, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	var req walletVerifyRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	w, err := h.svc.WalletVerify(c.Request.Context(), admin(c), agentID, req.Nonce, req.Signature)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, gin.H{"id": w.ID.String(), "namespace": string(w.Namespace), "address": w.Address, "verifiedAt": w.VerifiedAt})
}

func (h *Handler) ListTransfers(c *gin.Context) {
	agentID, ok := param(c, "agentId", ErrAgentNotFound)
	if !ok {
		return
	}
	var decision *DecisionOutcome
	if d := c.Query("decision"); d != "" {
		dd := DecisionOutcome(d)
		decision = &dd
	}
	vs, err := h.svc.ListTransfers(c.Request.Context(), admin(c), agentID, decision)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]transferResponse, 0, len(vs))
	for _, v := range vs {
		out = append(out, toTransferResponse(v))
	}
	shared.OK(c, out)
}

func (h *Handler) Approve(c *gin.Context) {
	id, ok := param(c, "paymentId", ErrTransferNotFound)
	if !ok {
		return
	}
	v, err := h.svc.Approve(c.Request.Context(), admin(c), id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toTransferResponse(v))
}

func (h *Handler) Deny(c *gin.Context) {
	id, ok := param(c, "paymentId", ErrTransferNotFound)
	if !ok {
		return
	}
	var req denyRequest
	_ = c.ShouldBindJSON(&req)
	v, err := h.svc.Deny(c.Request.Context(), admin(c), id, req.Reason)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toTransferResponse(v))
}

// ---- Agent (API key) ----

func (h *Handler) Me(c *gin.Context) {
	p := shared.MustPrincipal(c)
	shared.OK(c, gin.H{"agentId": p.AccountID.String(), "credentialId": p.CredentialID.String(), "scopes": p.Scopes})
}

func (h *Handler) Propose(c *gin.Context) {
	var req proposeRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	v, err := h.svc.Propose(c.Request.Context(), shared.MustPrincipal(c), ProposeInput{
		Recipient: req.Recipient, Chain: req.Chain, Asset: req.Asset, Amount: req.Amount,
		SenderAddress: req.SenderAddress, IdempotencyKey: req.IdempotencyKey,
		DestinationChain: req.DestinationChain, DestinationAsset: req.DestinationAsset,
		AmountType: req.AmountType, Instruction: req.Instruction,
	})
	if err != nil {
		shared.Fail(c, err)
		return
	}
	if v.Replayed {
		shared.OK(c, toTransferResponse(v))
		return
	}
	shared.Created(c, toTransferResponse(v))
}

func (h *Handler) GetTransfer(c *gin.Context) {
	id, ok := param(c, "id", ErrTransferNotFound)
	if !ok {
		return
	}
	v, err := h.svc.GetTransfer(c.Request.Context(), shared.MustPrincipal(c), id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toTransferResponse(v))
}

func (h *Handler) Authorization(c *gin.Context) {
	id, ok := param(c, "id", ErrTransferNotFound)
	if !ok {
		return
	}
	sa, err := h.svc.SignedAuthorization(c.Request.Context(), shared.MustPrincipal(c), id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, sa)
}

// PublicKeys lets signers fetch the key that MAX signs authorizations with.
func (h *Handler) PublicKeys(c *gin.Context) {
	keyID, pub, ok := h.svc.PublicKey()
	if !ok {
		shared.Fail(c, ErrSigningUnavailable)
		return
	}
	shared.OK(c, []any{publicKeyJSON(keyID, pub)})
}

func admin(c *gin.Context) uuid.UUID { return shared.MustPrincipal(c).AccountID }

func param(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		shared.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}
