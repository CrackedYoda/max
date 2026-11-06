package market

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/agents"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/shared"
)

// fakeAgents is the agent directory and the authorization store.
type fakeAgents struct {
	agent  agents.Agent
	policy agents.Policy
	spent  *big.Int
	authz  map[uuid.UUID]AgentAuthorization
}

func newFakeAgents(agentID, admin uuid.UUID, p agents.Policy) *fakeAgents {
	return &fakeAgents{
		agent:  agents.Agent{AccountID: agentID, AdministratorID: admin, Status: agents.StatusActive},
		policy: p, spent: new(big.Int), authz: map[uuid.UUID]AgentAuthorization{},
	}
}

func (f *fakeAgents) Get(_ context.Context, id uuid.UUID) (agents.Agent, error) {
	if id != f.agent.AccountID {
		return agents.Agent{}, agents.ErrAgentNotFound
	}
	return f.agent, nil
}
func (f *fakeAgents) GetPolicy(context.Context, uuid.UUID) (agents.Policy, error) {
	return f.policy, nil
}

func (f *fakeAgents) Decide(_ context.Context, _ uuid.UUID, decide func(agents.SpendFunc) NewAgentAuthorization) (AgentAuthorization, error) {
	n := decide(func(string, string) *big.Int { return new(big.Int).Set(f.spent) })
	a := AgentAuthorization{
		ID: uuid.New(), AgentID: n.AgentID, CredentialID: n.CredentialID, Action: n.Action, Decision: n.Decision,
		Reasons: n.Reasons, PolicyVersion: n.PolicyVersion, Chain: n.Chain, AssetAddress: n.Asset, Amount: n.Amount,
		SaleID: n.Ref.SaleID, OfferID: n.Ref.OfferID, ListingID: n.Ref.ListingID, Input: n.Input, CreatedAt: time.Now(),
	}
	f.authz[a.ID] = a
	return a, nil
}
func (f *fakeAgents) GetAuthorization(_ context.Context, id uuid.UUID) (AgentAuthorization, error) {
	a, ok := f.authz[id]
	if !ok {
		return AgentAuthorization{}, ErrAgentAuthorizationNotFound
	}
	return a, nil
}
func (f *fakeAgents) LatestPay(_ context.Context, agentID, saleID uuid.UUID) (*AgentAuthorization, error) {
	var out *AgentAuthorization
	for _, a := range f.authz {
		if a.AgentID == agentID && a.Action == ActionPay && a.SaleID != nil && *a.SaleID == saleID && a.Failure == "" &&
			a.Decision != DecisionDenied && (out == nil || a.CreatedAt.After(out.CreatedAt)) {
			a := a
			out = &a
		}
	}
	return out, nil
}
func (f *fakeAgents) Resolve(_ context.Context, id uuid.UUID, decision string, by uuid.UUID) (AgentAuthorization, error) {
	a, ok := f.authz[id]
	if !ok {
		return AgentAuthorization{}, ErrAgentAuthorizationNotFound
	}
	if a.Decision != DecisionPending {
		return AgentAuthorization{}, agents.ErrApprovalNotPending
	}
	now := time.Now()
	a.Decision, a.DecidedBy, a.DecidedAt = decision, &by, &now
	f.authz[id] = a
	return a, nil
}
func (f *fakeAgents) Finish(_ context.Context, id uuid.UUID, ref AuthRef, failure string) error {
	a := f.authz[id]
	if ref.SaleID != nil {
		a.SaleID = ref.SaleID
	}
	if ref.ListingID != nil {
		a.ListingID = ref.ListingID
	}
	a.Failure = failure
	f.authz[id] = a
	return nil
}
func (f *fakeAgents) Administrator(_ context.Context, id uuid.UUID) (*uuid.UUID, error) {
	if id != f.agent.AccountID {
		return nil, nil
	}
	admin := f.agent.AdministratorID
	return &admin, nil
}

// agentHarness is a market harness whose buyer is an agent with a 30 USDC
// per-transfer limit, administered by admin.
type agentHarness struct {
	*harness
	agents *fakeAgents
	admin  uuid.UUID
	cred   uuid.UUID
}

