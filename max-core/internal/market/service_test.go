package market

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-tron/base58"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/config"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

const devnetUSDC = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"

func solAddr() string {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	return base58.Encode(pub)
}

type fakeRepo struct {
	facts       NameFacts
	owner       uuid.UUID
	party       PartyFacts
	wallets     map[string]uuid.UUID // account|address
	restricted  map[uuid.UUID]bool
	listings    map[uuid.UUID]Listing
	sales       map[uuid.UUID]Sale
	opened      int
	nameListing *Listing

	// Offers (offers_test.go).
	nameID       uuid.UUID
	liveListing  *Listing
	offers       map[uuid.UUID]Offer
	restrictions map[string]*Restriction // account|kind
	strikes      map[uuid.UUID][]time.Time
	fundingSets  int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		facts:   NameFacts{Status: "active", OwnerStatus: "active"},
		wallets: map[string]uuid.UUID{}, restricted: map[uuid.UUID]bool{},
		listings: map[uuid.UUID]Listing{}, sales: map[uuid.UUID]Sale{},
		offers: map[uuid.UUID]Offer{}, restrictions: map[string]*Restriction{}, strikes: map[uuid.UUID][]time.Time{},
	}
}

func (f *fakeRepo) link(account uuid.UUID, addr string) {
	f.wallets[account.String()+"|"+addr] = uuid.New()
}

