package market

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

// Offer limits (spec §6 Offer, §7).
const (
	maxOpenOffers       = 20
	offerDefaultExpiry  = 7 * 24 * time.Hour
	offerMaxExpiry      = 30 * 24 * time.Hour
	fundingRecheckAfter = 5 * time.Minute
	strikeWindow        = 30 * 24 * time.Hour
	strikesToSuspend    = 3
	offerSuspensionFor  = 30 * 24 * time.Hour
)

var (
	ErrOfferNotFound    = shared.ErrNotFound.WithMessage("offer not found")
	errOfferNotLive     = shared.ErrConflict.WithMessage("this offer is no longer open")
	errOfferNotAccepted = shared.ErrConflict.WithMessage("this offer has not been accepted")
	errOfferDuplicate   = ErrOfferLimit.WithMessage("you already have an open offer on this name")
	errPayoutRequired   = shared.ErrValidation.WithMessage("payoutAddress is required when the name is not listed")
	errBuyerRestricted  = ErrMarketRestricted.WithMessage("the buyer is restricted from the marketplace")
	errNotLapsable      = shared.ErrConflict.WithMessage("this offer cannot lapse")
)

// SaleLapsesOffer reports whether an accepted offer's sale ended through the
// buyer's failure to pay: expired with no transaction submitted. Paid,
// cancelled, denied and manual-review sales never earn the buyer a strike.
func SaleLapsesOffer(saleStatus string, hasTx bool) bool {
	return saleStatus == SaleExpired && !hasTx
}

// MakeOffer places a live offer on a name, listed or not (spec §6 Offer 1).
func (s *Service) MakeOffer(ctx context.Context, p shared.Principal, in OfferInput) (Offer, error) {
	if err := s.trading(); err != nil {
		return Offer{}, err
	}
	a, ok := s.cfg.asset(in.Chain, in.Asset)
	if !ok {
		return Offer{}, ErrAssetNotAccepted
	}
	amount, err := s.minAmount("amount", in.Amount, a)
	if err != nil {
		return Offer{}, err
	}
	now := s.now()
	expires := now.Add(offerDefaultExpiry)
	if in.ExpiresAt != nil {
		switch {
		case !in.ExpiresAt.After(now):
			return Offer{}, shared.ErrValidation.WithMessage("expiresAt must be in the future")
		case in.ExpiresAt.After(now.Add(offerMaxExpiry)):
			return Offer{}, shared.ErrValidation.WithMessage("expiresAt must be within 30 days")
		}
		expires = *in.ExpiresAt
	}
	name, err := maxids.Normalize(in.Name)
	if err != nil {
		return Offer{}, maxids.ErrMaxIDNotFound
	}
	maxID, err := s.repo.MaxIDByName(ctx, name)
	if err != nil {
		return Offer{}, err
	}
	facts, owner, err := s.repo.NameFacts(ctx, maxID)
	if err != nil {
		return Offer{}, err
	}
	if ok, reason := Listable(facts, now); !ok {
		return Offer{}, NotListable(PublicReason(reason))
	}
	payer, err := s.linkedPayer(ctx, p.AccountID, a, in.FromAddress)
	if err != nil {
		return Offer{}, err
	}
	if err := s.notBanned(ctx, p.AccountID, ErrMarketRestricted); err != nil {
		return Offer{}, err
	}
	if r, err := s.repo.ActiveRestriction(ctx, p.AccountID, RestrictionOfferSuspension); err != nil {
		return Offer{}, err
	} else if r != nil {
		details := map[string]any{"kind": RestrictionOfferSuspension}
		if r.Until != nil {
			details["until"] = r.Until.UTC().Format(time.RFC3339)
		}
		return Offer{}, ErrMarketRestricted.WithMessage("you cannot make offers while your offers are suspended").WithDetails(details)
	}
	parties, err := s.repo.PartyFacts(ctx, p.AccountID, owner, payer)
	if err != nil {
		return Offer{}, err
	}
	if SelfTrade(parties) {
		return Offer{}, ErrSelfTrade
	}
	if n, err := s.repo.OpenOfferCount(ctx, p.AccountID); err != nil {
		return Offer{}, err
	} else if n >= maxOpenOffers {
		return Offer{}, ErrOfferLimit.WithMessage(fmt.Sprintf("you already have %d open offers", maxOpenOffers))
	}
	if err := s.authorize(ctx, p, ActionOffer, amount, a, AuthRef{MaxID: &maxID}); err != nil {
		return Offer{}, err
	}
	newOffer := NewOffer{
		MaxID: maxID, Buyer: p.AccountID, FromAddress: payer, Chain: a.Chain.Key, AssetAddress: a.Address,
		AssetDecimals: a.Decimals, AmountBaseUnits: amount.String(), ExpiresAt: expires,
	}
	draft := Offer{MaxID: maxID, BuyerAccountID: p.AccountID, FromAddress: payer, Chain: a.Chain.Key,
		AssetAddress: a.Address, AssetDecimals: a.Decimals, AmountBaseUnits: newOffer.AmountBaseUnits}
	if _, err := s.offerFunds(ctx, draft, a, ""); err != nil {
		return Offer{}, settleErr(err)
	}
	return s.repo.CreateOffer(ctx, newOffer, maxOpenOffers)
}

