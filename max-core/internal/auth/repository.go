package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

type repository struct {
	pool  *pgxpool.Pool
	q     *generated.Queries
	namer maxids.DefaultNamer
}

var _ Repository = (*repository)(nil)

// NewRepository returns the auth repository. namer names the default MAX ID
// every new account gets at its first login.
func NewRepository(pool *pgxpool.Pool, namer maxids.DefaultNamer) Repository {
	return &repository{pool: pool, q: generated.New(pool), namer: namer}
}

func (r *repository) CreateChallenge(ctx context.Context, c Challenge) error {
	_, err := r.q.CreateChallenge(ctx, generated.CreateChallengeParams{
		Nonce:          c.Nonce,
		Purpose:        string(c.Purpose),
		ChainNamespace: string(c.Namespace),
		Address:        c.Address,
		AccountID:      c.AccountID,
		Message:        c.Message,
		ExpiresAt:      c.ExpiresAt,
	})
	if err != nil {
		return shared.ErrInternal.WithError(err)
	}
	return nil
}

func (r *repository) ConsumeChallenge(ctx context.Context, nonce string) (Challenge, error) {
	row, err := r.q.ConsumeChallenge(ctx, nonce)
	if err != nil {
		if database.IsNoRows(err) {
			return Challenge{}, ErrChallengeInvalid
		}
		return Challenge{}, shared.ErrInternal.WithError(err)
	}
	return Challenge{
		Nonce:     row.Nonce,
		Purpose:   Purpose(row.Purpose),
		Namespace: chains.Namespace(row.ChainNamespace),
		Address:   row.Address,
		AccountID: row.AccountID,
		Message:   row.Message,
		ExpiresAt: row.ExpiresAt,
	}, nil
}

func (r *repository) LoginWithWallet(ctx context.Context, proof WalletProof) (Account, Wallet, bool, error) {
	acct, w, created, err := r.loginOnce(ctx, proof)
	// Two first-time logins for the same wallet can race; the loser hits the
	// partial unique index and simply retries as a returning login.
	if err != nil && database.IsUniqueViolation(err, "wallets_active_address_uniq") {
		acct, w, created, err = r.loginOnce(ctx, proof)
	}
	if err != nil {
		return Account{}, Wallet{}, false, shared.ErrInternal.WithError(err)
	}
	return acct, w, created, nil
}

func (r *repository) loginOnce(ctx context.Context, proof WalletProof) (acct Account, w Wallet, created bool, err error) {
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		existing, err := q.GetActiveWalletByAddress(ctx, generated.GetActiveWalletByAddressParams{
			ChainNamespace: string(proof.Namespace), Address: proof.Address,
		})
		if err == nil {
			a, err := q.GetAccount(ctx, existing.AccountID)
			if err != nil {
				return err
			}
			acct, w = toAccount(a), toWallet(existing)
			return nil
		}
		if !database.IsNoRows(err) {
			return err
		}

		a, err := q.CreateAccount(ctx, "human")
		if err != nil {
			return err
		}
		nw, err := q.CreateWallet(ctx, generated.CreateWalletParams{
			AccountID:      a.ID,
			ChainNamespace: string(proof.Namespace),
			Address:        proof.Address,
			ProofMessage:   proof.Message,
			ProofSignature: proof.Signature,
		})
		if err != nil {
			return err
		}
		// The default MAX ID is created in the same transaction, so the loser
		// of a first-login race (which fails on CreateWallet above) rolls
		// back without leaving an account or a default behind.
		if _, err := maxids.CreateDefault(ctx, q, r.namer, a.ID, maxids.TypeHuman,
			&maxids.WalletRef{ID: nw.ID, Namespace: chains.Namespace(nw.ChainNamespace)}); err != nil {
			return err
		}
		if err := audit.Write(ctx, q, audit.Entry{
			Actor: &a.ID, Action: "account.created", ResourceType: "account", ResourceID: a.ID.String(),
			Metadata: map[string]any{"via_wallet": nw.ID.String(), "namespace": nw.ChainNamespace},
		}); err != nil {
			return err
		}
		acct, w, created = toAccount(a), toWallet(nw), true
		return nil
	})
	return acct, w, created, err
}

