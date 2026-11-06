package market

import (
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
)

type createListingRequest struct {
	MaxID         string     `json:"maxId" binding:"required,uuid"`
	Price         string     `json:"price" binding:"required,max=80"`
	Chain         string     `json:"chain" binding:"required,max=32"`
	Asset         string     `json:"asset" binding:"required,max=128"`
	PayoutAddress string     `json:"payoutAddress" binding:"required,max=128"`
	ExpiresAt     *time.Time `json:"expiresAt"`
}

type updateListingRequest struct {
	Price string `json:"price" binding:"required,max=80"`
}

type buyRequest struct {
	FromAddress    string `json:"fromAddress" binding:"required,max=128"`
	IdempotencyKey string `json:"idempotencyKey" binding:"required,min=8,max=64,printascii"`
}

type makeOfferRequest struct {
	Name        string     `json:"name" binding:"required,max=128"`
	Amount      string     `json:"amount" binding:"required,max=80"`
	Chain       string     `json:"chain" binding:"required,max=32"`
	Asset       string     `json:"asset" binding:"required,max=128"`
	FromAddress string     `json:"fromAddress" binding:"required,max=128"`
	ExpiresAt   *time.Time `json:"expiresAt"`
}

// acceptOfferRequest: payoutAddress is required only when the name has no
// listing whose payout address can be used.
type acceptOfferRequest struct {
	PayoutAddress string `json:"payoutAddress" binding:"max=128"`
}

type declineOfferRequest struct {
	Note string `json:"note" binding:"max=280"`
}

type payOfferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"omitempty,min=8,max=64,printascii"`
}

type submitRequest struct {
	TxHash string `json:"txHash" binding:"required,max=128"`
}

type assetResponse struct {
	Chain    string `json:"chain"`
	Asset    string `json:"asset"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}

