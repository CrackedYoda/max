package market

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/config"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/shared"
)

// Fake repository: offers, strikes and restrictions.

func (f *fakeRepo) MaxIDByName(_ context.Context, _ string) (uuid.UUID, error) {
	return f.nameID, nil
}
func (f *fakeRepo) ActiveRestriction(_ context.Context, account uuid.UUID, kind string) (*Restriction, error) {
	return f.restrictions[account.String()+"|"+kind], nil
}
func (f *fakeRepo) Restrict(_ context.Context, account uuid.UUID, kind string, until *time.Time, reason string, by *uuid.UUID) error {
	f.restrictions[account.String()+"|"+kind] = &Restriction{ID: uuid.New(), AccountID: account, Kind: kind, Until: until, Reason: reason, CreatedBy: by}
	return nil
}
func (f *fakeRepo) OpenOfferCount(_ context.Context, buyer uuid.UUID) (int, error) {
	n := 0
	for _, o := range f.offers {
		if o.BuyerAccountID == buyer && (o.Status == OfferLive || o.Status == OfferUnfunded) {
			n++
		}
	}
	return n, nil
}
func (f *fakeRepo) CreateOffer(_ context.Context, in NewOffer, _ int) (Offer, error) {
	now := time.Now()
	o := Offer{ID: uuid.New(), MaxID: in.MaxID, BuyerAccountID: in.Buyer, FromAddress: in.FromAddress, Chain: in.Chain,
		AssetAddress: in.AssetAddress, AssetDecimals: in.AssetDecimals, AmountBaseUnits: in.AmountBaseUnits,
		Status: OfferLive, ExpiresAt: in.ExpiresAt, FundedCheckedAt: &now}
	f.offers[o.ID] = o
	return o, nil
}
func (f *fakeRepo) GetOffer(_ context.Context, id uuid.UUID) (Offer, error) {
	o, ok := f.offers[id]
	if !ok {
		return Offer{}, ErrOfferNotFound
	}
	return o, nil
}
func (f *fakeRepo) setOffer(id uuid.UUID, status string) {
	o := f.offers[id]
	o.Status = status
	f.offers[id] = o
}
func (f *fakeRepo) WithdrawOffer(_ context.Context, id, _ uuid.UUID) error {
	f.setOffer(id, OfferWithdrawn)
	return nil
}
func (f *fakeRepo) DeclineOffer(_ context.Context, id, _ uuid.UUID, note string) error {
	o := f.offers[id]
	o.Status, o.DeclineNote = OfferDeclined, note
	f.offers[id] = o
	return nil
}
func (f *fakeRepo) LiveListing(context.Context, uuid.UUID) (*Listing, error) {
	return f.liveListing, nil
}
func (f *fakeRepo) AcceptOffer(ctx context.Context, offerID, _ uuid.UUID, in NewSale) (Sale, error) {
	o := f.offers[offerID]
	if o.Status != OfferLive {
		return Sale{}, errOfferNotLive
	}
	for id, other := range f.offers {
		if id != offerID && other.MaxID == o.MaxID && (other.Status == OfferLive || other.Status == OfferUnfunded) {
			f.setOffer(id, OfferSuperseded)
		}
	}
	f.setOffer(offerID, OfferAccepted)
	if f.liveListing != nil {
		f.liveListing.Status = ListingLocked
	}
	return f.OpenSale(ctx, in)
}
func (f *fakeRepo) SaleByOffer(_ context.Context, offerID uuid.UUID) (Sale, error) {
	for _, s := range f.sales {
		if s.OfferID != nil && *s.OfferID == offerID {
			return s, nil
		}
	}
	return Sale{}, ErrSaleNotFound
}
func (f *fakeRepo) SetOfferFunded(_ context.Context, id uuid.UUID, funded bool, at time.Time) error {
	f.fundingSets++
	o := f.offers[id]
	if o.Status != OfferLive && o.Status != OfferUnfunded {
		return nil
	}
	o.Status = OfferUnfunded
	if funded {
		o.Status = OfferLive
	}
	o.FundedCheckedAt = &at
	f.offers[id] = o
	return nil
}
func (f *fakeRepo) DueFundingChecks(_ context.Context, olderThan time.Time, limit int) ([]Offer, error) {
	var out []Offer
	for _, o := range f.offers {
		if (o.Status == OfferLive || o.Status == OfferUnfunded) && o.ExpiresAt.After(time.Now()) &&
			(o.FundedCheckedAt == nil || o.FundedCheckedAt.Before(olderThan)) && len(out) < limit {
			out = append(out, o)
		}
	}
	return out, nil
}
func (f *fakeRepo) ExpireOffers(_ context.Context, _ int) (int, error) {
	n := 0
	for id, o := range f.offers {
		if (o.Status == OfferLive || o.Status == OfferUnfunded) && !o.ExpiresAt.After(time.Now()) {
			f.setOffer(id, OfferExpired)
			n++
		}
	}
	return n, nil
}
func (f *fakeRepo) LapseOffer(_ context.Context, offerID uuid.UUID, since time.Time, rule SuspendRule) (LapseResult, error) {
	o, ok := f.offers[offerID]
	if !ok {
		return LapseResult{}, ErrOfferNotFound
	}
	if o.Status == OfferLapsed {
		return LapseResult{Offer: o}, nil
	}
	if o.Status != OfferAccepted {
		return LapseResult{}, errOfferNotLive
	}
	sale, err := f.SaleByOffer(context.Background(), offerID)
	if err != nil || !SaleLapsesOffer(sale.Status, sale.TxHash != nil) {
		return LapseResult{}, errNotLapsable
	}
	f.setOffer(offerID, OfferLapsed)
	f.strikes[o.BuyerAccountID] = append(f.strikes[o.BuyerAccountID], time.Now())
	n := 0
	for _, at := range f.strikes[o.BuyerAccountID] {
		if at.After(since) {
			n++
		}
	}
	res := LapseResult{Offer: f.offers[offerID], Strikes: n}
	key := o.BuyerAccountID.String() + "|" + RestrictionOfferSuspension
	if r := rule(n, f.restrictions[key] != nil); r != nil {
		f.restrictions[key] = &Restriction{ID: uuid.New(), AccountID: o.BuyerAccountID, Kind: r.Kind, Until: r.Until, Reason: r.Reason}
		res.Suspension = f.restrictions[key]
	}
	return res, nil
}