// WithdrawOffer lets the buyer close their live or unfunded offer.
func (s *Service) WithdrawOffer(ctx context.Context, p shared.Principal, id uuid.UUID) error {
	if err := s.trading(); err != nil {
		return err
	}
	o, err := s.repo.GetOffer(ctx, id)
	if err != nil {
		return err
	}
	if o.BuyerAccountID != p.AccountID {
		return ErrOfferNotFound
	}
	return s.repo.WithdrawOffer(ctx, id, p.AccountID)
}

// DeclineOffer lets the name's owner refuse an open offer.
func (s *Service) DeclineOffer(ctx context.Context, p shared.Principal, id uuid.UUID, note string) error {
	if err := s.trading(); err != nil {
		return err
	}
	if _, err := s.ownerOffer(ctx, p, id); err != nil {
		return err
	}
	return s.repo.DeclineOffer(ctx, id, p.AccountID, note)
}

// ownerOffer returns an offer on a name p owns; anyone else gets not found.
func (s *Service) ownerOffer(ctx context.Context, p shared.Principal, id uuid.UUID) (Offer, error) {
	o, err := s.repo.GetOffer(ctx, id)
	if err != nil {
		return Offer{}, err
	}
	if _, owner, err := s.repo.NameFacts(ctx, o.MaxID); err != nil {
		return Offer{}, err
	} else if owner != p.AccountID {
		return Offer{}, ErrOfferNotFound
	}
	return o, nil
}

// AcceptOffer accepts a live offer and opens its sale (spec §6 Offer 3). The
// payout address is the seller's listing payout when the name is listed,
// otherwise payoutAddress, which must be the seller's active wallet.
func (s *Service) AcceptOffer(ctx context.Context, p shared.Principal, id uuid.UUID, payoutAddress string) (SaleView, error) {
	if err := s.trading(); err != nil {
		return SaleView{}, err
	}
	o, err := s.ownerOffer(ctx, p, id)
	if err != nil {
		return SaleView{}, err
	}
	switch {
	case o.Status == OfferUnfunded:
		return SaleView{}, ErrOfferUnfunded
	case o.Status != OfferLive, !o.ExpiresAt.After(s.now()):
		return SaleView{}, errOfferNotLive
	}
	facts, _, err := s.repo.NameFacts(ctx, o.MaxID)
	if err != nil {
		return SaleView{}, err
	}
	if ok, reason := Listable(facts, s.now()); !ok {
		return SaleView{}, NotListable(reason)
	}
	a, ok := s.cfg.asset(o.Chain, o.AssetAddress)
	if !ok {
		return SaleView{}, ErrAssetNotAccepted
	}
	payout, err := s.offerPayout(ctx, p, o, a, payoutAddress)
	if err != nil {
		return SaleView{}, err
	}
	if err := s.notBanned(ctx, o.BuyerAccountID, errBuyerRestricted); err != nil {
		return SaleView{}, err
	}
	parties, err := s.repo.PartyFacts(ctx, o.BuyerAccountID, p.AccountID, o.FromAddress)
	if err != nil {
		return SaleView{}, err
	}
	if SelfTrade(parties) {
		return SaleView{}, ErrSelfTrade
	}
	price, ok := new(big.Int).SetString(o.AmountBaseUnits, 10)
	if !ok || price.Sign() <= 0 {
		return SaleView{}, shared.ErrInternal.WithMessage("offer amount is corrupt")
	}
	maxID := o.MaxID
	if err := s.authorize(ctx, p, ActionAccept, price, a, AuthRef{MaxID: &maxID, OfferID: &id}); err != nil {
		return SaleView{}, err
	}
	quote, err := s.offerFunds(ctx, o, a, payout)
	if err != nil {
		var short *settle.FundsShortfall
		if errors.As(err, &short) {
			if ferr := s.repo.SetOfferFunded(ctx, id, false, s.now()); ferr != nil {
				return SaleView{}, ferr
			}
			return SaleView{}, ErrOfferUnfunded
		}
		return SaleView{}, settleErr(err)
	}

	fee, sellerAmt := SplitFee(price, s.cfg.FeeBPS)
	status, lock := SaleLocked, s.now().Add(s.cfg.OfferPayWindow)
	lockAt := &lock
	if th := s.cfg.thresholdBase(a); th != nil && price.Cmp(th) > 0 {
		status, lockAt = SalePendingReview, nil
	}
	sale, err := s.repo.AcceptOffer(ctx, id, p.AccountID, NewSale{
		MaxID: o.MaxID, OfferID: &id, Seller: p.AccountID, Buyer: o.BuyerAccountID,
		BuyerAddress: o.FromAddress, SellerPayout: payout, Treasury: s.cfg.Treasury[a.Chain.Namespace],
		Chain: a.Chain.Key, AssetAddress: a.Address, AssetDecimals: a.Decimals,
		Price: price.String(), Fee: fee.String(), SellerAmount: sellerAmt.String(), QuotedNetworkFee: quote.NetworkFee.String(),
		FeeBPS: s.cfg.FeeBPS, IdempotencyKey: "offer:" + id.String(), Status: status, LockExpiresAt: lockAt,
	})
	if err != nil {
		return SaleView{}, err
	}
	// The seller gets the terms; payment instructions go to the buyer via pay.
	return SaleView{Sale: sale, Quote: quoteView(sale, quote)}, nil
}

