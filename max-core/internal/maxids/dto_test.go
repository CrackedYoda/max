package maxids

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

func TestDestinationResponse_Source(t *testing.T) {
	w := uuid.New()
	m := MaxID{CanonicalName: "yoda", Destinations: []Destination{
		{Namespace: chains.NamespaceSolana, WalletID: &w, Address: "So1"},
		{Namespace: chains.NamespaceBitcoin, Address: "bc1q", Pasted: true},
	}}
	b, err := json.Marshal(toMaxIDResponse(m))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`{"namespace":"solana","chain":null,"address":"So1","source":"wallet","verified":true,"walletId":"` + w.String() + `"`,
		`{"namespace":"bitcoin","chain":null,"address":"bc1q","source":"pasted","verified":false,"walletId":null`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}

func TestDestinationRequest_ExactlyOneTarget(t *testing.T) {
	cases := []struct {
		name string
		req  destinationRequest
		ok   bool
	}{
		{"wallet", destinationRequest{WalletID: uuid.NewString()}, true},
		{"address", destinationRequest{Address: "bc1q", Confirmed: true}, true},
		{"both", destinationRequest{WalletID: uuid.NewString(), Address: "bc1q"}, false},
		{"neither", destinationRequest{}, false},
	}
	for _, c := range cases {
		err := c.req.validate()
		if c.ok != (err == nil) {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if err != nil && !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%s: err = %v, want validation error", c.name, err)
		}
	}
}

func TestDestinationResponse_Chain(t *testing.T) {
	m := MaxID{CanonicalName: "yoda", Destinations: []Destination{
		{Namespace: chains.NamespaceEVM, Address: "0x52908400098527886e0f7030069857d2e4169ee7", Pasted: true},
		{Namespace: chains.NamespaceEVM, ChainKey: "bnb", Address: "0x52908400098527886e0f7030069857d2e4169ee7", Pasted: true},
	}}
	b, _ := json.Marshal(toMaxIDResponse(m))
	s := string(b)
	if !strings.Contains(s, `"chain":null`) || !strings.Contains(s, `"chain":"bnb"`) {
		t.Errorf("chain field missing: %s", s)
	}
}
