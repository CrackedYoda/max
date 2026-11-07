package market

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Fake repository: admin writes. Each records what the service asked for;
// the guarded transitions themselves are covered by the integration tests.

type fakeAdminState struct {
	approvedLock map[uuid.UUID]time.Time
	reverseHold  time.Duration
	delistReason string
	restrictions map[uuid.UUID]Restriction
}

func (f *fakeRepo) adminState() *fakeAdminState {
	if f.admin.approvedLock == nil {
		f.admin.approvedLock = map[uuid.UUID]time.Time{}
		f.admin.restrictions = map[uuid.UUID]Restriction{}
	}
	return &f.admin
}

func (f *fakeRepo) AdminListings(context.Context, AdminFilter) ([]Listing, error) { return nil, nil }
func (f *fakeRepo) AdminOffers(context.Context, AdminFilter) ([]Offer, error)     { return nil, nil }
func (f *fakeRepo) AdminSales(context.Context, AdminFilter) ([]Sale, error)       { return nil, nil }
func (f *fakeRepo) AdminRestrictions(context.Context, *uuid.UUID, bool, int) ([]Restriction, error) {
	return nil, nil
}

func (f *fakeRepo) AdminDelist(_ context.Context, _, id uuid.UUID, reason string) (Listing, error) {
	l, ok := f.listings[id]
	switch {
	case !ok:
		return Listing{}, ErrListingNotFound
	case l.Status == ListingLocked:
		return Listing{}, errDelistLocked
	case l.Status != ListingActive:
		return Listing{}, ErrListingNotActive
	}
	l.Status, l.StatusReason = ListingAdminDelisted, reason
	f.listings[id] = l
	f.adminState().delistReason = reason
	return l, nil
}

func (f *fakeRepo) closeSale(id uuid.UUID, to string) Sale {
	s := f.sales[id]
	s.Status = to
	f.sales[id] = s
	if s.ListingID != nil {
		if l, ok := f.listings[*s.ListingID]; ok && l.Status == ListingLocked {
			l.Status = ListingActive
			f.listings[l.ID] = l
		}
	}
	return s
}

func (f *fakeRepo) AdminApproveSale(_ context.Context, _, id uuid.UUID, lock time.Time, _ string) (Sale, error) {
	s := f.sales[id]
	if s.Status != SalePendingReview {
		return Sale{}, errNotPendingReview
	}
	s.Status, s.LockExpiresAt = SaleLocked, &lock
	f.sales[id] = s
	f.adminState().approvedLock[id] = lock
	return s, nil
}

func (f *fakeRepo) AdminDenySale(_ context.Context, _, id uuid.UUID, reason string) (Sale, error) {
	s := f.closeSale(id, SaleCancelled)
	if s.OfferID != nil {
		o := f.offers[*s.OfferID]
		o.Status, o.DeclineNote = OfferDeclined, adminDenyNote(reason)
		f.offers[o.ID] = o
	}
	return s, nil
}

func (f *fakeRepo) AdminCancelSale(_ context.Context, _, id uuid.UUID, _ string) (Sale, error) {
	s := f.closeSale(id, SaleCancelled)
	if s.OfferID != nil {
		f.setOffer(*s.OfferID, OfferExpired)
	}
	return s, nil
}

func (f *fakeRepo) AdminReverseSale(_ context.Context, _, id uuid.UUID, _ string, hold time.Duration) (Sale, error) {
	s := f.sales[id]
	if f.owner != s.BuyerAccountID {
		return Sale{}, errBuyerNotOwner
	}
	s.Status = SaleReversed
	f.sales[id] = s
	f.owner = s.SellerAccountID
	f.adminState().reverseHold = hold
	return s, nil
}

func (f *fakeRepo) AdminRestrict(_ context.Context, actor, account uuid.UUID, in NewRestriction) (Restriction, error) {
	r := Restriction{ID: uuid.New(), AccountID: account, Kind: in.Kind, Until: in.Until, Reason: in.Reason, CreatedBy: &actor}
	f.adminState().restrictions[r.ID] = r
	if in.Kind == RestrictionBan {
		f.restricted[account] = true
	} else {
		f.restrictions[account.String()+"|"+in.Kind] = &r
	}
	return r, nil
}

func (f *fakeRepo) LiftRestriction(_ context.Context, _, id uuid.UUID, _ string) (Restriction, error) {
	r, ok := f.adminState().restrictions[id]
	if !ok || r.LiftedAt != nil {
		return Restriction{}, ErrRestrictionNotFound
	}
	now := time.Now()
	r.LiftedAt = &now
	f.admin.restrictions[id] = r
	if r.Kind == RestrictionBan {
		delete(f.restricted, r.AccountID)
	} else {
		delete(f.restrictions, r.AccountID.String()+"|"+r.Kind)
	}
	return r, nil
}

// Helpers.

var adminID = uuid.MustParse("00000000-0000-0000-0000-00000000a0a0")

