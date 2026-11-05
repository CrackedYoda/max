package market

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

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
			shared.Fail(c, ErrMarketDisabled)
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
			shared.Fail(c, ErrMarketPaused)
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
		shared.Fail(c, err)
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
		shared.Fail(c, err)
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
		shared.Fail(c, err)
		return
	}
	shared.OK(c, toEligibilityResponse(e))
}

func (h *Handler) CreateListing(c *gin.Context) {
	var req createListingRequest
	if err := shared.BindJSON(c, &req); err != nil {
		shared.Fail(c, err)
		return
	}
	maxID, err := uuid.Parse(req.MaxID)
	if err != nil {
		shared.Fail(c, maxids.ErrMaxIDNotFound)
		return
	}
	l, err := h.svc.List(c.Request.Context(), shared.MustPrincipal(c), ListInput{
		MaxID: maxID, Price: req.Price, Chain: req.Chain, Asset: req.Asset,
		PayoutAddr: req.PayoutAddress, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		shared.Fail(c, err)
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
		shared.Fail(c, err)
		return
	}
	l, err := h.svc.UpdatePrice(c.Request.Context(), shared.MustPrincipal(c), id, req.Price)
	if err != nil {
		shared.Fail(c, err)
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
		shared.Fail(c, err)
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
		shared.Fail(c, err)
		return
	}
	v, err := h.svc.Buy(c.Request.Context(), shared.MustPrincipal(c), id, BuyInput{FromAddress: req.FromAddress, IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		shared.Fail(c, err)
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
		shared.Fail(c, err)
		return
	}
	v, err := h.svc.Submit(c.Request.Context(), shared.MustPrincipal(c), id, req.TxHash)
	if err != nil {
		shared.Fail(c, err)
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
		shared.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toSaleViewResponse(v))
}

func (h *Handler) Me(c *gin.Context) {
	in, err := h.svc.Inbox(c.Request.Context(), shared.MustPrincipal(c))
	if err != nil {
		shared.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	shared.OK(c, toInboxResponse(in))
}

func param(c *gin.Context, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		shared.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}
