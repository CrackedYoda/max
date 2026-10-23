package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/routing"
	"github.com/max/max-core/internal/shared"
)

var errDuplicateIdempotency = errors.New("duplicate idempotency key")

type repository struct {
	pool *pgxpool.Pool
	q    *generated.Queries
}

var _ Repository = (*repository)(nil)

func NewRepository(pool *pgxpool.Pool) Repository {
	return &repository{pool: pool, q: generated.New(pool)}
}

func (r *repository) Create(ctx context.Context, p Payment) (Payment, error) {
	var out Payment
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.CreatePayment(ctx, generated.CreatePaymentParams{
			Purpose:               string(purposeOr(p.Purpose)),
			SenderAccountID:       p.SenderAccountID,
			SenderAddress:         p.SenderAddress,
			IdempotencyKey:        p.IdempotencyKey,
			MaxID:                 nilIfZero(p.MaxID),
			RecipientHandle:       p.RecipientHandle,
			RecipientAddress:      p.RecipientAddress,
			ResolutionVersion:     p.ResolutionVersion,
			Chain:                 p.Chain.Key,
			ChainNamespace:        string(p.Chain.Namespace),
			AssetKind:             string(p.Asset.Kind),
			AssetAddress:          p.Asset.Address,
			AssetDecimals:         int32(p.Asset.Decimals),
			AssetSymbol:           p.Asset.Symbol,
			AssetVerified:         p.Asset.Verified,
			TokenProgram:          p.Asset.TokenProgram,
			AmountBaseUnits:       p.Amount.String(),
			RouteType:             string(p.Route),
			Status:                string(p.Status),
			RequiredConfirmations: p.RequiredConfirmations,
			ExpiresAt:             p.ExpiresAt,
		})
		if err != nil {
			return err
		}
		detail := map[string]any{"recipient": p.RecipientHandle, "resolution_version": p.ResolutionVersion}
		if r := p.CrossChain; r != nil {
			if err := q.CreatePaymentRoute(ctx, generated.CreatePaymentRouteParams{
				PaymentID: row.ID, Provider: r.Provider, SwapType: string(r.SwapType),
				RequestedAmount: r.RequestedAmount.String(), OriginAssetID: r.OriginAssetID,
				DestinationAssetID: r.DestinationAssetID, DestinationChain: r.DestinationChain.Key,
				DestinationAssetKind: string(r.DestinationAsset.Kind), DestinationAssetAddress: r.DestinationAsset.Address,
				DestinationAssetDecimals: int32(r.DestinationAsset.Decimals), DestinationAssetSymbol: r.DestinationAsset.Symbol,
				DepositAddress: r.DepositAddress, RefundTo: r.RefundTo, AmountIn: r.AmountIn.String(),
				AmountOutExpected: r.AmountOutExpected.String(), MinAmountOut: r.MinAmountOut.String(),
				AmountInUsd: r.AmountInUSD, AmountOutUsd: r.AmountOutUSD, SlippageBps: int32(r.SlippageBps),
				TimeEstimateSeconds: int32(r.TimeEstimate.Seconds()), QuoteDeadline: r.QuoteDeadline,
				QuoteSignature: r.QuoteSignature, CorrelationID: r.CorrelationID, QuoteRaw: r.QuoteRaw,
			}); err != nil {
				return err
			}
			detail["provider"], detail["deposit_address"] = r.Provider, r.DepositAddress
			detail["destination_chain"], detail["min_amount_out"] = r.DestinationChain.Key, r.MinAmountOut.String()
		}
		if err := insertEvent(ctx, q, row.ID, "created", nil, p.Status, detail); err != nil {
			return err
		}
		out, err = r.load(ctx, q, row)
		return err
	})
	if err != nil {
		if database.IsUniqueViolation(err, "payments_sender_address_idempotency_key_key") {
			return Payment{}, errDuplicateIdempotency
		}
		if database.IsUniqueViolation(err, "payment_routes_deposit_address_key") {
			return Payment{}, ErrQuoteInvalid.WithMessage("provider reused a deposit address").WithError(err)
		}
		return Payment{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

func (r *repository) GetByIdempotencyKey(ctx context.Context, sender, key string) (Payment, error) {
	row, err := r.q.GetPaymentByIdempotencyKey(ctx, generated.GetPaymentByIdempotencyKeyParams{SenderAddress: sender, IdempotencyKey: key})
	return r.one(ctx, row, err)
}

func (r *repository) Get(ctx context.Context, id uuid.UUID) (Payment, error) {
	row, err := r.q.GetPayment(ctx, id)
	return r.one(ctx, row, err)
}

func (r *repository) Events(ctx context.Context, id uuid.UUID) ([]Event, error) {
	rows, err := r.q.ListPaymentEvents(ctx, id)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Event, 0, len(rows))
	for _, e := range rows {
		ev := Event{Type: e.EventType, ToStatus: Status(e.ToStatus), CreatedAt: e.CreatedAt}
		if e.FromStatus != nil {
			ev.FromStatus = Status(*e.FromStatus)
		}
		_ = json.Unmarshal(e.Detail, &ev.Detail)
		out = append(out, ev)
	}
	return out, nil
}

func (r *repository) ListBySender(ctx context.Context, accountID uuid.UUID, limit int32) ([]Payment, error) {
	rows, err := r.q.ListPaymentsBySender(ctx, generated.ListPaymentsBySenderParams{SenderAccountID: &accountID, Limit: limit})
	return r.many(ctx, rows, err)
}

func (r *repository) ListReceived(ctx context.Context, accountID uuid.UUID, limit int32) ([]Payment, error) {
	rows, err := r.q.ListPaymentsReceived(ctx, generated.ListPaymentsReceivedParams{OwnerAccountID: accountID, Limit: limit})
	return r.many(ctx, rows, err)
}

func (r *repository) Apply(ctx context.Context, id uuid.UUID, t Transition) (Payment, error) {
	var out Payment
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.TransitionPayment(ctx, generated.TransitionPaymentParams{
			ID: id, FromStatus: string(t.From), ToStatus: string(t.To), StatusReason: t.Reason,
			TxHash: t.TxHash, Confirmations: t.Confirmations, SubmittedAt: t.SubmittedAt,
			CompletedAt: t.CompletedAt, NextCheckInMs: durationMs(t.NextCheckIn),
		})
		if err != nil {
			if database.IsNoRows(err) {
				return ErrStaleTransition
			}
			return err
		}
		detail := t.Detail
		if t.Reason != "" {
			if detail == nil {
				detail = map[string]any{}
			}
			detail["reason"] = t.Reason
		}
		from := t.From
		if err := insertEvent(ctx, q, id, t.Event, &from, t.To, detail); err != nil {
			return err
		}
		out, err = r.load(ctx, q, row)
		return err
	})
	if err != nil {
		if database.IsUniqueViolation(err, "payments_chain_tx_uniq") {
			return Payment{}, ErrTxAlreadyUsed
		}
		var appErr *shared.AppError
		if errors.As(err, &appErr) {
			return Payment{}, err
		}
		return Payment{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

func (r *repository) Reschedule(ctx context.Context, id uuid.UUID, status Status, in time.Duration, confirmations int64) error {
	if err := r.q.ReschedulePayment(ctx, generated.ReschedulePaymentParams{
		ID: id, NextCheckInMs: in.Milliseconds(), Confirmations: confirmations, Status: string(status),
	}); err != nil {
		return shared.ErrInternal.WithError(err)
	}
	return nil
}

func (r *repository) LeaseDue(ctx context.Context, batch int32, lease time.Duration) ([]Payment, error) {
	rows, err := r.q.LeaseDuePayments(ctx, generated.LeaseDuePaymentsParams{
		LeaseSeconds: int32(lease.Seconds()), Batch: batch,
	})
	return r.many(ctx, rows, err)
}

func (r *repository) ExpireAwaiting(ctx context.Context) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		rows, err := q.ExpireAwaitingPayments(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			from := Status(row.OldStatus)
			if err := insertEvent(ctx, q, row.ID, "expired", &from, StatusExpired, nil); err != nil {
				return err
			}
			ids = append(ids, row.ID)
		}
		return nil
	})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	return ids, nil
}

