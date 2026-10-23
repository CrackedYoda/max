package agents

import (
	"crypto/rand"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"
	"time"
)

func sol(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(10_000_000)) } // n * 0.01 SOL

func TestEvaluate(t *testing.T) {
	policy := Policy{
		Limits: []AssetLimit{
			{Chain: "solana", Asset: "", MaxPerTransfer: sol(20), MaxPer24h: sol(50)}, // 0.2 / 0.5 SOL
		},
		AllowedRecipients: []string{"yoda"},
	}
	tests := []struct {
		name    string
		policy  Policy
		status  Status
		prop    Proposal
		spent   *big.Int
		want    DecisionOutcome
		reasons []string
	}{
		{"inside authority", policy, StatusActive, Proposal{"solana", "", sol(10), "yoda"}, sol(0), DecisionAutoApproved, nil},
		{"exactly at both limits", policy, StatusActive, Proposal{"solana", "", sol(20), "yoda"}, sol(30), DecisionAutoApproved, nil},
		{"over per-transfer", policy, StatusActive, Proposal{"solana", "", sol(21), "yoda"}, sol(0), DecisionPendingApproval, []string{"per-transfer"}},
		{"over rolling budget", policy, StatusActive, Proposal{"solana", "", sol(10), "yoda"}, sol(41), DecisionPendingApproval, []string{"24h spend"}},
		{"recipient not allowlisted", policy, StatusActive, Proposal{"solana", "", sol(1), "stranger"}, sol(0), DecisionPendingApproval, []string{"allowlist"}},
		{"asset not delegated", policy, StatusActive, Proposal{"solana", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", big.NewInt(1), "yoda"}, nil, DecisionPendingApproval, []string{"no delegated limit"}},
		{"same asset, other chain", policy, StatusActive, Proposal{"solana-devnet", "", sol(1), "yoda"}, nil, DecisionPendingApproval, []string{"no delegated limit"}},
		{"several problems are all reported", policy, StatusActive, Proposal{"solana", "", sol(60), "stranger"}, sol(0), DecisionPendingApproval, []string{"per-transfer", "24h spend", "allowlist"}},
		{"any recipient", Policy{Limits: policy.Limits, AllowAnyRecipient: true}, StatusActive, Proposal{"solana", "", sol(1), "stranger"}, nil, DecisionAutoApproved, nil},
		{"empty policy needs a human for everything", Policy{}, StatusActive, Proposal{"solana", "", big.NewInt(1), "yoda"}, nil, DecisionPendingApproval, nil},
		{"paused agent is denied outright", policy, StatusPaused, Proposal{"solana", "", sol(1), "yoda"}, nil, DecisionDenied, []string{"paused"}},
		{"zero amount denied", policy, StatusActive, Proposal{"solana", "", big.NewInt(0), "yoda"}, nil, DecisionDenied, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.policy, tt.status, tt.prop, tt.spent)
			if got.Outcome != tt.want {
				t.Fatalf("outcome = %s (%v), want %s", got.Outcome, got.Reasons, tt.want)
			}
			joined := strings.Join(got.Reasons, " | ")
			for _, r := range tt.reasons {
				if !strings.Contains(joined, r) {
					t.Errorf("reasons %q missing %q", joined, r)
				}
			}
		})
	}
}

