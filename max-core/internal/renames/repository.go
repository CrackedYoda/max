package renames

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/shared"
)

type repository struct {
	pool *pgxpool.Pool
	q    *generated.Queries
}

var _ Repository = (*repository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &repository{pool: pool, q: generated.New(pool)}
}

func (r *repository) GetOwnedMaxID(ctx context.Context, id, owner uuid.UUID) (MaxIDInfo, error) {
	row, err := r.q.GetMaxIDForOwner(ctx, generated.GetMaxIDForOwnerParams{ID: id, OwnerAccountID: owner})
	if err != nil {
		if database.IsNoRows(err) {
			return MaxIDInfo{}, maxids.ErrMaxIDNotFound
		}
		return MaxIDInfo{}, shared.ErrInternal.WithError(err)
	}
	return MaxIDInfo{ID: row.ID, Name: row.CanonicalName, Status: row.Status, RenameAvailableAt: row.RenameAvailableAt}, nil
}

func (r *repository) GetReserved(ctx context.Context, canonical string) (*maxids.Reserved, error) {
	row, err := r.q.GetReservedName(ctx, canonical)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, shared.ErrInternal.WithError(err)
	}
	return &maxids.Reserved{CanonicalName: row.CanonicalName, Policy: maxids.ClaimPolicy(row.ClaimPolicy)}, nil
}

func (r *repository) Reserve(ctx context.Context, p ReserveParams) (Request, error) {
	var out Request
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockName(ctx, p.NewName); err != nil {
			return err
		}
		usage, err := q.NameUsage(ctx, p.NewName)
		if err != nil {
			return err
		}
		switch {
		case usage.Taken, usage.PendingRename:
			return maxids.ErrNameTaken
		// An owner may rename back into their own quarantined name; nobody else may.
		case usage.QuarantinedFor != uuid.Nil && usage.QuarantinedFor != p.MaxID:
			return maxids.ErrNameQuarantined
		}

		m, err := q.LockMaxIDForOwner(ctx, generated.LockMaxIDForOwnerParams{ID: p.MaxID, OwnerAccountID: p.Owner})
		if err != nil {
			return err
		}
		if m.Status != "active" {
			return maxids.ErrMaxIDNotActive
		}
		if m.CanonicalName != p.OldName {
			return ErrIDChanged
		}
		if err := maxids.EnsureNotListed(ctx, q, p.MaxID); err != nil {
			return err
		}
		if err := maxids.EnsureNoOpenSale(ctx, q, p.MaxID); err != nil {
			return err
		}
		if time.Now().Before(m.RenameAvailableAt) {
			return ErrCooldown.WithMessage("next rename available at " + m.RenameAvailableAt.UTC().Format(time.RFC3339))
		}

		row, err := q.CreateRenameRequest(ctx, generated.CreateRenameRequestParams{
			MaxID: p.MaxID, OwnerAccountID: p.Owner, OldName: p.OldName, NewName: p.NewName, FeePaymentID: p.FeePaymentID,
		})
		if err != nil {
			return err
		}
		out = toRequest(row)
		return audit.Write(ctx, q, audit.Entry{
			Actor: &p.Owner, Action: "maxid.rename_requested", ResourceType: "max_id", ResourceID: p.MaxID.String(),
			Metadata: map[string]any{"old_name": p.OldName, "new_name": p.NewName, "fee_payment_id": p.FeePaymentID.String()},
		})
	})
	if err != nil {
		if database.IsUniqueViolation(err, "rename_requests_pending_id_uniq") {
			return Request{}, ErrRenamePending
		}
		if database.IsUniqueViolation(err, "rename_requests_pending_name_uniq") {
			return Request{}, maxids.ErrNameTaken
		}
		if database.IsNoRows(err) {
			return Request{}, maxids.ErrMaxIDNotFound
		}
		return Request{}, mapErr(err)
	}
	return out, nil
}

func (r *repository) Get(ctx context.Context, id, owner uuid.UUID) (Request, error) {
	row, err := r.q.GetRenameRequest(ctx, generated.GetRenameRequestParams{ID: id, OwnerAccountID: owner})
	if err != nil {
		if database.IsNoRows(err) {
			return Request{}, ErrRequestNotFound
		}
		return Request{}, shared.ErrInternal.WithError(err)
	}
	return toRequest(row), nil
}

func (r *repository) GetPending(ctx context.Context, maxID uuid.UUID) (*Request, error) {
	row, err := r.q.GetPendingRenameForMaxID(ctx, maxID)
	if err != nil {
		if database.IsNoRows(err) {
			return nil, nil
		}
		return nil, shared.ErrInternal.WithError(err)
	}
	req := toRequest(row)
	return &req, nil
}

