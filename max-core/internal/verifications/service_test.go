package verifications

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/xapi"
)

type fakeX struct {
	user     xapi.User
	revoked  int
	exchErr  error
	lastVerf string
}

func (f *fakeX) Enabled() bool { return true }
func (f *fakeX) AuthorizeURL(state, ch string) string {
	return "https://x.com/i/oauth2/authorize?state=" + state
}
func (f *fakeX) Exchange(_ context.Context, _, verifier string) (string, error) {
	f.lastVerf = verifier
	return "tok", f.exchErr
}
func (f *fakeX) Me(context.Context, string) (xapi.User, error) { return f.user, nil }
func (f *fakeX) Revoke(context.Context, string) error          { f.revoked++; return nil }

type fakeRepo struct {
	states   map[string]OAuthState
	latest   *Verification
	reserved map[string]Reserved
	saved    int
	active   []ClaimParams
	pending  []ClaimParams
}

func (f *fakeRepo) CreateState(_ context.Context, h []byte, a, s uuid.UUID, v string, _ time.Time) error {
	f.states[string(h)] = OAuthState{AccountID: a, SessionID: s, Verifier: v}
	return nil
}
func (f *fakeRepo) ConsumeState(_ context.Context, h []byte) (OAuthState, error) {
	st, ok := f.states[string(h)]
	if !ok {
		return OAuthState{}, ErrOAuthStateInvalid
	}
	delete(f.states, string(h))
	return st, nil
}
func (f *fakeRepo) SaveVerification(_ context.Context, a uuid.UUID, u xapi.User, _ time.Time) (Verification, []Claim, error) {
	f.saved++
	v := Verification{ID: uuid.New(), AccountID: a, ProviderUserID: u.ID, Username: u.Username, VerifiedAt: time.Now()}
	f.latest = &v
	return v, nil, nil
}
func (f *fakeRepo) ListVerifications(context.Context, uuid.UUID) ([]Verification, error) {
	return nil, nil
}
func (f *fakeRepo) LatestVerification(context.Context, uuid.UUID) (*Verification, error) {
	return f.latest, nil
}
func (f *fakeRepo) RevokeVerification(context.Context, uuid.UUID, *uuid.UUID, string, uuid.UUID) error {
	return nil
}
func (f *fakeRepo) GetReserved(_ context.Context, n string) (*Reserved, error) {
	if r, ok := f.reserved[n]; ok {
		return &r, nil
	}
	return nil, nil
}
func (f *fakeRepo) ClaimActive(_ context.Context, p ClaimParams) (Claim, error) {
	f.active = append(f.active, p)
	return Claim{ID: uuid.New(), CanonicalName: p.Reserved.CanonicalName, Status: ClaimActive}, nil
}
func (f *fakeRepo) CreatePendingClaim(_ context.Context, p ClaimParams) (Claim, error) {
	f.pending = append(f.pending, p)
	return Claim{ID: uuid.New(), CanonicalName: p.Reserved.CanonicalName, Status: ClaimPendingReview}, nil
}
func (f *fakeRepo) ListClaimsByAccount(context.Context, uuid.UUID) ([]Claim, error) { return nil, nil }
func (f *fakeRepo) ListClaimsByStatus(context.Context, ClaimStatus, int32) ([]Claim, error) {
	return nil, nil
}
func (f *fakeRepo) ApproveClaim(context.Context, uuid.UUID, uuid.UUID, time.Time, string) (Claim, error) {
	return Claim{}, nil
}
func (f *fakeRepo) DenyClaim(context.Context, uuid.UUID, uuid.UUID, string) (Claim, error) {
	return Claim{}, nil
}
func (f *fakeRepo) RevokeClaim(context.Context, uuid.UUID, uuid.UUID, string) (Claim, error) {
	return Claim{}, nil
}
func (f *fakeRepo) LapseOverdue(context.Context, time.Duration) ([]Claim, error) { return nil, nil }

