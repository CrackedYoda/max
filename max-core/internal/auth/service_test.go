package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mr-tron/base58"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

type fakeRepo struct {
	mu         sync.Mutex
	challenges map[string]Challenge
	used       map[string]bool
	accounts   map[uuid.UUID]Account
	wallets    map[string]Wallet // key: namespace|address
	sessions   map[string]Session
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		challenges: map[string]Challenge{}, used: map[string]bool{},
		accounts: map[uuid.UUID]Account{}, wallets: map[string]Wallet{}, sessions: map[string]Session{},
	}
}

func (f *fakeRepo) CreateChallenge(_ context.Context, c Challenge) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.challenges[c.Nonce] = c
	return nil
}

func (f *fakeRepo) ConsumeChallenge(_ context.Context, nonce string) (Challenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.challenges[nonce]
	if !ok || f.used[nonce] || time.Now().After(c.ExpiresAt) {
		return Challenge{}, ErrChallengeInvalid
	}
	f.used[nonce] = true
	return c, nil
}

func (f *fakeRepo) LoginWithWallet(_ context.Context, p WalletProof) (Account, Wallet, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := string(p.Namespace) + "|" + p.Address
	if w, ok := f.wallets[key]; ok {
		return f.accounts[w.AccountID], w, false, nil
	}
	a := Account{ID: uuid.New(), Type: "human", Status: "active"}
	w := Wallet{ID: uuid.New(), AccountID: a.ID, Namespace: p.Namespace, Address: p.Address}
	f.accounts[a.ID], f.wallets[key] = a, w
	return a, w, true, nil
}

func (f *fakeRepo) LinkWallet(_ context.Context, accountID uuid.UUID, p WalletProof) (Wallet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := string(p.Namespace) + "|" + p.Address
	if w, ok := f.wallets[key]; ok {
		if w.AccountID != accountID {
			return Wallet{}, ErrWalletLinkedElsewhere
		}
		return w, nil
	}
	w := Wallet{ID: uuid.New(), AccountID: accountID, Namespace: p.Namespace, Address: p.Address}
	f.wallets[key] = w
	return w, nil
}

func (f *fakeRepo) GetAccount(_ context.Context, id uuid.UUID) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[id]
	if !ok {
		return Account{}, shared.ErrNotFound
	}
	return a, nil
}

func (f *fakeRepo) GetAccountIDs(context.Context, uuid.UUID) (*IDRef, *IDRef, error) {
	return nil, nil, nil
}

func (f *fakeRepo) CreateSession(_ context.Context, accountID uuid.UUID, hash []byte, _, _ string, exp time.Time) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := Session{ID: uuid.New(), AccountID: accountID, ExpiresAt: exp}
	f.sessions[string(hash)] = s
	return s, nil
}

func (f *fakeRepo) GetSessionByTokenHash(_ context.Context, hash []byte) (Session, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[string(hash)]
	if !ok {
		return Session{}, "", ErrSessionInvalid
	}
	return s, f.accounts[s.AccountID].Status, nil
}

func (f *fakeRepo) RevokeSession(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, s := range f.sessions {
		if s.ID == id {
			delete(f.sessions, k)
		}
	}
	return nil
}

func newTestService() (*service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo, Config{
		Domain: "localhost:3000", URI: "http://localhost:3000",
		ChallengeTTL: 5 * time.Minute, SessionTTL: time.Hour,
		SessionSecret: strings.Repeat("s", 32),
	}).(*service)
	return svc, repo
}

type solWallet struct {
	addr string
	priv ed25519.PrivateKey
}

func newSolWallet() solWallet {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return solWallet{addr: base58.Encode(pub), priv: priv}
}

func (w solWallet) sign(msg string) string {
	return base58.Encode(ed25519.Sign(w.priv, []byte(msg)))
}

func login(t *testing.T, svc *service, w solWallet) VerifyResult {
	t.Helper()
	ctx := context.Background()
	ch, err := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLogin, Namespace: "solana", Address: w.addr})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	res, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: w.sign(ch.Message)})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return res
}