func TestCredentialKeys(t *testing.T) {
	key, prefix, secret, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	if !IsAPIKey(key) {
		t.Fatal("generated key not recognized")
	}
	p, s, err := parseKey(key)
	if err != nil || p != prefix || s != secret {
		t.Fatalf("parse: %v %s %s", err, p, s)
	}
	stored := hashSecret("server-secret", secret)
	if !secretMatches("server-secret", secret, stored) {
		t.Fatal("secret should match")
	}
	if secretMatches("server-secret", secret[:42]+"x", stored) || secretMatches("other-secret", secret, stored) {
		t.Fatal("wrong secret or server key accepted")
	}
	for _, bad := range []string{"", "maxk_", "maxk_abc_def", "sk_" + key[5:], key + "x", "maxk_zzzzzzzzzzzzzzzz_" + secret} {
		if _, _, err := parseKey(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestAttestation(t *testing.T) {
	seed := make([]byte, 32)
	_, _ = rand.Read(seed)
	a, err := NewAttestor(seed)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	payload := map[string]any{"paymentId": "p1", "amountBaseUnits": "100000000", "expiresAt": now.Add(10 * time.Minute)}
	sa, err := a.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAuthorization(a.PublicKey(), sa, now); err != nil {
		t.Fatalf("valid authorization rejected: %v", err)
	}

	claims, _ := sa.Claims()
	tampered := sa
	tampered.Payload = base64.StdEncoding.EncodeToString([]byte(strings.Replace(string(claims), "100000000", "900000000", 1)))
	if err := VerifyAuthorization(a.PublicKey(), tampered, now); err == nil {
		t.Fatal("tampered amount accepted")
	}
	other, _ := NewAttestor(make([]byte, 32))
	if err := VerifyAuthorization(other.PublicKey(), sa, now); err == nil {
		t.Fatal("wrong key accepted")
	}
	if err := VerifyAuthorization(a.PublicKey(), sa, now.Add(time.Hour)); err == nil {
		t.Fatal("expired authorization accepted")
	}
}

func TestBuildPolicy(t *testing.T) {
	tests := []struct {
		name    string
		in      PolicyInput
		wantErr string
		check   func(Policy) bool
	}{
		{"SOL decimals", PolicyInput{Limits: []LimitInput{{Chain: "solana", Asset: "native", MaxPerTransfer: "0.2", MaxPer24h: "0.5"}}, AllowedRecipients: []string{"MAX.Yoda", "yoda"}},
			"", func(p Policy) bool {
				return p.Limits[0].MaxPerTransfer.String() == "200000000" && len(p.AllowedRecipients) == 1 && p.AllowedRecipients[0] == "yoda"
			}},
		{"verified USDC uses 6 decimals", PolicyInput{Limits: []LimitInput{{Chain: "base", Asset: "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", MaxPerTransfer: "25", MaxPer24h: "100"}}},
			"", func(p Policy) bool { return p.Limits[0].MaxPer24h.String() == "100000000" }},
		{"unverified token cannot be delegated", PolicyInput{Limits: []LimitInput{{Chain: "base", Asset: "0x1111111111111111111111111111111111111111", MaxPerTransfer: "1", MaxPer24h: "1"}}}, "verified tokens", nil},
		{"per-transfer above daily", PolicyInput{Limits: []LimitInput{{Chain: "solana", MaxPerTransfer: "2", MaxPer24h: "1"}}}, "cannot exceed", nil},
		{"duplicate asset", PolicyInput{Limits: []LimitInput{{Chain: "solana", MaxPerTransfer: "1", MaxPer24h: "1"}, {Chain: "solana", Asset: "native", MaxPerTransfer: "1", MaxPer24h: "1"}}}, "duplicate", nil},
		{"unknown chain", PolicyInput{Limits: []LimitInput{{Chain: "tron", MaxPerTransfer: "1", MaxPer24h: "1"}}}, "unknown chain", nil},
		{"bad recipient", PolicyInput{AllowedRecipients: []string{"x"}}, "invalid recipient", nil},
		{"too precise", PolicyInput{Limits: []LimitInput{{Chain: "solana", MaxPerTransfer: "0.0000000001", MaxPer24h: "1"}}}, "decimal places", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := buildPolicy(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !tt.check(p) {
				t.Fatalf("unexpected policy %+v", p)
			}
		})
	}
}

// TestIssuedKeyAuthenticates pins the issue→authenticate round trip, which
// must hash the same part of the key on both sides.
func TestIssuedKeyAuthenticates(t *testing.T) {
	s := &service{Deps: Deps{CredentialSecret: strings.Repeat("k", 32)}, now: time.Now}
	issued, err := s.newCredential([16]byte{1}, [16]byte{2}, "runtime", []string{"payments:read"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	stored := s.keyHash(issued.Secret)
	_, secret, err := parseKey(issued.Secret)
	if err != nil || !secretMatches(s.CredentialSecret, secret, stored) {
		t.Fatalf("issued key does not authenticate: %v", err)
	}
}
