package maxids

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"yoda", "yoda", false},
		{"max.yoda", "yoda", false},
		{"MAX.Yoda", "yoda", false},
		{"  research-bot  ", "research-bot", false},
		{"a1b2c3", "a1b2c3", false},
		{"yo", "", true},
		{"max.yo", "", true},
		{"this-name-is-way-too-long-for-max-ids", "", true},
		{"-yoda", "", true},
		{"yoda-", "", true},
		{"yo--da", "", true},
		{"yo_da", "", true},
		{"yo.da", "", true},
		{"yоda", "", true}, // Cyrillic о homoglyph
		{"yöda", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Normalize(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

type gateRecord struct {
	owner   uuid.UUID
	counted *float64
	need    float64
	passed  bool
}

type fakeRepo struct {
	creates  int
	gates    []gateRecord
	reserved map[string]Reserved
	ids      map[uuid.UUID]MaxID
	wallets  map[uuid.UUID]struct {
		owner uuid.UUID
		ns    chains.Namespace
	}
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		reserved: map[string]Reserved{
			"admin":  {CanonicalName: "admin", Policy: PolicyBlocked},
			"solana": {CanonicalName: "solana", Policy: PolicyXOAuth, XHandle: "solana"},
		},
		ids: map[uuid.UUID]MaxID{},
		wallets: map[uuid.UUID]struct {
			owner uuid.UUID
			ns    chains.Namespace
		}{},
	}
}

func (f *fakeRepo) GetReserved(_ context.Context, n string) (*Reserved, error) {
	if r, ok := f.reserved[n]; ok {
		return &r, nil
	}
	return nil, nil
}

func (f *fakeRepo) NameStatus(_ context.Context, n string) (string, error) {
	for _, m := range f.ids {
		if m.CanonicalName == n {
			return "taken", nil
		}
	}
	return "", nil
}

func (f *fakeRepo) CountByOwner(_ context.Context, owner uuid.UUID) (int64, error) {
	var n int64
	for _, m := range f.ids {
		if m.OwnerAccountID == owner {
			n++
		}
	}
	return n, nil
}

func (f *fakeRepo) ListActiveWallets(context.Context, uuid.UUID) ([]WalletRef, error) {
	return nil, nil
}

func (f *fakeRepo) RecordGateCheck(_ context.Context, owner uuid.UUID, required float64, counted *float64, passed bool) error {
	f.gates = append(f.gates, gateRecord{owner, counted, required, passed})
	return nil
}

func (f *fakeRepo) Create(ctx context.Context, owner uuid.UUID, n string, typ Type, display string) (MaxID, error) {
	f.creates++
	if st, _ := f.NameStatus(ctx, n); st != "" {
		return MaxID{}, ErrNameTaken
	}
	m := MaxID{ID: uuid.New(), CanonicalName: n, OwnerAccountID: owner, Type: typ, Status: "active", DisplayName: display, ResolutionVersion: 1}
	f.ids[m.ID] = m
	return m, nil
}

func (f *fakeRepo) ListByOwner(_ context.Context, owner uuid.UUID) ([]MaxID, error) {
	var out []MaxID
	for _, m := range f.ids {
		if m.OwnerAccountID == owner {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetForOwner(_ context.Context, id, owner uuid.UUID) (MaxID, error) {
	m, ok := f.ids[id]
	if !ok || m.OwnerAccountID != owner {
		return MaxID{}, ErrMaxIDNotFound
	}
	return m, nil
}

func (f *fakeRepo) UpdateProfile(ctx context.Context, id, owner uuid.UUID, in ProfileInput) (MaxID, error) {
	m, err := f.GetForOwner(ctx, id, owner)
	if err != nil {
		return MaxID{}, err
	}
	m.DisplayName, m.Description, m.AvatarURL = in.DisplayName, in.Description, in.AvatarURL
	f.ids[id] = m
	return m, nil
}

func (f *fakeRepo) GetOwnedWalletNamespace(_ context.Context, walletID, owner uuid.UUID) (chains.Namespace, error) {
	w, ok := f.wallets[walletID]
	if !ok || w.owner != owner {
		return "", ErrWalletNotFound
	}
	return w.ns, nil
}

func (f *fakeRepo) SetDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace, walletID uuid.UUID) (MaxID, error) {
	m, err := f.GetForOwner(ctx, id, owner)
	if err != nil {
		return MaxID{}, err
	}
	m.Destinations = append(m.Destinations, Destination{Namespace: ns, WalletID: &walletID})
	m.ResolutionVersion++
	f.ids[id] = m
	return m, nil
}

func (f *fakeRepo) SetPastedDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace, address string) (MaxID, error) {
	m, err := f.GetForOwner(ctx, id, owner)
	if err != nil {
		return MaxID{}, err
	}
	m.Destinations = append(m.Destinations, Destination{Namespace: ns, Address: address, Pasted: true})
	m.ResolutionVersion++
	f.ids[id] = m
	return m, nil
}

func (f *fakeRepo) SetChainDestination(ctx context.Context, id, owner uuid.UUID, chainKey string, walletID *uuid.UUID, address string) (MaxID, error) {
	m, err := f.GetForOwner(ctx, id, owner)
	if err != nil {
		return MaxID{}, err
	}
	m.Destinations = append(m.Destinations, Destination{Namespace: chains.NamespaceEVM, ChainKey: chainKey, WalletID: walletID, Address: address, Pasted: walletID == nil})
	m.ResolutionVersion++
	f.ids[id] = m
	return m, nil
}

func (f *fakeRepo) RemoveChainDestination(ctx context.Context, id, owner uuid.UUID, chainKey string) (MaxID, error) {
	m, err := f.GetForOwner(ctx, id, owner)
	if err != nil {
		return MaxID{}, err
	}
	kept := m.Destinations[:0]
	found := false
	for _, d := range m.Destinations {
		if d.ChainKey == chainKey {
			found = true
			continue
		}
		kept = append(kept, d)
	}
	if !found {
		return MaxID{}, ErrDestinationNotFound
	}
	m.Destinations = kept
	f.ids[id] = m
	return m, nil
}

func (f *fakeRepo) RemoveDestination(ctx context.Context, id, owner uuid.UUID, ns chains.Namespace) (MaxID, error) {
	return f.GetForOwner(ctx, id, owner)
}

func (f *fakeRepo) SetSettlement(ctx context.Context, id, owner uuid.UUID, st Settlement) (MaxID, error) {
	m, err := f.GetForOwner(ctx, id, owner)
	if err != nil {
		return MaxID{}, err
	}
	m.Settlement = &st
	m.ResolutionVersion++
	f.ids[id] = m
	return m, nil
}

func (f *fakeRepo) RemoveSettlement(ctx context.Context, id, owner uuid.UUID) (MaxID, error) {
	return f.GetForOwner(ctx, id, owner)
}

func (f *fakeRepo) addWallet(owner uuid.UUID, ns chains.Namespace) uuid.UUID {
	id := uuid.New()
	f.wallets[id] = struct {
		owner uuid.UUID
		ns    chains.Namespace
	}{owner, ns}
	return id
}

func TestCheckAvailability(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	ctx := context.Background()
	_, _ = svc.Claim(ctx, uuid.New(), ClaimInput{Name: "taken"})

	tests := []struct {
		name       string
		wantAvail  bool
		wantReason string
		wantPolicy ClaimPolicy
	}{
		{"max.fresh", true, "", ""},
		{"Taken", false, "taken", ""},
		{"admin", false, "reserved", PolicyBlocked},
		{"max.solana", false, "reserved", PolicyXOAuth},
		{"x", false, "invalid", ""},
		{"max.u7k3q9x2m4tab8zz", false, "invalid", ""},
		{"quiet-river-4821", false, "invalid", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := svc.CheckAvailability(ctx, tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if a.Available != tt.wantAvail || a.Reason != tt.wantReason || a.ClaimPolicy != tt.wantPolicy {
				t.Errorf("got %+v", a)
			}
			if IsDefaultShape(strings.TrimPrefix(tt.name, "max.")) && a.Detail != "this name uses a reserved format" {
				t.Errorf("detail = %q", a.Detail)
			}
			if bare := strings.TrimPrefix(tt.name, "max."); IsDefaultShape(bare) {
				if a.CanonicalName != bare || a.Handle != Handle(bare) {
					t.Errorf("canonical/handle = %q/%q, want %q/%q", a.CanonicalName, a.Handle, bare, Handle(bare))
				}
			}
		})
	}
}

func TestClaim(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()

	tests := []struct {
		name    string
		setup   func(*fakeRepo)
		in      ClaimInput
		wantErr error
	}{
		{"ok", nil, ClaimInput{Name: "max.yoda"}, nil},
		{"agent ok", nil, ClaimInput{Name: "researchbot", Type: TypeAgent}, nil},
		{"invalid name", nil, ClaimInput{Name: "yo"}, ErrInvalidName},
		{"invalid type", nil, ClaimInput{Name: "yoda", Type: "robot"}, ErrInvalidType},
		{"blocked reserved", nil, ClaimInput{Name: "admin"}, ErrNameReserved},
		{"x-verifiable reserved", nil, ClaimInput{Name: "solana"}, ErrNameNeedsVerification},
		{"taken", func(r *fakeRepo) {
			_, _ = r.Create(ctx, uuid.New(), "yoda", TypeHuman, "yoda")
		}, ClaimInput{Name: "yoda"}, ErrNameTaken},
		{"limit reached", func(r *fakeRepo) {
			for i := 0; i < MaxIDsPerAccount; i++ {
				_, _ = r.Create(ctx, owner, uuid.NewString()[:8], TypeHuman, "")
			}
		}, ClaimInput{Name: "onemore"}, ErrIDLimitReached},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			m, err := NewService(repo, nil).Claim(ctx, owner, tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if m.OwnerAccountID != owner || m.DisplayName == "" {
				t.Errorf("unexpected result %+v", m)
			}
		})
	}
}

func TestSetDestination(t *testing.T) {
	ctx := context.Background()
	owner, stranger := uuid.New(), uuid.New()

	tests := []struct {
		name      string
		namespace string
		wallet    func(*fakeRepo) uuid.UUID
		caller    uuid.UUID
		wantErr   error
	}{
		{"ok", "solana", func(r *fakeRepo) uuid.UUID { return r.addWallet(owner, chains.NamespaceSolana) }, owner, nil},
		{"namespace mismatch", "evm", func(r *fakeRepo) uuid.UUID { return r.addWallet(owner, chains.NamespaceSolana) }, owner, ErrNamespaceMismatch},
		{"someone else's wallet", "solana", func(r *fakeRepo) uuid.UUID { return r.addWallet(stranger, chains.NamespaceSolana) }, owner, ErrWalletNotFound},
		{"bitcoin unsupported", "bitcoin", func(r *fakeRepo) uuid.UUID { return uuid.New() }, owner, ErrUnsupportedNamespace},
		{"not the owner of the id", "solana", func(r *fakeRepo) uuid.UUID { return r.addWallet(stranger, chains.NamespaceSolana) }, stranger, ErrMaxIDNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewService(repo, nil)
			m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
			walletID := tt.wallet(repo)

			got, err := svc.SetDestination(ctx, tt.caller, m.ID, tt.namespace, walletID)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ResolutionVersion != m.ResolutionVersion+1 {
				t.Error("resolution version must bump on destination change")
			}
		})
	}
}

