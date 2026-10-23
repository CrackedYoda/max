package agents

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
	"github.com/max/max-core/internal/audit"
	"github.com/max/max-core/internal/database"
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

func (r *repository) CreateAgent(ctx context.Context, admin uuid.UUID, name, description string) (Agent, Policy, error) {
	var a Agent
	var p Policy
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		acct, err := q.CreateAccount(ctx, "agent")
		if err != nil {
			return err
		}
		row, err := q.CreateAgent(ctx, generated.CreateAgentParams{
			AccountID: acct.ID, AdministratorAccountID: admin, Name: name, Description: description,
		})
		if err != nil {
			return err
		}
		pol, err := q.CreateAgentPolicy(ctx, acct.ID)
		if err != nil {
			return err
		}
		a = toAgent(row)
		if p, err = toPolicy(pol); err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &admin, Action: "agent.created", ResourceType: "agent", ResourceID: acct.ID.String(),
			Metadata: map[string]any{"name": name},
		})
	})
	if err != nil {
		return Agent{}, Policy{}, shared.ErrInternal.WithError(err)
	}
	return a, p, nil
}

func (r *repository) GetForAdministrator(ctx context.Context, agentID, admin uuid.UUID) (Agent, error) {
	row, err := r.q.GetAgentForAdministrator(ctx, generated.GetAgentForAdministratorParams{AccountID: agentID, AdministratorAccountID: admin})
	if err != nil {
		return Agent{}, notFound(err, ErrAgentNotFound)
	}
	return toAgent(row), nil
}

func (r *repository) Get(ctx context.Context, agentID uuid.UUID) (Agent, error) {
	row, err := r.q.GetAgent(ctx, agentID)
	if err != nil {
		return Agent{}, notFound(err, ErrAgentNotFound)
	}
	return toAgent(row), nil
}

func (r *repository) ListForAdministrator(ctx context.Context, admin uuid.UUID) ([]Agent, error) {
	rows, err := r.q.ListAgentsByAdministrator(ctx, admin)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Agent, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAgent(row))
	}
	return out, nil
}

func (r *repository) Update(ctx context.Context, a Agent, actor uuid.UUID) (Agent, error) {
	var out Agent
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.UpdateAgent(ctx, generated.UpdateAgentParams{
			AccountID: a.AccountID, Name: a.Name, Description: a.Description, Status: string(a.Status),
		})
		if err != nil {
			return err
		}
		out = toAgent(row)
		return audit.Write(ctx, q, audit.Entry{
			Actor: &actor, Action: "agent.updated", ResourceType: "agent", ResourceID: a.AccountID.String(),
			Metadata: map[string]any{"status": string(a.Status)},
		})
	})
	if err != nil {
		return Agent{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

type limitJSON struct {
	Chain          string `json:"chain"`
	Asset          string `json:"asset"`
	MaxPerTransfer string `json:"maxPerTransfer"`
	MaxPer24h      string `json:"maxPer24h"`
}

func (r *repository) GetPolicy(ctx context.Context, agentID uuid.UUID) (Policy, error) {
	row, err := r.q.GetAgentPolicy(ctx, agentID)
	if err != nil {
		return Policy{}, notFound(err, ErrAgentNotFound)
	}
	p, err := toPolicy(row)
	if err != nil {
		return Policy{}, shared.ErrInternal.WithError(err)
	}
	return p, nil
}

func (r *repository) UpdatePolicy(ctx context.Context, agentID uuid.UUID, p Policy, actor uuid.UUID) (Policy, error) {
	limits := make([]limitJSON, 0, len(p.Limits))
	for _, l := range p.Limits {
		limits = append(limits, limitJSON{Chain: l.Chain, Asset: l.Asset, MaxPerTransfer: l.MaxPerTransfer.String(), MaxPer24h: l.MaxPer24h.String()})
	}
	b, err := json.Marshal(limits)
	if err != nil {
		return Policy{}, err
	}
	recipients := p.AllowedRecipients
	if recipients == nil {
		recipients = []string{}
	}

	var out Policy
	err = database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.UpdateAgentPolicy(ctx, generated.UpdateAgentPolicyParams{
			AgentAccountID: agentID, Limits: b, AllowedRecipients: recipients, AllowAnyRecipient: p.AllowAnyRecipient,
		})
		if err != nil {
			return err
		}
		if out, err = toPolicy(row); err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &actor, Action: "agent.policy_updated", ResourceType: "agent", ResourceID: agentID.String(),
			Metadata: map[string]any{"version": out.Version, "limits": limits, "allowed_recipients": recipients, "allow_any_recipient": p.AllowAnyRecipient},
		})
	})
	if err != nil {
		return Policy{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

func (r *repository) CreateCredential(ctx context.Context, c Credential, hash []byte) (Credential, error) {
	var out Credential
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		out, err = createCredential(ctx, r.q.WithTx(tx), c, hash)
		return err
	})
	if err != nil {
		return Credential{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

func createCredential(ctx context.Context, q *generated.Queries, c Credential, hash []byte) (Credential, error) {
	row, err := q.CreateAgentCredential(ctx, generated.CreateAgentCredentialParams{
		AgentAccountID: c.AgentID, Name: c.Name, Prefix: c.Prefix, SecretHash: hash,
		Scopes: scopeStrings(c.Scopes), CreatedBy: c.CreatedBy, ExpiresAt: c.ExpiresAt,
	})
	if err != nil {
		return Credential{}, err
	}
	actor := c.CreatedBy
	if err := audit.Write(ctx, q, audit.Entry{
		Actor: &actor, Action: "agent.credential_issued", ResourceType: "agent", ResourceID: c.AgentID.String(),
		Metadata: map[string]any{"credential_id": row.ID.String(), "prefix": row.Prefix, "scopes": row.Scopes},
	}); err != nil {
		return Credential{}, err
	}
	return toCredential(row), nil
}

func (r *repository) RotateCredential(ctx context.Context, agentID, oldID uuid.UUID, next Credential, hash []byte) (Credential, error) {
	var out Credential
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		var err error
		if out, err = createCredential(ctx, q, next, hash); err != nil {
			return err
		}
		n, err := q.RevokeAgentCredential(ctx, generated.RevokeAgentCredentialParams{ID: oldID, AgentAccountID: agentID, ReplacedBy: &out.ID})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrCredentialNotFound
		}
		return nil
	})
	return out, mapErr(err)
}