// seedSale stores a sale on the harness listing (or on an offer when offer
// is set) in the given status.
func (h *harness) seedSale(status string, offer *Offer, tx bool) Sale {
	s := Sale{ID: uuid.New(), MaxID: h.maxID, SellerAccountID: h.seller, BuyerAccountID: h.buyer, BuyerAddress: h.payer,
		SellerPayoutAddress: h.payout, Chain: "solana-devnet", AssetAddress: devnetUSDC, AssetDecimals: 6,
		PriceBaseUnits: "25000000", FeeBPS: 1000, FeeBaseUnits: "2500000", SellerBaseUnits: "22500000", Status: status}
	if offer != nil {
		s.OfferID = &offer.ID
	} else {
		id := h.listing
		s.ListingID = &id
	}
	if tx {
		hash := validSig()
		s.TxHash = &hash
	}
	if status == SalePendingReview || status == SaleLocked {
		l := h.repo.listings[h.listing]
		l.Status = ListingLocked
		h.repo.listings[h.listing] = l
	}
	h.repo.sales[s.ID] = s
	return s
}

func (h *harness) fixClock() time.Time {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	h.svc.now = func() time.Time { return now }
	return now
}

func TestAdminApprove_StartsLock(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	now := h.fixClock()

	buy := h.seedSale(SalePendingReview, nil, false)
	got, err := h.svc.AdminApprove(ctx, adminID, buy.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SaleLocked || !got.LockExpiresAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("buy-now approval: %s lock %v", got.Status, got.LockExpiresAt)
	}

	o := h.seedOffer(h.buyer, h.payer, OfferAccepted)
	offerSale := h.seedSale(SalePendingReview, &o, false)
	got, err = h.svc.AdminApprove(ctx, adminID, offerSale.ID, "checked")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LockExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("offer approval must use the pay window: %v", got.LockExpiresAt)
	}

	// Only a sale awaiting review can be approved.
	if _, err := h.svc.AdminApprove(ctx, adminID, buy.ID, ""); code(err) != "CONFLICT" {
		t.Fatalf("second approval: %v", err)
	}
	if _, err := h.svc.AdminApprove(ctx, adminID, uuid.New(), ""); code(err) != "NOT_FOUND" {
		t.Fatalf("unknown sale: %v", err)
	}
}

func TestAdminApprove_DisabledMarket(t *testing.T) {
	h := newHarness(t, nil)
	h.svc.cfg.Enabled = false
	s := h.seedSale(SalePendingReview, nil, false)
	if _, err := h.svc.AdminApprove(context.Background(), adminID, s.ID, ""); code(err) != "MARKET_DISABLED" {
		t.Fatalf("got %v", err)
	}
}

func TestAdminDeny_ReleasesListing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	s := h.seedSale(SalePendingReview, nil, false)
	if _, err := h.svc.AdminDeny(ctx, adminID, s.ID, "  "); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("deny without reason: %v", err)
	}
	got, err := h.svc.AdminDeny(ctx, adminID, s.ID, "suspicious buyer")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SaleCancelled || h.repo.listings[h.listing].Status != ListingActive {
		t.Fatalf("deny: sale %s listing %s", got.Status, h.repo.listings[h.listing].Status)
	}

	// An offer sale: the offer is declined with the admin's reason.
	o := h.seedOffer(h.buyer, h.payer, OfferAccepted)
	os := h.seedSale(SalePendingReview, &o, false)
	if _, err := h.svc.AdminDeny(ctx, adminID, os.ID, "wash trade"); err != nil {
		t.Fatal(err)
	}
	if got := h.repo.offers[o.ID]; got.Status != OfferDeclined || got.DeclineNote != "denied by admin: wash trade" {
		t.Fatalf("offer after deny: %+v", got)
	}

	// Deny is only for sales awaiting review.
	locked := h.seedSale(SaleLocked, nil, false)
	if _, err := h.svc.AdminDeny(ctx, adminID, locked.ID, "late"); code(err) != "CONFLICT" {
		t.Fatalf("deny of a locked sale: %v", err)
	}
}

func TestAdminDelist_ShowsReason(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	if _, err := h.svc.AdminDelist(ctx, adminID, h.listing, ""); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("delist without reason: %v", err)
	}
	l, err := h.svc.AdminDelist(ctx, adminID, h.listing, " trademark complaint ")
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != ListingAdminDelisted || l.StatusReason != "trademark complaint" {
		t.Fatalf("delist: %+v", l)
	}
}

func TestAdminCancel_OnlyBeforeTx(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	for _, tc := range []struct {
		status string
		tx     bool
		ok     bool
	}{
		{SaleLocked, false, true},
		{SaleManualReview, true, true},
		{SaleLocked, true, false},
		{SaleSubmitted, true, false},
		{SaleConfirming, true, false},
		{SalePendingReview, false, false},
		{SaleCompleted, true, false},
	} {
		s := h.seedSale(tc.status, nil, tc.tx)
		_, err := h.svc.AdminCancel(ctx, adminID, s.ID, "stuck")
		if tc.ok != (err == nil) {
			t.Errorf("cancel %s (tx %v): %v", tc.status, tc.tx, err)
		}
		if !tc.ok && code(err) != "CONFLICT" {
			t.Errorf("cancel %s (tx %v): want CONFLICT, got %v", tc.status, tc.tx, err)
		}
	}
	s := h.seedSale(SaleLocked, nil, false)
	if _, err := h.svc.AdminCancel(ctx, adminID, s.ID, ""); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("cancel without reason: %v", err)
	}
}

