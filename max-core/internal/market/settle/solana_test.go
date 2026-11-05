package settle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/solanarpc"
)

const (
	usdc     = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	otherMnt = "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"
	buyer    = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
	seller   = "EKpQGSJtjMFqKZ9KQanSqYXRcF8fBopzLHYxdM65zcjm"
	treasury = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	rent     = 2039280
	zeroRent = 890880
)

type fakeRPC struct {
	tx         *solanarpc.Transaction
	status     string
	existing   map[string]bool
	asset      uint64
	noBuyerATA bool // buyer's ATA does not exist
	buyerATA   string
	native     uint64
	rent       uint64
}

func (f *fakeRPC) Transaction(context.Context, string) (*solanarpc.Transaction, error) {
	return f.tx, nil
}
func (f *fakeRPC) ConfirmationStatus(context.Context, string) (string, error) { return f.status, nil }
func (f *fakeRPC) AccountInfo(_ context.Context, a string) (*solanarpc.AccountInfo, error) {
	if a == f.buyerATA && !f.noBuyerATA {
		var ai solanarpc.AccountInfo
		raw := fmt.Sprintf(`{"type":"account","info":{"tokenAmount":{"amount":"%d"}}}`, f.asset)
		if err := json.Unmarshal([]byte(`{"data":{"parsed":`+raw+`}}`), &ai); err != nil {
			panic(err)
		}
		return &ai, nil
	}
	if f.existing[a] {
		return &solanarpc.AccountInfo{}, nil
	}
	return nil, nil
}
func (f *fakeRPC) Balance(context.Context, string) (uint64, error) { return f.native, nil }
func (f *fakeRPC) TokenAccountsByOwner(context.Context, string, string) (uint64, error) {
	return f.asset, nil
}
func (f *fakeRPC) MinimumBalanceForRentExemption(_ context.Context, size int) (uint64, error) {
	switch size {
	case 165:
		return f.rent, nil
	case 0:
		return zeroRent, nil
	}
	return 0, fmt.Errorf("unexpected size %d", size)
}

func order(t *testing.T) Order {
	t.Helper()
	ch, _ := chains.Lookup("solana")
	return Order{
		SaleID: uuid.MustParse("11111111-2222-3333-4444-555555555555"), Chain: ch,
		Mint: usdc, Decimals: 6, Buyer: buyer, Seller: seller, Treasury: treasury,
		Price: big.NewInt(10_000_000), Fee: big.NewInt(1_000_000), SellerAmount: big.NewInt(9_000_000),
	}
}