func (r *repository) GetCredentialByPrefix(ctx context.Context, prefix string) (Credential, []byte, Status, error) {
	row, err := r.q.GetCredentialByPrefix(ctx, prefix)
	if err != nil {
		return Credential{}, nil, "", notFound(err, ErrCredentialNotFound)
	}
	c := toCredential(generated.AgentCredential{
		ID: row.ID, AgentAccountID: row.AgentAccountID, Name: row.Name, Prefix: row.Prefix, Scopes: row.Scopes,
		CreatedBy: row.CreatedBy, ExpiresAt: row.ExpiresAt, LastUsedAt: row.LastUsedAt, RevokedAt: row.RevokedAt,
		ReplacedBy: row.ReplacedBy, CreatedAt: row.CreatedAt,
	})
	return c, row.SecretHash, Status(row.AgentStatus), nil
}

func (r *repository) ListCredentials(ctx context.Context, agentID uuid.UUID) ([]Credential, error) {
	rows, err := r.q.ListAgentCredentials(ctx, agentID)
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]Credential, 0, len(rows))
	for _, row := range rows {
		out = append(out, toCredential(row))
	}
	return out, nil
}

func (r *repository) RevokeCredential(ctx context.Context, agentID, credID, actor uuid.UUID) error {
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		n, err := q.RevokeAgentCredential(ctx, generated.RevokeAgentCredentialParams{ID: credID, AgentAccountID: agentID})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrCredentialNotFound
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &actor, Action: "agent.credential_revoked", ResourceType: "agent", ResourceID: agentID.String(),
			Metadata: map[string]any{"credential_id": credID.String()},
		})
	})
	return mapErr(err)
}

func (r *repository) TouchCredential(ctx context.Context, id uuid.UUID) error {
	return r.q.TouchAgentCredential(ctx, id)
}

func (r *repository) DecideUnderLock(ctx context.Context, agentID uuid.UUID, decide func(SpendFunc) TransferAuthorization) (TransferAuthorization, error) {
	var out TransferAuthorization
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockAgentBudget(ctx, agentID.String()); err != nil {
			return err
		}
		rows, err := q.ListAgentSpend24h(ctx, agentID)
		if err != nil {
			return err
		}
		spent := map[string]*big.Int{}
		for _, row := range rows {
			v, ok := new(big.Int).SetString(row.AmountBaseUnits, 10)
			if !ok {
				return fmt.Errorf("invalid committed amount %q", row.AmountBaseUnits)
			}
			key := row.Chain + "|" + row.AssetAddress
			if spent[key] == nil {
				spent[key] = new(big.Int)
			}
			spent[key].Add(spent[key], v)
		}

		a := decide(func(chain, asset string) *big.Int {
			if v := spent[chain+"|"+asset]; v != nil {
				return new(big.Int).Set(v)
			}
			return new(big.Int)
		})
		reasons, err := json.Marshal(orEmpty(a.Reasons))
		if err != nil {
			return err
		}
		row, err := q.CreateTransferAuthorization(ctx, generated.CreateTransferAuthorizationParams{
			PaymentID: a.PaymentID, AgentAccountID: a.AgentID, CredentialID: a.CredentialID,
			Decision: string(a.Decision), Reasons: reasons, PolicyVersion: int32(a.PolicyVersion),
			Chain: a.Chain, AssetAddress: a.AssetAddress, AmountBaseUnits: a.Amount.String(), Instruction: a.Instruction,
		})
		if err != nil {
			return err
		}
		out, err = toAuthorization(row)
		return err
	})
	if err != nil {
		return TransferAuthorization{}, shared.ErrInternal.WithError(err)
	}
	return out, nil
}