func TestAdminReverse_RestoresSeller(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	s := h.seedSale(SaleCompleted, nil, true)
	h.repo.owner = h.buyer
	got, err := h.svc.AdminReverse(ctx, adminID, s.ID, "stolen funds")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SaleReversed || h.repo.owner != h.seller || h.repo.admin.reverseHold != 72*time.Hour {
		t.Fatalf("reverse: %s owner seller=%v hold %v", got.Status, h.repo.owner == h.seller, h.repo.admin.reverseHold)
	}
	other := h.seedSale(SaleLocked, nil, false)
	if _, err := h.svc.AdminReverse(ctx, adminID, other.ID, "x"); code(err) != "CONFLICT" {
		t.Fatalf("reverse of an open sale: %v", err)
	}
}

func TestAdminReverse_RequiresReason(t *testing.T) {
	h := newHarness(t, nil)
	s := h.seedSale(SaleCompleted, nil, true)
	h.repo.owner = h.buyer
	for _, reason := range []string{"", "   "} {
		if _, err := h.svc.AdminReverse(context.Background(), adminID, s.ID, reason); code(err) != "VALIDATION_FAILED" {
			t.Fatalf("reason %q: %v", reason, err)
		}
	}
	if h.repo.sales[s.ID].Status != SaleCompleted {
		t.Fatal("a refused reversal changed the sale")
	}
}

func TestAdminRestrict_Validation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	now := h.fixClock()
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	for name, in := range map[string]NewRestriction{
		"bad kind":    {Kind: "mute", Reason: "x"},
		"no reason":   {Kind: RestrictionBan, Reason: " "},
		"past until":  {Kind: RestrictionBan, Reason: "x", Until: &past},
		"empty kind":  {Reason: "x"},
		"nil account": {Kind: RestrictionBan, Reason: "x"},
	} {
		account := h.buyer
		if name == "nil account" {
			account = uuid.Nil
		}
		if _, err := h.svc.AdminRestrict(ctx, adminID, account, in); code(err) != "VALIDATION_FAILED" {
			t.Errorf("%s: %v", name, err)
		}
	}
	r, err := h.svc.AdminRestrict(ctx, adminID, h.buyer, NewRestriction{Kind: RestrictionOfferSuspension, Reason: "spam", Until: &future})
	if err != nil || r.CreatedBy == nil || *r.CreatedBy != adminID {
		t.Fatalf("restrict: %+v %v", r, err)
	}
}

// ban bans account through the admin service.
func (h *harness) ban(t *testing.T, account uuid.UUID) Restriction {
	t.Helper()
	r, err := h.svc.AdminRestrict(context.Background(), adminID, account, NewRestriction{Kind: RestrictionBan, Reason: "fraud"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRestriction_BanBlocksListOfferBuy(t *testing.T) {
	h := newHarness(t, nil)
	h.ban(t, h.seller)
	if _, err := h.list("25"); code(err) != "NOT_LISTABLE" {
		t.Fatalf("banned seller lists: %v", err)
	}

	h = newHarness(t, nil)
	h.ban(t, h.buyer)
	if _, err := h.buy("idem-key-1"); code(err) != "MARKET_RESTRICTED" {
		t.Fatalf("banned buyer buys: %v", err)
	}
	if _, err := h.offer("20", nil); code(err) != "MARKET_RESTRICTED" {
		t.Fatalf("banned buyer offers: %v", err)
	}
}

func TestRestriction_LiftRestores(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	sellerBan, buyerBan := h.ban(t, h.seller), h.ban(t, h.buyer)
	for _, r := range []Restriction{sellerBan, buyerBan} {
		got, err := h.svc.AdminLiftRestriction(ctx, adminID, r.ID, "")
		if err != nil || got.LiftedAt == nil {
			t.Fatalf("lift: %+v %v", got, err)
		}
	}
	if _, err := h.svc.AdminLiftRestriction(ctx, adminID, buyerBan.ID, ""); code(err) != "NOT_FOUND" {
		t.Fatalf("second lift: %v", err)
	}
	if _, err := h.list("25"); err != nil {
		t.Fatalf("list after lift: %v", err)
	}
	if _, err := h.offer("20", nil); err != nil {
		t.Fatalf("offer after lift: %v", err)
	}
	if _, err := h.buy("idem-key-2"); err != nil {
		t.Fatalf("buy after lift: %v", err)
	}
}

func TestAdminFilter_Validation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, nil)
	if _, err := h.svc.AdminSales(ctx, AdminFilter{Status: "bogus"}); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("bad sale status: %v", err)
	}
	if _, err := h.svc.AdminListings(ctx, AdminFilter{Status: SaleSubmitted}); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("sale status on listings: %v", err)
	}
	if _, err := h.svc.AdminOffers(ctx, AdminFilter{Status: OfferLapsed, Name: "yoda"}); err != nil {
		t.Fatalf("valid offer filter: %v", err)
	}
}