func TestUpdateProfile_RejectsNonHTTPSAvatar(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	owner := uuid.New()
	m, _ := svc.Claim(context.Background(), owner, ClaimInput{Name: "yoda"})

	for _, u := range []string{"http://x.com/a.png", "javascript:alert(1)", "not a url"} {
		if _, err := svc.UpdateProfile(context.Background(), owner, m.ID, ProfileInput{AvatarURL: u}); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%q: err = %v", u, err)
		}
	}
}

func TestSetSettlement(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()

	tests := []struct {
		name    string
		chain   string
		asset   string
		wallets []chains.Namespace
		wantErr error
	}{
		{"USDC on Solana", "solana", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", []chains.Namespace{chains.NamespaceSolana}, nil},
		{"native ETH on Base", "base", "native", []chains.Namespace{chains.NamespaceEVM}, nil},
		{"no wallet on that chain", "solana", "", []chains.Namespace{chains.NamespaceEVM}, ErrSettlementNoWallet},
		{"testnet refused", "solana-devnet", "", []chains.Namespace{chains.NamespaceSolana}, ErrSettlementChain},
		{"unknown chain", "tron", "", nil, ErrSettlementChain},
		{"bad asset for chain", "solana", "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", []chains.Namespace{chains.NamespaceSolana}, ErrSettlementAsset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewService(repo, nil)
			m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
			for _, ns := range tt.wallets {
				w := repo.addWallet(owner, ns)
				m, _ = svc.SetDestination(ctx, owner, m.ID, string(ns), w)
				m.Destinations = append(m.Destinations, Destination{Namespace: ns, WalletID: &w})
				repo.ids[m.ID] = m
			}
			got, err := svc.SetSettlement(ctx, owner, m.ID, tt.chain, tt.asset, true)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Settlement == nil || got.Settlement.Chain != tt.chain {
				t.Fatalf("settlement %+v", got.Settlement)
			}
		})
	}
}

