package payments

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/routing"
)

const (
	// depositGrace is how long after intent expiry the quote must stay live,
	// so a deposit signed at the last moment still lands before refunds start.
	depositGrace         = 10 * time.Minute
	providerPollInterval = 5 * time.Second
)

func (s *service) prepareCrossChain(ctx context.Context, in PrepareInput, chain chains.Chain, sender string, dest chains.Chain, destAssetParam string) (Prepared, error) {
	if s.provider == nil || !s.cfg.RoutingEnabled {
		return Prepared{}, ErrRoutingDisabled
	}
	if dest.ReceiveOnly {
		return Prepared{}, receiveOnly(dest)
	}
	if chain.Testnet || dest.Testnet {
		return Prepared{}, ErrRouteUnavailable.WithMessage("cross-chain routes are available on mainnet chains only")
	}

	res, err := s.resolver.Resolve(ctx, in.Recipient, dest.Key, destAssetParam)
	if err != nil {
		return Prepared{}, err
	}
	if in.ResolutionVersion != nil && *in.ResolutionVersion != res.ResolutionVersion {
		return Prepared{}, ErrResolutionStale
	}
	recipient, err := chains.NormalizeAddress(dest.Namespace, res.Address)
	if err != nil {
		return Prepared{}, err
	}
	if dest.Namespace == chain.Namespace && recipient == sender {
		return Prepared{}, ErrSelfPayment
	}

	srcAddr, ok := canonicalAssetParam(chain.Namespace, in.Asset)
	if !ok {
		return Prepared{}, ErrAssetUnavailable
	}
	srcKind := "native"
	if srcAddr != "" {
		srcKind = "token"
	}
	srcAsset, warnings, err := s.assetFor(ctx, chain, srcKind, srcAddr)
	if err != nil {
		return Prepared{}, err
	}
	srcRef := routing.AssetRef{Chain: chain, Kind: routing.AssetKind(srcAsset.Kind), Address: srcAsset.Address}
	srcProv, err := s.provider.Asset(ctx, srcRef)
	if err != nil {
		return Prepared{}, mapRoutingErr(err)
	}
	if srcProv.Decimals != srcAsset.Decimals {
		return Prepared{}, ErrRouteUnavailable.WithMessage("provider and chain disagree on the source token's decimals")
	}

	destAsset, destWarnings, err := s.destinationAsset(ctx, dest, res.Asset.Address)
	if err != nil {
		return Prepared{}, err
	}
	warnings = append(warnings, destWarnings...)

	swapType, amountDecimals := routing.ExactInput, srcAsset.Decimals
	if in.AmountType == "output" {
		swapType, amountDecimals = routing.ExactOutput, destAsset.Decimals
	}
	amount, err := ParseAmount(in.Amount, amountDecimals)
	if err != nil {
		return Prepared{}, ErrInvalidAmount.WithMessage(err.Error())
	}

	slippage := s.cfg.DefaultSlippageBps
	if in.SlippageBps != nil {
		if *in.SlippageBps < 1 || *in.SlippageBps > s.cfg.MaxSlippageBps {
			return Prepared{}, ErrInvalidSlippage.WithMessage(fmt.Sprintf("slippageBps must be between 1 and %d", s.cfg.MaxSlippageBps))
		}
		slippage = *in.SlippageBps
	}

	now := s.now()
	expires := now.Add(s.cfg.IntentTTL)
	req := routing.QuoteRequest{
		Origin:      srcRef,
		Destination: routing.AssetRef{Chain: dest, Kind: routing.AssetKind(destAsset.Kind), Address: destAsset.Address},
		SwapType:    swapType, Amount: amount,
		Recipient:   chains.DisplayAddress(dest.Namespace, recipient),
		RefundTo:    chains.DisplayAddress(chain.Namespace, sender),
		SlippageBps: slippage, Deadline: now.Add(s.cfg.QuoteDeadline),
	}
	q, err := s.provider.Quote(ctx, req)
	if err != nil {
		return Prepared{}, mapRoutingErr(err)
	}
	if err := routing.ValidateQuote(req, q, now, expires.Add(depositGrace)); err != nil {
		return Prepared{}, ErrQuoteInvalid.WithError(err)
	}
	if err := s.checkRouteLimit(q); err != nil {
		return Prepared{}, err
	}
	// The provider's quote deadline is when the deposit address goes inactive;
	// the deadline MAX requested is when refunds begin. Lock the earlier one so
	// late-submit windows and stall detection never outlive the refund point.
	deadline := req.Deadline
	if q.Deadline.Before(deadline) {
		deadline = q.Deadline
	}
	deposit, err := chains.NormalizeAddress(chain.Namespace, q.DepositAddress)
	if err != nil || deposit == sender {
		return Prepared{}, ErrQuoteInvalid.WithMessage("provider returned an invalid deposit address")
	}

	p, err := s.repo.Create(ctx, Payment{
		SenderAccountID: in.Caller, SenderAddress: sender, IdempotencyKey: in.IdempotencyKey,
		MaxID: res.MaxID, RecipientHandle: res.Handle, RecipientAddress: recipient,
		ResolutionVersion: res.ResolutionVersion, Chain: chain, Asset: srcAsset, Amount: q.AmountIn,
		Route: RouteCrossChain, Status: holdOr(in, StatusQuoted),
		RequiredConfirmations: requiredConfirmations(chain), ExpiresAt: expires,
		CrossChain: &CrossChainRoute{
			Provider: q.Provider, SwapType: swapType, RequestedAmount: amount,
			OriginAssetID: q.OriginAssetID, DestinationAssetID: q.DestinationAssetID,
			DestinationChain: dest, DestinationAsset: destAsset,
			DepositAddress: deposit, RefundTo: sender,
			AmountIn: q.AmountIn, AmountOutExpected: q.AmountOut, MinAmountOut: q.MinAmountOut,
			AmountInUSD: q.AmountInUSD, AmountOutUSD: q.AmountOutUSD, SlippageBps: slippage,
			TimeEstimate: q.TimeEstimate, QuoteDeadline: deadline,
			QuoteSignature: q.Signature, CorrelationID: q.CorrelationID, QuoteRaw: q.Raw,
		},
	})
	if errors.Is(err, errDuplicateIdempotency) {
		return s.replayAfterRace(ctx, in, chain, sender)
	}
	if err != nil {
		return Prepared{}, err
	}

	r := p.CrossChain
	warnings = append(warnings, fmt.Sprintf(
		"Cross-chain via NEAR Intents: send exactly %s %s on %s. %s receives at least %s %s on %s (expected %s). "+
			"If the swap cannot complete by %s, your deposit is refunded to your %s address, minus any refund fee.",
		FormatAmount(p.Amount, p.Asset.Decimals), symbolOr(p.Asset, chain.NativeCoin), chain.Name,
		p.RecipientHandle, FormatAmount(r.MinAmountOut, destAsset.Decimals), symbolOr(destAsset, dest.NativeCoin), dest.Name,
		FormatAmount(r.AmountOutExpected, destAsset.Decimals), r.QuoteDeadline.UTC().Format(time.RFC3339), chain.Name))
	return s.finishPrepared(ctx, p, warnings), nil
}

