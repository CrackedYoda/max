package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/max/max-core/db/generated"
	"github.com/max/max-core/internal/agents"
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/shared"
)

// Agents on the marketplace (spec §8). Humans always go ahead. An agent's
// buy and pay spend against agent_policies.limits, together with its
// transfers; its list and accept need market.canSell and the minimum sale
// price. Anything outside the policy is held: the action does not happen, its
// full input is stored on the authorization, and the agent's administrator
// approves (the action is replayed as the agent) or denies it.

var ErrAgentAuthorizationNotFound = shared.ErrNotFound.WithMessage("agent market authorization not found")

// AgentDirectory reads agents and their policies (agents.Repository).
type AgentDirectory interface {
	Get(ctx context.Context, agentID uuid.UUID) (agents.Agent, error)
	GetPolicy(ctx context.Context, agentID uuid.UUID) (agents.Policy, error)
}

// AgentStore persists agent_market_authorizations.
type AgentStore interface {
	// Decide serialises per agent with transfers (agents.LockSpend): it reads
	// the rolling spend, lets decide build the authorization and stores it,
	// all in one transaction.
	Decide(ctx context.Context, agentID uuid.UUID, decide func(agents.SpendFunc) NewAgentAuthorization) (AgentAuthorization, error)
	GetAuthorization(ctx context.Context, id uuid.UUID) (AgentAuthorization, error)
	// LatestPay returns the agent's newest approved or pending, unfailed
	// authorization to pay a sale, or nil.
	LatestPay(ctx context.Context, agentID, saleID uuid.UUID) (*AgentAuthorization, error)
	// Resolve moves a pending authorization to decision (compare-and-swap)
	// and writes the audit log; a decided one is agents.ErrApprovalNotPending.
	Resolve(ctx context.Context, id uuid.UUID, decision string, by uuid.UUID) (AgentAuthorization, error)
	// Finish records the objects an action created, or its failure.
	Finish(ctx context.Context, id uuid.UUID, ref AuthRef, failure string) error
	// Administrator returns the agent's administrator, or nil for a non-agent.
	Administrator(ctx context.Context, agentID uuid.UUID) (*uuid.UUID, error)
	// List returns the agent's authorizations, newest first; decision ""
	// means all. Name is filled in.
	List(ctx context.Context, agentID uuid.UUID, decision string, limit int) ([]AgentAuthorization, error)
}

// agentAuthorizationListLimit caps the administrator's list.
const agentAuthorizationListLimit = 100

type agentPolicy struct {
	dir   AgentDirectory
	store AgentStore
}

// NewAgentPolicy is the market AgentPolicy for agent API keys.
func NewAgentPolicy(dir AgentDirectory, store AgentStore) AgentPolicy {
	return &agentPolicy{dir: dir, store: store}
}

// replayKey marks a context replaying an approved authorization: its policy
// check is skipped, because the administrator already decided.
type replayKey struct{}

type replay struct {
	id, agent uuid.UUID
	action    string
}

func (ap *agentPolicy) Authorize(ctx context.Context, p shared.Principal, action string, amount *big.Int, chain, asset string, ref AuthRef, in ActionInput) (Decision, error) {
	if !p.IsAgentCredential() {
		return Decision{Outcome: DecisionApproved}, nil
	}
	terms := Terms{Amount: amount.String(), Chain: chain, Asset: asset}
	if r, ok := ctx.Value(replayKey{}).(replay); ok && r.agent == p.AccountID && r.action == action {
		return ap.replayDecision(ctx, r, terms)
	}
	in.Terms = &terms
	// Paying the same sale again (to fetch fresh instructions) is not new
	// spend: reuse the live authorization rather than count it twice.
	if action == ActionPay && ref.SaleID != nil {
		prior, err := ap.store.LatestPay(ctx, p.AccountID, *ref.SaleID)
		if err != nil {
			return Decision{}, err
		}
		if prior != nil {
			return decisionOf(*prior), nil
		}
	}

	agent, err := ap.dir.Get(ctx, p.AccountID)
	if err != nil {
		return Decision{}, err
	}
	policy, err := ap.dir.GetPolicy(ctx, p.AccountID)
	if err != nil {
		return Decision{}, err
	}
	a, err := ap.store.Decide(ctx, p.AccountID, func(spent agents.SpendFunc) NewAgentAuthorization {
		ev := agents.EvaluateMarket(policy, agent.Status, action, amount, chain, asset, spent(chain, asset))
		return NewAgentAuthorization{
			AgentID: p.AccountID, CredentialID: *p.CredentialID, Action: action, Decision: string(ev.Outcome),
			Reasons: ev.Reasons, PolicyVersion: policy.Version, Chain: chain, Asset: asset, Amount: amount.String(),
			Ref: ref, Input: in,
		}
	})
	if err != nil {
		return Decision{}, err
	}
	return decisionOf(a), nil
}

