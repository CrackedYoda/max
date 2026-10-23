package maxids

import (
	"context"
	"errors"
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

type fakeRepo struct {
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

func (f *fakeRepo) Create(ctx context.Context, owner uuid.UUID, n string, typ Type, display string) (MaxID, error) {
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
	m.Destinations = append(m.Destinations, Destination{Namespace: ns, WalletID: walletID})
	m.ResolutionVersion++
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
	svc := NewService(repo)
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
			m, err := NewService(repo).Claim(ctx, owner, tt.in)
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
			svc := NewService(repo)
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
	svc := NewService(repo)
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
			svc := NewService(repo)
			m, _ := svc.Claim(ctx, owner, ClaimInput{Name: "yoda"})
			for _, ns := range tt.wallets {
				w := repo.addWallet(owner, ns)
				m, _ = svc.SetDestination(ctx, owner, m.ID, string(ns), w)
				m.Destinations = append(m.Destinations, Destination{Namespace: ns, WalletID: w})
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
