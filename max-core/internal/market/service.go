package market

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/mr-tron/base58"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/shared"
)

// Not-found and validation errors reuse the shared codes; §9 names only the
// market-specific ones.
var (
	ErrListingNotFound = shared.ErrNotFound.WithMessage("listing not found")
	ErrSaleNotFound    = shared.ErrNotFound.WithMessage("sale not found")
	errPayoutNotLinked = shared.ErrValidation.WithMessage("payoutAddress must be an active wallet linked to your account on this chain")
	errPayerNotLinked  = shared.ErrValidation.WithMessage("fromAddress must be an active wallet linked to your account on this chain")
	errBadTxHash       = shared.ErrValidation.WithMessage("txHash is not a valid transaction signature for this chain")
	errAwaitingReview  = shared.ErrConflict.WithMessage("this sale is awaiting marketplace review")
)

// Observer re-check delays until Task 9's worker owns scheduling.
const (
	recheckPending  = 10 * time.Second
	recheckNotFound = 15 * time.Second
)

type Service struct {
	repo     Repository
	adapters map[chains.Namespace]settle.Adapter
	cfg      Config
	agents   AgentPolicy
	log      *zap.Logger
	now      func() time.Time
}

func NewService(repo Repository, adapters map[chains.Namespace]settle.Adapter, cfg Config, agents AgentPolicy, log *zap.Logger) *Service {
	if agents == nil {
		agents = AllowAll{}
	}
	return &Service{repo: repo, adapters: adapters, cfg: cfg, agents: agents, log: log, now: time.Now}
}

// Config returns the validated configuration (the HTTP gate reads it).
func (s *Service) Config() Config { return s.cfg }

func (s *Service) enabled() error {
	if !s.cfg.Enabled {
		return ErrMarketDisabled
	}
	return nil
}

func (s *Service) trading() error {
	if err := s.enabled(); err != nil {
		return err
	}
	if s.cfg.TradingPaused {
		return ErrMarketPaused
	}
	return nil
}

func (s *Service) authorize(ctx context.Context, p shared.Principal, action string, amount *big.Int, a Asset, ref AuthRef) error {
	d, err := s.agents.Authorize(ctx, p, action, amount, a.Chain.Key, a.Address, ref)
	if err != nil {
		return err
	}
	switch d.Outcome {
	case DecisionApproved:
		return nil
	case DecisionPending:
		details := map[string]any{"reasons": d.Reasons}
		if d.AuthorizationID != nil {
			details["authorizationId"] = d.AuthorizationID.String()
		}
		return ErrPendingApproval.WithDetails(details)
	}
	return shared.ErrForbidden.WithMessage("the agent policy denies this market action")
}

// Eligibility reports whether the owner can list maxID now and on what terms.
func (s *Service) Eligibility(ctx context.Context, p shared.Principal, maxID uuid.UUID) (Eligibility, error) {
	if err := s.enabled(); err != nil {
		return Eligibility{}, err
	}
	facts, owner, err := s.repo.NameFacts(ctx, maxID)
	if err != nil {
		return Eligibility{}, err
	}
	if owner != p.AccountID {
		return Eligibility{}, maxids.ErrMaxIDNotFound
	}
	ok, reason := Listable(facts, s.now())
	return Eligibility{
		MaxID: maxID, Listable: ok, Reason: reason, ListableAt: facts.ListableAt, RenameAvailableAt: facts.RenameAvailableAt,
		FeeBPS: s.cfg.FeeBPS, MinPrice: s.cfg.MinPrice, Assets: s.cfg.Assets,
	}, nil
}