// replayDecision approves a replay only on the terms the administrator
// approved: a listing repriced meanwhile must not spend (or sell) beyond
// what was evaluated.
func (ap *agentPolicy) replayDecision(ctx context.Context, r replay, now Terms) (Decision, error) {
	a, err := ap.store.GetAuthorization(ctx, r.id)
	if err != nil {
		return Decision{}, err
	}
	approved := Terms{Amount: a.Amount, Chain: a.Chain, Asset: a.AssetAddress}
	if a.Input.Terms != nil {
		approved = *a.Input.Terms
	}
	if now != approved {
		return Decision{}, changedSinceApproval(r.action)
	}
	id := r.id
	return Decision{Outcome: DecisionApproved, AuthorizationID: &id}, nil
}

func changedSinceApproval(action string) error {
	switch action {
	case ActionBuy:
		return ErrListingNotActive.WithMessage("the listing changed since approval was requested")
	case ActionAccept, ActionPay:
		return shared.ErrConflict.WithMessage("the offer changed since approval was requested")
	}
	return shared.ErrConflict.WithMessage("the listing terms changed since approval was requested")
}

func decisionOf(a AgentAuthorization) Decision {
	id := a.ID
	d := Decision{AuthorizationID: &id, Reasons: a.Reasons}
	switch agents.DecisionOutcome(a.Decision) {
	case agents.DecisionAutoApproved, agents.DecisionApproved:
		d.Outcome = DecisionApproved
	case agents.DecisionPendingApproval:
		d.Outcome = DecisionPending
	default:
		d.Outcome = DecisionDenied
	}
	return d
}

func (ap *agentPolicy) Finish(ctx context.Context, id uuid.UUID, ref AuthRef, failure error) error {
	msg := ""
	if failure != nil {
		msg = failureText(failure)
	}
	return ap.store.Finish(ctx, id, ref, msg)
}

// failureText is what an administrator sees: the error code and message,
// never internal causes.
func failureText(err error) string {
	var app *shared.AppError
	if !errors.As(err, &app) || app.Status >= 500 {
		return "internal error"
	}
	return app.Code + ": " + app.Message
}

func (ap *agentPolicy) Resolve(ctx context.Context, admin, id uuid.UUID, decision string) (AgentAuthorization, error) {
	a, err := ap.store.GetAuthorization(ctx, id)
	if err != nil {
		return AgentAuthorization{}, err
	}
	owner, err := ap.store.Administrator(ctx, a.AgentID)
	if err != nil {
		return AgentAuthorization{}, err
	}
	if owner == nil || *owner != admin {
		return AgentAuthorization{}, ErrAgentAuthorizationNotFound
	}
	return ap.store.Resolve(ctx, id, decision, admin)
}