// destinationAsset describes the asset the recipient receives. Token metadata
// comes from the provider and is cross-checked against MAX's verified list.
func (s *service) destinationAsset(ctx context.Context, dest chains.Chain, displayAddr string) (Asset, []string, error) {
	ref := routing.AssetRef{Chain: dest, Kind: routing.AssetNative}
	if displayAddr != "" {
		addr, err := chains.NormalizeAddress(dest.Namespace, displayAddr)
		if err != nil {
			return Asset{}, nil, ErrAssetUnavailable
		}
		ref = routing.AssetRef{Chain: dest, Kind: routing.AssetToken, Address: addr}
	}
	prov, err := s.provider.Asset(ctx, ref)
	if err != nil {
		return Asset{}, nil, mapRoutingErr(err)
	}
	asset := Asset{Kind: AssetKind(ref.Kind), Address: ref.Address, Decimals: prov.Decimals, Symbol: prov.Symbol}

	if ref.Kind == routing.AssetNative {
		if prov.Decimals != dest.NativeDecimals {
			return Asset{}, nil, ErrRouteUnavailable.WithMessage("provider reports unexpected decimals for the native coin")
		}
		asset.Symbol, asset.Verified = dest.NativeCoin, true
		return asset, nil, nil
	}
	if known, ok := chains.LookupVerifiedToken(dest.Key, ref.Address); ok {
		if known.Decimals != prov.Decimals {
			return Asset{}, nil, ErrRouteUnavailable.WithMessage("provider token metadata does not match MAX's verified record")
		}
		asset.Symbol, asset.Verified = known.Symbol, true
		return asset, nil, nil
	}
	return asset, []string{"The destination token is not on MAX's verified list; confirm it with the recipient."}, nil
}