func (f *fakeRepo) NameFacts(context.Context, uuid.UUID) (NameFacts, uuid.UUID, error) {
	return f.facts, f.owner, nil
}
func (f *fakeRepo) PartyFacts(_ context.Context, buyer, seller uuid.UUID, _ string) (PartyFacts, error) {
	p := f.party
	p.BuyerAccount, p.SellerAccount = buyer, seller
	return p, nil
}
func (f *fakeRepo) ActiveWalletID(_ context.Context, account uuid.UUID, _ chains.Namespace, addr string) (uuid.UUID, error) {
	return f.wallets[account.String()+"|"+addr], nil
}
func (f *fakeRepo) Restricted(_ context.Context, account uuid.UUID, _ string) (bool, error) {
	return f.restricted[account], nil
}
func (f *fakeRepo) CreateListing(_ context.Context, in NewListing) (Listing, error) {
	l := Listing{ID: uuid.New(), MaxID: in.MaxID, SellerAccountID: in.Seller, Chain: in.Chain, AssetAddress: in.AssetAddress,
		AssetDecimals: in.AssetDecimals, PriceBaseUnits: in.PriceBaseUnits, PayoutAddress: in.PayoutAddress, Status: ListingActive}
	f.listings[l.ID] = l
	return l, nil
}
func (f *fakeRepo) GetListing(_ context.Context, id uuid.UUID) (Listing, error) {
	l, ok := f.listings[id]
	if !ok {
		return Listing{}, ErrListingNotFound
	}
	return l, nil
}
func (f *fakeRepo) UpdateListingPrice(_ context.Context, id, _ uuid.UUID, price string) (Listing, error) {
	l := f.listings[id]
	l.PriceBaseUnits = price
	f.listings[id] = l
	return l, nil
}
func (f *fakeRepo) CancelListing(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (f *fakeRepo) OpenSale(_ context.Context, in NewSale) (Sale, error) {
	f.opened++
	s := Sale{ID: uuid.New(), MaxID: in.MaxID, ListingID: in.ListingID, OfferID: in.OfferID, SellerAccountID: in.Seller, BuyerAccountID: in.Buyer,
		BuyerAddress: in.BuyerAddress, SellerPayoutAddress: in.SellerPayout, TreasuryAddress: in.Treasury, Chain: in.Chain,
		AssetAddress: in.AssetAddress, AssetDecimals: in.AssetDecimals, PriceBaseUnits: in.Price, FeeBPS: in.FeeBPS,
		FeeBaseUnits: in.Fee, SellerBaseUnits: in.SellerAmount, IdempotencyKey: in.IdempotencyKey, Status: in.Status,
		LockExpiresAt: in.LockExpiresAt}
	f.sales[s.ID] = s
	return s, nil
}
func (f *fakeRepo) SaleByIdempotencyKey(_ context.Context, buyer uuid.UUID, key string) (*Sale, error) {
	for _, s := range f.sales {
		if s.BuyerAccountID == buyer && s.IdempotencyKey == key {
			return &s, nil
		}
	}
	return nil, nil
}
func (f *fakeRepo) AttachTx(_ context.Context, id, _ uuid.UUID, h string) (Sale, error) {
	s := f.sales[id]
	s.Status, s.TxHash = SaleSubmitted, &h
	f.sales[id] = s
	return s, nil
}
func (f *fakeRepo) ProgressSale(_ context.Context, id uuid.UUID, p Progress) (Sale, error) {
	s := f.sales[id]
	s.Status = p.To
	f.sales[id] = s
	return s, nil
}
func (f *fakeRepo) CompleteSale(_ context.Context, id uuid.UUID, _ time.Duration, _ int64) (Sale, error) {
	s := f.sales[id]
	s.Status = SaleCompleted
	f.sales[id] = s
	return s, nil
}
func (f *fakeRepo) ExpireSale(_ context.Context, id uuid.UUID, _ string) (Sale, error) {
	s := f.sales[id]
	s.Status = SaleExpired
	f.sales[id] = s
	return s, nil
}
func (f *fakeRepo) GetSale(_ context.Context, id uuid.UUID) (Sale, error) {
	s, ok := f.sales[id]
	if !ok {
		return Sale{}, ErrSaleNotFound
	}
	return s, nil
}
func (f *fakeRepo) SearchListings(context.Context, ListingQuery) ([]ListingCard, string, error) {
	return nil, "", nil
}
func (f *fakeRepo) NameView(context.Context, string) (NameView, error) {
	return NameView{Facts: f.facts, Listing: f.nameListing}, nil
}
func (f *fakeRepo) Inbox(context.Context, uuid.UUID) (Inbox, error) { return Inbox{}, nil }

type fakeAdapter struct {
	fundsErr error
	obs      settle.Observation
}

func (a *fakeAdapter) Quote(context.Context, settle.Order) (settle.Quote, error) {
	return settle.Quote{NetworkFee: big.NewInt(5000), RentPerATA: big.NewInt(0)}, nil
}
func (a *fakeAdapter) CheckFunds(context.Context, settle.Order, settle.Quote) error {
	return a.fundsErr
}
func (a *fakeAdapter) BuildTx(o settle.Order, _ settle.Quote) (settle.Instructions, error) {
	return settle.Instructions{Solana: []settle.SolanaIx{{Kind: "memo", Memo: "max:sale:" + o.SaleID.String()}}}, nil
}
func (a *fakeAdapter) Observe(context.Context, settle.Order, string) (settle.Observation, error) {
	return a.obs, nil
}
func (a *fakeAdapter) Balances(context.Context, chains.Chain, string) (map[string]*big.Int, error) {
	return nil, nil
}

type harness struct {
	svc            *Service
	repo           *fakeRepo
	adapter        *fakeAdapter
	seller, buyer  uuid.UUID
	payout, payer  string
	maxID, listing uuid.UUID
}

func newHarness(t *testing.T, mutate func(*config.MarketConfig)) *harness {
	t.Helper()
	mc := config.MarketConfig{
		Enabled: true, Assets: "solana-devnet:" + devnetUSDC, FeeBPS: 1000, HoldPeriod: 72 * time.Hour,
		BuyLock: 15 * time.Minute, OfferPayWindow: 24 * time.Hour, MinOffer: "1", TreasurySolana: solAddr(),
	}
	if mutate != nil {
		mutate(&mc)
	}
	cfg, err := NewConfig(mc, map[string]string{"solana-devnet": "https://rpc.test"})
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{repo: newFakeRepo(), adapter: &fakeAdapter{}, seller: uuid.New(), buyer: uuid.New(),
		payout: solAddr(), payer: solAddr(), maxID: uuid.New()}
	h.repo.owner = h.seller
	h.repo.nameID = h.maxID
	h.repo.link(h.seller, h.payout)
	h.repo.link(h.buyer, h.payer)
	h.listing = uuid.New()
	h.repo.listings[h.listing] = Listing{ID: h.listing, MaxID: h.maxID, SellerAccountID: h.seller, Chain: "solana-devnet",
		AssetAddress: devnetUSDC, AssetDecimals: 6, PriceBaseUnits: "25000000", PayoutAddress: h.payout, Status: ListingActive}
	h.svc = NewService(h.repo, map[chains.Namespace]settle.Adapter{chains.NamespaceSolana: h.adapter}, cfg, AllowAll{}, zap.NewNop())
	return h
}

func human(id uuid.UUID) shared.Principal {
	return shared.Principal{AccountID: id, AccountType: "human"}
}

func (h *harness) list(price string) (Listing, error) {
	return h.svc.List(context.Background(), human(h.seller), ListInput{
		MaxID: h.maxID, Price: price, Chain: "solana-devnet", Asset: devnetUSDC, PayoutAddr: h.payout,
	})
}

func (h *harness) buy(key string) (SaleView, error) {
	return h.svc.Buy(context.Background(), human(h.buyer), h.listing, BuyInput{FromAddress: h.payer, IdempotencyKey: key})
}

func code(err error) string {
	var app *shared.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func TestList_OK(t *testing.T) {
	h := newHarness(t, nil)
	l, err := h.list("25.5")
	if err != nil {
		t.Fatal(err)
	}
	if l.PriceBaseUnits != "25500000" || l.AssetAddress != devnetUSDC || l.Chain != "solana-devnet" || l.AssetDecimals != 6 {
		t.Fatalf("listing %+v", l)
	}
}

func TestList_RejectsNotListable(t *testing.T) {
	now := time.Now()
	cases := map[string]NameFacts{
		ReasonNotActive:      {Status: "locked", OwnerStatus: "active"},
		ReasonOwnerSuspended: {Status: "active", OwnerStatus: "suspended"},
		ReasonReservedClaim:  {Status: "active", OwnerStatus: "active", HasLiveReservedClaim: true},
		ReasonHoldingPeriod:  {Status: "active", OwnerStatus: "active", ListableAt: now.Add(time.Hour)},
		ReasonRenameCooldown: {Status: "active", OwnerStatus: "active", RenameAvailableAt: now.Add(time.Hour)},
		ReasonRenamePending:  {Status: "active", OwnerStatus: "active", RenamePending: true},
		ReasonMarketBanned:   {Status: "active", OwnerStatus: "active", OwnerBanned: true},
	}
	for reason, facts := range cases {
		h := newHarness(t, nil)
		h.repo.facts = facts
		_, err := h.list("25")
		var app *shared.AppError
		if !errors.As(err, &app) || app.Code != "NOT_LISTABLE" || app.Details["reason"] != reason {
			t.Errorf("%s: got %v", reason, err)
		}
	}
}

func TestList_RejectsNonOwner(t *testing.T) {
	h := newHarness(t, nil)
	h.repo.owner = uuid.New()
	if _, err := h.list("25"); !errors.Is(err, maxids.ErrMaxIDNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestList_RejectsPayoutNotOwned(t *testing.T) {
	h := newHarness(t, nil)
	h.payout = solAddr() // valid address, not linked to the seller
	_, err := h.list("25")
	if code(err) != "VALIDATION_FAILED" {
		t.Fatalf("got %v", err)
	}
}

func TestList_RejectsBelowMinimum(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.list("0.99"); !errors.Is(err, ErrOfferBelowMinimum) {
		t.Fatalf("got %v", err)
	}
}

func TestList_RejectsAssetNotAccepted(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.svc.List(context.Background(), human(h.seller), ListInput{
		MaxID: h.maxID, Price: "25", Chain: "solana", Asset: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", PayoutAddr: h.payout,
	})
	if !errors.Is(err, ErrAssetNotAccepted) {
		t.Fatalf("got %v", err)
	}
}

func TestList_PausedAndDisabled(t *testing.T) {
	h := newHarness(t, func(c *config.MarketConfig) { c.TradingPaused = true })
	if _, err := h.list("25"); !errors.Is(err, ErrMarketPaused) {
		t.Fatalf("paused: %v", err)
	}
	h = newHarness(t, func(c *config.MarketConfig) { c.Enabled = false })
	if _, err := h.list("25"); !errors.Is(err, ErrMarketDisabled) {
		t.Fatalf("disabled: %v", err)
	}
}

func TestBuy_OK(t *testing.T) {
	h := newHarness(t, nil)
	v, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	s := v.Sale
	if s.Status != SaleLocked || s.LockExpiresAt == nil || time.Until(*s.LockExpiresAt) < 14*time.Minute {
		t.Fatalf("sale %+v", s)
	}
	if s.FeeBaseUnits != "2500000" || s.SellerBaseUnits != "22500000" || s.BuyerAddress != h.payer || s.SellerPayoutAddress != h.payout {
		t.Fatalf("snapshot %+v", s)
	}
	if v.Instructions == nil || len(v.Instructions.Solana) != 1 || v.Instructions.Solana[0].Memo != "max:sale:"+s.ID.String() {
		t.Fatalf("instructions %+v", v.Instructions)
	}
	q := v.Quote
	if q == nil || q.Price != "25" || q.Fee != "2.5" || q.SellerReceives != "22.5" || q.NetworkFee != "0.000005" || q.TotalAsset != "25" {
		t.Fatalf("quote %+v", q)
	}
}

func TestBuy_SelfTrade(t *testing.T) {
	for name, p := range map[string]PartyFacts{
		"shared wallet": {SharedWallet: true},
		"payer":         {PayerWalletEverSellers: true},
	} {
		h := newHarness(t, nil)
		h.repo.party = p
		if _, err := h.buy("buy-key-0001"); !errors.Is(err, ErrSelfTrade) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	h := newHarness(t, nil)
	h.buyer = h.seller
	h.repo.link(h.seller, h.payer)
	if _, err := h.buy("buy-key-0001"); !errors.Is(err, ErrSelfTrade) {
		t.Errorf("same account: got %v", err)
	}
}

func TestBuy_InsufficientFunds(t *testing.T) {
	h := newHarness(t, nil)
	h.adapter.fundsErr = &settle.FundsShortfall{
		NeedAsset: big.NewInt(25_000_000), HaveAsset: big.NewInt(1), NeedNative: big.NewInt(5000), HaveNative: big.NewInt(0),
	}
	_, err := h.buy("buy-key-0001")
	var app *shared.AppError
	if !errors.As(err, &app) || app.Code != "INSUFFICIENT_FUNDS" {
		t.Fatalf("got %v", err)
	}
	want := map[string]any{"needAsset": "25000000", "haveAsset": "1", "needNative": "5000", "haveNative": "0"}
	for k, v := range want {
		if app.Details[k] != v {
			t.Errorf("%s = %v, want %v", k, app.Details[k], v)
		}
	}
	if h.repo.opened != 0 {
		t.Fatal("sale opened despite missing funds")
	}
}

func TestBuy_ReviewThreshold(t *testing.T) {
	h := newHarness(t, func(c *config.MarketConfig) { c.ReviewThreshold = "10" })
	v, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	if v.Sale.Status != SalePendingReview || v.Sale.LockExpiresAt != nil || v.Instructions != nil {
		t.Fatalf("view %+v", v)
	}
}

func TestBuy_Paused(t *testing.T) {
	h := newHarness(t, func(c *config.MarketConfig) { c.TradingPaused = true })
	if _, err := h.buy("buy-key-0001"); !errors.Is(err, ErrMarketPaused) {
		t.Fatalf("got %v", err)
	}
}

func TestBuy_IdempotentReplay(t *testing.T) {
	h := newHarness(t, nil)
	a, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	// The listing is locked now; a replay must still return the same sale.
	l := h.repo.listings[h.listing]
	l.Status = ListingLocked
	h.repo.listings[h.listing] = l
	b, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	if a.Sale.ID != b.Sale.ID || h.repo.opened != 1 || !b.Replayed || b.Instructions == nil {
		t.Fatalf("replay: %v vs %v, opened %d", a.Sale.ID, b.Sale.ID, h.repo.opened)
	}
}

func TestBuy_LockedListingIsNameLocked(t *testing.T) {
	h := newHarness(t, nil)
	l := h.repo.listings[h.listing]
	l.Status = ListingLocked
	h.repo.listings[h.listing] = l
	if _, err := h.buy("buy-key-0001"); !errors.Is(err, ErrNameLocked) {
		t.Fatalf("got %v", err)
	}
}

func TestBuy_RejectsUnlinkedPayer(t *testing.T) {
	h := newHarness(t, nil)
	h.payer = solAddr()
	if _, err := h.buy("buy-key-0001"); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("got %v", err)
	}
}

func TestBuy_BannedBuyer(t *testing.T) {
	h := newHarness(t, nil)
	h.repo.restricted[h.buyer] = true
	if _, err := h.buy("buy-key-0001"); !errors.Is(err, ErrMarketRestricted) {
		t.Fatalf("got %v", err)
	}
}

func TestSubmit_WrongBuyer(t *testing.T) {
	h := newHarness(t, nil)
	v, err := h.buy("buy-key-0001")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.svc.Submit(context.Background(), human(uuid.New()), v.Sale.ID, validSig())
	var app *shared.AppError
	if !errors.As(err, &app) || app.Status != http.StatusNotFound {
		t.Fatalf("got %v", err)
	}
}

func validSig() string {
	b := make([]byte, 64)
	_, _ = rand.Read(b)
	return base58.Encode(b)
}

func TestSubmit_OKAndIdempotent(t *testing.T) {
	h := newHarness(t, nil)
	v, _ := h.buy("buy-key-0001")
	sig := validSig()
	out, err := h.svc.Submit(context.Background(), human(h.buyer), v.Sale.ID, sig)
	if err != nil || out.Sale.Status != SaleSubmitted {
		t.Fatalf("submit: %v %+v", err, out.Sale)
	}
	if _, err := h.svc.Submit(context.Background(), human(h.buyer), v.Sale.ID, sig); err != nil {
		t.Fatalf("resubmit same hash: %v", err)
	}
	if _, err := h.svc.Submit(context.Background(), human(h.buyer), v.Sale.ID, validSig()); !errors.Is(err, ErrTxMismatch) {
		t.Fatalf("different hash: %v", err)
	}
	if _, err := h.svc.Submit(context.Background(), human(h.buyer), v.Sale.ID, "nope"); code(err) != "VALIDATION_FAILED" {
		t.Fatalf("bad hash: %v", err)
	}
}

func TestSubmit_AllowedWhilePaused(t *testing.T) {
	h := newHarness(t, nil)
	v, _ := h.buy("buy-key-0001")
	h.svc.cfg.TradingPaused = true
	if _, err := h.svc.Submit(context.Background(), human(h.buyer), v.Sale.ID, validSig()); err != nil {
		t.Fatalf("submit while paused: %v", err)
	}
}

func TestObserveSale_Transitions(t *testing.T) {
	cases := []struct {
		obs  settle.Observation
		want string
	}{
		{settle.Observation{Status: settle.StatusConfirmed, Matches: true}, SaleCompleted},
		{settle.Observation{Status: settle.StatusConfirmed, Matches: false, Reason: "wrong amount"}, SaleManualReview},
		{settle.Observation{Status: settle.StatusFailed}, SaleExpired},
		{settle.Observation{Status: settle.StatusPending}, SaleConfirming},
		{settle.Observation{Status: settle.StatusNotFound}, SaleSubmitted},
	}
	for _, c := range cases {
		h := newHarness(t, nil)
		v, _ := h.buy("buy-key-0001")
		if _, err := h.svc.Submit(context.Background(), human(h.buyer), v.Sale.ID, validSig()); err != nil {
			t.Fatal(err)
		}
		h.adapter.obs = c.obs
		s, err := h.svc.ObserveSale(context.Background(), v.Sale.ID)
		if err != nil || s.Status != c.want {
			t.Errorf("%s: got %q %v, want %q", c.obs.Status, s.Status, err, c.want)
		}
	}
}

func TestGetSale_OnlyParties(t *testing.T) {
	h := newHarness(t, nil)
	v, _ := h.buy("buy-key-0001")
	if _, err := h.svc.GetSale(context.Background(), human(uuid.New()), v.Sale.ID); !errors.Is(err, ErrSaleNotFound) {
		t.Fatalf("stranger: %v", err)
	}
	got, err := h.svc.GetSale(context.Background(), human(h.buyer), v.Sale.ID)
	if err != nil || got.Instructions == nil {
		t.Fatalf("buyer view: %v %+v", err, got)
	}
	got, err = h.svc.GetSale(context.Background(), human(h.seller), v.Sale.ID)
	if err != nil || got.Instructions != nil {
		t.Fatalf("seller view: %v %+v", err, got)
	}
}

func TestNewConfig_Startup(t *testing.T) {
	base := config.MarketConfig{Enabled: true, Assets: "solana-devnet:" + devnetUSDC, FeeBPS: 1000, BuyLock: time.Minute,
		OfferPayWindow: time.Hour, MinOffer: "1", TreasurySolana: solAddr()}
	rpc := map[string]string{"solana-devnet": "https://rpc.test"}
	if _, err := NewConfig(base, rpc); err != nil {
		t.Fatalf("valid: %v", err)
	}
	noTreasury := base
	noTreasury.TreasurySolana = ""
	if _, err := NewConfig(noTreasury, rpc); err == nil {
		t.Error("missing treasury accepted")
	}
	if _, err := NewConfig(base, nil); err == nil {
		t.Error("missing RPC accepted")
	}
	unverified := base
	unverified.Assets = "solana-devnet:" + solAddr()
	if _, err := NewConfig(unverified, rpc); err == nil {
		t.Error("unverified token accepted")
	}
	evm := base
	evm.Assets = "base:0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	if _, err := NewConfig(evm, rpc); err == nil {
		t.Error("EVM settlement asset accepted")
	}
	off := config.MarketConfig{MinOffer: "1"}
	if _, err := NewConfig(off, nil); err != nil {
		t.Errorf("disabled with no assets: %v", err)
	}
}

func TestNameView_HidesModerationReasons(t *testing.T) {
	for facts, want := range map[NameFacts]string{
		{Status: "active", OwnerStatus: "active", OwnerBanned: true}:   ReasonUnavailable,
		{Status: "active", OwnerStatus: "suspended"}:                   ReasonUnavailable,
		{Status: "active", OwnerStatus: "active", RenamePending: true}: ReasonRenamePending,
	} {
		h := newHarness(t, nil)
		h.repo.facts = facts
		v, err := h.svc.NameView(context.Background(), "yoda")
		if err != nil || v.Listable || v.Reason != want {
			t.Errorf("%+v: got %q %v, want %q", facts, v.Reason, err, want)
		}
	}
	// The owner's own eligibility keeps the real reason.
	h := newHarness(t, nil)
	h.repo.facts = NameFacts{Status: "active", OwnerStatus: "active", OwnerBanned: true}
	if e, err := h.svc.Eligibility(context.Background(), human(h.seller), h.maxID); err != nil || e.Reason != ReasonMarketBanned {
		t.Errorf("eligibility reason %q %v", e.Reason, err)
	}
}

func TestNameView_HidesExpiredListing(t *testing.T) {
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	for _, c := range []struct {
		status  string
		expires *time.Time
		shown   bool
	}{
		{ListingActive, &past, false},
		{ListingActive, &future, true},
		{ListingActive, nil, true},
		{ListingLocked, &past, true}, // an open sale is still in progress
	} {
		h := newHarness(t, nil)
		h.repo.nameListing = &Listing{ID: uuid.New(), Status: c.status, ExpiresAt: c.expires}
		v, err := h.svc.NameView(context.Background(), "yoda")
		if err != nil || (v.Listing != nil) != c.shown {
			t.Errorf("%s expires %v: shown=%v err=%v", c.status, c.expires, v.Listing != nil, err)
		}
	}
}