func (ap *agentPolicy) List(ctx context.Context, admin, agentID uuid.UUID, decision string) ([]AgentAuthorization, error) {
	owner, err := ap.store.Administrator(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if owner == nil || *owner != admin {
		return nil, agents.ErrAgentNotFound
	}
	return ap.store.List(ctx, agentID, decision, agentAuthorizationListLimit)
}

// ListAgentAuthorizations lets an agent's administrator find and inspect its
// market authorizations, e.g. those pending approval.
func (s *Service) ListAgentAuthorizations(ctx context.Context, admin, agentID uuid.UUID, decision string) ([]AgentAuthorization, error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	switch agents.DecisionOutcome(decision) {
	case "", agents.DecisionAutoApproved, agents.DecisionPendingApproval, agents.DecisionApproved, agents.DecisionDenied:
	default:
		return nil, shared.ErrValidation.WithMessage("decision must be auto_approved, pending_approval, approved or denied")
	}
	return s.agents.List(ctx, admin, agentID, decision)
}

// ApproveAgentAction lets an agent's administrator approve a held action. The
// action is replayed as the agent with its stored input through the normal
// service path, so every market check runs again; only the policy check is
// skipped. A replay that fails (say, the name sold meanwhile) is recorded on
// the authorization and its error returned.
func (s *Service) ApproveAgentAction(ctx context.Context, admin, id uuid.UUID) (AgentActionResult, error) {
	if err := s.enabled(); err != nil {
		return AgentActionResult{}, err
	}
	a, err := s.agents.Resolve(ctx, admin, id, string(agents.DecisionApproved))
	if err != nil {
		return AgentActionResult{}, err
	}
	cred := a.CredentialID
	p := shared.Principal{AccountID: a.AgentID, AccountType: "agent", CredentialID: &cred}
	// The approval is committed: run the replay and record its outcome even
	// if the administrator's request is cancelled meanwhile.
	ctx = context.WithoutCancel(ctx)
	rctx := context.WithValue(ctx, replayKey{}, replay{id: a.ID, agent: a.AgentID, action: a.Action})

	res := AgentActionResult{Authorization: a}
	var ref AuthRef
	in := a.Input
	switch {
	case in.Op == OpList && in.List != nil:
		var l Listing
		if l, err = s.List(rctx, p, *in.List); err == nil {
			res.Listing, ref.ListingID = &l, &l.ID
		}
	case in.Op == OpUpdatePrice && in.ListingID != nil:
		var l Listing
		if l, err = s.UpdatePrice(rctx, p, *in.ListingID, in.Price); err == nil {
			res.Listing, ref.ListingID = &l, &l.ID
		}
	case in.Op == OpBuy && in.ListingID != nil && in.Buy != nil:
		var v SaleView
		if v, err = s.Buy(rctx, p, *in.ListingID, *in.Buy); err == nil {
			res.Sale, ref.SaleID = &v, &v.Sale.ID
		}
	case in.Op == OpAccept && in.OfferID != nil:
		var v SaleView
		if v, err = s.AcceptOffer(rctx, p, *in.OfferID, in.PayoutAddress); err == nil {
			res.Sale, ref.SaleID = &v, &v.Sale.ID
		}
	case in.Op == OpPay && in.OfferID != nil:
		var v SaleView
		if v, err = s.PayOffer(rctx, p, *in.OfferID, in.IdempotencyKey); err == nil {
			res.Sale, ref.SaleID = &v, &v.Sale.ID
		}
	default:
		err = shared.ErrInternal.WithMessage(fmt.Sprintf("authorization %s has no replayable input", a.ID))
	}
	// Errors before the policy check never reached the action's own finish.
	s.finisher(ctx, &a.ID)(ref, err)
	if err != nil {
		return AgentActionResult{}, err
	}
	if ref.SaleID != nil {
		res.Authorization.SaleID = ref.SaleID
	}
	if ref.ListingID != nil {
		res.Authorization.ListingID = ref.ListingID
	}
	return res, nil
}

// DenyAgentAction lets an agent's administrator refuse a held action.
func (s *Service) DenyAgentAction(ctx context.Context, admin, id uuid.UUID) (AgentAuthorization, error) {
	if err := s.enabled(); err != nil {
		return AgentAuthorization{}, err
	}
	return s.agents.Resolve(ctx, admin, id, string(agents.DecisionDenied))
}

// ---- Postgres store ----

type agentStore struct {
	pool *pgxpool.Pool
	q    *generated.Queries
}

var _ AgentStore = (*agentStore)(nil)

func NewAgentStore(pool *pgxpool.Pool) AgentStore {
	return &agentStore{pool: pool, q: generated.New(pool)}
}

func (r *agentStore) Decide(ctx context.Context, agentID uuid.UUID, decide func(agents.SpendFunc) NewAgentAuthorization) (AgentAuthorization, error) {
	var out AgentAuthorization
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		spent, err := agents.LockSpend(ctx, q, agentID)
		if err != nil {
			return err
		}
		n := decide(spent)
		reasons, err := json.Marshal(orEmpty(n.Reasons))
		if err != nil {
			return err
		}
		payload, err := json.Marshal(n.Input)
		if err != nil {
			return err
		}
		row, err := q.CreateAgentMarketAuthorization(ctx, generated.CreateAgentMarketAuthorizationParams{
			AgentAccountID: n.AgentID, CredentialID: n.CredentialID, Action: n.Action,
			SaleID: n.Ref.SaleID, OfferID: n.Ref.OfferID, ListingID: n.Ref.ListingID,
			Decision: n.Decision, Reasons: reasons, PolicyVersion: int32(n.PolicyVersion),
			Chain: n.Chain, AssetAddress: n.Asset, AmountBaseUnits: n.Amount, Payload: payload,
		})
		if err != nil {
			return err
		}
		out, err = toAgentAuthorization(row)
		return err
	})
	if err != nil {
		return AgentAuthorization{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

func (r *agentStore) GetAuthorization(ctx context.Context, id uuid.UUID) (AgentAuthorization, error) {
	row, err := r.q.GetAgentMarketAuthorization(ctx, id)
	if err != nil {
		return AgentAuthorization{}, mapErr(err, ErrAgentAuthorizationNotFound)
	}
	return wrapInternal(toAgentAuthorization(row))
}

func (r *agentStore) LatestPay(ctx context.Context, agentID, saleID uuid.UUID) (*AgentAuthorization, error) {
	row, err := r.q.GetLatestAgentMarketPay(ctx, generated.GetLatestAgentMarketPayParams{AgentAccountID: agentID, SaleID: &saleID})
	if database.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	a, err := wrapInternal(toAgentAuthorization(row))
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *agentStore) Resolve(ctx context.Context, id uuid.UUID, decision string, by uuid.UUID) (AgentAuthorization, error) {
	var out AgentAuthorization
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.DecideAgentMarketAuthorization(ctx, generated.DecideAgentMarketAuthorizationParams{ID: id, Decision: decision, DecidedBy: &by})
		if database.IsNoRows(err) {
			return agents.ErrApprovalNotPending.WithMessage("this market action is not awaiting approval")
		}
		if err != nil {
			return err
		}
		if out, err = toAgentAuthorization(row); err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &by, Action: "agent.market_" + decision, ResourceType: "agent_market_authorization", ResourceID: id.String(),
			Metadata: map[string]any{"agent_id": out.AgentID.String(), "action": out.Action, "amount": out.Amount, "chain": out.Chain},
		})
	})
	if err != nil {
		return AgentAuthorization{}, mapErr(err, nil)
	}
	return out, nil
}

