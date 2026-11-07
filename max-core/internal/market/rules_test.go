package market

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/max/max-core/internal/maxids"
)

func TestSplitFee(t *testing.T) {
	rows := []struct {
		price      int64
		bps        int
		fee, payee int64
	}{
		{1_000_000, 1000, 100_000, 900_000},
		{999_999, 1000, 99_999, 900_000},
		{1, 1000, 0, 1},
		{500, 0, 0, 500},
		{500, 10000, 500, 0},
	}
	for _, r := range rows {
		p := big.NewInt(r.price)
		fee, seller := SplitFee(p, r.bps)
		if fee.Int64() != r.fee || seller.Int64() != r.payee {
			t.Errorf("SplitFee(%d,%d)=(%s,%s)", r.price, r.bps, fee, seller)
		}
		if new(big.Int).Add(fee, seller).Cmp(p) != 0 {
			t.Errorf("fee+seller != price for %d", r.price)
		}
		if p.Int64() != r.price {
			t.Errorf("input mutated")
		}
	}
	huge, _ := new(big.Int).SetString("99999999999999999999999999999999999999", 10)
	fee, seller := SplitFee(huge, 1000)
	if new(big.Int).Add(fee, seller).Cmp(huge) != 0 {
		t.Error("big sum mismatch")
	}
}

func TestSplitFeePanics(t *testing.T) {
	for name, f := range map[string]func(){
		"nil":     func() { SplitFee(nil, 10) },
		"neg":     func() { SplitFee(big.NewInt(-1), 10) },
		"bpsLow":  func() { SplitFee(big.NewInt(1), -1) },
		"bpsHigh": func() { SplitFee(big.NewInt(1), 10001) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected panic", name)
				}
			}()
			f()
		}()
	}
}

func TestListable(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	good := NameFacts{Status: "active", OwnerStatus: "active", ListableAt: past, RenameAvailableAt: past}
	mod := func(fn func(*NameFacts)) NameFacts { f := good; fn(&f); return f }
	rows := []struct {
		name   string
		f      NameFacts
		ok     bool
		reason string
	}{
		{"happy", good, true, ""},
		{"boundary", mod(func(f *NameFacts) { f.ListableAt = now; f.RenameAvailableAt = now }), true, ""},
		{"not_active", mod(func(f *NameFacts) { f.Status = "released" }), false, ReasonNotActive},
		{"owner_suspended", mod(func(f *NameFacts) { f.OwnerStatus = "suspended" }), false, ReasonOwnerSuspended},
		{"reserved_claim", mod(func(f *NameFacts) { f.HasLiveReservedClaim = true }), false, ReasonReservedClaim},
		{"holding_period", mod(func(f *NameFacts) { f.ListableAt = future }), false, ReasonHoldingPeriod},
		{"rename_cooldown", mod(func(f *NameFacts) { f.RenameAvailableAt = future }), false, ReasonRenameCooldown},
		{"rename_pending", mod(func(f *NameFacts) { f.RenamePending = true }), false, ReasonRenamePending},
		{"market_banned", mod(func(f *NameFacts) { f.OwnerBanned = true }), false, ReasonMarketBanned},
		{"order", NameFacts{Status: "x", OwnerStatus: "y", HasLiveReservedClaim: true, RenamePending: true, OwnerBanned: true, ListableAt: future, RenameAvailableAt: future}, false, ReasonNotActive},
		{"order2", mod(func(f *NameFacts) { f.RenamePending = true; f.OwnerBanned = true }), false, ReasonRenamePending},
	}
	for _, r := range rows {
		ok, reason := Listable(r.f, now)
		if ok != r.ok || reason != r.reason {
			t.Errorf("%s: got (%v,%q)", r.name, ok, reason)
		}
	}
}