// List creates an active fixed-price listing (spec §6 List).
func (s *Service) List(ctx context.Context, p shared.Principal, in ListInput) (Listing, error) {
	if err := s.trading(); err != nil {
		return Listing{}, err
	}
	a, ok := s.cfg.asset(in.Chain, in.Asset)
	if !ok {
		return Listing{}, ErrAssetNotAccepted
	}
	price, err := s.price(in.Price, a)
	if err != nil {
		return Listing{}, err
	}
	facts, owner, err := s.repo.NameFacts(ctx, in.MaxID)
	if err != nil {
		return Listing{}, err
	}
	if owner != p.AccountID {
		return Listing{}, maxids.ErrMaxIDNotFound
	}
	if ok, reason := Listable(facts, s.now()); !ok {
		return Listing{}, NotListable(reason)
	}
	payout, err := chains.NormalizeAddress(a.Chain.Namespace, in.PayoutAddr)
	if err != nil || payout == s.cfg.Treasury[a.Chain.Namespace] {
		return Listing{}, errPayoutNotLinked
	}
	if id, err := s.repo.ActiveWalletID(ctx, p.AccountID, a.Chain.Namespace, payout); err != nil {
		return Listing{}, err
	} else if id == uuid.Nil {
		return Listing{}, errPayoutNotLinked
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		return Listing{}, shared.ErrValidation.WithMessage("expiresAt must be in the future")
	}
	maxID := in.MaxID
	if err := s.authorize(ctx, p, ActionList, price, a, AuthRef{MaxID: &maxID}); err != nil {
		return Listing{}, err
	}
	return s.repo.CreateListing(ctx, NewListing{
		MaxID: in.MaxID, Seller: p.AccountID, Chain: a.Chain.Key, AssetAddress: a.Address, AssetDecimals: a.Decimals,
		PriceBaseUnits: price.String(), PayoutAddress: payout, ExpiresAt: in.ExpiresAt,
	})
}

// price parses a decimal amount and enforces the marketplace minimum.
func (s *Service) price(raw string, a Asset) (*big.Int, error) {
	v, err := payments.ParseAmount(raw, a.Decimals)
	if err != nil {
		return nil, shared.ErrValidation.WithMessage("price: " + err.Error())
	}
	if v.Cmp(s.cfg.minBase(a)) < 0 {
		return nil, ErrOfferBelowMinimum.WithMessage("price is below the marketplace minimum of " + s.cfg.MinPrice + " " + a.Symbol)
	}
	return v, nil
}

func (s *Service) ownedListing(ctx context.Context, p shared.Principal, id uuid.UUID) (Listing, Asset, error) {
	l, err := s.repo.GetListing(ctx, id)
	if err != nil {
		return Listing{}, Asset{}, err
	}
	if l.SellerAccountID != p.AccountID {
		return Listing{}, Asset{}, ErrListingNotFound
	}
	a, ok := s.cfg.asset(l.Chain, l.AssetAddress)
	if !ok {
		return Listing{}, Asset{}, ErrAssetNotAccepted
	}
	return l, a, nil
}

// UpdatePrice changes an active listing's price.
func (s *Service) UpdatePrice(ctx context.Context, p shared.Principal, id uuid.UUID, rawPrice string) (Listing, error) {
	if err := s.trading(); err != nil {
		return Listing{}, err
	}
	l, a, err := s.ownedListing(ctx, p, id)
	if err != nil {
		return Listing{}, err
	}
	price, err := s.price(rawPrice, a)
	if err != nil {
		return Listing{}, err
	}
	maxID := l.MaxID
	if err := s.authorize(ctx, p, ActionList, price, a, AuthRef{MaxID: &maxID, ListingID: &id}); err != nil {
		return Listing{}, err
	}
	return s.repo.UpdateListingPrice(ctx, id, p.AccountID, price.String())
}

// Delist cancels an active listing.
func (s *Service) Delist(ctx context.Context, p shared.Principal, id uuid.UUID) error {
	if err := s.trading(); err != nil {
		return err
	}
	l, err := s.repo.GetListing(ctx, id)
	if err != nil {
		return err
	}
	if l.SellerAccountID != p.AccountID {
		return ErrListingNotFound
	}
	return s.repo.CancelListing(ctx, id, p.AccountID)
}

