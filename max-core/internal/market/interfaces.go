package market

import (
	"context"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/shared"
)

// Listing is a market_listings row. Name is filled where the query joins it.
type Listing struct {
	ID, MaxID, SellerAccountID uuid.UUID
	Name                       string
	Chain, AssetAddress        string
	AssetDecimals              int
	PriceBaseUnits             string
	PayoutAddress              string
	Status, StatusReason       string
	ExpiresAt                  *time.Time
	CreatedAt, UpdatedAt       time.Time
}

// Sale is a market_sales snapshot. Name is filled where the query joins it.
type Sale struct {
	ID, MaxID                                          uuid.UUID
	Name                                               string
	ListingID, OfferID                                 *uuid.UUID
	SellerAccountID, BuyerAccountID                    uuid.UUID
	BuyerAddress, SellerPayoutAddress, TreasuryAddress string
	Chain, AssetAddress                                string
	AssetDecimals                                      int
	PriceBaseUnits                                     string
	FeeBPS                                             int
	FeeBaseUnits, SellerBaseUnits, QuotedNetworkFee    string
	IdempotencyKey                                     string
	Status, StatusReason                               string
	LockExpiresAt                                      *time.Time
	TxHash                                             *string
	Confirmations                                      int64
	SubmittedAt, CompletedAt, NextCheckAt              *time.Time
	CreatedAt, UpdatedAt                               time.Time
}

type NewListing struct {
	MaxID, Seller       uuid.UUID
	Chain, AssetAddress string
	AssetDecimals       int
	PriceBaseUnits      string
	PayoutAddress       string
	ExpiresAt           *time.Time
}

// NewSale is the snapshot OpenSale inserts. Exactly one of ListingID and
// OfferID is set.
type NewSale struct {
	MaxID                                      uuid.UUID
	ListingID, OfferID                         *uuid.UUID
	Seller, Buyer                              uuid.UUID
	BuyerAddress, SellerPayout, Treasury       string
	Chain, AssetAddress                        string
	AssetDecimals                              int
	Price, Fee, SellerAmount, QuotedNetworkFee string
	FeeBPS                                     int
	IdempotencyKey                             string
	Status                                     string
	LockExpiresAt                              *time.Time
}

// Progress is one guarded sale status move recorded by the observer.
type Progress struct {
	From, To, Reason string
	Confirmations    int64
	NextCheckAt      *time.Time
	Event            string
	Detail           map[string]any
}

// Expiry is one guarded move of a sale to expired. The sale must be in one of
// From; with LockedBefore set it must also be unpaid with its lock ended at or
// before that time. Otherwise ExpireSale changes nothing and returns the sale
// as it is.
type Expiry struct {
	From         []string
	LockedBefore *time.Time
	Reason       string
	Detail       map[string]any
}

// ListingQuery filters the public listing search. Prices are decimal asset amounts.
type ListingQuery struct {
	Q                  string
	MinPrice, MaxPrice string
	Sort               string // newest | price_asc | price_desc
	Cursor             string
	Limit              int
}

// ListingCard is a listing as shown in public search.
type ListingCard struct {
	ListingID, MaxID    uuid.UUID
	Name                string
	Chain, AssetAddress string
	AssetDecimals       int
	PriceBaseUnits      string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
}

// LastSale is the most recent completed sale of a name.
type LastSale struct {
	Chain, AssetAddress string
	AssetDecimals       int
	PriceBaseUnits      string
	CompletedAt         time.Time
}

// NameView is the public market view of one name (spec §6 Public read model).
// Listable and Reason are computed by the service.
type NameView struct {
	MaxID          uuid.UUID
	Name           string
	Facts          NameFacts
	Listing        *Listing
	LiveOfferCount int64
	LastSale       *LastSale
	Listable       bool
	Reason         string
}

// Inbox is the caller's own market activity. OffersReceived holds only live
// offers on names the caller owns; OffersMade holds all of the caller's own.
type Inbox struct {
	Listings       []Listing
	Sales          []Sale
	OffersReceived []Offer
	OffersMade     []Offer
}

// Offer is a market_offers row. Name is filled where the query joins it.
type Offer struct {
	ID, MaxID, BuyerAccountID        uuid.UUID
	Name                             string
	FromAddress, Chain, AssetAddress string
	AssetDecimals                    int
	AmountBaseUnits                  string
	Status, DeclineNote              string
	ExpiresAt                        time.Time
	FundedCheckedAt                  *time.Time
	CreatedAt, UpdatedAt             time.Time
}