func TestCreateChallenge_Validation(t *testing.T) {
	svc, _ := newTestService()
	w := newSolWallet()
	caller := uuid.New()

	tests := []struct {
		name    string
		in      ChallengeInput
		wantErr error
	}{
		{"login ok", ChallengeInput{Purpose: PurposeLogin, Namespace: "solana", Address: w.addr}, nil},
		{"link ok", ChallengeInput{Purpose: PurposeLink, Namespace: "solana", Address: w.addr, Caller: &caller}, nil},
		{"bad purpose", ChallengeInput{Purpose: "steal", Namespace: "solana", Address: w.addr}, ErrInvalidPurpose},
		{"bitcoin not linkable yet", ChallengeInput{Purpose: PurposeLogin, Namespace: "bitcoin", Address: "bc1q"}, ErrUnsupportedChain},
		{"unknown namespace", ChallengeInput{Purpose: PurposeLogin, Namespace: "tron", Address: w.addr}, ErrUnsupportedChain},
		{"bad address", ChallengeInput{Purpose: PurposeLogin, Namespace: "evm", Address: "0x123"}, ErrInvalidAddress},
		{"link without session", ChallengeInput{Purpose: PurposeLink, Namespace: "solana", Address: w.addr}, shared.ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := svc.CreateChallenge(context.Background(), tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !strings.Contains(ch.Message, "Nonce: "+ch.Nonce) || !strings.Contains(ch.Message, "localhost:3000") {
				t.Errorf("message missing nonce or domain:\n%s", ch.Message)
			}
			if !strings.Contains(ch.Message, "does not authorize any payment") {
				t.Errorf("message must state it is not a payment authorization")
			}
		})
	}
}

func TestVerify_LoginCreatesThenReuses(t *testing.T) {
	svc, _ := newTestService()
	w := newSolWallet()

	first := login(t, svc, w)
	if !first.AccountCreated || first.SessionToken == "" || first.Session == nil {
		t.Fatalf("first login should create account and session: %+v", first)
	}
	second := login(t, svc, w)
	if second.AccountCreated || second.Account.ID != first.Account.ID {
		t.Fatalf("second login should reuse account")
	}

	sess, err := svc.Authenticate(context.Background(), first.SessionToken)
	if err != nil || sess.AccountID != first.Account.ID {
		t.Fatalf("authenticate: %v", err)
	}
}

func TestVerify_Rejections(t *testing.T) {
	ctx := context.Background()

	t.Run("replayed nonce", func(t *testing.T) {
		svc, _ := newTestService()
		w := newSolWallet()
		ch, _ := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLogin, Namespace: "solana", Address: w.addr})
		sig := w.sign(ch.Message)
		if _, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: sig}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: sig}); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("replay err = %v", err)
		}
	})

	t.Run("signature from another wallet", func(t *testing.T) {
		svc, _ := newTestService()
		victim, attacker := newSolWallet(), newSolWallet()
		ch, _ := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLogin, Namespace: "solana", Address: victim.addr})
		if _, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: attacker.sign(ch.Message)}); !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("failed attempt burns the nonce", func(t *testing.T) {
		svc, _ := newTestService()
		w := newSolWallet()
		ch, _ := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLogin, Namespace: "solana", Address: w.addr})
		_, _ = svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: newSolWallet().sign(ch.Message)})
		if _, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: w.sign(ch.Message)}); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("expired challenge", func(t *testing.T) {
		svc, _ := newTestService()
		svc.now = func() time.Time { return time.Now().Add(-time.Hour) }
		w := newSolWallet()
		ch, _ := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLogin, Namespace: "solana", Address: w.addr})
		if _, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: w.sign(ch.Message)}); !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("link challenge used by a different session", func(t *testing.T) {
		svc, _ := newTestService()
		owner := login(t, svc, newSolWallet())
		other := login(t, svc, newSolWallet())
		extra := newSolWallet()
		ch, _ := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLink, Namespace: "solana", Address: extra.addr, Caller: &owner.Account.ID})
		_, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: extra.sign(ch.Message), Caller: &other.Account.ID})
		if !errors.Is(err, ErrChallengeInvalid) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("linking a wallet owned by another account", func(t *testing.T) {
		svc, _ := newTestService()
		taken := newSolWallet()
		login(t, svc, taken)
		me := login(t, svc, newSolWallet())
		ch, _ := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLink, Namespace: "solana", Address: taken.addr, Caller: &me.Account.ID})
		_, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: taken.sign(ch.Message), Caller: &me.Account.ID})
		if !errors.Is(err, ErrWalletLinkedElsewhere) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestVerify_LinkAddsWallet(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()
	me := login(t, svc, newSolWallet())
	extra := newSolWallet()

	ch, err := svc.CreateChallenge(ctx, ChallengeInput{Purpose: PurposeLink, Namespace: "solana", Address: extra.addr, Caller: &me.Account.ID})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Verify(ctx, VerifyInput{Nonce: ch.Nonce, Signature: extra.sign(ch.Message), Caller: &me.Account.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionToken != "" || res.Session != nil {
		t.Fatal("link must not mint a session")
	}
	if got := repo.wallets[string(chains.NamespaceSolana)+"|"+extra.addr]; got.AccountID != me.Account.ID {
		t.Fatal("wallet not linked to caller")
	}
}

func TestAuthenticate_LogoutInvalidates(t *testing.T) {
	svc, _ := newTestService()
	res := login(t, svc, newSolWallet())
	if err := svc.Logout(context.Background(), res.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), res.SessionToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("err = %v", err)
	}
}