type stubGate struct {
	err     error
	enabled bool
}

func (g stubGate) Check(context.Context, uuid.UUID) error { return g.err }
func (g stubGate) Enabled() bool                          { return g.enabled }

func TestClaim_GateRunsBeforeCreate(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	insufficient := ErrInsufficientRegistrationBalance.WithDetails(map[string]any{"requiredUsd": 100.0, "countedUsd": 90.0})
	for _, gateErr := range []error{insufficient, ErrPriceUnavailable} {
		repo := newFakeRepo()
		_, err := NewService(repo, stubGate{err: gateErr, enabled: true}).Claim(ctx, owner, ClaimInput{Name: "yoda"})
		if !errors.Is(err, gateErr) {
			t.Fatalf("err = %v, want %v", err, gateErr)
		}
		if repo.creates != 0 {
			t.Fatalf("Create called %d times despite gate failure", repo.creates)
		}
	}
}

func TestClaim_GateAuditedOnFailureAndPass(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	repo := newFakeRepo()
	insufficient := ErrInsufficientRegistrationBalance.WithDetails(map[string]any{"requiredUsd": 100.0, "countedUsd": 90.0})
	_, _ = NewService(repo, stubGate{err: insufficient, enabled: true}).Claim(ctx, owner, ClaimInput{Name: "yoda"})
	if _, err := NewService(repo, stubGate{enabled: true}).Claim(ctx, owner, ClaimInput{Name: "yoda"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.gates) != 2 || repo.gates[0].passed || repo.gates[0].counted == nil || *repo.gates[0].counted != 90 || repo.gates[0].need != 100 || !repo.gates[1].passed {
		t.Fatalf("gate audit = %+v", repo.gates)
	}
}

func TestClaim_DisabledGateNotAudited(t *testing.T) {
	repo := newFakeRepo()
	if _, err := NewService(repo, stubGate{}).Claim(context.Background(), uuid.New(), ClaimInput{Name: "yoda"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.gates) != 0 {
		t.Fatalf("disabled gate audited: %+v", repo.gates)
	}
}

func TestClaim_GateRunsAfterCheapChecks(t *testing.T) {
	repo := newFakeRepo()
	_, err := NewService(repo, stubGate{err: ErrPriceUnavailable, enabled: true}).Claim(context.Background(), uuid.New(), ClaimInput{Name: "admin"})
	if !errors.Is(err, ErrNameReserved) {
		t.Fatalf("err = %v", err)
	}
}

func TestPasteDestination(t *testing.T) {
	ctx := context.Background()
	owner, stranger := uuid.New(), uuid.New()
	tests := []struct {
		name      string
		caller    uuid.UUID
		namespace string
		address   string
		confirmed bool
		wantErr   error
		wantAddr  string
	}{
		{"bitcoin ok", owner, "bitcoin", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", true, nil, "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq"},
		{"evm normalized lowercase", owner, "evm", "0x52908400098527886E0F7030069857D2E4169EE7", true, nil, "0x52908400098527886e0f7030069857d2e4169ee7"},
		{"tron ok", owner, "tron", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", true, nil, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"},
		{"requires confirmation", owner, "bitcoin", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", false, ErrConfirmationRequired, ""},
		{"wrong namespace for address", owner, "evm", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", true, ErrInvalidAddress, ""},
		{"unknown namespace", owner, "dogecoin", "D8vFz4p1L37jdg47HXKtSHA5uYLYxbGgPD", true, ErrUnsupportedNamespace, ""},
		{"not the owner", stranger, "tron", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", true, ErrMaxIDNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(newFakeRepo(), nil)
			m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
			got, err := svc.PasteDestination(ctx, tt.caller, m.ID, tt.namespace, tt.address, tt.confirmed)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			d := got.Destinations[len(got.Destinations)-1]
			if !d.Pasted || d.WalletID != nil || d.Address != tt.wantAddr {
				t.Errorf("destination = %+v", d)
			}
		})
	}
}

func TestSetDestination_WalletOnPasteOnlyNamespace(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
	for _, ns := range []string{"bitcoin", "litecoin", "tron"} {
		if _, err := svc.SetDestination(ctx, owner, m.ID, ns, uuid.New()); !errors.Is(err, ErrUnsupportedNamespace) {
			t.Errorf("%s: err = %v", ns, err)
		}
	}
}

func TestChainDestination(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	evmAddr := "0x52908400098527886E0F7030069857D2E4169EE7"
	tests := []struct {
		name    string
		chain   string
		wallet  func(*fakeRepo) uuid.UUID
		address string
		wantErr error
	}{
		{"wallet override on bnb", "bnb", func(r *fakeRepo) uuid.UUID { return r.addWallet(owner, chains.NamespaceEVM) }, "", nil},
		{"pasted override on base", "base", nil, evmAddr, nil},
		{"solana wallet on an evm chain", "bnb", func(r *fakeRepo) uuid.UUID { return r.addWallet(owner, chains.NamespaceSolana) }, "", ErrNamespaceMismatch},
		{"non-evm chain", "solana", nil, "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM", ErrUnsupportedNamespace},
		{"unknown chain", "dogechain", nil, evmAddr, ErrUnsupportedNamespace},
		{"bad address", "avalanche", nil, "0x1234", ErrInvalidAddress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewService(repo, nil)
			m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
			var got MaxID
			var err error
			if tt.wallet != nil {
				got, err = svc.SetChainDestination(ctx, owner, m.ID, tt.chain, tt.wallet(repo))
			} else {
				got, err = svc.PasteChainDestination(ctx, owner, m.ID, tt.chain, tt.address, true)
			}
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			d := got.Destinations[len(got.Destinations)-1]
			if d.ChainKey != tt.chain || d.Namespace != chains.NamespaceEVM {
				t.Errorf("destination = %+v", d)
			}
			if tt.address != "" && d.Address != strings.ToLower(tt.address) {
				t.Errorf("address = %s, want lowercase canonical", d.Address)
			}
		})
	}
}

func TestPasteChainDestination_RequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	svc := NewService(newFakeRepo(), nil)
	m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
	if _, err := svc.PasteChainDestination(ctx, owner, m.ID, "bnb", "0x52908400098527886e0f7030069857d2e4169ee7", false); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveChainDestination(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	svc := NewService(newFakeRepo(), nil)
	m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
	if _, err := svc.RemoveChainDestination(ctx, owner, m.ID, "solana"); !errors.Is(err, ErrUnsupportedNamespace) {
		t.Errorf("non-evm chain: err = %v", err)
	}
	svc.PasteChainDestination(ctx, owner, m.ID, "bnb", "0x52908400098527886e0f7030069857d2e4169ee7", true)
	got, err := svc.RemoveChainDestination(ctx, owner, m.ID, "bnb")
	if err != nil || len(got.Destinations) != 0 {
		t.Fatalf("remove: %v %+v", err, got.Destinations)
	}
}

func TestSetSettlement_AcceptsChainOverride(t *testing.T) {
	ctx := context.Background()
	owner := uuid.New()
	repo := newFakeRepo()
	svc := NewService(repo, nil)
	m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
	if _, err := svc.SetChainDestination(ctx, owner, m.ID, "base", repo.addWallet(owner, chains.NamespaceEVM)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSettlement(ctx, owner, m.ID, "base", "native", true); err != nil {
		t.Errorf("settlement on an overridden chain: %v", err)
	}
	if _, err := svc.SetSettlement(ctx, owner, m.ID, "arbitrum", "native", true); !errors.Is(err, ErrSettlementNoWallet) {
		t.Errorf("settlement on a chain with no destination: %v", err)
	}
}