// offerPayout picks where the seller is paid for an offer sale.
func (s *Service) offerPayout(ctx context.Context, p shared.Principal, o Offer, a Asset, raw string) (string, error) {
	l, err := s.repo.LiveListing(ctx, o.MaxID)
	if err != nil {
		return "", err
	}
	if l != nil && l.SellerAccountID == p.AccountID {
		if l.Status == ListingLocked {
			return "", ErrNameLocked
		}
		if c, ok := chains.Lookup(l.Chain); ok && c.Namespace == a.Chain.Namespace {
			return l.PayoutAddress, nil
		}
	}
	if raw == "" {
		return "", errPayoutRequired
	}
	payout, err := chains.NormalizeAddress(a.Chain.Namespace, raw)
	if err != nil || payout == s.cfg.Treasury[a.Chain.Namespace] {
		return "", errPayoutNotLinked
	}
	if wid, err := s.repo.ActiveWalletID(ctx, p.AccountID, a.Chain.Namespace, payout); err != nil {
		return "", err
	} else if wid == uuid.Nil {
		return "", errPayoutNotLinked
	}
	return payout, nil
}

// PayOffer gives the buyer of an accepted offer a fresh quote and the
// instructions to sign, after re-checking funds. Like submit, it works while
// trading is paused: the sale is already open. The idempotency key is
// accepted for API symmetry; paying creates nothing, so repeats are harmless.
func (s *Service) PayOffer(ctx context.Context, p shared.Principal, id uuid.UUID, _ string) (SaleView, error) {
	if err := s.enabled(); err != nil {
		return SaleView{}, err
	}
	o, err := s.repo.GetOffer(ctx, id)
	if err != nil {
		return SaleView{}, err
	}
	if o.BuyerAccountID != p.AccountID {
		return SaleView{}, ErrOfferNotFound
	}
	if o.Status != OfferAccepted && o.Status != OfferCompleted {
		return SaleView{}, errOfferNotAccepted
	}
	sale, err := s.repo.SaleByOffer(ctx, id)
	if err != nil {
		return SaleView{}, err
	}
	switch {
	case sale.Status == SaleExpired, sale.Status == SaleCancelled:
		return SaleView{}, ErrSaleExpired.WithMessage("this sale is " + sale.Status + " and no longer accepts payment")
	case sale.Status != SaleLocked, sale.TxHash != nil:
		return SaleView{Sale: sale}, nil // awaiting review, or already paid
	}
	adapter, order, err := s.order(sale)
	if err != nil {
		return SaleView{}, err
	}
	q, err := adapter.Quote(ctx, order)
	if err != nil {
		return SaleView{}, settleErr(err)
	}
	if err := adapter.CheckFunds(ctx, order, q); err != nil {
		return SaleView{}, settleErr(err)
	}
	ins, err := adapter.BuildTx(order, q)
	if err != nil {
		return SaleView{}, settleErr(err)
	}
	return SaleView{Sale: sale, Quote: quoteView(sale, q), Instructions: &ins}, nil
}