func (r *agentStore) Finish(ctx context.Context, id uuid.UUID, ref AuthRef, failure string) error {
	var f *string
	if failure != "" {
		f = &failure
	}
	return database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.FinishAgentMarketAuthorization(ctx, generated.FinishAgentMarketAuthorizationParams{
			ID: id, SaleID: ref.SaleID, OfferID: ref.OfferID, ListingID: ref.ListingID, Failure: f,
		})
		if err != nil {
			return err
		}
		if f == nil {
			return nil
		}
		return audit.Write(ctx, q, audit.Entry{
			Action: "agent.market_action_failed", ResourceType: "agent_market_authorization", ResourceID: id.String(),
			Metadata: map[string]any{"agent_id": row.AgentAccountID.String(), "action": row.Action, "failure": failure},
		})
	})
}

func (r *agentStore) Administrator(ctx context.Context, agentID uuid.UUID) (*uuid.UUID, error) {
	id, err := r.q.GetAgentAdministratorID(ctx, agentID)
	if database.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	return &id, nil
}

func (r *agentStore) List(ctx context.Context, agentID uuid.UUID, decision string, limit int) ([]AgentAuthorization, error) {
	var d *string
	if decision != "" {
		d = &decision
	}
	rows, err := r.q.ListAgentMarketAuthorizations(ctx, generated.ListAgentMarketAuthorizationsParams{
		AgentAccountID: agentID, Decision: d, RowLimit: int32(limit),
	})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]AgentAuthorization, 0, len(rows))
	for _, row := range rows {
		a, err := wrapInternal(toAgentAuthorization(generated.AgentMarketAuthorization{
			ID: row.ID, AgentAccountID: row.AgentAccountID, CredentialID: row.CredentialID, Action: row.Action,
			SaleID: row.SaleID, OfferID: row.OfferID, ListingID: row.ListingID, Decision: row.Decision, Reasons: row.Reasons,
			PolicyVersion: row.PolicyVersion, Chain: row.Chain, AssetAddress: row.AssetAddress, AmountBaseUnits: row.AmountBaseUnits,
			DecidedBy: row.DecidedBy, DecidedAt: row.DecidedAt, CreatedAt: row.CreatedAt, Payload: row.Payload,
			Failure: row.Failure, FinishedAt: row.FinishedAt,
		}))
		if err != nil {
			return nil, err
		}
		if row.Name != nil {
			a.Name = *row.Name
		}
		out = append(out, a)
	}
	return out, nil
}

func toAgentAuthorization(r generated.AgentMarketAuthorization) (AgentAuthorization, error) {
	a := AgentAuthorization{
		ID: r.ID, AgentID: r.AgentAccountID, CredentialID: r.CredentialID, Action: r.Action, Decision: r.Decision,
		PolicyVersion: int(r.PolicyVersion), Chain: r.Chain, AssetAddress: r.AssetAddress, Amount: r.AmountBaseUnits,
		SaleID: r.SaleID, OfferID: r.OfferID, ListingID: r.ListingID,
		DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt, FinishedAt: r.FinishedAt, CreatedAt: r.CreatedAt,
	}
	if r.Failure != nil {
		a.Failure = *r.Failure
	}
	if err := json.Unmarshal(r.Reasons, &a.Reasons); err != nil {
		return AgentAuthorization{}, err
	}
	if err := json.Unmarshal(r.Payload, &a.Input); err != nil {
		return AgentAuthorization{}, err
	}
	return a, nil
}

func wrapInternal(a AgentAuthorization, err error) (AgentAuthorization, error) {
	if err != nil {
		return AgentAuthorization{}, shared.ErrInternal.WithError(err)
	}
	return a, nil
}