func (s *service) checkRouteLimit(q routing.Quote) error {
	if s.cfg.MaxCrossChainUSD <= 0 {
		return nil
	}
	usd, err := strconv.ParseFloat(q.AmountInUSD, 64)
	if err != nil {
		return ErrQuoteInvalid.WithMessage("quote is missing a USD valuation needed to enforce limits")
	}
	if usd > s.cfg.MaxCrossChainUSD {
		return ErrRouteLimit.WithMessage(fmt.Sprintf("cross-chain payments are limited to $%.0f; this one is $%.2f", s.cfg.MaxCrossChainUSD, usd))
	}
	return nil
}

func mapRoutingErr(err error) error {
	var rej *routing.QuoteRejectedError
	switch {
	case errors.As(err, &rej):
		return ErrQuoteRejected.WithMessage(rej.Reason)
	case errors.Is(err, routing.ErrRouteUnavailable):
		return ErrRouteUnavailable
	}
	return ErrRoutingProvider.WithError(err)
}

func symbolOr(a Asset, native string) string {
	if a.Symbol != "" {
		return a.Symbol
	}
	if a.Kind == AssetNative {
		return native
	}
	return "tokens"
}

// processCrossChain runs two independent checks: MAX verifies the deposit on
// the origin chain itself, then follows the provider until it settles or
// refunds. Neither side alone can mark a route completed.
func (s *service) processCrossChain(ctx context.Context, p Payment) error {
	r := p.CrossChain
	if p.Status == StatusSubmitted || (p.Status == StatusConfirming && r.DepositConfirmedAt == nil) {
		return s.processDeposit(ctx, p)
	}
	if p.Status == StatusConfirming || p.Status == StatusRefundPending {
		return s.processSettlement(ctx, p)
	}
	return nil
}

func (s *service) processDeposit(ctx context.Context, p Payment) error {
	now := s.now()
	next := pollInterval(p.Chain)
	obs, err := s.gateway.Observe(ctx, p)
	if err != nil {
		s.log.Warn("deposit observation failed", zap.String("payment_id", p.ID.String()), zap.Error(err))
		return s.repo.Reschedule(ctx, p.ID, p.Status, 15*time.Second, p.Confirmations)
	}

	switch {
	case !obs.Found:
		if p.SubmittedAt != nil && now.Sub(*p.SubmittedAt) > s.notFoundTimeout(p.Chain) {
			if p.Chain.Namespace == chains.NamespaceSolana {
				return s.apply(ctx, p, Transition{To: StatusFailed, Event: "not_found",
					Reason: "deposit transaction never landed on-chain"})
			}
			return s.apply(ctx, p, Transition{To: StatusManualReview, Event: "not_found",
				Reason: "deposit transaction not seen on-chain after timeout; it may still be pending"})
		}
		return s.repo.Reschedule(ctx, p.ID, p.Status, next, 0)

	case obs.Reverted:
		return s.apply(ctx, p, Transition{To: StatusFailed, Event: "reverted",
			Reason: "deposit transaction failed on-chain; no funds reached the provider"})

	case obs.Mismatch != "":
		// Funds may still have reached the deposit address (e.g. a wrong
		// amount), in which case the provider refunds them; let it decide.
		st, serr := s.provider.Status(ctx, p.CrossChain.DepositAddress)
		if serr == nil && st.State != routing.StatePendingDeposit {
			return s.apply(ctx, p, Transition{To: StatusRefundPending, Event: "deposit_mismatch",
				Reason:      "deposit does not match the quote (" + obs.Mismatch + "); awaiting provider refund",
				NextCheckIn: ptr(providerPollInterval)})
		}
		return s.apply(ctx, p, Transition{To: StatusFailed, Event: "mismatch",
			Reason: "transaction does not pay the quoted deposit: " + obs.Mismatch})

	case !obs.Final:
		if p.Status == StatusSubmitted {
			return s.apply(ctx, p, Transition{To: StatusConfirming, Event: "confirming",
				Reason: "deposit seen on-chain; waiting for confirmations", Confirmations: &obs.Confirmations, NextCheckIn: &next})
		}
		return s.repo.Reschedule(ctx, p.ID, p.Status, next, obs.Confirmations)
	}

	if err := s.repo.MarkDepositConfirmed(ctx, p.ID, p.Status); err != nil {
		return err
	}
	if p.Status == StatusSubmitted {
		return s.apply(ctx, p, Transition{To: StatusConfirming, Event: "confirming",
			Reason: "deposit confirmed; NEAR Intents is executing the swap", Confirmations: &obs.Confirmations,
			NextCheckIn: ptr(providerPollInterval)})
	}
	return s.repo.Reschedule(ctx, p.ID, p.Status, providerPollInterval, obs.Confirmations)
}