func newAgentHarness(t *testing.T) *agentHarness {
	t.Helper()
	h := newHarness(t, nil)
	ah := &agentHarness{harness: h, admin: uuid.New(), cred: uuid.New()}
	ah.agents = newFakeAgents(h.buyer, ah.admin, agents.Policy{
		Version: 3,
		Limits: []agents.AssetLimit{{Chain: "solana-devnet", Asset: devnetUSDC,
			MaxPerTransfer: big.NewInt(30_000_000), MaxPer24h: big.NewInt(100_000_000)}},
	})
	h.svc = NewService(h.repo, map[chains.Namespace]settle.Adapter{chains.NamespaceSolana: h.adapter}, h.svc.cfg,
		NewAgentPolicy(ah.agents, ah.agents), zap.NewNop())
	return ah
}

func (ah *agentHarness) agentPrincipal() shared.Principal {
	cred := ah.cred
	return shared.Principal{AccountID: ah.buyer, AccountType: "agent", CredentialID: &cred, Scopes: []string{"market:buy"}}
}

func (ah *agentHarness) agentBuy(key string) (SaleView, error) {
	return ah.svc.Buy(context.Background(), ah.agentPrincipal(), ah.listing, BuyInput{FromAddress: ah.payer, IdempotencyKey: key})
}