func (r *repository) MarkDepositConfirmed(ctx context.Context, id uuid.UUID, status Status) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		n, err := q.MarkRouteDepositConfirmed(ctx, id)
		if err != nil || n == 0 {
			return err
		}
		return insertEvent(ctx, q, id, "deposit_confirmed", &status, status, nil)
	})
	if err != nil {
		return shared.ErrInternal.WithError(err)
	}
	return nil
}

func (r *repository) UpdateRouteProgress(ctx context.Context, id uuid.UUID, p RouteProgress) error {
	txs, err := json.Marshal(p.DestinationTxs)
	if err != nil {
		return err
	}
	if p.DestinationTxs == nil {
		txs = []byte("[]")
	}
	var reason *string
	if p.RefundReason != "" {
		reason = &p.RefundReason
	}
	if err := r.q.UpdateRouteProgress(ctx, generated.UpdateRouteProgressParams{
		PaymentID: id, ProviderStatus: p.ProviderStatus, ProviderUpdatedAt: p.ProviderUpdatedAt,
		AmountOutActual: bigString(p.AmountOutActual), RefundedAmount: bigString(p.RefundedAmount),
		RefundReason: reason, DestinationTxHashes: txs,
	}); err != nil {
		return shared.ErrInternal.WithError(err)
	}
	return nil
}