func (r *repository) GetAuthorization(ctx context.Context, paymentID uuid.UUID) (TransferAuthorization, error) {
	row, err := r.q.GetTransferAuthorization(ctx, paymentID)
	if err != nil {
		return TransferAuthorization{}, notFound(err, ErrTransferNotFound)
	}
	a, err := toAuthorization(row)
	if err != nil {
		return TransferAuthorization{}, shared.ErrInternal.WithError(err)
	}
	return a, nil
}

func (r *repository) DecideApproval(ctx context.Context, paymentID uuid.UUID, d DecisionOutcome, by uuid.UUID) (TransferAuthorization, error) {
	var out TransferAuthorization
	err := database.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.DecideTransferAuthorization(ctx, generated.DecideTransferAuthorizationParams{
			PaymentID: paymentID, Decision: string(d), DecidedBy: &by,
		})
		if err != nil {
			if database.IsNoRows(err) {
				return ErrApprovalNotPending
			}
			return err
		}
		if out, err = toAuthorization(row); err != nil {
			return err
		}
		return audit.Write(ctx, q, audit.Entry{
			Actor: &by, Action: "agent.transfer_" + string(d), ResourceType: "payment", ResourceID: paymentID.String(),
			Metadata: map[string]any{"agent_id": out.AgentID.String(), "amount": out.Amount.String(), "chain": out.Chain},
		})
	})
	return out, mapErr(err)
}

func (r *repository) ListAuthorizations(ctx context.Context, agentID uuid.UUID, decision *DecisionOutcome, limit int32) ([]TransferAuthorization, error) {
	var d *string
	if decision != nil {
		s := string(*decision)
		d = &s
	}
	rows, err := r.q.ListTransferAuthorizations(ctx, generated.ListTransferAuthorizationsParams{AgentAccountID: agentID, Decision: d, Limit: limit})
	if err != nil {
		return nil, shared.ErrInternal.WithError(err)
	}
	out := make([]TransferAuthorization, 0, len(rows))
	for _, row := range rows {
		a, err := toAuthorization(row)
		if err != nil {
			return nil, shared.ErrInternal.WithError(err)
		}
		out = append(out, a)
	}
	return out, nil
}

func toAgent(r generated.Agent) Agent {
	return Agent{
		AccountID: r.AccountID, AdministratorID: r.AdministratorAccountID, Name: r.Name,
		Description: r.Description, Status: Status(r.Status), CreatedAt: r.CreatedAt,
	}
}

func toPolicy(r generated.AgentPolicy) (Policy, error) {
	var raw []limitJSON
	if err := json.Unmarshal(r.Limits, &raw); err != nil {
		return Policy{}, err
	}
	p := Policy{Version: int(r.Version), AllowedRecipients: r.AllowedRecipients, AllowAnyRecipient: r.AllowAnyRecipient, UpdatedAt: r.UpdatedAt}
	for _, l := range raw {
		perTx, ok1 := new(big.Int).SetString(l.MaxPerTransfer, 10)
		perDay, ok2 := new(big.Int).SetString(l.MaxPer24h, 10)
		if !ok1 || !ok2 {
			return Policy{}, fmt.Errorf("policy has invalid limit for %s", l.Chain)
		}
		p.Limits = append(p.Limits, AssetLimit{Chain: l.Chain, Asset: l.Asset, MaxPerTransfer: perTx, MaxPer24h: perDay})
	}
	return p, nil
}

func toCredential(r generated.AgentCredential) Credential {
	c := Credential{
		ID: r.ID, AgentID: r.AgentAccountID, Name: r.Name, Prefix: r.Prefix, CreatedBy: r.CreatedBy,
		ExpiresAt: r.ExpiresAt, LastUsedAt: r.LastUsedAt, RevokedAt: r.RevokedAt, ReplacedBy: r.ReplacedBy, CreatedAt: r.CreatedAt,
	}
	for _, s := range r.Scopes {
		c.Scopes = append(c.Scopes, Scope(s))
	}
	return c
}

func toAuthorization(r generated.AgentTransferAuthorization) (TransferAuthorization, error) {
	amount, ok := new(big.Int).SetString(r.AmountBaseUnits, 10)
	if !ok {
		return TransferAuthorization{}, fmt.Errorf("authorization has invalid amount %q", r.AmountBaseUnits)
	}
	a := TransferAuthorization{
		PaymentID: r.PaymentID, AgentID: r.AgentAccountID, CredentialID: r.CredentialID,
		Decision: DecisionOutcome(r.Decision), PolicyVersion: int(r.PolicyVersion), Chain: r.Chain,
		AssetAddress: r.AssetAddress, Amount: amount, Instruction: r.Instruction,
		DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt, CreatedAt: r.CreatedAt,
	}
	_ = json.Unmarshal(r.Reasons, &a.Reasons)
	return a, nil
}

func scopeStrings(sc []Scope) []string {
	out := make([]string, len(sc))
	for i, s := range sc {
		out[i] = string(s)
	}
	return out
}

func orEmpty(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func notFound(err, sentinel error) error {
	if database.IsNoRows(err) {
		return sentinel
	}
	return shared.ErrInternal.WithError(err)
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var appErr *shared.AppError
	if errors.As(err, &appErr) {
		return err
	}
	return shared.ErrInternal.WithError(err)
}