type listingResponse struct {
	ID             string     `json:"id"`
	MaxID          string     `json:"maxId"`
	Handle         string     `json:"handle,omitempty"`
	Chain          string     `json:"chain"`
	Asset          string     `json:"asset"`
	Symbol         string     `json:"symbol"`
	Price          string     `json:"price"`
	PriceBaseUnits string     `json:"priceBaseUnits"`
	PayoutAddress  string     `json:"payoutAddress"`
	Status         string     `json:"status"`
	StatusReason   string     `json:"statusReason,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// publicListingResponse is a listing as anyone may see it: no payout wallet,
// no internal status reasons.
type publicListingResponse struct {
	ID        string     `json:"id"`
	Chain     string     `json:"chain"`
	Asset     string     `json:"asset"`
	Symbol    string     `json:"symbol"`
	Price     string     `json:"price"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type saleResponse struct {
	ID                  string     `json:"id"`
	MaxID               string     `json:"maxId"`
	Handle              string     `json:"handle,omitempty"`
	ListingID           string     `json:"listingId,omitempty"`
	OfferID             string     `json:"offerId,omitempty"`
	BuyerAddress        string     `json:"buyerAddress"`
	SellerPayoutAddress string     `json:"sellerPayoutAddress"`
	TreasuryAddress     string     `json:"treasuryAddress"`
	Chain               string     `json:"chain"`
	Asset               string     `json:"asset"`
	Symbol              string     `json:"symbol"`
	Price               string     `json:"price"`
	PriceBaseUnits      string     `json:"priceBaseUnits"`
	FeeBPS              int        `json:"feeBps"`
	Fee                 string     `json:"fee"`
	SellerReceives      string     `json:"sellerReceives"`
	Status              string     `json:"status"`
	StatusReason        string     `json:"statusReason,omitempty"`
	LockExpiresAt       *time.Time `json:"lockExpiresAt,omitempty"`
	TxHash              string     `json:"txHash,omitempty"`
	Confirmations       int64      `json:"confirmations"`
	SubmittedAt         *time.Time `json:"submittedAt,omitempty"`
	CompletedAt         *time.Time `json:"completedAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

type quoteResponse struct {
	Price          string `json:"price"`
	Fee            string `json:"fee"`
	SellerReceives string `json:"sellerReceives"`
	NetworkFee     string `json:"networkFee"`
	TotalAsset     string `json:"totalAsset"`
	TotalNative    string `json:"totalNative"`
}

type solanaIxResponse struct {
	Kind     string            `json:"kind"`
	Accounts map[string]string `json:"accounts,omitempty"`
	Amount   string            `json:"amount,omitempty"`
	Decimals int               `json:"decimals,omitempty"`
	Memo     string            `json:"memo,omitempty"`
}

type instructionsResponse struct {
	Solana []solanaIxResponse `json:"solana,omitempty"`
}

type saleViewResponse struct {
	Sale         saleResponse          `json:"sale"`
	Quote        *quoteResponse        `json:"quote,omitempty"`
	Instructions *instructionsResponse `json:"instructions,omitempty"`
}

type cardResponse struct {
	ListingID      string     `json:"listingId"`
	MaxID          string     `json:"maxId"`
	Handle         string     `json:"handle"`
	Chain          string     `json:"chain"`
	Asset          string     `json:"asset"`
	Symbol         string     `json:"symbol"`
	Price          string     `json:"price"`
	PriceBaseUnits string     `json:"priceBaseUnits"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type searchResponse struct {
	Listings   []cardResponse `json:"listings"`
	NextCursor string         `json:"nextCursor,omitempty"`
}

type lastSaleResponse struct {
	Chain       string    `json:"chain"`
	Asset       string    `json:"asset"`
	Symbol      string    `json:"symbol"`
	Price       string    `json:"price"`
	CompletedAt time.Time `json:"completedAt"`
}

type nameViewResponse struct {
	MaxID          string                 `json:"maxId"`
	Handle         string                 `json:"handle"`
	Listable       bool                   `json:"listable"`
	Reason         string                 `json:"reason,omitempty"`
	Listing        *publicListingResponse `json:"listing,omitempty"`
	LiveOfferCount int64                  `json:"liveOfferCount"`
	LastSale       *lastSaleResponse      `json:"lastSale,omitempty"`
}

type eligibilityResponse struct {
	MaxID             string          `json:"maxId"`
	Listable          bool            `json:"listable"`
	Reason            string          `json:"reason,omitempty"`
	ListableAt        time.Time       `json:"listableAt"`
	RenameAvailableAt time.Time       `json:"renameAvailableAt"`
	FeeBPS            int             `json:"feeBps"`
	MinPrice          string          `json:"minPrice"`
	Assets            []assetResponse `json:"assets"`
}

// offerResponse is the buyer's own view of an offer.
type offerResponse struct {
	ID              string     `json:"id"`
	MaxID           string     `json:"maxId"`
	Handle          string     `json:"handle,omitempty"`
	FromAddress     string     `json:"fromAddress"`
	Chain           string     `json:"chain"`
	Asset           string     `json:"asset"`
	Symbol          string     `json:"symbol"`
	Amount          string     `json:"amount"`
	AmountBaseUnits string     `json:"amountBaseUnits"`
	Status          string     `json:"status"`
	DeclineNote     string     `json:"declineNote,omitempty"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	FundedCheckedAt *time.Time `json:"fundedCheckedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// receivedOfferResponse is a live offer as the name's owner sees it: the
// terms, not the buyer's wallet.
type receivedOfferResponse struct {
	ID              string    `json:"id"`
	MaxID           string    `json:"maxId"`
	Handle          string    `json:"handle,omitempty"`
	Chain           string    `json:"chain"`
	Asset           string    `json:"asset"`
	Symbol          string    `json:"symbol"`
	Amount          string    `json:"amount"`
	AmountBaseUnits string    `json:"amountBaseUnits"`
	Status          string    `json:"status"`
	ExpiresAt       time.Time `json:"expiresAt"`
	CreatedAt       time.Time `json:"createdAt"`
}

type inboxResponse struct {
	Listings       []listingResponse       `json:"listings"`
	Sales          []saleResponse          `json:"sales"`
	OffersReceived []receivedOfferResponse `json:"offersReceived"`
	OffersMade     []offerResponse         `json:"offersMade"`
}

// amount renders base units as a decimal string of the asset.
func amount(baseUnits string, decimals int) string {
	v, ok := new(big.Int).SetString(baseUnits, 10)
	if !ok {
		return "0"
	}
	return payments.FormatAmount(v, decimals)
}

// assetLabel returns the display address and symbol of a chain asset.
func assetLabel(chainKey, addr string) (string, string) {
	chain, ok := chains.Lookup(chainKey)
	if !ok {
		return addr, ""
	}
	sym := ""
	if t, ok := chains.LookupVerifiedToken(chain.Key, addr); ok {
		sym = t.Symbol
	}
	return chains.DisplayAddress(chain.Namespace, addr), sym
}

func handle(name string) string {
	if name == "" {
		return ""
	}
	return maxids.Handle(name)
}

func toListingResponse(l Listing) listingResponse {
	asset, sym := assetLabel(l.Chain, l.AssetAddress)
	return listingResponse{
		ID: l.ID.String(), MaxID: l.MaxID.String(), Handle: handle(l.Name), Chain: l.Chain, Asset: asset, Symbol: sym,
		Price: amount(l.PriceBaseUnits, l.AssetDecimals), PriceBaseUnits: l.PriceBaseUnits, PayoutAddress: l.PayoutAddress,
		Status: l.Status, StatusReason: l.StatusReason, ExpiresAt: l.ExpiresAt, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func toSaleResponse(s Sale) saleResponse {
	asset, sym := assetLabel(s.Chain, s.AssetAddress)
	out := saleResponse{
		ID: s.ID.String(), MaxID: s.MaxID.String(), Handle: handle(s.Name), BuyerAddress: s.BuyerAddress,
		SellerPayoutAddress: s.SellerPayoutAddress, TreasuryAddress: s.TreasuryAddress, Chain: s.Chain, Asset: asset, Symbol: sym,
		Price: amount(s.PriceBaseUnits, s.AssetDecimals), PriceBaseUnits: s.PriceBaseUnits, FeeBPS: s.FeeBPS,
		Fee: amount(s.FeeBaseUnits, s.AssetDecimals), SellerReceives: amount(s.SellerBaseUnits, s.AssetDecimals),
		Status: s.Status, StatusReason: s.StatusReason, LockExpiresAt: s.LockExpiresAt, Confirmations: s.Confirmations,
		SubmittedAt: s.SubmittedAt, CompletedAt: s.CompletedAt, CreatedAt: s.CreatedAt,
	}
	if s.ListingID != nil {
		out.ListingID = s.ListingID.String()
	}
	if s.OfferID != nil {
		out.OfferID = s.OfferID.String()
	}
	if s.TxHash != nil {
		out.TxHash = *s.TxHash
	}
	return out
}

func toSaleViewResponse(v SaleView) saleViewResponse {
	out := saleViewResponse{Sale: toSaleResponse(v.Sale)}
	if q := v.Quote; q != nil {
		out.Quote = &quoteResponse{Price: q.Price, Fee: q.Fee, SellerReceives: q.SellerReceives, NetworkFee: q.NetworkFee,
			TotalAsset: q.TotalAsset, TotalNative: q.TotalNative}
	}
	if v.Instructions != nil {
		ins := &instructionsResponse{Solana: make([]solanaIxResponse, 0, len(v.Instructions.Solana))}
		for _, ix := range v.Instructions.Solana {
			ins.Solana = append(ins.Solana, solanaIxResponse{Kind: ix.Kind, Accounts: ix.Accounts, Amount: ix.Amount, Decimals: ix.Decimals, Memo: ix.Memo})
		}
		out.Instructions = ins
	}
	return out
}

func toCardResponse(c ListingCard) cardResponse {
	asset, sym := assetLabel(c.Chain, c.AssetAddress)
	return cardResponse{
		ListingID: c.ListingID.String(), MaxID: c.MaxID.String(), Handle: handle(c.Name), Chain: c.Chain, Asset: asset, Symbol: sym,
		Price: amount(c.PriceBaseUnits, c.AssetDecimals), PriceBaseUnits: c.PriceBaseUnits, ExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt,
	}
}

func toNameViewResponse(v NameView) nameViewResponse {
	out := nameViewResponse{MaxID: v.MaxID.String(), Handle: handle(v.Name), Listable: v.Listable, Reason: v.Reason, LiveOfferCount: v.LiveOfferCount}
	if v.Listing != nil {
		l := *v.Listing
		asset, sym := assetLabel(l.Chain, l.AssetAddress)
		out.Listing = &publicListingResponse{
			ID: l.ID.String(), Chain: l.Chain, Asset: asset, Symbol: sym, Price: amount(l.PriceBaseUnits, l.AssetDecimals),
			Status: l.Status, ExpiresAt: l.ExpiresAt, CreatedAt: l.CreatedAt,
		}
	}
	if s := v.LastSale; s != nil {
		asset, sym := assetLabel(s.Chain, s.AssetAddress)
		out.LastSale = &lastSaleResponse{Chain: s.Chain, Asset: asset, Symbol: sym, Price: amount(s.PriceBaseUnits, s.AssetDecimals), CompletedAt: s.CompletedAt}
	}
	return out
}

func toEligibilityResponse(e Eligibility) eligibilityResponse {
	out := eligibilityResponse{
		MaxID: e.MaxID.String(), Listable: e.Listable, Reason: e.Reason, ListableAt: e.ListableAt,
		RenameAvailableAt: e.RenameAvailableAt, FeeBPS: e.FeeBPS, MinPrice: e.MinPrice, Assets: make([]assetResponse, 0, len(e.Assets)),
	}
	for _, a := range e.Assets {
		out.Assets = append(out.Assets, assetResponse{Chain: a.Chain.Key, Asset: chains.DisplayAddress(a.Chain.Namespace, a.Address), Symbol: a.Symbol, Decimals: a.Decimals})
	}
	return out
}

func toOfferResponse(o Offer) offerResponse {
	asset, sym := assetLabel(o.Chain, o.AssetAddress)
	return offerResponse{
		ID: o.ID.String(), MaxID: o.MaxID.String(), Handle: handle(o.Name), FromAddress: o.FromAddress, Chain: o.Chain,
		Asset: asset, Symbol: sym, Amount: amount(o.AmountBaseUnits, o.AssetDecimals), AmountBaseUnits: o.AmountBaseUnits,
		Status: o.Status, DeclineNote: o.DeclineNote, ExpiresAt: o.ExpiresAt, FundedCheckedAt: o.FundedCheckedAt, CreatedAt: o.CreatedAt,
	}
}

func toReceivedOfferResponse(o Offer) receivedOfferResponse {
	asset, sym := assetLabel(o.Chain, o.AssetAddress)
	return receivedOfferResponse{
		ID: o.ID.String(), MaxID: o.MaxID.String(), Handle: handle(o.Name), Chain: o.Chain, Asset: asset, Symbol: sym,
		Amount: amount(o.AmountBaseUnits, o.AssetDecimals), AmountBaseUnits: o.AmountBaseUnits, Status: o.Status,
		ExpiresAt: o.ExpiresAt, CreatedAt: o.CreatedAt,
	}
}

func toInboxResponse(in Inbox) inboxResponse {
	out := inboxResponse{Listings: make([]listingResponse, 0, len(in.Listings)), Sales: make([]saleResponse, 0, len(in.Sales)),
		OffersReceived: make([]receivedOfferResponse, 0, len(in.OffersReceived)), OffersMade: make([]offerResponse, 0, len(in.OffersMade))}
	for _, o := range in.OffersReceived {
		out.OffersReceived = append(out.OffersReceived, toReceivedOfferResponse(o))
	}
	for _, o := range in.OffersMade {
		out.OffersMade = append(out.OffersMade, toOfferResponse(o))
	}
	for _, l := range in.Listings {
		out.Listings = append(out.Listings, toListingResponse(l))
	}
	for _, s := range in.Sales {
		out.Sales = append(out.Sales, toSaleResponse(s))
	}
	return out
}

type agentAuthorizationResponse struct {
	ID              string     `json:"id"`
	AgentID         string     `json:"agentId"`
	Action          string     `json:"action"`
	Decision        string     `json:"decision"`
	Reasons         []string   `json:"reasons"`
	PolicyVersion   int        `json:"policyVersion"`
	Chain           string     `json:"chain"`
	Asset           string     `json:"asset"`
	AmountBaseUnits string     `json:"amountBaseUnits"`
	SaleID          *string    `json:"saleId,omitempty"`
	OfferID         *string    `json:"offerId,omitempty"`
	ListingID       *string    `json:"listingId,omitempty"`
	Failure         string     `json:"failure,omitempty"`
	DecidedAt       *time.Time `json:"decidedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	// Summary says what the held action would do.
	Summary agentActionSummary `json:"summary"`
}

// agentActionSummary is the readable part of an authorization's payload.
// Price (buy, list) and Amount (accept, pay) are in base units.
type agentActionSummary struct {
	Op        string  `json:"op"`
	Name      string  `json:"name,omitempty"`
	ListingID *string `json:"listingId,omitempty"`
	OfferID   *string `json:"offerId,omitempty"`
	SaleID    *string `json:"saleId,omitempty"`
	Price     string  `json:"price,omitempty"`
	Amount    string  `json:"amount,omitempty"`
}

type agentActionResponse struct {
	Authorization agentAuthorizationResponse `json:"authorization"`
	Listing       *listingResponse           `json:"listing,omitempty"`
	Sale          *saleViewResponse          `json:"sale,omitempty"`
}

func toAgentAuthorizationResponse(a AgentAuthorization) agentAuthorizationResponse {
	idStr := func(id *uuid.UUID) *string {
		if id == nil {
			return nil
		}
		s := id.String()
		return &s
	}
	return agentAuthorizationResponse{
		ID: a.ID.String(), AgentID: a.AgentID.String(), Action: a.Action, Decision: a.Decision, Reasons: orEmpty(a.Reasons),
		PolicyVersion: a.PolicyVersion, Chain: a.Chain, Asset: a.AssetAddress, AmountBaseUnits: a.Amount,
		SaleID: idStr(a.SaleID), OfferID: idStr(a.OfferID), ListingID: idStr(a.ListingID), Failure: a.Failure,
		DecidedAt: a.DecidedAt, CreatedAt: a.CreatedAt, Summary: summarize(a, idStr),
	}
}

func summarize(a AgentAuthorization, idStr func(*uuid.UUID) *string) agentActionSummary {
	in := a.Input
	sum := agentActionSummary{Op: in.Op, Name: a.Name}
	listingID, offerID := in.ListingID, in.OfferID
	if listingID == nil {
		listingID = a.ListingID
	}
	if offerID == nil {
		offerID = a.OfferID
	}
	switch in.Op {
	case OpBuy, OpList, OpUpdatePrice:
		sum.ListingID, sum.Price = idStr(listingID), a.Amount
	case OpAccept:
		sum.OfferID, sum.Amount = idStr(offerID), a.Amount
	case OpPay:
		sum.OfferID, sum.SaleID, sum.Amount = idStr(offerID), idStr(a.SaleID), a.Amount
	}
	return sum
}

// Admin views (spec §9 Admin) show full data, party accounts included.

type adminReasonRequest struct {
	Reason string `json:"reason" binding:"max=500"`
}

type adminRestrictionRequest struct {
	AccountID string     `json:"accountId" binding:"required,uuid"`
	Kind      string     `json:"kind" binding:"required,oneof=ban offer_suspension"`
	Until     *time.Time `json:"until"`
	Reason    string     `json:"reason" binding:"required,max=500"`
}

type adminListingResponse struct {
	listingResponse
	SellerAccountID string `json:"sellerAccountId"`
}

type adminOfferResponse struct {
	offerResponse
	BuyerAccountID string    `json:"buyerAccountId"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type adminSaleResponse struct {
	saleResponse
	SellerAccountID  string     `json:"sellerAccountId"`
	BuyerAccountID   string     `json:"buyerAccountId"`
	QuotedNetworkFee string     `json:"quotedNetworkFee"`
	NextCheckAt      *time.Time `json:"nextCheckAt,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

type restrictionResponse struct {
	ID        string     `json:"id"`
	AccountID string     `json:"accountId"`
	Kind      string     `json:"kind"`
	Until     *time.Time `json:"until,omitempty"`
	Reason    string     `json:"reason"`
	CreatedBy string     `json:"createdBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	LiftedAt  *time.Time `json:"liftedAt,omitempty"`
	Active    bool       `json:"active"`
}

func toAdminListingResponse(l Listing) adminListingResponse {
	return adminListingResponse{listingResponse: toListingResponse(l), SellerAccountID: l.SellerAccountID.String()}
}

func toAdminOfferResponse(o Offer) adminOfferResponse {
	return adminOfferResponse{offerResponse: toOfferResponse(o), BuyerAccountID: o.BuyerAccountID.String(), UpdatedAt: o.UpdatedAt}
}

func toAdminSaleResponse(s Sale) adminSaleResponse {
	return adminSaleResponse{
		saleResponse: toSaleResponse(s), SellerAccountID: s.SellerAccountID.String(), BuyerAccountID: s.BuyerAccountID.String(),
		QuotedNetworkFee: s.QuotedNetworkFee, NextCheckAt: s.NextCheckAt, UpdatedAt: s.UpdatedAt,
	}
}

func toRestrictionResponse(r Restriction, now time.Time) restrictionResponse {
	out := restrictionResponse{
		ID: r.ID.String(), AccountID: r.AccountID.String(), Kind: r.Kind, Until: r.Until, Reason: r.Reason,
		CreatedAt: r.CreatedAt, LiftedAt: r.LiftedAt, Active: restrictionActive(r, now),
	}
	if r.CreatedBy != nil {
		out.CreatedBy = r.CreatedBy.String()
	}
	return out
}