// Buy opens a buy-now sale on an active listing (spec §6 Buy now).
func (s *Service) Buy(ctx context.Context, p shared.Principal, listingID uuid.UUID, in BuyInput) (SaleView, error) {
	if err := s.trading(); err != nil {
		return SaleView{}, err
	}
	if prior, err := s.repo.SaleByIdempotencyKey(ctx, p.AccountID, in.IdempotencyKey); err != nil {
		return SaleView{}, err
	} else if prior != nil {
		if prior.ListingID == nil || *prior.ListingID != listingID {
			return SaleView{}, payments.ErrIdempotencyConflict.WithMessage("idempotency key was already used for a different listing")
		}
		v := s.view(ctx, p, *prior)
		v.Replayed = true
		return v, nil
	}

	l, err := s.repo.GetListing(ctx, listingID)
	if err != nil {
		return SaleView{}, err
	}
	switch {
	case l.Status == ListingLocked:
		return SaleView{}, ErrNameLocked
	case l.Status != ListingActive, l.ExpiresAt != nil && !l.ExpiresAt.After(s.now()):
		return SaleView{}, ErrListingNotActive
	}
	a, ok := s.cfg.asset(l.Chain, l.AssetAddress)
	if !ok {
		return SaleView{}, ErrAssetNotAccepted
	}
	facts, owner, err := s.repo.NameFacts(ctx, l.MaxID)
	if err != nil {
		return SaleView{}, err
	}
	if owner != l.SellerAccountID {
		return SaleView{}, ErrListingNotActive
	}
	if ok, reason := Listable(facts, s.now()); !ok {
		return SaleView{}, NotListable(reason)
	}
	if banned, err := s.repo.Restricted(ctx, p.AccountID, "ban"); err != nil {
		return SaleView{}, err
	} else if banned {
		return SaleView{}, ErrMarketRestricted
	}
	payer, err := chains.NormalizeAddress(a.Chain.Namespace, in.FromAddress)
	if err != nil {
		return SaleView{}, errPayerNotLinked
	}
	if id, err := s.repo.ActiveWalletID(ctx, p.AccountID, a.Chain.Namespace, payer); err != nil {
		return SaleView{}, err
	} else if id == uuid.Nil {
		return SaleView{}, errPayerNotLinked
	}
	parties, err := s.repo.PartyFacts(ctx, p.AccountID, l.SellerAccountID, payer)
	if err != nil {
		return SaleView{}, err
	}
	if SelfTrade(parties) {
		return SaleView{}, ErrSelfTrade
	}
	price, ok := new(big.Int).SetString(l.PriceBaseUnits, 10)
	if !ok || price.Sign() <= 0 {
		return SaleView{}, shared.ErrInternal.WithMessage("listing price is corrupt")
	}
	maxID := l.MaxID
	if err := s.authorize(ctx, p, ActionBuy, price, a, AuthRef{MaxID: &maxID, ListingID: &listingID}); err != nil {
		return SaleView{}, err
	}

	fee, sellerAmt := SplitFee(price, s.cfg.FeeBPS)
	treasury := s.cfg.Treasury[a.Chain.Namespace]
	adapter, ok := s.adapters[a.Chain.Namespace]
	if !ok || treasury == "" {
		return SaleView{}, ErrAssetNotAccepted
	}
	order := settle.Order{
		Chain: a.Chain, Mint: a.Address, Decimals: a.Decimals, Buyer: payer, Seller: l.PayoutAddress, Treasury: treasury,
		Price: price, Fee: fee, SellerAmount: sellerAmt,
	}
	quote, err := adapter.Quote(ctx, order)
	if err != nil {
		return SaleView{}, settleErr(err)
	}
	if err := adapter.CheckFunds(ctx, order, quote); err != nil {
		return SaleView{}, settleErr(err)
	}

	status, lock := SaleLocked, s.now().Add(s.cfg.BuyLock)
	lockAt := &lock
	if th := s.cfg.thresholdBase(a); th != nil && price.Cmp(th) > 0 {
		status, lockAt = SalePendingReview, nil
	}
	sale, err := s.repo.OpenSale(ctx, NewSale{
		MaxID: l.MaxID, ListingID: &listingID, Seller: l.SellerAccountID, Buyer: p.AccountID,
		BuyerAddress: payer, SellerPayout: l.PayoutAddress, Treasury: treasury,
		Chain: a.Chain.Key, AssetAddress: a.Address, AssetDecimals: a.Decimals,
		Price: price.String(), Fee: fee.String(), SellerAmount: sellerAmt.String(), QuotedNetworkFee: quote.NetworkFee.String(),
		FeeBPS: s.cfg.FeeBPS, IdempotencyKey: in.IdempotencyKey, Status: status, LockExpiresAt: lockAt,
	})
	if err != nil {
		return SaleView{}, err
	}
	if sale.Status != status || sale.BuyerAddress != payer {
		// A concurrent request with the same key won the insert; serve its sale.
		v := s.view(ctx, p, sale)
		v.Replayed = true
		return v, nil
	}
	v := SaleView{Sale: sale, Quote: quoteView(sale, quote)}
	if sale.Status == SaleLocked {
		order.SaleID = sale.ID
		ins, err := adapter.BuildTx(order, quote)
		if err != nil {
			return SaleView{}, settleErr(err)
		}
		v.Instructions = &ins
	}
	return v, nil
}

// settleErr maps adapter failures: a shortfall is the buyer's problem, anything
// else is an RPC or programming failure.
func settleErr(err error) error {
	var short *settle.FundsShortfall
	if errors.As(err, &short) {
		return InsufficientFunds(str(short.NeedAsset), str(short.HaveAsset), str(short.NeedNative), str(short.HaveNative))
	}
	var app *shared.AppError
	if errors.As(err, &app) {
		return err
	}
	return shared.ErrInternal.WithMessage("the settlement network could not be reached; try again shortly").WithError(err)
}