func TestSelfTrade(t *testing.T) {
	a, b, admin := uuid.New(), uuid.New(), uuid.New()
	rows := []struct {
		name string
		p    PartyFacts
		want bool
	}{
		{"unrelated", PartyFacts{BuyerAccount: a, SellerAccount: b}, false},
		{"unrelated admins", PartyFacts{BuyerAccount: a, SellerAccount: b, BuyerAdmin: &admin, SellerAdmin: ptr(uuid.New())}, false},
		{"same account", PartyFacts{BuyerAccount: a, SellerAccount: a}, true},
		{"shared wallet", PartyFacts{BuyerAccount: a, SellerAccount: b, SharedWallet: true}, true},
		{"payer wallet was seller's", PartyFacts{BuyerAccount: a, SellerAccount: b, PayerWalletEverSellers: true}, true},
		{"buyer agent of seller", PartyFacts{BuyerAccount: a, SellerAccount: b, BuyerAdmin: &b}, true},
		{"seller agent of buyer", PartyFacts{BuyerAccount: a, SellerAccount: b, SellerAdmin: &a}, true},
		{"same admin", PartyFacts{BuyerAccount: a, SellerAccount: b, BuyerAdmin: &admin, SellerAdmin: &admin}, true},
	}
	for _, r := range rows {
		if got := SelfTrade(r.p); got != r.want {
			t.Errorf("%s: got %v", r.name, got)
		}
	}
}

func ptr(u uuid.UUID) *uuid.UUID { return &u }

func TestErrors(t *testing.T) {
	e := NotListable(ReasonHoldingPeriod)
	if e.Details["reason"] != ReasonHoldingPeriod || e.Status != 422 || e.Code != "NOT_LISTABLE" {
		t.Errorf("bad NotListable: %+v", e)
	}
	if ErrNotListable.Details != nil {
		t.Error("sentinel mutated")
	}
	if !errors.Is(ErrNameLocked, maxids.ErrNameLocked) {
		t.Error("NAME_LOCKED not shared")
	}
	want := map[string]int{
		"MARKET_DISABLED": 404, "MARKET_PAUSED": 503, "NOT_LISTABLE": 422, "SELF_TRADE": 422,
		"ASSET_NOT_ACCEPTED": 422, "OFFER_BELOW_MINIMUM": 422, "INSUFFICIENT_FUNDS": 422,
		"OFFER_UNFUNDED": 422, "TX_MISMATCH": 422, "MARKET_RESTRICTED": 403,
		"LISTING_NOT_ACTIVE": 409, "SALE_EXPIRED": 409, "OFFER_LIMIT": 409, "PENDING_APPROVAL": 202,
		"SALE_LIMIT": 409,
	}
	got := map[string]int{
		ErrMarketDisabled.Code: ErrMarketDisabled.Status, ErrMarketPaused.Code: ErrMarketPaused.Status,
		ErrNotListable.Code: ErrNotListable.Status, ErrSelfTrade.Code: ErrSelfTrade.Status,
		ErrAssetNotAccepted.Code: ErrAssetNotAccepted.Status, ErrOfferBelowMinimum.Code: ErrOfferBelowMinimum.Status,
		ErrInsufficientFunds.Code: ErrInsufficientFunds.Status, ErrOfferUnfunded.Code: ErrOfferUnfunded.Status,
		ErrTxMismatch.Code: ErrTxMismatch.Status, ErrMarketRestricted.Code: ErrMarketRestricted.Status,
		ErrListingNotActive.Code: ErrListingNotActive.Status, ErrSaleExpired.Code: ErrSaleExpired.Status,
		ErrOfferLimit.Code: ErrOfferLimit.Status, ErrPendingApproval.Code: ErrPendingApproval.Status,
		ErrSaleLimit.Code: ErrSaleLimit.Status,
	}
	for c, s := range want {
		if got[c] != s {
			t.Errorf("%s status %d want %d", c, got[c], s)
		}
	}
}

func TestListable_DefaultID(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	// A default is never listable, even when every other fact is clean, and
	// the check wins over every other reason.
	f := NameFacts{Status: "active", OwnerStatus: "active", ListableAt: past, RenameAvailableAt: past, IsDefault: true}
	if ok, reason := Listable(f, now); ok || reason != ReasonDefaultID {
		t.Fatalf("clean default: (%v,%q)", ok, reason)
	}
	f.Status, f.OwnerBanned = "released", true
	if ok, reason := Listable(f, now); ok || reason != ReasonDefaultID {
		t.Fatalf("default with other failures: (%v,%q)", ok, reason)
	}
}

func TestPublicReason_DefaultMasked(t *testing.T) {
	if got := PublicReason(ReasonDefaultID); got != ReasonUnavailable {
		t.Fatalf("PublicReason(default_id) = %q", got)
	}
}