func (r *repository) one(ctx context.Context, row generated.Payment, err error) (Payment, error) {
	if err != nil {
		if database.IsNoRows(err) {
			return Payment{}, ErrPaymentNotFound
		}
		return Payment{}, shared.ErrInternal.WithError(err)
	}
	p, err := r.load(ctx, r.q, row)
	if err != nil {
		return Payment{}, shared.ErrInternal.WithError(err)
	}
	return p, nil
}

func (r *repository) many(ctx context.Context, rows []generated.Payment, err error) ([]Payment, error) {
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Payment, 0, len(rows))
	for _, row := range rows {
		p, err := r.load(ctx, r.q, row)
		if err != nil {
			return nil, shared.ErrInternal.WithError(err)
		}
		out = append(out, p)
	}
	return out, nil
}

// load maps a payment row and attaches its route for cross-chain payments.
func (r *repository) load(ctx context.Context, q *generated.Queries, row generated.Payment) (Payment, error) {
	p, err := toPayment(row)
	if err != nil || p.Route != RouteCrossChain {
		return p, err
	}
	rt, err := q.GetPaymentRoute(ctx, row.ID)
	if err != nil {
		return Payment{}, fmt.Errorf("load route for %s: %w", row.ID, err)
	}
	p.CrossChain, err = toRoute(rt)
	return p, err
}

