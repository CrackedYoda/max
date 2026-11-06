package market

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/max/max-core/internal/agents"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Gate hides every market route while the market is disabled (spec §7).
func (h *Handler) Gate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.svc.cfg.Enabled {
			fail(c, ErrMarketDisabled)
			return
		}
		c.Next()
	}
}

// Trading refuses new market activity while trading is paused. Reads and
// sale submission are not behind it, so open sales can still finish.
func (h *Handler) Trading() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.svc.cfg.TradingPaused {
			fail(c, ErrMarketPaused)
			return
		}
		c.Next()
	}
}

func (h *Handler) Search(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	cards, next, err := h.svc.Search(c.Request.Context(), ListingQuery{
		Q: c.Query("q"), MinPrice: c.Query("minPrice"), MaxPrice: c.Query("maxPrice"),
		Sort: c.Query("sort"), Cursor: c.Query("cursor"), Limit: limit,
	})
	if err != nil {
		fail(c, err)
		return
	}
	out := searchResponse{Listings: make([]cardResponse, 0, len(cards)), NextCursor: next}
	for _, card := range cards {
		out.Listings = append(out.Listings, toCardResponse(card))
	}
	shared.OK(c, out)
}

func (h *Handler) Name(c *gin.Context) {
	v, err := h.svc.NameView(c.Request.Context(), c.Param("name"))
	if err != nil {
		fail(c, err)
		return
	}
	shared.OK(c, toNameViewResponse(v))
}

