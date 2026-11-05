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

// Inbox is the caller's own market activity. Offers join it in Task 8.
type Inbox struct {
	Listings []Listing
	Sales    []Sale
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
	// ExpireSale closes a locked, submitted or confirming sale as expired and
	// returns a locked listing on the name to active.
	ExpireSale(ctx context.Context, saleID uuid.UUID, reason string) (Sale, error)
	GetSale(ctx context.Context, id uuid.UUID) (Sale, error)

	SearchListings(ctx context.Context, q ListingQuery) ([]ListingCard, string, error)
	NameView(ctx context.Context, name string) (NameView, error)
	Inbox(ctx context.Context, account uuid.UUID) (Inbox, error)
}

// Agent policy actions.
const (
	ActionBuy    = "buy"
	ActionOffer  = "offer"
	ActionList   = "list"
	ActionAccept = "accept"
)

// Decision outcomes.
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
// (spec §8). Task 10 implements it for agent credentials.
type AgentPolicy interface {
	Authorize(ctx context.Context, p shared.Principal, action string, amount *big.Int, chain, asset string, ref AuthRef) (Decision, error)
}

// AllowAll approves every action. It is the policy for human sessions.
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, shared.Principal, string, *big.Int, string, string, AuthRef) (Decision, error) {
	return Decision{Outcome: DecisionApproved}, nil
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
	MaxID                           uuid.UUID
	Price, Chain, Asset, PayoutAddr string
	ExpiresAt                       *time.Time
}

type BuyInput struct {
	FromAddress, IdempotencyKey string
}