func pendingID(t *testing.T, err error) uuid.UUID {
	t.Helper()
	var app *shared.AppError
	if !errors.As(err, &app) || app.Code != "PENDING_APPROVAL" {
		t.Fatalf("want PENDING_APPROVAL, got %v", err)
	}
	id, err := uuid.Parse(app.Details["authorizationId"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAgentPolicy_HumanAutoApproved(t *testing.T) {
	ah := newAgentHarness(t)
	pol := NewAgentPolicy(ah.agents, ah.agents)
	d, err := pol.Authorize(context.Background(), human(ah.buyer), ActionBuy, big.NewInt(1e12), "solana-devnet", devnetUSDC, AuthRef{}, ActionInput{})
	if err != nil || d.Outcome != DecisionApproved || d.AuthorizationID != nil {
		t.Fatalf("human: %+v %v", d, err)
	}
	if len(ah.agents.authz) != 0 {
		t.Fatal("a human action wrote an authorization row")
	}
	// A human buys a listing above any agent limit straight away.
	if _, err := ah.buy("human-buy-0001"); err != nil {
		t.Fatal(err)
	}
}

func TestAgentPolicy_AgentWithinLimitAutoApproved(t *testing.T) {
	ah := newAgentHarness(t)
	v, err := ah.agentBuy("agent-buy-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(ah.agents.authz) != 1 {
		t.Fatalf("rows %d", len(ah.agents.authz))
	}
	for _, a := range ah.agents.authz {
		if a.Decision != "auto_approved" || a.Amount != "25000000" || a.SaleID == nil || *a.SaleID != v.Sale.ID ||
			a.CredentialID != ah.cred || a.PolicyVersion != 3 || a.Input.Op != OpBuy || a.Input.Buy.IdempotencyKey != "agent-buy-0001" {
			t.Fatalf("authorization %+v", a)
		}
	}
}

func TestAgentPolicy_FailedActionRecorded(t *testing.T) {
	ah := newAgentHarness(t)
	ah.adapter.fundsErr = shortfall()
	if _, err := ah.agentBuy("agent-buy-0002"); code(err) != "INSUFFICIENT_FUNDS" {
		t.Fatalf("got %v", err)
	}
	for _, a := range ah.agents.authz {
		if a.Failure == "" {
			t.Fatalf("failure not recorded: %+v", a)
		}
	}
}

func TestApprove_ReplaysBuy(t *testing.T) {
	ah := newAgentHarness(t)
	l := ah.repo.listings[ah.listing]
	l.PriceBaseUnits = "40000000" // above the 30 USDC per-transfer limit
	ah.repo.listings[ah.listing] = l

	_, err := ah.agentBuy("agent-buy-0003")
	id := pendingID(t, err)
	if ah.repo.opened != 0 {
		t.Fatal("a pending buy opened a sale")
	}
	if a := ah.agents.authz[id]; a.Decision != DecisionPending || a.Input.Op != OpBuy || a.Input.ListingID == nil || *a.Input.ListingID != ah.listing {
		t.Fatalf("pending row %+v", a)
	}

	res, err := ah.svc.ApproveAgentAction(context.Background(), ah.admin, id)
	if err != nil {
		t.Fatal(err)
	}
	if ah.repo.opened != 1 || res.Sale == nil || res.Sale.Sale.BuyerAccountID != ah.buyer || res.Sale.Sale.BuyerAddress != ah.payer ||
		res.Sale.Sale.IdempotencyKey != "agent-buy-0003" {
		t.Fatalf("replay result %+v", res)
	}
	a := ah.agents.authz[id]
	if a.Decision != DecisionApproved || a.DecidedBy == nil || *a.DecidedBy != ah.admin || a.SaleID == nil || *a.SaleID != res.Sale.Sale.ID || a.Failure != "" {
		t.Fatalf("approved row %+v", a)
	}
	// Approving twice is refused.
	if _, err := ah.svc.ApproveAgentAction(context.Background(), ah.admin, id); !errors.Is(err, agents.ErrApprovalNotPending) {
		t.Fatalf("double approve: %v", err)
	}
	// The agent retrying with the same key gets the replayed sale.
	if v, err := ah.agentBuy("agent-buy-0003"); err != nil || !v.Replayed || v.Sale.ID != res.Sale.Sale.ID {
		t.Fatalf("agent retry: %+v %v", v, err)
	}
}

func TestApprove_ReplayFailureRecorded(t *testing.T) {
	ah := newAgentHarness(t)
	l := ah.repo.listings[ah.listing]
	l.PriceBaseUnits = "40000000"
	ah.repo.listings[ah.listing] = l
	_, err := ah.agentBuy("agent-buy-0004")
	id := pendingID(t, err)

	l.Status = ListingSold // sold meanwhile
	ah.repo.listings[ah.listing] = l
	if _, err := ah.svc.ApproveAgentAction(context.Background(), ah.admin, id); !errors.Is(err, ErrListingNotActive) {
		t.Fatalf("got %v", err)
	}
	if a := ah.agents.authz[id]; a.Decision != DecisionApproved || a.Failure == "" {
		t.Fatalf("row %+v", a)
	}
}

func TestApprove_OnlyAdministrator(t *testing.T) {
	ah := newAgentHarness(t)
	l := ah.repo.listings[ah.listing]
	l.PriceBaseUnits = "40000000"
	ah.repo.listings[ah.listing] = l
	_, err := ah.agentBuy("agent-buy-0005")
	id := pendingID(t, err)

	for _, who := range []uuid.UUID{uuid.New(), ah.buyer, ah.seller} {
		if _, err := ah.svc.ApproveAgentAction(context.Background(), who, id); !errors.Is(err, ErrAgentAuthorizationNotFound) {
			t.Fatalf("approve by %v: %v", who, err)
		}
		if _, err := ah.svc.DenyAgentAction(context.Background(), who, id); !errors.Is(err, ErrAgentAuthorizationNotFound) {
			t.Fatalf("deny by %v: %v", who, err)
		}
	}
	if ah.agents.authz[id].Decision != DecisionPending || ah.repo.opened != 0 {
		t.Fatal("a stranger changed the authorization")
	}
	a, err := ah.svc.DenyAgentAction(context.Background(), ah.admin, id)
	if err != nil || a.Decision != DecisionDenied || ah.repo.opened != 0 {
		t.Fatalf("deny: %+v %v", a, err)
	}
}

func TestAgentPolicy_SellNeedsCanSell(t *testing.T) {
	h := newHarness(t, nil)
	admin, cred := uuid.New(), uuid.New()
	fa := newFakeAgents(h.seller, admin, agents.Policy{Market: agents.MarketPolicy{CanSell: false, MinSalePrice: "0"}})
	h.svc = NewService(h.repo, map[chains.Namespace]settle.Adapter{chains.NamespaceSolana: h.adapter}, h.svc.cfg, NewAgentPolicy(fa, fa), zap.NewNop())
	seller := shared.Principal{AccountID: h.seller, AccountType: "agent", CredentialID: &cred}
	before := len(h.repo.listings)
	_, err := h.svc.List(context.Background(), seller, ListInput{MaxID: h.maxID, Price: "25", Chain: "solana-devnet", Asset: devnetUSDC, PayoutAddr: h.payout})
	id := pendingID(t, err)
	if len(h.repo.listings) != before || fa.authz[id].Action != ActionList || fa.authz[id].Input.List == nil {
		t.Fatalf("pending list: %+v", fa.authz[id])
	}
	// Accepting an offer is a sale too.
	o := h.seedOffer(h.buyer, h.payer, OfferLive)
	_, err = h.svc.AcceptOffer(context.Background(), seller, o.ID, h.payout)
	if aid := pendingID(t, err); fa.authz[aid].Action != ActionAccept || h.repo.opened != 0 {
		t.Fatalf("pending accept: %+v", fa.authz[aid])
	}
	// The administrator approves the listing; the replay creates it.
	res, err := h.svc.ApproveAgentAction(context.Background(), admin, id)
	if err != nil || res.Listing == nil || res.Listing.SellerAccountID != h.seller || res.Listing.PriceBaseUnits != "25000000" {
		t.Fatalf("approve list: %+v %v", res, err)
	}
}

func TestAgentPolicy_PayChecked(t *testing.T) {
	ah := newAgentHarness(t)
	o := ah.seedOffer(ah.buyer, ah.payer, OfferAccepted)
	o.AmountBaseUnits = "40000000"
	ah.repo.offers[o.ID] = o
	oid := o.ID
	lock := time.Now().Add(time.Hour)
	sale := Sale{ID: uuid.New(), MaxID: o.MaxID, OfferID: &oid, SellerAccountID: ah.seller, BuyerAccountID: ah.buyer,
		BuyerAddress: ah.payer, SellerPayoutAddress: ah.payout, TreasuryAddress: solAddr(), Chain: "solana-devnet",
		AssetAddress: devnetUSDC, AssetDecimals: 6, PriceBaseUnits: "40000000", FeeBaseUnits: "4000000", SellerBaseUnits: "36000000",
		Status: SaleLocked, LockExpiresAt: &lock}
	ah.repo.sales[sale.ID] = sale

	_, err := ah.svc.PayOffer(context.Background(), ah.agentPrincipal(), o.ID, "pay-key-0001")
	id := pendingID(t, err)
	// Asking again while pending reuses the same authorization.
	_, err = ah.svc.PayOffer(context.Background(), ah.agentPrincipal(), o.ID, "pay-key-0001")
	if again := pendingID(t, err); again != id || len(ah.agents.authz) != 1 {
		t.Fatalf("second pay made a new authorization")
	}
	res, err := ah.svc.ApproveAgentAction(context.Background(), ah.admin, id)
	if err != nil || res.Sale == nil || res.Sale.Instructions == nil {
		t.Fatalf("approve pay: %+v %v", res, err)
	}
	// Once approved, the agent gets instructions without a new evaluation.
	v, err := ah.svc.PayOffer(context.Background(), ah.agentPrincipal(), o.ID, "pay-key-0001")
	if err != nil || v.Instructions == nil || len(ah.agents.authz) != 1 {
		t.Fatalf("pay after approval: %+v %v (%d rows)", v, err, len(ah.agents.authz))
	}
}

func TestMakeOffer_AgentNotSpend(t *testing.T) {
	ah := newAgentHarness(t)
	// 40 USDC is above the per-transfer limit, but an offer spends nothing until pay.
	o, err := ah.svc.MakeOffer(context.Background(), ah.agentPrincipal(), OfferInput{
		Name: "yoda", Amount: "40", Chain: "solana-devnet", Asset: devnetUSDC, FromAddress: ah.payer,
	})
	if err != nil || o.Status != OfferLive || len(ah.agents.authz) != 0 {
		t.Fatalf("offer: %+v %v", o, err)
	}
}