type NewOffer struct {
	MaxID, Buyer                     uuid.UUID
	FromAddress, Chain, AssetAddress string
	AssetDecimals                    int
	AmountBaseUnits                  string
	ExpiresAt                        time.Time
}

type OfferInput struct {
	Name, Amount, Chain, Asset, FromAddress string
	ExpiresAt                               *time.Time
}

// Restriction kinds (spec §4 market_restrictions.kind).
const (
	RestrictionBan             = "ban"
	RestrictionOfferSuspension = "offer_suspension"
)

// Restriction is an active market_restrictions row.
type Restriction struct {
	ID, AccountID uuid.UUID
	Kind          string
	Until         *time.Time
	Reason        string
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
}

type NewRestriction struct {
	Kind   string
	Until  *time.Time
	Reason string
}

// SuspendRule decides, inside the lapse transaction, whether the buyer's
// strikes in the window warrant a restriction. suspended reports an active
// offer_suspension already in place.
type SuspendRule func(strikes int, suspended bool) *NewRestriction

// LapseResult is the outcome of LapseOffer. Suspension is set only when one
// was created by this lapse.
type LapseResult struct {
	Offer      Offer
	Strikes    int
	Suspension *Restriction
}

// Eligibility tells an owner whether, and on what terms, a name can be listed.
type Eligibility struct {
	MaxID                         uuid.UUID
	Listable                      bool
	Reason                        string
	ListableAt, RenameAvailableAt time.Time
	FeeBPS                        int
	MinPrice                      string
	Assets                        []Asset
}