// Helpers.

func (h *harness) offer(amount string, mutate func(*OfferInput)) (Offer, error) {
	in := OfferInput{Name: "yoda", Amount: amount, Chain: "solana-devnet", Asset: devnetUSDC, FromAddress: h.payer}
	if mutate != nil {
		mutate(&in)
	}
	return h.svc.MakeOffer(context.Background(), human(h.buyer), in)
}

// seedOffer stores an offer directly, as if made earlier.
func (h *harness) seedOffer(buyer uuid.UUID, from, status string) Offer {
	checked := time.Now().Add(-time.Hour)
	o := Offer{ID: uuid.New(), MaxID: h.maxID, BuyerAccountID: buyer, FromAddress: from, Chain: "solana-devnet",
		AssetAddress: devnetUSDC, AssetDecimals: 6, AmountBaseUnits: "20000000", Status: status,
		ExpiresAt: time.Now().Add(72 * time.Hour), FundedCheckedAt: &checked}
	h.repo.offers[o.ID] = o
	return o
}

func shortfall() error {
	return &settle.FundsShortfall{NeedAsset: big.NewInt(20_000_000), HaveAsset: big.NewInt(1), NeedNative: big.NewInt(5000), HaveNative: big.NewInt(0)}
}

func TestMakeOffer_OK(t *testing.T) {
	h := newHarness(t, nil)
	o, err := h.offer("20", nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != OfferLive || o.AmountBaseUnits != "20000000" || o.MaxID != h.maxID || o.FromAddress != h.payer || o.BuyerAccountID != h.buyer {
		t.Fatalf("offer %+v", o)
	}
}

func TestMakeOffer_RequiresFundedWallet(t *testing.T) {
	h := newHarness(t, nil)
	h.adapter.fundsErr = shortfall()
	_, err := h.offer("20", nil)
	if code(err) != "INSUFFICIENT_FUNDS" {
		t.Fatalf("got %v", err)
	}
	if len(h.repo.offers) != 0 {
		t.Fatal("offer stored despite missing funds")
	}
}

func TestMakeOffer_WalletMustBeBuyers(t *testing.T) {
	for name, addr := range map[string]func(h *harness) string{
		"seller's wallet": func(h *harness) string { return h.payout },
		"unlinked":        func(*harness) string { return solAddr() },
		"garbage":         func(*harness) string { return "not-an-address" },
	} {
		h := newHarness(t, nil)
		_, err := h.offer("20", func(in *OfferInput) { in.FromAddress = addr(h) })
		if code(err) != "VALIDATION_FAILED" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestMakeOffer_LimitTwenty(t *testing.T) {
	h := newHarness(t, nil)
	for i := 0; i < 19; i++ {
		o := h.seedOffer(h.buyer, h.payer, OfferLive)
		o.MaxID = uuid.New()
		h.repo.offers[o.ID] = o
	}
	h.seedOffer(h.buyer, h.payer, OfferDeclined) // closed offers do not count
	if _, err := h.offer("20", nil); err != nil {
		t.Fatalf("20th offer: %v", err)
	}
	if _, err := h.offer("20", nil); !errors.Is(err, ErrOfferLimit) {
		t.Fatalf("21st offer: %v", err)
	}
}

func TestMakeOffer_BelowMinimum(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.offer("0.5", nil); !errors.Is(err, ErrOfferBelowMinimum) {
		t.Fatalf("got %v", err)
	}
}

func TestMakeOffer_Suspended(t *testing.T) {
	for _, kind := range []string{RestrictionOfferSuspension, RestrictionBan} {
		h := newHarness(t, nil)
		until := time.Now().Add(24 * time.Hour)
		_ = h.repo.Restrict(context.Background(), h.buyer, kind, &until, "test", nil)
		h.repo.restricted[h.buyer] = kind == RestrictionBan
		if _, err := h.offer("20", nil); !errors.Is(err, ErrMarketRestricted) {
			t.Errorf("%s: got %v", kind, err)
		}
	}
}

func TestMakeOffer_ExpiryClamp(t *testing.T) {
	h := newHarness(t, nil)
	o, err := h.offer("20", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(o.ExpiresAt); d < 7*24*time.Hour-time.Minute || d > 7*24*time.Hour {
		t.Fatalf("default expiry in %v", d)
	}
	for name, d := range map[string]time.Duration{"40d": 40 * 24 * time.Hour, "past": -time.Minute} {
		h := newHarness(t, nil)
		at := time.Now().Add(d)
		if _, err := h.offer("20", func(in *OfferInput) { in.ExpiresAt = &at }); code(err) != "VALIDATION_FAILED" {
			t.Errorf("%s: got %v", name, err)
		}
	}
	h = newHarness(t, nil)
	at := time.Now().Add(29 * 24 * time.Hour)
	if o, err := h.offer("20", func(in *OfferInput) { in.ExpiresAt = &at }); err != nil || !o.ExpiresAt.Equal(at) {
		t.Fatalf("29d: %v %v", err, o.ExpiresAt)
	}
}

func TestMakeOffer_SelfTradeAndNotListable(t *testing.T) {
	h := newHarness(t, nil)
	h.repo.party = PartyFacts{SharedWallet: true}
	if _, err := h.offer("20", nil); !errors.Is(err, ErrSelfTrade) {
		t.Fatalf("self trade: %v", err)
	}
	h = newHarness(t, nil)
	h.repo.facts = NameFacts{Status: "active", OwnerStatus: "active", OwnerBanned: true}
	_, err := h.offer("20", nil)
	var app *shared.AppError
	if !errors.As(err, &app) || app.Code != "NOT_LISTABLE" || app.Details["reason"] != ReasonUnavailable {
		t.Fatalf("not listable: %v", err)
	}
}

func TestMakeOffer_Paused(t *testing.T) {
	h := newHarness(t, func(c *config.MarketConfig) { c.TradingPaused = true })
	if _, err := h.offer("20", nil); !errors.Is(err, ErrMarketPaused) {
		t.Fatalf("got %v", err)
	}
}

func (h *harness) accept(id uuid.UUID, payout string) (SaleView, error) {
	return h.svc.AcceptOffer(context.Background(), human(h.seller), id, payout)
}

func TestAccept_UnfundedFlipsAndFails(t *testing.T) {
	h := newHarness(t, nil)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	h.adapter.fundsErr = shortfall()
	if _, err := h.accept(o.ID, h.payout); !errors.Is(err, ErrOfferUnfunded) {
		t.Fatalf("got %v", err)
	}
	if got := h.repo.offers[o.ID].Status; got != OfferUnfunded {
		t.Fatalf("status %s", got)
	}
	if h.repo.opened != 0 {
		t.Fatal("sale opened")
	}
	// An unfunded offer cannot be accepted until the funding check flips it back.
	h.adapter.fundsErr = nil
	if _, err := h.accept(o.ID, h.payout); !errors.Is(err, ErrOfferUnfunded) {
		t.Fatalf("accept unfunded: %v", err)
	}
}

func TestAccept_SupersedesOthers(t *testing.T) {
	h := newHarness(t, nil)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	other := uuid.New()
	o2 := h.seedOffer(other, solAddr(), OfferLive)
	o3 := h.seedOffer(uuid.New(), solAddr(), OfferUnfunded)
	v, err := h.accept(o.ID, h.payout)
	if err != nil {
		t.Fatal(err)
	}
	s := v.Sale
	if s.OfferID == nil || *s.OfferID != o.ID || s.ListingID != nil || s.Status != SaleLocked || s.BuyerAddress != h.payer ||
		s.SellerPayoutAddress != h.payout || s.PriceBaseUnits != "20000000" || s.FeeBaseUnits != "2000000" || s.SellerBaseUnits != "18000000" {
		t.Fatalf("sale %+v", s)
	}
	if s.LockExpiresAt == nil || time.Until(*s.LockExpiresAt) < 23*time.Hour {
		t.Fatalf("lock %v", s.LockExpiresAt)
	}
	if v.Instructions != nil {
		t.Fatal("seller must not receive payment instructions")
	}
	for id, want := range map[uuid.UUID]string{o.ID: OfferAccepted, o2.ID: OfferSuperseded, o3.ID: OfferSuperseded} {
		if got := h.repo.offers[id].Status; got != want {
			t.Errorf("offer %s: %s, want %s", id, got, want)
		}
	}
}

func TestAccept_PayoutFromListing(t *testing.T) {
	h := newHarness(t, nil)
	l := h.repo.listings[h.listing]
	h.repo.liveListing = &l
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	v, err := h.accept(o.ID, "") // no body payout needed with a listing
	if err != nil {
		t.Fatal(err)
	}
	if v.Sale.SellerPayoutAddress != h.payout || h.repo.liveListing.Status != ListingLocked {
		t.Fatalf("sale %+v listing %s", v.Sale, h.repo.liveListing.Status)
	}
}

func TestAccept_PayoutRequiredWithoutListing(t *testing.T) {
	for name, payout := range map[string]string{"missing": "", "not seller's": solAddr()} {
		h := newHarness(t, nil)
		o := h.seedOffer(h.buyer, h.payer, OfferLive)
		if _, err := h.accept(o.ID, payout); code(err) != "VALIDATION_FAILED" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestAccept_OnlyOwnerAndPaused(t *testing.T) {
	h := newHarness(t, nil)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	if _, err := h.svc.AcceptOffer(context.Background(), human(uuid.New()), o.ID, h.payout); !errors.Is(err, ErrOfferNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	h.svc.cfg.TradingPaused = true
	if _, err := h.accept(o.ID, h.payout); !errors.Is(err, ErrMarketPaused) {
		t.Fatalf("paused: %v", err)
	}
}

func TestAccept_ReviewThreshold(t *testing.T) {
	h := newHarness(t, func(c *config.MarketConfig) { c.ReviewThreshold = "10" })
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	v, err := h.accept(o.ID, h.payout)
	if err != nil {
		t.Fatal(err)
	}
	if v.Sale.Status != SalePendingReview || v.Sale.LockExpiresAt != nil {
		t.Fatalf("sale %+v", v.Sale)
	}
}

func TestPayOffer_ReturnsFreshQuote(t *testing.T) {
	h := newHarness(t, nil)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	acc, err := h.accept(o.ID, h.payout)
	if err != nil {
		t.Fatal(err)
	}
	h.svc.cfg.TradingPaused = true // paying an accepted offer works while paused
	v, err := h.svc.PayOffer(context.Background(), human(h.buyer), o.ID, "pay-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	if v.Sale.ID != acc.Sale.ID || v.Quote == nil || v.Quote.Price != "20" || v.Quote.Fee != "2" || v.Quote.NetworkFee != "0.000005" {
		t.Fatalf("view %+v quote %+v", v.Sale, v.Quote)
	}
	if v.Instructions == nil || v.Instructions.Solana[0].Memo != "max:sale:"+acc.Sale.ID.String() {
		t.Fatalf("instructions %+v", v.Instructions)
	}
	if _, err := h.svc.PayOffer(context.Background(), human(uuid.New()), o.ID, "pay-key-0001"); !errors.Is(err, ErrOfferNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	h.adapter.fundsErr = shortfall()
	if _, err := h.svc.PayOffer(context.Background(), human(h.buyer), o.ID, "pay-key-0001"); code(err) != "INSUFFICIENT_FUNDS" {
		t.Fatalf("short: %v", err)
	}
}

func TestPayOffer_NotAccepted(t *testing.T) {
	h := newHarness(t, nil)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	if _, err := h.svc.PayOffer(context.Background(), human(h.buyer), o.ID, "pay-key-0001"); !errors.Is(err, errOfferNotAccepted) {
		t.Fatalf("got %v", err)
	}
}

func TestRecheck_FlipsBothWays(t *testing.T) {
	h := newHarness(t, nil)
	live := h.seedOffer(h.buyer, h.payer, OfferLive)
	unfunded := h.seedOffer(uuid.New(), solAddr(), OfferUnfunded)
	fresh := h.seedOffer(uuid.New(), solAddr(), OfferLive)
	now := time.Now()
	fresh.FundedCheckedAt = &now
	h.repo.offers[fresh.ID] = fresh
	expired := h.seedOffer(uuid.New(), solAddr(), OfferLive)
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	h.repo.offers[expired.ID] = expired

	h.adapter.fundsErr = shortfall()
	if _, err := h.svc.RecheckFunding(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uuid.UUID]string{live.ID: OfferUnfunded, unfunded.ID: OfferUnfunded, fresh.ID: OfferLive, expired.ID: OfferExpired} {
		if got := h.repo.offers[id].Status; got != want {
			t.Errorf("after shortfall %s: %s, want %s", id, got, want)
		}
	}

	// Make both due again; funds arrive.
	old := time.Now().Add(-time.Hour)
	for _, id := range []uuid.UUID{live.ID, unfunded.ID} {
		o := h.repo.offers[id]
		o.FundedCheckedAt = &old
		h.repo.offers[id] = o
	}
	h.adapter.fundsErr = nil
	if _, err := h.svc.RecheckFunding(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{live.ID, unfunded.ID} {
		if got := h.repo.offers[id].Status; got != OfferLive {
			t.Errorf("after funding %s: %s", id, got)
		}
	}

	// RPC failures leave the status alone.
	for _, id := range []uuid.UUID{live.ID} {
		o := h.repo.offers[id]
		o.FundedCheckedAt = &old
		h.repo.offers[id] = o
	}
	h.adapter.fundsErr = errors.New("rpc down")
	sets := h.repo.fundingSets
	if _, err := h.svc.RecheckFunding(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	if h.repo.fundingSets != sets || h.repo.offers[live.ID].Status != OfferLive {
		t.Fatal("RPC failure changed funding status")
	}
}

func TestLapse_ThirdStrikeSuspends(t *testing.T) {
	h := newHarness(t, nil)
	key := h.buyer.String() + "|" + RestrictionOfferSuspension
	for i := 1; i <= 3; i++ {
		o := h.seedOffer(h.buyer, h.payer, OfferAccepted)
		h.seedOfferSale(o, SaleExpired, nil)
		if err := h.svc.LapseAcceptedOffer(context.Background(), o.ID); err != nil {
			t.Fatal(err)
		}
		if got := h.repo.offers[o.ID].Status; got != OfferLapsed {
			t.Fatalf("strike %d: offer %s", i, got)
		}
		r := h.repo.restrictions[key]
		if i < 3 && r != nil {
			t.Fatalf("suspended after %d strikes", i)
		}
		if i == 3 {
			if r == nil || r.Until == nil {
				t.Fatal("no suspension after the third strike")
			}
			if d := time.Until(*r.Until); d < 30*24*time.Hour-time.Minute || d > 30*24*time.Hour {
				t.Fatalf("suspended for %v", d)
			}
			// Lapsing again is a no-op: no fourth strike.
			if err := h.svc.LapseAcceptedOffer(context.Background(), o.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := len(h.repo.strikes[h.buyer]); n != 3 {
		t.Fatalf("strikes = %d", n)
	}
	// A suspended buyer cannot make offers.
	if _, err := h.offer("20", nil); !errors.Is(err, ErrMarketRestricted) {
		t.Fatalf("offer while suspended: %v", err)
	}
}

func TestWithdrawAndDecline(t *testing.T) {
	h := newHarness(t, nil)
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	if err := h.svc.WithdrawOffer(context.Background(), human(h.seller), o.ID); !errors.Is(err, ErrOfferNotFound) {
		t.Fatalf("seller withdraw: %v", err)
	}
	if err := h.svc.DeclineOffer(context.Background(), human(h.buyer), o.ID, "no"); !errors.Is(err, ErrOfferNotFound) {
		t.Fatalf("buyer decline: %v", err)
	}
	if err := h.svc.DeclineOffer(context.Background(), human(h.seller), o.ID, "too low"); err != nil {
		t.Fatal(err)
	}
	if got := h.repo.offers[o.ID]; got.Status != OfferDeclined || got.DeclineNote != "too low" {
		t.Fatalf("declined %+v", got)
	}
	o2 := h.seedOffer(h.buyer, h.payer, OfferLive)
	if err := h.svc.WithdrawOffer(context.Background(), human(h.buyer), o2.ID); err != nil || h.repo.offers[o2.ID].Status != OfferWithdrawn {
		t.Fatalf("withdraw: %v %s", err, h.repo.offers[o2.ID].Status)
	}
}

// seedOfferSale stores the sale of an accepted offer in the given state.
func (h *harness) seedOfferSale(o Offer, status string, tx *string) {
	id := o.ID
	s := Sale{ID: uuid.New(), MaxID: o.MaxID, OfferID: &id, SellerAccountID: h.seller, BuyerAccountID: o.BuyerAccountID, Status: status, TxHash: tx}
	h.repo.sales[s.ID] = s
}

func TestSaleLapsesOffer(t *testing.T) {
	for _, c := range []struct {
		status string
		hasTx  bool
		want   bool
	}{
		{SaleExpired, false, true},
		{SaleExpired, true, false},
		{SaleManualReview, false, false},
		{SaleManualReview, true, false},
		{SaleCancelled, false, false},
		{SalePendingReview, false, false},
		{SaleLocked, false, false},
		{SaleCompleted, true, false},
	} {
		if got := SaleLapsesOffer(c.status, c.hasTx); got != c.want {
			t.Errorf("%s tx=%v: %v, want %v", c.status, c.hasTx, got, c.want)
		}
	}
}

func TestLapse_RefusedWhenBuyerNotAtFault(t *testing.T) {
	tx := validSig()
	for name, c := range map[string]struct {
		status string
		tx     *string
	}{
		"expired with tx": {SaleExpired, &tx},
		"manual review":   {SaleManualReview, &tx},
		"cancelled":       {SaleCancelled, nil},
		"still locked":    {SaleLocked, nil},
	} {
		h := newHarness(t, nil)
		o := h.seedOffer(h.buyer, h.payer, OfferAccepted)
		h.seedOfferSale(o, c.status, c.tx)
		if err := h.svc.LapseAcceptedOffer(context.Background(), o.ID); !errors.Is(err, errNotLapsable) {
			t.Errorf("%s: got %v", name, err)
		}
		if h.repo.offers[o.ID].Status != OfferAccepted || len(h.repo.strikes[h.buyer]) != 0 {
			t.Errorf("%s: offer %s, strikes %d", name, h.repo.offers[o.ID].Status, len(h.repo.strikes[h.buyer]))
		}
	}
}