// RecheckFunding expires offers past their expiry and re-runs CheckFunds on
// live and unfunded offers not checked for fundingRecheckAfter, flipping them
// between live and unfunded (spec §6 Offer 2). It returns how many offers
// changed status. Task 9's worker calls it each tick.
func (s *Service) RecheckFunding(ctx context.Context, limit int) (int, error) {
	if !s.cfg.Enabled {
		return 0, nil
	}
	changed, err := s.repo.ExpireOffers(ctx, limit)
	if err != nil {
		return 0, err
	}
	due, err := s.repo.DueFundingChecks(ctx, s.now().Add(-fundingRecheckAfter), limit)
	if err != nil {
		return changed, err
	}
	for _, o := range due {
		a, ok := s.cfg.asset(o.Chain, o.AssetAddress)
		if !ok {
			continue
		}
		_, ferr := s.offerFunds(ctx, o, a, "")
		var short *settle.FundsShortfall
		if ferr != nil && !errors.As(ferr, &short) {
			s.log.Warn("market offer funding check", zap.String("offer_id", o.ID.String()), zap.Error(ferr))
			continue
		}
		funded := ferr == nil
		if funded == (o.Status == OfferLive) {
			continue
		}
		if err := s.repo.SetOfferFunded(ctx, o.ID, funded, s.now()); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// LapseAcceptedOffer closes an accepted offer whose pay window ran out: the
// offer lapses, the buyer gets a strike, and the third strike within 30 days
// suspends their offers for 30 days. All in one transaction. Task 9's expiry
// worker calls it after expiring the sale.
func (s *Service) LapseAcceptedOffer(ctx context.Context, offerID uuid.UUID) error {
	now := s.now()
	res, err := s.repo.LapseOffer(ctx, offerID, now.Add(-strikeWindow), func(strikes int, suspended bool) *NewRestriction {
		if strikes < strikesToSuspend || suspended {
			return nil
		}
		until := now.Add(offerSuspensionFor)
		return &NewRestriction{Kind: RestrictionOfferSuspension, Until: &until,
			Reason: fmt.Sprintf("%d accepted offers left unpaid within 30 days", strikes)}
	})
	if err != nil {
		return err
	}
	if res.Suspension != nil {
		s.log.Info("market offer suspension", zap.String("account_id", res.Offer.BuyerAccountID.String()), zap.Int("strikes", res.Strikes))
	}
	return nil
}

// offerOrder builds the settlement order for an offer. With no payout known
// yet, the buyer's own address stands in for the seller; offerFunds then
// assumes the seller's token account must be created.
func (s *Service) offerOrder(o Offer, a Asset, payout string) (settle.Adapter, settle.Order, error) {
	treasury := s.cfg.Treasury[a.Chain.Namespace]
	adapter, ok := s.adapters[a.Chain.Namespace]
	if !ok || treasury == "" {
		return nil, settle.Order{}, ErrAssetNotAccepted
	}
	price, ok := new(big.Int).SetString(o.AmountBaseUnits, 10)
	if !ok || price.Sign() <= 0 {
		return nil, settle.Order{}, shared.ErrInternal.WithMessage("offer amount is corrupt")
	}
	fee, sellerAmt := SplitFee(price, s.cfg.FeeBPS)
	seller := payout
	if seller == "" {
		seller = o.FromAddress
	}
	return adapter, settle.Order{
		Chain: a.Chain, Mint: a.Address, Decimals: a.Decimals, Buyer: o.FromAddress, Seller: seller, Treasury: treasury,
		Price: price, Fee: fee, SellerAmount: sellerAmt,
	}, nil
}

// offerFunds takes a fresh quote and checks the offer's wallet covers it. The
// raw adapter error is returned so callers can tell a shortfall from an RPC
// failure.
func (s *Service) offerFunds(ctx context.Context, o Offer, a Asset, payout string) (settle.Quote, error) {
	adapter, order, err := s.offerOrder(o, a, payout)
	if err != nil {
		return settle.Quote{}, err
	}
	q, err := adapter.Quote(ctx, order)
	if err != nil {
		return settle.Quote{}, err
	}
	if payout == "" && !q.CreateSellerATA && q.NetworkFee != nil && q.RentPerATA != nil {
		q.NetworkFee = new(big.Int).Add(q.NetworkFee, q.RentPerATA)
		q.CreateSellerATA = true
	}
	if err := adapter.CheckFunds(ctx, order, q); err != nil {
		return settle.Quote{}, err
	}
	return q, nil
}

// linkedPayer normalises fromAddress and checks it is the account's active
// wallet in the asset's namespace.
func (s *Service) linkedPayer(ctx context.Context, account uuid.UUID, a Asset, raw string) (string, error) {
	payer, err := chains.NormalizeAddress(a.Chain.Namespace, raw)
	if err != nil {
		return "", errPayerNotLinked
	}
	if id, err := s.repo.ActiveWalletID(ctx, account, a.Chain.Namespace, payer); err != nil {
		return "", err
	} else if id == uuid.Nil {
		return "", errPayerNotLinked
	}
	return payer, nil
}

func (s *Service) notBanned(ctx context.Context, account uuid.UUID, errBanned error) error {
	banned, err := s.repo.Restricted(ctx, account, RestrictionBan)
	if err != nil {
		return err
	}
	if banned {
		return errBanned
	}
	return nil
}