func (r *repository) LinkWallet(ctx context.Context, accountID uuid.UUID, proof WalletProof) (Wallet, error) {
	var out Wallet
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		existing, err := q.GetActiveWalletByAddress(ctx, generated.GetActiveWalletByAddressParams{
			ChainNamespace: string(proof.Namespace), Address: proof.Address,
		})
		if err == nil {
			if existing.AccountID != accountID {
				return ErrWalletLinkedElsewhere
			}
			out = toWallet(existing)
			return nil
		}
		if !database.IsNoRows(err) {
			return err
		}

		nw, err := q.CreateWallet(ctx, generated.CreateWalletParams{
			AccountID:      accountID,
			ChainNamespace: string(proof.Namespace),
			Address:        proof.Address,
			ProofMessage:   proof.Message,
			ProofSignature: proof.Signature,
		})
		if err != nil {
			return err
		}
		out = toWallet(nw)
		return audit.Write(ctx, q, audit.Entry{
			Actor: &accountID, Action: "wallet.linked", ResourceType: "wallet", ResourceID: nw.ID.String(),
			Metadata: map[string]any{"namespace": nw.ChainNamespace, "address": nw.Address},
		})
	})
	if err != nil {
		if database.IsUniqueViolation(err, "wallets_active_address_uniq") {
			return Wallet{}, ErrWalletLinkedElsewhere
		}
		var appErr *shared.AppError
		if errors.As(err, &appErr) {
			return Wallet{}, err
		}
		return Wallet{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

func (r *repository) GetAccount(ctx context.Context, id uuid.UUID) (Account, error) {
	a, err := r.q.GetAccount(ctx, id)
	if err != nil {
		if database.IsNoRows(err) {
			return Account{}, shared.ErrNotFound
		}
		return Account{}, shared.ErrInternal.WithError(err)
	}
	return toAccount(a), nil
}

func (r *repository) CreateSession(ctx context.Context, accountID uuid.UUID, tokenHash []byte, ip, userAgent string, expiresAt time.Time) (Session, error) {
	s, err := r.q.CreateSession(ctx, generated.CreateSessionParams{
		AccountID: accountID,
		TokenHash: tokenHash,
		Ip:        nonEmpty(ip),
		UserAgent: nonEmpty(userAgent),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return Session{}, shared.ErrInternal.WithError(err)
	}
	return Session{ID: s.ID, AccountID: s.AccountID, ExpiresAt: s.ExpiresAt}, nil
}

func (r *repository) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, string, error) {
	row, err := r.q.GetActiveSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if database.IsNoRows(err) {
			return Session{}, "", ErrSessionInvalid
		}
		return Session{}, "", shared.ErrInternal.WithError(err)
	}
	return Session{ID: row.ID, AccountID: row.AccountID, AccountType: row.AccountType, ExpiresAt: row.ExpiresAt}, row.AccountStatus, nil
}

func (r *repository) RevokeSession(ctx context.Context, id uuid.UUID) error {
	if err := r.q.RevokeSession(ctx, id); err != nil {
		return shared.ErrInternal.WithError(err)
	}
	return nil
}

func toAccount(a generated.Account) Account {
	return Account{ID: a.ID, Type: a.Type, Status: a.Status, CreatedAt: a.CreatedAt}
}

func toWallet(w generated.Wallet) Wallet {
	return Wallet{
		ID: w.ID, AccountID: w.AccountID, Namespace: chains.Namespace(w.ChainNamespace),
		Address: w.Address, VerifiedAt: w.VerifiedAt,
	}
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	if len(s) > 512 {
		s = s[:512]
	}
	return &s
}