func str(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

// Submit records the buyer's broadcast transaction. It works while trading is
// paused so in-flight sales can finish.
func (s *Service) Submit(ctx context.Context, p shared.Principal, saleID uuid.UUID, txHash string) (SaleView, error) {
	if err := s.enabled(); err != nil {
		return SaleView{}, err
	}
	sale, err := s.repo.GetSale(ctx, saleID)
	if err != nil {
		return SaleView{}, err
	}
	if sale.BuyerAccountID != p.AccountID {
		return SaleView{}, ErrSaleNotFound
	}
	hash, err := normalizeTxHash(sale.Chain, txHash)
	if err != nil {
		return SaleView{}, err
	}
	if sale.TxHash != nil {
		if *sale.TxHash != hash {
			return SaleView{}, ErrTxMismatch.WithMessage("a different transaction was already submitted for this sale")
		}
		return SaleView{Sale: sale}, nil
	}
	switch sale.Status {
	case SaleLocked:
	case SalePendingReview:
		return SaleView{}, errAwaitingReview
	default:
		return SaleView{}, ErrSaleExpired.WithMessage("this sale is " + sale.Status + " and no longer accepts a transaction")
	}
	out, err := s.repo.AttachTx(ctx, saleID, p.AccountID, hash)
	if err != nil {
		return SaleView{}, err
	}
	return SaleView{Sale: out}, nil
}

func normalizeTxHash(chainKey, h string) (string, error) {
	chain, ok := chains.Lookup(chainKey)
	if !ok || chain.Namespace != chains.NamespaceSolana {
		return "", errBadTxHash
	}
	if b, err := base58.Decode(h); err != nil || len(b) != 64 {
		return "", errBadTxHash
	}
	return h, nil
}

// ObserveSale checks a submitted sale against the chain once and advances it.
// Task 9's worker drives it on a schedule; tests call it directly.
func (s *Service) ObserveSale(ctx context.Context, saleID uuid.UUID) (Sale, error) {
	sale, err := s.repo.GetSale(ctx, saleID)
	if err != nil {
		return Sale{}, err
	}
	if (sale.Status != SaleSubmitted && sale.Status != SaleConfirming) || sale.TxHash == nil {
		return sale, nil
	}
	adapter, order, err := s.order(sale)
	if err != nil {
		return Sale{}, err
	}
	obs, err := adapter.Observe(ctx, order, *sale.TxHash)
	if err != nil {
		return Sale{}, err
	}
	switch {
	case obs.Status == settle.StatusConfirmed && obs.Matches:
		return s.CompleteObserved(ctx, saleID, obs.Confirmations)
	case obs.Status == settle.StatusConfirmed:
		// Funds moved but not as agreed: an admin must sort out refunds.
		return s.repo.ProgressSale(ctx, saleID, Progress{
			From: sale.Status, To: SaleManualReview, Reason: "transaction does not match the sale: " + obs.Reason,
			Confirmations: obs.Confirmations, Event: "tx_mismatch", Detail: map[string]any{"reason": obs.Reason},
		})
	case obs.Status == settle.StatusFailed:
		return s.repo.ExpireSale(ctx, saleID, "transaction failed on chain")
	case obs.Status == settle.StatusPending:
		next := s.now().Add(recheckPending)
		return s.repo.ProgressSale(ctx, saleID, Progress{
			From: sale.Status, To: SaleConfirming, Confirmations: obs.Confirmations, NextCheckAt: &next, Event: "confirming",
		})
	default:
		next := s.now().Add(recheckNotFound)
		return s.repo.ProgressSale(ctx, saleID, Progress{
			From: sale.Status, To: sale.Status, Confirmations: sale.Confirmations, NextCheckAt: &next,
		})
	}
}

// CompleteObserved completes a sale whose transaction has been verified.
func (s *Service) CompleteObserved(ctx context.Context, saleID uuid.UUID, confirmations int64) (Sale, error) {
	return s.repo.CompleteSale(ctx, saleID, s.cfg.HoldPeriod, confirmations)
}

// order rebuilds the settlement order from a sale snapshot.
func (s *Service) order(sale Sale) (settle.Adapter, settle.Order, error) {
	chain, ok := chains.Lookup(sale.Chain)
	if !ok {
		return nil, settle.Order{}, ErrAssetNotAccepted
	}
	adapter, ok := s.adapters[chain.Namespace]
	if !ok {
		return nil, settle.Order{}, ErrAssetNotAccepted
	}
	price, ok1 := new(big.Int).SetString(sale.PriceBaseUnits, 10)
	fee, ok2 := new(big.Int).SetString(sale.FeeBaseUnits, 10)
	seller, ok3 := new(big.Int).SetString(sale.SellerBaseUnits, 10)
	if !ok1 || !ok2 || !ok3 {
		return nil, settle.Order{}, shared.ErrInternal.WithMessage("sale amounts are corrupt")
	}
	return adapter, settle.Order{
		SaleID: sale.ID, Chain: chain, Mint: sale.AssetAddress, Decimals: sale.AssetDecimals,
		Buyer: sale.BuyerAddress, Seller: sale.SellerPayoutAddress, Treasury: sale.TreasuryAddress,
		Price: price, Fee: fee, SellerAmount: seller,
	}, nil
}

// GetSale returns a sale to either party. The buyer of a locked sale also gets
// a fresh quote and the instructions to sign.
func (s *Service) GetSale(ctx context.Context, p shared.Principal, id uuid.UUID) (SaleView, error) {
	if err := s.enabled(); err != nil {
		return SaleView{}, err
	}
	sale, err := s.repo.GetSale(ctx, id)
	if err != nil {
		return SaleView{}, err
	}
	if sale.BuyerAccountID != p.AccountID && sale.SellerAccountID != p.AccountID {
		return SaleView{}, ErrSaleNotFound
	}
	return s.view(ctx, p, sale), nil
}

// view adds a quote and instructions for the buyer of a locked sale. Adapter
// failures are logged and leave them out; the sale itself is still returned.
func (s *Service) view(ctx context.Context, p shared.Principal, sale Sale) SaleView {
	v := SaleView{Sale: sale}
	if sale.Status != SaleLocked || sale.BuyerAccountID != p.AccountID || sale.TxHash != nil {
		return v
	}
	adapter, order, err := s.order(sale)
	if err != nil {
		return v
	}
	q, err := adapter.Quote(ctx, order)
	if err != nil {
		s.log.Warn("market quote for sale view", zap.String("sale_id", sale.ID.String()), zap.Error(err))
		return v
	}
	ins, err := adapter.BuildTx(order, q)
	if err != nil {
		s.log.Warn("market instructions for sale view", zap.String("sale_id", sale.ID.String()), zap.Error(err))
		return v
	}
	v.Quote, v.Instructions = quoteView(sale, q), &ins
	return v
}

func quoteView(sale Sale, q settle.Quote) *QuoteView {
	nativeDecimals := 9
	if c, ok := chains.Lookup(sale.Chain); ok {
		nativeDecimals = c.NativeDecimals
	}
	amt := func(s string) string {
		v, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return "0"
		}
		return payments.FormatAmount(v, sale.AssetDecimals)
	}
	network := "0"
	if q.NetworkFee != nil {
		network = payments.FormatAmount(q.NetworkFee, nativeDecimals)
	}
	return &QuoteView{
		Price: amt(sale.PriceBaseUnits), Fee: amt(sale.FeeBaseUnits), SellerReceives: amt(sale.SellerBaseUnits),
		NetworkFee: network, TotalAsset: amt(sale.PriceBaseUnits), TotalNative: network,
	}
}

// Search is the public listing search.
func (s *Service) Search(ctx context.Context, q ListingQuery) ([]ListingCard, string, error) {
	if err := s.enabled(); err != nil {
		return nil, "", err
	}
	return s.repo.SearchListings(ctx, q)
}

// NameView is the public market view of a name.
func (s *Service) NameView(ctx context.Context, rawName string) (NameView, error) {
	if err := s.enabled(); err != nil {
		return NameView{}, err
	}
	name, err := maxids.Normalize(rawName)
	if err != nil {
		return NameView{}, maxids.ErrMaxIDNotFound
	}
	v, err := s.repo.NameView(ctx, name)
	if err != nil {
		return NameView{}, err
	}
	v.Listable, v.Reason = Listable(v.Facts, s.now())
	return v, nil
}

// Inbox is the caller's own listings and sales.
func (s *Service) Inbox(ctx context.Context, p shared.Principal) (Inbox, error) {
	if err := s.enabled(); err != nil {
		return Inbox{}, err
	}
	return s.repo.Inbox(ctx, p.AccountID)
}