type Repository interface {
	// NameFacts returns the listability facts and the current owner.
	NameFacts(ctx context.Context, maxID uuid.UUID) (NameFacts, uuid.UUID, error)
	// PartyFacts reads wallet history (any status) and agent administrators.
	PartyFacts(ctx context.Context, buyer, seller uuid.UUID, payer string) (PartyFacts, error)
	// ActiveWalletID returns the account's active wallet with this address in
	// the namespace, or uuid.Nil.
	ActiveWalletID(ctx context.Context, account uuid.UUID, ns chains.Namespace, address string) (uuid.UUID, error)
	Restricted(ctx context.Context, account uuid.UUID, kind string) (bool, error)

	// CreateListing locks the max_ids row, re-checks listability under that
	// lock and inserts an active listing.
	CreateListing(ctx context.Context, in NewListing) (Listing, error)
	GetListing(ctx context.Context, id uuid.UUID) (Listing, error)
	UpdateListingPrice(ctx context.Context, id, seller uuid.UUID, price string) (Listing, error)
	CancelListing(ctx context.Context, id, seller uuid.UUID) error

	// OpenSale locks the max_ids row and the listing, refuses if an open sale
	// exists (ErrNameLocked), inserts the sale and its event and locks the listing.
	OpenSale(ctx context.Context, in NewSale) (Sale, error)
	SaleByIdempotencyKey(ctx context.Context, buyer uuid.UUID, key string) (*Sale, error)
	// AttachTx moves a locked sale to submitted. A tx hash already used by
	// another sale maps to ErrTxMismatch.
	AttachTx(ctx context.Context, saleID, buyer uuid.UUID, txHash string) (Sale, error)
	ProgressSale(ctx context.Context, saleID uuid.UUID, p Progress) (Sale, error)
	// CompleteSale performs spec §6 Complete in one transaction. When the
	// ownership transfer is refused (owner changed, rename pending) the sale
	// moves to manual_review instead and is returned without error.
	CompleteSale(ctx context.Context, saleID uuid.UUID, hold time.Duration, confirmations int64) (Sale, error)
	// ExpireSale closes a sale as expired under the max_ids and sale locks when
	// e's guard holds, and returns a locked listing on the name to active. An
	// offer sale that had a transaction closes its accepted offer as expired
	// (no strike); an unpaid one is left for LapseAcceptedOffer.
	ExpireSale(ctx context.Context, saleID uuid.UUID, e Expiry) (Sale, error)
	// AttachLateTx records a transaction for an expired, unpaid sale (spec §6
	// Expiry and late payment). Under the max_ids lock: when the seller still
	// owns the name and no other sale is open on it the sale moves to
	// confirming and locks the name's active listing; otherwise it moves to
	// manual_review, because the buyer may have paid.
	AttachLateTx(ctx context.Context, saleID, buyer uuid.UUID, txHash string) (Sale, error)
	GetSale(ctx context.Context, id uuid.UUID) (Sale, error)

	SearchListings(ctx context.Context, q ListingQuery) ([]ListingCard, string, error)
	NameView(ctx context.Context, name string) (NameView, error)
	Inbox(ctx context.Context, account uuid.UUID) (Inbox, error)

	// Offers (spec §6 Offer).
	MaxIDByName(ctx context.Context, name string) (uuid.UUID, error)
	// ActiveRestriction returns the longest-running active restriction of a
	// kind, or nil.
	ActiveRestriction(ctx context.Context, account uuid.UUID, kind string) (*Restriction, error)
	Restrict(ctx context.Context, account uuid.UUID, kind string, until *time.Time, reason string, by *uuid.UUID) error
	// OpenOfferCount counts the buyer's live and unfunded offers.
	OpenOfferCount(ctx context.Context, buyer uuid.UUID) (int, error)
	// CreateOffer re-checks the open-offer limit and the one-open-offer-per-name
	// rule under a per-buyer lock and inserts a live offer.
	CreateOffer(ctx context.Context, in NewOffer, maxOpen int) (Offer, error)
	GetOffer(ctx context.Context, id uuid.UUID) (Offer, error)
	WithdrawOffer(ctx context.Context, id, buyer uuid.UUID) error
	// DeclineOffer locks the name, checks seller still owns it and declines a
	// live or unfunded offer.
	DeclineOffer(ctx context.Context, id, seller uuid.UUID, note string) error
	// LiveListing returns the active or locked listing on a name, or nil.
	LiveListing(ctx context.Context, maxID uuid.UUID) (*Listing, error)
	// AcceptOffer, in one transaction: locks the name and re-checks
	// listability, accepts the live offer, supersedes the other live and
	// unfunded offers on the name, locks the listing if any, and opens the sale.
	AcceptOffer(ctx context.Context, offerID, seller uuid.UUID, in NewSale) (Sale, error)
	SaleByOffer(ctx context.Context, offerID uuid.UUID) (Sale, error)
	// SetOfferFunded flips a live or unfunded offer; closed offers are left alone.
	SetOfferFunded(ctx context.Context, id uuid.UUID, funded bool, checkedAt time.Time) error
	// DueFundingChecks claims live and unfunded offers last checked before
	// olderThan (FOR UPDATE SKIP LOCKED), stamping them so no other worker
	// claims them while the caller checks funds.
	DueFundingChecks(ctx context.Context, olderThan time.Time, limit int) ([]Offer, error)
	// ExpireOffers closes live and unfunded offers past expires_at.
	ExpireOffers(ctx context.Context, limit int) (int, error)
	// LapseOffer, in one transaction: accepted offer -> lapsed, records a
	// strike, counts strikes since strikesSince and applies rule. Lapsing an
	// already lapsed offer is a no-op.
	LapseOffer(ctx context.Context, offerID uuid.UUID, strikesSince time.Time, rule SuspendRule) (LapseResult, error)

	// Worker (Task 9).
	// ClaimDueSales leases submitted and confirming sales whose next_check_at
	// has passed, pushing it forward by lease (FOR UPDATE SKIP LOCKED).
	ClaimDueSales(ctx context.Context, limit int, lease time.Duration) ([]Sale, error)
	// ClaimSalesPastDeadline leases submitted and confirming sales whose
	// deadline anchor (see txDeadline) is at or before cutoff and that no
	// worker holds.
	ClaimSalesPastDeadline(ctx context.Context, cutoff time.Time, limit int, lease time.Duration) ([]Sale, error)
	// LeaseSale pushes a submitted or confirming sale's next_check_at to
	// now + lease; false means the sale is no longer submitted or confirming.
	LeaseSale(ctx context.Context, saleID uuid.UUID, lease time.Duration) (bool, error)
	// ExpiredLocks lists unpaid locked sales whose lock ended at or before cutoff.
	ExpiredLocks(ctx context.Context, cutoff time.Time, limit int) ([]Sale, error)
	// UnlapsedOffers lists accepted offers whose sale expired unpaid.
	UnlapsedOffers(ctx context.Context, limit int) ([]uuid.UUID, error)
	// ExpireListings cancels active listings past expires_at (reason "expired").
	ExpireListings(ctx context.Context, cutoff time.Time, limit int) (int, error)
}

// Agent policy actions (agent_market_authorizations.action).
const (
	ActionBuy    = "buy"
	ActionOffer  = "offer"
	ActionList   = "list"
	ActionAccept = "accept"
	ActionPay    = "pay"
)