func (h *Handler) Eligibility(c *gin.Context) {
	id, ok := param(c, "maxId", maxids.ErrMaxIDNotFound)
	if !ok {
		return
	}
	e, err := h.svc.Eligibility(c.Request.Context(), shared.MustPrincipal(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	shared.OK(c, toEligibilityResponse(e))
}

func (h *Handler) CreateListing(c *gin.Context) {
	var req createListingRequest
	if err := shared.BindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}
	maxID, err := uuid.Parse(req.MaxID)
	if err != nil {
		fail(c, maxids.ErrMaxIDNotFound)
		return
	}
	l, err := h.svc.List(c.Request.Context(), shared.MustPrincipal(c), ListInput{
		MaxID: maxID, Price: req.Price, Chain: req.Chain, Asset: req.Asset,
		PayoutAddr: req.PayoutAddress, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		fail(c, err)
		return
	}
	shared.Created(c, toListingResponse(l))
}

func (h *Handler) UpdateListing(c *gin.Context) {
	id, ok := param(c, "id", ErrListingNotFound)
	if !ok {
		return
	}
	var req updateListingRequest
	if err := shared.BindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}
	l, err := h.svc.UpdatePrice(c.Request.Context(), shared.MustPrincipal(c), id, req.Price)
	if err != nil {
		fail(c, err)
		return
	}
	shared.OK(c, toListingResponse(l))
}

func (h *Handler) DeleteListing(c *gin.Context) {
	id, ok := param(c, "id", ErrListingNotFound)
	if !ok {
		return
	}
	if err := h.svc.Delist(c.Request.Context(), shared.MustPrincipal(c), id); err != nil {
		fail(c, err)
		return
	}
	shared.NoContent(c)
}

func (h *Handler) Buy(c *gin.Context) {
	id, ok := param(c, "id", ErrListingNotFound)
	if !ok {
		return
	}
	var req buyRequest
	if err := shared.BindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}
	v, err := h.svc.Buy(c.Request.Context(), shared.MustPrincipal(c), id, BuyInput{FromAddress: req.FromAddress, IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	if v.Replayed {
		shared.OK(c, toSaleViewResponse(v))
		return
	}
	shared.Created(c, toSaleViewResponse(v))
}

func (h *Handler) Submit(c *gin.Context) {
	id, ok := param(c, "id", ErrSaleNotFound)
	if !ok {
		return
	}
	var req submitRequest
	if err := shared.BindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}
	v, err := h.svc.Submit(c.Request.Context(), shared.MustPrincipal(c), id, req.TxHash)
	if err != nil {
		fail(c, err)
		return
	}
	shared.OK(c, toSaleViewResponse(v))
}

func (h *Handler) GetSale(c *gin.Context) {
	id, ok := param(c, "id", ErrSaleNotFound)
	if !ok {
		return
	}
	v, err := h.svc.GetSale(c.Request.Context(), shared.MustPrincipal(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toSaleViewResponse(v))
}

func (h *Handler) Me(c *gin.Context) {
	in, err := h.svc.Inbox(c.Request.Context(), shared.MustPrincipal(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toInboxResponse(in))
}

func param(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) MakeOffer(c *gin.Context) {
	var req makeOfferRequest
	if err := shared.BindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}
	o, err := h.svc.MakeOffer(c.Request.Context(), shared.MustPrincipal(c), OfferInput{
		Name: req.Name, Amount: req.Amount, Chain: req.Chain, Asset: req.Asset, FromAddress: req.FromAddress, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		fail(c, err)
		return
	}
	shared.Created(c, toOfferResponse(o))
}

func (h *Handler) WithdrawOffer(c *gin.Context) {
	id, ok := param(c, "id", ErrOfferNotFound)
	if !ok {
		return
	}
	if err := h.svc.WithdrawOffer(c.Request.Context(), shared.MustPrincipal(c), id); err != nil {
		fail(c, err)
		return
	}
	shared.NoContent(c)
}

func (h *Handler) AcceptOffer(c *gin.Context) {
	id, ok := param(c, "id", ErrOfferNotFound)
	if !ok {
		return
	}
	var req acceptOfferRequest
	if !bindOptional(c, &req) {
		return
	}
	v, err := h.svc.AcceptOffer(c.Request.Context(), shared.MustPrincipal(c), id, req.PayoutAddress)
	if err != nil {
		fail(c, err)
		return
	}
	shared.Created(c, toSaleViewResponse(v))
}

func (h *Handler) DeclineOffer(c *gin.Context) {
	id, ok := param(c, "id", ErrOfferNotFound)
	if !ok {
		return
	}
	var req declineOfferRequest
	if !bindOptional(c, &req) {
		return
	}
	if err := h.svc.DeclineOffer(c.Request.Context(), shared.MustPrincipal(c), id, strings.TrimSpace(req.Note)); err != nil {
		fail(c, err)
		return
	}
	shared.NoContent(c)
}

func (h *Handler) PayOffer(c *gin.Context) {
	id, ok := param(c, "id", ErrOfferNotFound)
	if !ok {
		return
	}
	var req payOfferRequest
	if !bindOptional(c, &req) {
		return
	}
	v, err := h.svc.PayOffer(c.Request.Context(), shared.MustPrincipal(c), id, req.IdempotencyKey)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toSaleViewResponse(v))
}

// bindOptional binds a JSON body when one is sent; an empty body is fine.
func bindOptional(c *gin.Context, dst any) bool {
	if c.Request.ContentLength == 0 {
		return true
	}
	if err := shared.BindJSON(c, dst); err != nil {
		fail(c, err)
		return false
	}
	return true
}

// fail writes an error. A held agent action is not an error: nothing
// happened, and the agent's administrator decides (spec §8). It answers 202
// with {status: "pending_approval", authorizationId, reasons}.
func fail(c *gin.Context, err error) {
	var app *shared.AppError
	if errors.As(err, &app) && app.Code == ErrPendingApproval.Code {
		body := gin.H{"status": DecisionPending, "authorizationId": app.Details["authorizationId"], "reasons": app.Details["reasons"]}
		c.AbortWithStatusJSON(http.StatusAccepted, gin.H{"data": body})
		return
	}
	shared.Fail(c, err)
}

// ApproveAgentAction is POST /v1/agent-approvals/market/:authorizationId/approve
// (session, the agent's administrator).
func (h *Handler) ApproveAgentAction(c *gin.Context) {
	id, ok := param(c, "authorizationId", ErrAgentAuthorizationNotFound)
	if !ok {
		return
	}
	res, err := h.svc.ApproveAgentAction(c.Request.Context(), shared.MustPrincipal(c).AccountID, id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := agentActionResponse{Authorization: toAgentAuthorizationResponse(res.Authorization)}
	if res.Listing != nil {
		l := toListingResponse(*res.Listing)
		out.Listing = &l
	}
	if res.Sale != nil {
		v := toSaleViewResponse(*res.Sale)
		out.Sale = &v
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, out)
}

// ListAgentAuthorizations is GET /v1/agents/:agentId/market-authorizations
// ?decision= (session, the agent's administrator).
func (h *Handler) ListAgentAuthorizations(c *gin.Context) {
	agentID, ok := param(c, "agentId", agents.ErrAgentNotFound)
	if !ok {
		return
	}
	as, err := h.svc.ListAgentAuthorizations(c.Request.Context(), shared.MustPrincipal(c).AccountID, agentID, c.Query("decision"))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	out := make([]agentAuthorizationResponse, 0, len(as))
	for _, a := range as {
		out = append(out, toAgentAuthorizationResponse(a))
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, out)
}

// DenyAgentAction is POST /v1/agent-approvals/market/:authorizationId/deny.
func (h *Handler) DenyAgentAction(c *gin.Context) {
	id, ok := param(c, "authorizationId", ErrAgentAuthorizationNotFound)
	if !ok {
		return
	}
	a, err := h.svc.DenyAgentAction(c.Request.Context(), shared.MustPrincipal(c).AccountID, id)
	if err != nil {
		shared.Fail(c, err)
		return
	}
	shared.OK(c, agentActionResponse{Authorization: toAgentAuthorizationResponse(a)})
}

// Admin routes (spec §9 Admin): /v1/admin/market/..., session-only and
// limited to ADMIN_ACCOUNT_IDS by the router.

// adminFilter reads ?status=&accountId=&name=&limit= (limit at most 100).
func adminFilter(c *gin.Context) (AdminFilter, bool) {
	f := AdminFilter{Status: c.Query("status"), Name: c.Query("name")}
	if raw := c.Query("accountId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			fail(c, shared.ErrValidation.WithMessage("accountId must be a UUID"))
			return f, false
		}
		f.AccountID = &id
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			fail(c, shared.ErrValidation.WithMessage("limit must be between 1 and 100"))
			return f, false
		}
		f.Limit = n
	}
	return f, true
}

func (h *Handler) AdminListings(c *gin.Context) {
	f, ok := adminFilter(c)
	if !ok {
		return
	}
	ls, err := h.svc.AdminListings(c.Request.Context(), f)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminListingResponse, 0, len(ls))
	for _, l := range ls {
		out = append(out, toAdminListingResponse(l))
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, out)
}