func (s *service) processSettlement(ctx context.Context, p Payment) error {
	r := p.CrossChain
	now := s.now()
	st, err := s.provider.Status(ctx, r.DepositAddress)
	if errors.Is(err, routing.ErrDepositNotFound) {
		return s.stallOrWait(ctx, p, now)
	}
	if err != nil {
		s.log.Warn("route status failed", zap.String("payment_id", p.ID.String()), zap.Error(err))
		return s.repo.Reschedule(ctx, p.ID, p.Status, 15*time.Second, p.Confirmations)
	}

	if string(st.State) != r.ProviderStatus || st.AmountOut != nil || st.RefundedAmount != nil {
		var updated *time.Time
		if !st.UpdatedAt.IsZero() {
			updated = &st.UpdatedAt
		}
		if err := s.repo.UpdateRouteProgress(ctx, p.ID, RouteProgress{
			ProviderStatus: string(st.State), ProviderUpdatedAt: updated, AmountOutActual: st.AmountOut,
			RefundedAmount: st.RefundedAmount, RefundReason: st.RefundReason, DestinationTxs: st.DestinationTxs,
		}); err != nil {
			return err
		}
	}

	switch st.State {
	case routing.StateSuccess:
		if p.Status == StatusRefundPending {
			return s.apply(ctx, p, Transition{To: StatusManualReview, Event: "unexpected_success",
				Reason: "provider reports success where a refund was expected"})
		}
		if st.AmountOut == nil || st.AmountOut.Cmp(r.MinAmountOut) < 0 {
			return s.apply(ctx, p, Transition{To: StatusManualReview, Event: "below_minimum",
				Reason: "provider settled below the quoted minimum output"})
		}
		completed := now
		return s.apply(ctx, p, Transition{To: StatusCompleted, Event: "completed", CompletedAt: &completed,
			Detail: map[string]any{"amount_out": st.AmountOut.String(), "destination_txs": st.DestinationTxs}})

	case routing.StateRefunded:
		return s.apply(ctx, p, Transition{To: StatusRefunded, Event: "refunded",
			Reason: "swap did not complete; provider refunded the deposit" + reasonSuffix(st.RefundReason),
			Detail: map[string]any{"refunded_amount": bigOrEmpty(st.RefundedAmount)}})

	case routing.StateIncompleteDeposit:
		if p.Status == StatusConfirming {
			return s.apply(ctx, p, Transition{To: StatusRefundPending, Event: "incomplete_deposit",
				Reason: "provider received less than quoted; it refunds after the quote deadline", NextCheckIn: ptr(providerPollInterval)})
		}
		return s.stallOrWait(ctx, p, now)

	case routing.StateFailed:
		return s.apply(ctx, p, Transition{To: StatusManualReview, Event: "provider_failed",
			Reason: "routing provider reports the swap failed; funds require manual reconciliation"})
	}
	return s.stallOrWait(ctx, p, now)
}

// stallOrWait keeps polling, escalating to a human if the provider has gone
// quiet well past the point it should have settled or refunded.
func (s *service) stallOrWait(ctx context.Context, p Payment, now time.Time) error {
	if now.After(p.CrossChain.QuoteDeadline.Add(s.cfg.CrossChainStallTimeout)) {
		return s.apply(ctx, p, Transition{To: StatusManualReview, Event: "stalled",
			Reason: "routing provider has not settled or refunded long after the quote deadline"})
	}
	return s.repo.Reschedule(ctx, p.ID, p.Status, providerPollInterval, p.Confirmations)
}

func reasonSuffix(r string) string {
	if r == "" {
		return ""
	}
	return " (" + r + ")"
}

func bigOrEmpty(v *big.Int) string {
	if v == nil {
		return ""
	}
	return v.String()
}
