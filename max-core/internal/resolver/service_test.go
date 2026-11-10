package resolver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

const (
	solAddr  = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	evmAddr  = "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	bonkMint = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
	usdcBase = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	tronAddr = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	bnbAddr  = "0x52908400098527886e0f7030069857d2e4169ee7"
)

type fakeRepo struct {
	profiles map[string]Profile
}

func (f fakeRepo) GetProfile(_ context.Context, canonical string) (Profile, error) {
	p, ok := f.profiles[canonical]
	if !ok {
		return Profile{}, ErrMaxIDNotFound
	}
	return p, nil
}

func newSvc() Service {
	return NewService(fakeRepo{profiles: map[string]Profile{
		"yoda": {
			MaxID: uuid.New(), CanonicalName: "yoda", Type: "human", ResolutionVersion: 3,
			Destinations: map[chains.Namespace]string{chains.NamespaceSolana: solAddr, chains.NamespaceEVM: evmAddr, chains.NamespaceTron: tronAddr},
		},
		"split": {
			MaxID: uuid.New(), CanonicalName: "split", Type: "human",
			Destinations: map[chains.Namespace]string{chains.NamespaceEVM: evmAddr},
			Overrides:    map[string]string{"bnb": bnbAddr},
		},
		"bnbonly": {
			MaxID: uuid.New(), CanonicalName: "bnbonly", Type: "human",
			Overrides: map[string]string{"bnb": bnbAddr},
		},
		"solonly": {
			MaxID: uuid.New(), CanonicalName: "solonly", Type: "agent",
			Destinations: map[chains.Namespace]string{chains.NamespaceSolana: solAddr},
		},
	}})
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name      string
		maxID     string
		chain     string
		asset     string
		wantAddr  string
		wantKind  AssetKind
		wantNotes int
		wantErr   error
	}{
		{"SOL native", "max.yoda", "solana", "", solAddr, AssetNative, 0, nil},
		{"BONK by mint", "max.yoda", "solana", bonkMint, solAddr, AssetToken, 1, nil},
		{"ETH on base returns checksummed address", "yoda", "base", "native", chains.ChecksumEVMAddress(evmAddr), AssetNative, 0, nil},
		{"USDC on base", "MAX.YODA", "base", usdcBase, chains.ChecksumEVMAddress(evmAddr), AssetToken, 0, nil},
		{"testnet carries a note", "yoda", "solana-devnet", "", solAddr, AssetNative, 1, nil},
		{"no matching wallet never guesses", "solonly", "ethereum", "", "", "", 0, ErrNoDestination},
		{"unknown chain", "yoda", "dogecoin", "", "", "", 0, ErrUnsupportedChain},
		{"evm asset on solana", "yoda", "solana", usdcBase, "", "", 0, ErrInvalidAsset},
		{"symbol is not an asset id", "yoda", "solana", "BONK", "", "", 0, ErrInvalidAsset},
		{"unknown id", "nobody", "solana", "", "", "", 0, ErrMaxIDNotFound},
		{"invalid id", "x", "solana", "", "", "", 0, ErrMaxIDNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := newSvc().Resolve(context.Background(), tt.maxID, tt.chain, tt.asset)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Address != tt.wantAddr || res.Asset.Kind != tt.wantKind || len(res.Notes) != tt.wantNotes {
				t.Errorf("got addr=%s kind=%s notes=%v", res.Address, res.Asset.Kind, res.Notes)
			}
			if !strings.HasPrefix(res.Handle, "max.") || res.MaxID == uuid.Nil {
				t.Errorf("unexpected metadata %+v", res)
			}
		})
	}
}

func TestProfile_ReportsReachableChains(t *testing.T) {
	_, reach, err := newSvc().Profile(context.Background(), "solonly")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reach {
		want := r.Chain.Namespace == chains.NamespaceSolana
		if r.Reachable != want {
			t.Errorf("%s reachable = %v, want %v", r.Chain.Key, r.Reachable, want)
		}
	}
}

func TestResolve_ReceiveOnly(t *testing.T) {
	res, err := newSvc().Resolve(context.Background(), "yoda", "tron", "native")
	if err != nil {
		t.Fatal(err)
	}
	if res.Address != tronAddr || len(res.Notes) != 1 || res.Notes[0] != "Receive-only on MAX: send from your own wallet." {
		t.Errorf("got %+v", res)
	}
	if _, err := newSvc().Resolve(context.Background(), "yoda", "tron", tronAddr); !errors.Is(err, ErrInvalidAsset) {
		t.Errorf("token on receive-only chain: err = %v", err)
	}
	if _, err := newSvc().Resolve(context.Background(), "yoda", "bitcoin", ""); !errors.Is(err, ErrNoDestination) {
		t.Errorf("bitcoin without destination: err = %v", err)
	}
}

func TestResolve_ChainOverride(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name, chain, want string
		wantErr           error
	}{
		{"split", "bnb", chains.ChecksumEVMAddress(bnbAddr), nil},
		{"split", "base", chains.ChecksumEVMAddress(evmAddr), nil},
		{"bnbonly", "bnb", chains.ChecksumEVMAddress(bnbAddr), nil},
		{"bnbonly", "base", "", ErrNoDestination},
	} {
		res, err := newSvc().Resolve(ctx, tt.name, tt.chain, "")
		if tt.wantErr != nil {
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("%s on %s: err = %v", tt.name, tt.chain, err)
			}
			continue
		}
		if err != nil || res.Address != tt.want {
			t.Errorf("%s on %s: got %q, %v", tt.name, tt.chain, res.Address, err)
		}
	}
	_, reach, err := newSvc().Profile(ctx, "bnbonly")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reach {
		if want := r.Chain.Key == "bnb"; r.Reachable != want {
			t.Errorf("bnbonly reach %s = %v", r.Chain.Key, r.Reachable)
		}
	}
}