func newTest() (*service, *fakeRepo, *fakeX) {
	repo := &fakeRepo{states: map[string]OAuthState{}, reserved: map[string]Reserved{
		"solana":   {CanonicalName: "solana", Policy: "x_oauth", XHandle: "solana"},
		"coinbase": {CanonicalName: "coinbase", Policy: "manual_review", XHandle: "coinbase"},
		"admin":    {CanonicalName: "admin", Policy: "blocked"},
	}}
	x := &fakeX{user: xapi.User{ID: "1001", Username: "Solana"}}
	return NewService(repo, x, DefaultConfig(), zap.NewNop()).(*service), repo, x
}

func stateFrom(url string) string { return url[strings.Index(url, "state=")+6:] }

func TestCompleteX(t *testing.T) {
	ctx := context.Background()
	acct, sess, other := uuid.New(), uuid.New(), uuid.New()

	tests := []struct {
		name    string
		account *uuid.UUID
		session *uuid.UUID
		oauth   string
		code    string
		wantErr error
	}{
		{"success", &acct, &sess, "", "code", nil},
		{"user declined at X", &acct, &sess, "access_denied", "", ErrOAuthDenied},
		{"no session on callback", nil, nil, "", "code", ErrOAuthSessionMismatch},
		{"different account completes the flow (OAuth CSRF)", &other, &sess, "", "code", ErrOAuthSessionMismatch},
		{"same account, different browser session", &acct, &other, "", "code", ErrOAuthSessionMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, x := newTest()
			u, _, err := svc.StartX(ctx, acct, sess, false)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = svc.CompleteX(ctx, tt.account, tt.session, stateFrom(u), tt.code, tt.oauth)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if (repo.saved == 1) != (tt.wantErr == nil) {
				t.Fatal("verification saved on a failed flow, or not saved on success")
			}
			if tt.wantErr == nil && x.revoked != 1 {
				t.Fatal("X access token must be revoked after use")
			}
			// Every state is single-use, whatever happened.
			if _, _, err := svc.CompleteX(ctx, &acct, &sess, stateFrom(u), "code", ""); !errors.Is(err, ErrOAuthStateInvalid) {
				t.Fatalf("replayed state: %v", err)
			}
		})
	}
}

func TestStartX_RequiresSession(t *testing.T) {
	svc, _, _ := newTest()
	if _, _, err := svc.StartX(context.Background(), uuid.New(), uuid.Nil, false); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestClaimReserved(t *testing.T) {
	ctx := context.Background()
	acct := uuid.New()
	fresh := &Verification{ID: uuid.New(), ProviderUserID: "1001", Username: "SOLANA", VerifiedAt: time.Now()}

	tests := []struct {
		name        string
		latest      *Verification
		claim       string
		wantErr     error
		wantPending bool
	}{
		{"handle matches case-insensitively", fresh, "max.solana", nil, false},
		{"brand names go to manual review", &Verification{ID: uuid.New(), Username: "coinbase", VerifiedAt: time.Now()}, "coinbase", nil, true},
		{"no verification", nil, "solana", ErrVerificationRequired, false},
		{"verified a different X account", &Verification{ID: uuid.New(), Username: "solana_fan", VerifiedAt: time.Now()}, "solana", ErrXHandleMismatch, false},
		{"verification too old", &Verification{ID: uuid.New(), Username: "solana", VerifiedAt: time.Now().Add(-time.Hour)}, "solana", ErrVerificationStale, false},
		{"blocked names can't be claimed", fresh, "admin", maxids.ErrNameReserved, false},
		{"unreserved names use the normal claim", fresh, "yoda", ErrNotReserved, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newTest()
			repo.latest = tt.latest
			res, err := svc.ClaimReserved(ctx, acct, tt.claim, "")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if res.Pending != tt.wantPending {
				t.Fatalf("pending = %v", res.Pending)
			}
			if !tt.wantPending && repo.active[0].Verification.ProviderUserID != tt.latest.ProviderUserID {
				t.Fatal("claim must pin the verified X user id")
			}
		})
	}
}