func ata(t *testing.T, owner string) string {
	t.Helper()
	a, err := chains.AssociatedTokenAddress(owner, usdc, TokenProgram)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func adapter(f *fakeRPC) Adapter { return NewSolana(map[string]SolanaRPC{"solana": f}) }

func TestQuote_IncludesRentForMissingATAs(t *testing.T) {
	o := order(t)
	f := &fakeRPC{rent: rent, existing: map[string]bool{ata(t, treasury): true}}
	q, err := adapter(f).Quote(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !q.CreateSellerATA || q.CreateTreasuryATA {
		t.Fatalf("flags = %+v", q)
	}
	if want := big.NewInt(5000 + rent); q.NetworkFee.Cmp(want) != 0 {
		t.Fatalf("fee = %v want %v", q.NetworkFee, want)
	}
	if q.RentPerATA.Int64() != rent {
		t.Fatalf("rent = %v", q.RentPerATA)
	}
}

func TestQuote_UnknownChain(t *testing.T) {
	o := order(t)
	o.Chain = chains.Chain{Key: "solana-devnet"}
	if _, err := adapter(&fakeRPC{}).Quote(context.Background(), o); err == nil {
		t.Fatal("expected error")
	}
}

func quote() Quote {
	return Quote{NetworkFee: big.NewInt(7000), RentPerATA: big.NewInt(rent)}
}

func fundsRPC(t *testing.T, asset, native uint64) *fakeRPC {
	return &fakeRPC{asset: asset, native: native, buyerATA: ata(t, buyer)}
}

func TestCheckFunds_OK(t *testing.T) {
	f := fundsRPC(t, 10_000_000, 7000+zeroRent)
	if err := adapter(f).CheckFunds(context.Background(), order(t), quote()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFunds_ShortAsset(t *testing.T) {
	f := fundsRPC(t, 9_999_999, 9000+zeroRent)
	err := adapter(f).CheckFunds(context.Background(), order(t), quote())
	var s *FundsShortfall
	if !errors.As(err, &s) {
		t.Fatalf("err = %v", err)
	}
	if s.NeedAsset.Int64() != 10_000_000 || s.HaveAsset.Int64() != 9_999_999 ||
		s.NeedNative.Int64() != 7000+zeroRent || s.HaveNative.Int64() != 9000+zeroRent {
		t.Fatalf("%+v", s)
	}
}

func TestCheckFunds_ShortNative(t *testing.T) {
	f := fundsRPC(t, 20_000_000, 7000+zeroRent-1)
	err := adapter(f).CheckFunds(context.Background(), order(t), quote())
	var s *FundsShortfall
	if !errors.As(err, &s) {
		t.Fatalf("err = %v", err)
	}
	if s.NeedNative.Int64() != 7000+zeroRent || s.HaveNative.Int64() != 7000+zeroRent-1 || s.HaveAsset.Int64() != 20_000_000 {
		t.Fatalf("%+v", s)
	}
}

// Fee alone is covered, but paying it would leave the buyer below rent exemption.
func TestCheckFunds_NativeMustStayRentExempt(t *testing.T) {
	f := fundsRPC(t, 20_000_000, 7000)
	var s *FundsShortfall
	if err := adapter(f).CheckFunds(context.Background(), order(t), quote()); !errors.As(err, &s) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckFunds_MissingBuyerATAIsZero(t *testing.T) {
	f := fundsRPC(t, 99_000_000, 1_000_000_000)
	f.noBuyerATA = true
	var s *FundsShortfall
	err := adapter(f).CheckFunds(context.Background(), order(t), quote())
	if !errors.As(err, &s) || s.HaveAsset.Sign() != 0 {
		t.Fatalf("err = %v", err)
	}
}

func kinds(ix Instructions) []string {
	var k []string
	for _, i := range ix.Solana {
		k = append(k, i.Kind)
	}
	return k
}

func TestBuildTx_Order(t *testing.T) {
	o := order(t)
	ix, err := adapter(&fakeRPC{}).BuildTx(o, Quote{CreateSellerATA: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create_ata_idempotent", "transfer_checked", "transfer_checked", "memo"}
	if fmt.Sprint(kinds(ix)) != fmt.Sprint(want) {
		t.Fatalf("kinds = %v", kinds(ix))
	}
	s := ix.Solana
	if s[0].Accounts["ata"] != ata(t, seller) || s[0].Accounts["owner"] != seller {
		t.Fatalf("create = %+v", s[0])
	}
	if s[1].Accounts["destination"] != ata(t, treasury) || s[1].Amount != "1000000" || s[1].Decimals != 6 {
		t.Fatalf("fee transfer = %+v", s[1])
	}
	if s[2].Accounts["destination"] != ata(t, seller) || s[2].Amount != "9000000" ||
		s[2].Accounts["source"] != ata(t, buyer) || s[2].Accounts["authority"] != buyer {
		t.Fatalf("seller transfer = %+v", s[2])
	}
	if s[3].Memo != "max:sale:11111111-2222-3333-4444-555555555555" {
		t.Fatalf("memo = %q", s[3].Memo)
	}
}

func TestBuildTx_TreasuryCreateOnlyWhenNeeded(t *testing.T) {
	o := order(t)
	ix, _ := adapter(&fakeRPC{}).BuildTx(o, Quote{CreateTreasuryATA: true, CreateSellerATA: true})
	if len(ix.Solana) != 5 || ix.Solana[0].Accounts["owner"] != treasury || ix.Solana[1].Accounts["owner"] != seller {
		t.Fatalf("kinds = %v", kinds(ix))
	}
	ix, _ = adapter(&fakeRPC{}).BuildTx(o, Quote{})
	if len(ix.Solana) != 3 {
		t.Fatalf("kinds = %v", kinds(ix))
	}
}

// --- Observe ---

type ixSpec struct {
	program string
	parsed  any
}

func transferChecked(mint, src, dst, auth string, amount string) ixSpec {
	return ixSpec{TokenProgram, map[string]any{"type": "transferChecked", "info": map[string]any{
		"source": src, "destination": dst, "mint": mint, "authority": auth,
		"tokenAmount": map[string]any{"amount": amount, "decimals": 6},
	}}}
}

func memo(text string) ixSpec { return ixSpec{MemoProgram, text} }

func mkTx(t *testing.T, payer string, failed bool, ixs ...ixSpec) *solanarpc.Transaction {
	t.Helper()
	var list []map[string]any
	for _, i := range ixs {
		list = append(list, map[string]any{"programId": i.program, "parsed": i.parsed})
	}
	meta := map[string]any{"err": nil}
	if failed {
		meta["err"] = map[string]any{"InstructionError": []any{0, "Custom"}}
	}
	raw, _ := json.Marshal(map[string]any{
		"meta": meta,
		"transaction": map[string]any{"message": map[string]any{
			"accountKeys":  []map[string]any{{"pubkey": payer, "signer": true, "writable": true}},
			"instructions": list,
		}},
	})
	var tx solanarpc.Transaction
	if err := json.Unmarshal(raw, &tx); err != nil {
		t.Fatal(err)
	}
	return &tx
}

func goodIxs(t *testing.T) []ixSpec {
	return []ixSpec{
		{ATAProgram, map[string]any{"type": "createIdempotent", "info": map[string]any{}}},
		transferChecked(usdc, ata(t, buyer), ata(t, treasury), buyer, "1000000"),
		transferChecked(usdc, ata(t, buyer), ata(t, seller), buyer, "9000000"),
		memo("max:sale:11111111-2222-3333-4444-555555555555"),
	}
}

func observe(t *testing.T, tx *solanarpc.Transaction, status string) Observation {
	t.Helper()
	obs, err := adapter(&fakeRPC{tx: tx, status: status}).Observe(context.Background(), order(t), "sig")
	if err != nil {
		t.Fatal(err)
	}
	return obs
}

func TestObserve_Exact(t *testing.T) {
	obs := observe(t, mkTx(t, buyer, false, goodIxs(t)...), "finalized")
	if !obs.Matches || obs.Status != StatusConfirmed || obs.Confirmations != 1 || obs.Reason != "" {
		t.Fatalf("%+v", obs)
	}
}

func TestObserve_NotFound(t *testing.T) {
	obs := observe(t, nil, "")
	if obs.Status != StatusNotFound || obs.Matches {
		t.Fatalf("%+v", obs)
	}
}

func TestObserve_TxFailed(t *testing.T) {
	obs := observe(t, mkTx(t, buyer, true, goodIxs(t)...), "finalized")
	if obs.Status != StatusFailed || obs.Matches || obs.Reason == "" {
		t.Fatalf("%+v", obs)
	}
}

func TestObserve_NotFinalized(t *testing.T) {
	for _, st := range []string{"", "processed", "confirmed"} {
		obs := observe(t, mkTx(t, buyer, false, goodIxs(t)...), st)
		if obs.Status != StatusPending || obs.Matches || obs.Reason == "" {
			t.Fatalf("status %q: %+v", st, obs)
		}
	}
}

func TestObserve_Mismatches(t *testing.T) {
	b, tr, se := ata(t, buyer), ata(t, treasury), ata(t, seller)
	memoOK := memo("max:sale:11111111-2222-3333-4444-555555555555")
	cases := map[string]struct {
		payer string
		ixs   []ixSpec
	}{
		"WrongSender": {seller, goodIxs(t)},
		"WrongSplit": {buyer, []ixSpec{
			transferChecked(usdc, b, tr, buyer, "9000000"),
			transferChecked(usdc, b, se, buyer, "1000000"), memoOK}},
		"WrongFee": {buyer, []ixSpec{
			transferChecked(usdc, b, tr, buyer, "999999"),
			transferChecked(usdc, b, se, buyer, "9000000"), memoOK}},
		"WrongSellerAmount": {buyer, []ixSpec{
			transferChecked(usdc, b, tr, buyer, "1000000"),
			transferChecked(usdc, b, se, buyer, "8999999"), memoOK}},
		"WrongSourceTreasury": {buyer, []ixSpec{
			transferChecked(usdc, ata(t, seller), tr, buyer, "1000000"),
			transferChecked(usdc, b, se, buyer, "9000000"), memoOK}},
		"WrongSourceSeller": {buyer, []ixSpec{
			transferChecked(usdc, b, tr, buyer, "1000000"),
			transferChecked(usdc, ata(t, seller), se, buyer, "9000000"), memoOK}},
		"TransferCheckedFromBuyerATAToOther": {buyer, append(goodIxs(t),
			transferChecked(usdc, b, ata(t, otherMnt), seller, "1"))},
		"WrongMint": {buyer, []ixSpec{
			transferChecked(otherMnt, b, tr, buyer, "1000000"),
			transferChecked(usdc, b, se, buyer, "9000000"), memoOK}},
		"MissingMemo": {buyer, goodIxs(t)[:3]},
		"WrongMemo":   {buyer, append(goodIxs(t)[:3], memo("max:sale:other"))},
		"ExtraTransferFromBuyer": {buyer, append(goodIxs(t),
			transferChecked(usdc, b, ata(t, "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"), buyer, "1"))},
		"UncheckedTransferFromBuyer": {buyer, append(goodIxs(t), ixSpec{TokenProgram, map[string]any{
			"type": "transfer", "info": map[string]any{"source": b, "destination": tr, "authority": buyer, "amount": "5"}}})},
		"DuplicateTreasury": {buyer, append(goodIxs(t),
			transferChecked(usdc, b, tr, buyer, "1000000"))},
		"WrongAuthority": {buyer, []ixSpec{
			transferChecked(usdc, b, tr, seller, "1000000"),
			transferChecked(usdc, b, se, buyer, "9000000"), memoOK}},
		"WrongProgram": {buyer, []ixSpec{
			{chains.SPLToken2022Program, goodIxs(t)[1].parsed},
			transferChecked(usdc, b, se, buyer, "9000000"), memoOK}},
	}
	reasons := map[string]string{}
	for name, c := range cases {
		obs := observe(t, mkTx(t, c.payer, false, c.ixs...), "finalized")
		if obs.Matches || obs.Reason == "" {
			t.Errorf("%s: %+v", name, obs)
			continue
		}
		if obs.Status != StatusConfirmed {
			t.Errorf("%s: status %s", name, obs.Status)
		}
		reasons[name] = obs.Reason
	}
	// Distinct reasons for the headline cases.
	seen := map[string]string{}
	for _, n := range []string{"WrongSender", "WrongSplit", "WrongSellerAmount", "WrongSourceTreasury", "WrongSourceSeller", "WrongMint", "MissingMemo", "ExtraTransferFromBuyer", "WrongAuthority"} {
		if prev, dup := seen[reasons[n]]; dup {
			t.Errorf("%s and %s share reason %q", n, prev, reasons[n])
		}
		seen[reasons[n]] = n
	}
}

func TestObserve_UnknownChain(t *testing.T) {
	o := order(t)
	o.Chain = chains.Chain{Key: "solana-devnet"}
	if _, err := adapter(&fakeRPC{}).Observe(context.Background(), o, "sig"); err == nil {
		t.Fatal("expected error")
	}
}

func TestBalances(t *testing.T) {
	ch, _ := chains.Lookup("solana")
	m, err := adapter(&fakeRPC{asset: 5, native: 9}).Balances(context.Background(), ch, buyer)
	if err != nil {
		t.Fatal(err)
	}
	if m["native"].Int64() != 9 || m[usdc].Int64() != 5 {
		t.Fatalf("%v", m)
	}
}