// Decision outcomes. DecisionApproved means "go ahead": a human, an
// agent within policy, or an administrator's approval.
const (
	DecisionApproved = "approved"
	DecisionPending  = "pending_approval"
	DecisionDenied   = "denied"
)

// AuthRef names the market object an authorization is about.
type AuthRef struct {
	MaxID, ListingID, OfferID, SaleID *uuid.UUID
}

type Decision struct {
	Outcome         string
	AuthorizationID *uuid.UUID
	Reasons         []string
}

// AgentPolicy decides whether a principal may perform a market action now
// (spec §8).
type AgentPolicy interface {
	// Authorize decides an action. in is everything needed to replay the
	// action if it is held for approval. A decision with an AuthorizationID
	// must be followed by Finish once the action has run.
	Authorize(ctx context.Context, p shared.Principal, action string, amount *big.Int, chain, asset string, ref AuthRef, in ActionInput) (Decision, error)
	// Finish records what an authorized action created, or the error that
	// stopped it (a failed action never counts toward spend).
	Finish(ctx context.Context, authorizationID uuid.UUID, ref AuthRef, failure error) error
	// Resolve moves a pending authorization to approved or denied for the
	// agent's administrator; anyone else gets ErrAgentAuthorizationNotFound.
	Resolve(ctx context.Context, admin, authorizationID uuid.UUID, decision string) (AgentAuthorization, error)
}

// AllowAll approves every action and has no authorizations. Tests and
// deployments without agents use it.
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, shared.Principal, string, *big.Int, string, string, AuthRef, ActionInput) (Decision, error) {
	return Decision{Outcome: DecisionApproved}, nil
}

func (AllowAll) Finish(context.Context, uuid.UUID, AuthRef, error) error { return nil }

func (AllowAll) Resolve(context.Context, uuid.UUID, uuid.UUID, string) (AgentAuthorization, error) {
	return AgentAuthorization{}, ErrAgentAuthorizationNotFound
}

// Replayable operations stored in an authorization's payload.
const (
	OpList        = "list"
	OpUpdatePrice = "update_price"
	OpBuy         = "buy"
	OpAccept      = "accept"
	OpPay         = "pay"
)

// ActionInput is an agent action's full input, stored with its authorization
// so an administrator's approval can replay it exactly.
type ActionInput struct {
	Op            string     `json:"op"`
	List          *ListInput `json:"list,omitempty"`
	ListingID     *uuid.UUID `json:"listingId,omitempty"`
	OfferID       *uuid.UUID `json:"offerId,omitempty"`
	Price         string     `json:"price,omitempty"`
	Buy           *BuyInput  `json:"buy,omitempty"`
	PayoutAddress string     `json:"payoutAddress,omitempty"`
	// IdempotencyKey is pay's key (accepted for API symmetry).
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

// AgentAuthorization is an agent_market_authorizations row.
type AgentAuthorization struct {
	ID, AgentID, CredentialID   uuid.UUID
	Action, Decision            string
	Reasons                     []string
	PolicyVersion               int
	Chain, AssetAddress, Amount string
	SaleID, OfferID, ListingID  *uuid.UUID
	Input                       ActionInput
	Failure                     string
	DecidedBy                   *uuid.UUID
	DecidedAt, FinishedAt       *time.Time
	CreatedAt                   time.Time
}

type NewAgentAuthorization struct {
	AgentID, CredentialID uuid.UUID
	Action, Decision      string
	Reasons               []string
	PolicyVersion         int
	Chain, Asset, Amount  string
	Ref                   AuthRef
	Input                 ActionInput
}

// AgentActionResult is an approved agent action after its replay.
type AgentActionResult struct {
	Authorization AgentAuthorization
	Listing       *Listing
	Sale          *SaleView
}

// QuoteView is the buyer-facing cost of a sale as decimal strings.
type QuoteView struct {
	Price, Fee, SellerReceives string // asset
	NetworkFee                 string // native coin
	TotalAsset, TotalNative    string
}

type SaleView struct {
	Sale         Sale
	Quote        *QuoteView
	Instructions *settle.Instructions
	// Replayed is true when an idempotency key returned an existing sale.
	Replayed bool
}

type ListInput struct {
	MaxID      uuid.UUID  `json:"maxId"`
	Price      string     `json:"price"`
	Chain      string     `json:"chain"`
	Asset      string     `json:"asset"`
	PayoutAddr string     `json:"payoutAddress"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type BuyInput struct {
	FromAddress    string `json:"fromAddress"`
	IdempotencyKey string `json:"idempotencyKey"`
}