func (r *repository) ListPending(ctx context.Context, limit int32) ([]Request, error) {
	rows, err := r.q.ListPendingRenames(ctx, limit)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Request, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRequest(row))
	}
	return out, nil
}

func (r *repository) Finalize(ctx context.Context, requestID uuid.UUID, cooldown, quarantine, hold time.Duration) (Request, error) {
	var out Request
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		req, err := q.LockRenameRequest(ctx, requestID)
		if err != nil {
			return err
		}
		if Status(req.Status) != StatusAwaitingPayment {
			out = toRequest(req)
			return nil // already finalized or closed; idempotent
		}
		if err := q.LockName(ctx, req.NewName); err != nil {
			return err
		}

		m, err := q.ApplyRename(ctx, generated.ApplyRenameParams{
			ID: req.MaxID, OldName: req.OldName, NewName: req.NewName, CooldownSeconds: int32(cooldown.Seconds()), HoldSeconds: holdSeconds(hold),
		})
		if database.IsNoRows(err) {
			closed, cerr := q.CloseRenameRequest(ctx, generated.CloseRenameRequestParams{
				ID: requestID, Status: string(StatusFailed), FailureReason: "the MAX ID changed while the rename was pending",
			})
			out = toRequest(closed)
			return cerr
		}
		if err != nil {
			return err
		}
		if err := q.ReleaseQuarantine(ctx, req.NewName); err != nil {
			return err
		}
		if err := q.QuarantineName(ctx, generated.QuarantineNameParams{
			CanonicalName: req.OldName, MaxID: req.MaxID, QuarantineSeconds: int32(quarantine.Seconds()),
		}); err != nil {
			return err
		}
		feeID := req.FeePaymentID
		if err := q.InsertMaxIDHistory(ctx, generated.InsertMaxIDHistoryParams{
			MaxID: req.MaxID, OldName: req.OldName, NewName: req.NewName, FeePaymentID: &feeID,
		}); err != nil {
			return err
		}
		now := time.Now()
		closed, err := q.CloseRenameRequest(ctx, generated.CloseRenameRequestParams{
			ID: requestID, Status: string(StatusCompleted), CompletedAt: &now,
		})
		if err != nil {
			return err
		}
		out = toRequest(closed)
		owner := req.OwnerAccountID
		// The audit entry doubles as the owner notification record until a
		// notification channel exists.
		return audit.Write(ctx, q, audit.Entry{
			Actor: &owner, Action: "maxid.renamed", ResourceType: "max_id", ResourceID: req.MaxID.String(),
			Metadata: map[string]any{
				"old_name": req.OldName, "new_name": req.NewName, "fee_payment_id": feeID.String(),
				"rename_available_at": m.RenameAvailableAt, "resolution_version": m.ResolutionVersion,
			},
		})
	})
	if err != nil {
		if database.IsNoRows(err) {
			return Request{}, ErrRequestNotFound
		}
		return Request{}, mapErr(err)
	}
	return out, nil
}

func (r *repository) Close(ctx context.Context, requestID uuid.UUID, status Status, reason string) (Request, error) {
	row, err := r.q.CloseRenameRequest(ctx, generated.CloseRenameRequestParams{ID: requestID, Status: string(status), FailureReason: reason})
	if err != nil {
		if database.IsNoRows(err) {
			return Request{}, ErrRequestNotFound
		}
		return Request{}, shared.ErrInternal.WithError(err)
	}
	return toRequest(row), nil
}

func (r *repository) History(ctx context.Context, maxID uuid.UUID) ([]HistoryEntry, error) {
	rows, err := r.q.ListMaxIDHistory(ctx, maxID)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]HistoryEntry, 0, len(rows))
	for _, h := range rows {
		out = append(out, HistoryEntry{OldName: h.OldName, NewName: h.NewName, FeePaymentID: h.FeePaymentID, ChangedAt: h.ChangedAt})
	}
	return out, nil
}

func toRequest(r generated.RenameRequest) Request {
	return Request{
		ID: r.ID, MaxID: r.MaxID, OwnerID: r.OwnerAccountID, OldName: r.OldName, NewName: r.NewName,
		Status: Status(r.Status), FeePaymentID: r.FeePaymentID, FailureReason: r.FailureReason,
		CreatedAt: r.CreatedAt, CompletedAt: r.CompletedAt,
	}
}

func mapErr(err error) error {
	var appErr *shared.AppError
	if errors.As(err, &appErr) {
		return err
	}
	return shared.ErrInternal.WithError(err)
}

func holdSeconds(d time.Duration) int32 {
	if d <= 0 {
		d = maxids.DefaultHoldPeriod
	}
	return int32(d.Seconds())
}