func toRoute(r generated.PaymentRoute) (*CrossChainRoute, error) {
	dest, ok := chains.Lookup(r.DestinationChain)
	if !ok {
		return nil, fmt.Errorf("route references unknown chain %q", r.DestinationChain)
	}
	out := &CrossChainRoute{
		Provider: r.Provider, SwapType: routing.SwapType(r.SwapType),
		OriginAssetID: r.OriginAssetID, DestinationAssetID: r.DestinationAssetID, DestinationChain: dest,
		DestinationAsset: Asset{
			Kind: AssetKind(r.DestinationAssetKind), Address: r.DestinationAssetAddress,
			Decimals: int(r.DestinationAssetDecimals), Symbol: r.DestinationAssetSymbol,
		},
		DepositAddress: r.DepositAddress, RefundTo: r.RefundTo,
		AmountInUSD: r.AmountInUsd, AmountOutUSD: r.AmountOutUsd, SlippageBps: int(r.SlippageBps),
		TimeEstimate:  time.Duration(r.TimeEstimateSeconds) * time.Second,
		QuoteDeadline: r.QuoteDeadline, QuoteSignature: r.QuoteSignature, CorrelationID: r.CorrelationID,
		QuoteRaw: r.QuoteRaw, DepositConfirmedAt: r.DepositConfirmedAt, ProviderStatus: r.ProviderStatus,
		ProviderUpdatedAt: r.ProviderUpdatedAt,
	}
	for dst, src := range map[**big.Int]string{
		&out.RequestedAmount: r.RequestedAmount, &out.AmountIn: r.AmountIn,
		&out.AmountOutExpected: r.AmountOutExpected, &out.MinAmountOut: r.MinAmountOut,
	} {
		v, ok := new(big.Int).SetString(src, 10)
		if !ok {
			return nil, fmt.Errorf("route has invalid amount %q", src)
		}
		*dst = v
	}
	if r.AmountOutActual != nil {
		out.AmountOutActual, _ = new(big.Int).SetString(*r.AmountOutActual, 10)
	}
	if r.RefundedAmount != nil {
		out.RefundedAmount, _ = new(big.Int).SetString(*r.RefundedAmount, 10)
	}
	if r.RefundReason != nil {
		out.RefundReason = *r.RefundReason
	}
	_ = json.Unmarshal(r.DestinationTxHashes, &out.DestinationTxs)
	return out, nil
}

func nilIfZero(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func purposeOr(p Purpose) Purpose {
	if p == "" {
		return PurposeTransfer
	}
	return p
}

func bigString(v *big.Int) *string {
	if v == nil {
		return nil
	}
	s := v.String()
	return &s
}

func durationMs(d *time.Duration) *int64 {
	if d == nil {
		return nil
	}
	ms := d.Milliseconds()
	return &ms
}

func insertEvent(ctx context.Context, q *generated.Queries, id uuid.UUID, typ string, from *Status, to Status, detail map[string]any) error {
	b := []byte("{}")
	if len(detail) > 0 {
		var err error
		if b, err = json.Marshal(detail); err != nil {
			return err
		}
	}
	var fromStr *string
	if from != nil {
		s := string(*from)
		fromStr = &s
	}
	return q.InsertPaymentEvent(ctx, generated.InsertPaymentEventParams{
		PaymentID: id, EventType: typ, FromStatus: fromStr, ToStatus: string(to), Detail: b,
	})
}

func toPayment(row generated.Payment) (Payment, error) {
	chain, ok := chains.Lookup(row.Chain)
	if !ok {
		return Payment{}, fmt.Errorf("payment %s references unknown chain %q", row.ID, row.Chain)
	}
	amount, ok := new(big.Int).SetString(row.AmountBaseUnits, 10)
	if !ok {
		return Payment{}, fmt.Errorf("payment %s has invalid amount", row.ID)
	}
	p := Payment{
		ID: row.ID, Purpose: Purpose(row.Purpose), SenderAccountID: row.SenderAccountID, SenderAddress: row.SenderAddress,
		IdempotencyKey: row.IdempotencyKey, RecipientHandle: row.RecipientHandle,
		RecipientAddress: row.RecipientAddress, ResolutionVersion: row.ResolutionVersion, Chain: chain,
		Asset: Asset{
			Kind: AssetKind(row.AssetKind), Address: row.AssetAddress, Decimals: int(row.AssetDecimals),
			Symbol: row.AssetSymbol, Verified: row.AssetVerified, TokenProgram: row.TokenProgram,
		},
		Amount: amount, Route: RouteType(row.RouteType), Status: Status(row.Status),
		StatusReason: row.StatusReason, Confirmations: row.Confirmations,
		RequiredConfirmations: row.RequiredConfirmations, ExpiresAt: row.ExpiresAt,
		SubmittedAt: row.SubmittedAt, CompletedAt: row.CompletedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.TxHash != nil {
		p.TxHash = *row.TxHash
	}
	if row.MaxID != nil {
		p.MaxID = *row.MaxID
	}
	return p, nil
}