func (h *Handler) AdminOffers(c *gin.Context) {
	f, ok := adminFilter(c)
	if !ok {
		return
	}
	os, err := h.svc.AdminOffers(c.Request.Context(), f)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminOfferResponse, 0, len(os))
	for _, o := range os {
		out = append(out, toAdminOfferResponse(o))
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, out)
}

func (h *Handler) AdminSales(c *gin.Context) {
	f, ok := adminFilter(c)
	if !ok {
		return
	}
	ss, err := h.svc.AdminSales(c.Request.Context(), f)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminSaleResponse, 0, len(ss))
	for _, s := range ss {
		out = append(out, toAdminSaleResponse(s))
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, out)
}

// adminReason binds an optional {reason} body; the service decides whether
// the reason is required.
func adminReason(c *gin.Context) (string, bool) {
	var req adminReasonRequest
	if !bindOptional(c, &req) {
		return "", false
	}
	return req.Reason, true
}

func (h *Handler) AdminDelist(c *gin.Context) {
	id, ok := param(c, "id", ErrListingNotFound)
	if !ok {
		return
	}
	reason, ok := adminReason(c)
	if !ok {
		return
	}
	l, err := h.svc.AdminDelist(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, reason)
	if err != nil {
		fail(c, err)
		return
	}
	shared.OK(c, toAdminListingResponse(l))
}

// adminSaleAction serves POST /v1/admin/market/sales/:id/{approve,deny,cancel,reverse}.
func (h *Handler) adminSaleAction(c *gin.Context, fn func(ctx context.Context, actor, saleID uuid.UUID, reason string) (Sale, error)) {
	id, ok := param(c, "id", ErrSaleNotFound)
	if !ok {
		return
	}
	reason, ok := adminReason(c)
	if !ok {
		return
	}
	s, err := fn(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, reason)
	if err != nil {
		fail(c, err)
		return
	}
	shared.OK(c, toAdminSaleResponse(s))
}

func (h *Handler) AdminApproveSale(c *gin.Context) { h.adminSaleAction(c, h.svc.AdminApprove) }
func (h *Handler) AdminDenySale(c *gin.Context)    { h.adminSaleAction(c, h.svc.AdminDeny) }
func (h *Handler) AdminCancelSale(c *gin.Context)  { h.adminSaleAction(c, h.svc.AdminCancel) }
func (h *Handler) AdminReverseSale(c *gin.Context) { h.adminSaleAction(c, h.svc.AdminReverse) }

func (h *Handler) AdminRestrict(c *gin.Context) {
	var req adminRestrictionRequest
	if err := shared.BindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}
	r, err := h.svc.AdminRestrict(c.Request.Context(), shared.MustPrincipal(c).AccountID, uuid.MustParse(req.AccountID),
		NewRestriction{Kind: req.Kind, Until: req.Until, Reason: req.Reason})
	if err != nil {
		fail(c, err)
		return
	}
	shared.Created(c, toRestrictionResponse(r, h.svc.now()))
}

func (h *Handler) AdminLiftRestriction(c *gin.Context) {
	id, ok := param(c, "id", ErrRestrictionNotFound)
	if !ok {
		return
	}
	reason, ok := adminReason(c)
	if !ok {
		return
	}
	r, err := h.svc.AdminLiftRestriction(c.Request.Context(), shared.MustPrincipal(c).AccountID, id, reason)
	if err != nil {
		fail(c, err)
		return
	}
	shared.OK(c, toRestrictionResponse(r, h.svc.now()))
}

// AdminRestrictions is GET /v1/admin/market/restrictions?accountId=&all=true
// (active only unless all=true), so an admin can find a restriction to lift.
func (h *Handler) AdminRestrictions(c *gin.Context) {
	f, ok := adminFilter(c)
	if !ok {
		return
	}
	rs, err := h.svc.AdminRestrictions(c.Request.Context(), f.AccountID, c.Query("all") == "true", f.Limit)
	if err != nil {
		fail(c, err)
		return
	}
	now := h.svc.now()
	out := make([]restrictionResponse, 0, len(rs))
	for _, r := range rs {
		out = append(out, toRestrictionResponse(r, now))
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, out)
}
