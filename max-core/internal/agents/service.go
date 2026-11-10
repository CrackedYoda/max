package agents

import (
	"context"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/auth"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/shared"
)

const (
	maxCredentialTTL   = 365 * 24 * time.Hour
	maxInstructionLen  = 1000
	maxAllowlistLength = 100
)

type Deps struct {
	Repo         Repository
	Payments     Payments
	Wallets      Wallets
	IDs          IDs
	WalletProofs WalletProofs
	// Attestor may be nil, in which case signed authorizations are unavailable.
	Attestor *Attestor
	// CredentialSecret keys the HMAC over API key secrets.
	CredentialSecret string
	Log              *zap.Logger
}

type service struct {
	Deps
	now func() time.Time
}

var _ Service = (*service)(nil)

func NewService(d Deps) Service {
	return &service{Deps: d, now: time.Now}
}

// ---- Administrator operations ----

func (s *service) CreateAgent(ctx context.Context, admin uuid.UUID, name, description string) (AgentDetail, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return AgentDetail{}, shared.ErrValidation.WithMessage("name is required")
	}
	a, p, err := s.Repo.CreateAgent(ctx, admin, name, strings.TrimSpace(description))
	if err != nil {
		return AgentDetail{}, err
	}
	return AgentDetail{Agent: a, Policy: p}, nil
}

func (s *service) ListAgents(ctx context.Context, admin uuid.UUID) ([]Agent, error) {
	return s.Repo.ListForAdministrator(ctx, admin)
}

func (s *service) GetAgent(ctx context.Context, admin, agentID uuid.UUID) (AgentDetail, error) {
	a, err := s.Repo.GetForAdministrator(ctx, agentID, admin)
	if err != nil {
		return AgentDetail{}, err
	}
	d := AgentDetail{Agent: a}
	if d.Policy, err = s.Repo.GetPolicy(ctx, agentID); err != nil {
		return AgentDetail{}, err
	}
	if d.Credentials, err = s.Repo.ListCredentials(ctx, agentID); err != nil {
		return AgentDetail{}, err
	}
	if d.Wallets, err = s.Wallets.List(ctx, agentID); err != nil {
		return AgentDetail{}, err
	}
	if d.IDs, err = s.IDs.ListMine(ctx, agentID); err != nil {
		return AgentDetail{}, err
	}
	return d, nil
}

func (s *service) UpdateAgent(ctx context.Context, admin, agentID uuid.UUID, name, description string, status Status) (Agent, error) {
	a, err := s.Repo.GetForAdministrator(ctx, agentID, admin)
	if err != nil {
		return Agent{}, err
	}
	if n := strings.TrimSpace(name); n != "" {
		a.Name = n
	}
	a.Description = strings.TrimSpace(description)
	if status != "" {
		if status != StatusActive && status != StatusPaused {
			return Agent{}, shared.ErrValidation.WithMessage("status must be active or paused")
		}
		a.Status = status
	}
	return s.Repo.Update(ctx, a, admin)
}

func (s *service) SetPolicy(ctx context.Context, admin, agentID uuid.UUID, in PolicyInput) (Policy, error) {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return Policy{}, err
	}
	p, err := buildPolicy(in)
	if err != nil {
		return Policy{}, err
	}
	if in.Market == nil {
		cur, err := s.Repo.GetPolicy(ctx, agentID)
		if err != nil {
			return Policy{}, err
		}
		p.Market = cur.Market
	}
	return s.Repo.UpdatePolicy(ctx, agentID, p, admin)
}

// buildPolicy converts human-entered limits into base units. Decimals come
// from the chain (native) or MAX's verified token list; unverified tokens
// cannot be delegated, because their decimals could be spoofed.
func buildPolicy(in PolicyInput) (Policy, error) {
	p := Policy{AllowAnyRecipient: in.AllowAnyRecipient}
	seen := map[string]bool{}
	for _, l := range in.Limits {
		chain, ok := chains.LookupSendable(l.Chain)
		if !ok {
			return Policy{}, ErrInvalidPolicy.WithMessage("unknown chain " + l.Chain)
		}
		asset, decimals := "", chain.NativeDecimals
		if a := strings.TrimSpace(l.Asset); a != "" && !strings.EqualFold(a, "native") {
			norm, err := chains.NormalizeAddress(chain.Namespace, a)
			if err != nil {
				return Policy{}, ErrInvalidPolicy.WithMessage("invalid asset address for " + chain.Key)
			}
			tok, ok := chains.LookupVerifiedToken(chain.Key, norm)
			if !ok {
				return Policy{}, ErrInvalidPolicy.WithMessage("only native coins and verified tokens can be delegated")
			}
			asset, decimals = norm, tok.Decimals
		}
		key := chain.Key + "|" + asset
		if seen[key] {
			return Policy{}, ErrInvalidPolicy.WithMessage("duplicate limit for " + key)
		}
		seen[key] = true

		perTx, err := payments.ParseAmount(l.MaxPerTransfer, decimals)
		if err != nil {
			return Policy{}, ErrInvalidPolicy.WithMessage("maxPerTransfer: " + err.Error())
		}
		perDay, err := payments.ParseAmount(l.MaxPer24h, decimals)
		if err != nil {
			return Policy{}, ErrInvalidPolicy.WithMessage("maxPer24h: " + err.Error())
		}
		if perTx.Cmp(perDay) > 0 {
			return Policy{}, ErrInvalidPolicy.WithMessage("maxPerTransfer cannot exceed maxPer24h")
		}
		p.Limits = append(p.Limits, AssetLimit{Chain: chain.Key, Asset: asset, MaxPerTransfer: perTx, MaxPer24h: perDay})
	}

	if in.Market != nil {
		m, err := buildMarketPolicy(*in.Market)
		if err != nil {
			return Policy{}, err
		}
		p.Market = m
	}

	if len(in.AllowedRecipients) > maxAllowlistLength {
		return Policy{}, ErrInvalidPolicy.WithMessage("allowlist is too long")
	}
	for _, r := range in.AllowedRecipients {
		canonical, err := maxids.Normalize(r)
		if err != nil {
			return Policy{}, ErrInvalidPolicy.WithMessage("invalid recipient " + r)
		}
		if !contains(p.AllowedRecipients, canonical) {
			p.AllowedRecipients = append(p.AllowedRecipients, canonical)
		}
	}
	return p, nil
}

// buildMarketPolicy validates the selling policy. Minimums are per verified
// asset, like limits, and exact base-unit integers.
func buildMarketPolicy(in MarketPolicyInput) (MarketPolicy, error) {
	m := MarketPolicy{CanSell: in.CanSell}
	seen := map[string]bool{}
	for _, sm := range in.MinSalePrices {
		chain, ok := chains.LookupSendable(sm.Chain)
		if !ok {
			return MarketPolicy{}, ErrInvalidPolicy.WithMessage("market.minSalePrices: unknown chain " + sm.Chain)
		}
		asset := ""
		if a := strings.TrimSpace(sm.Asset); a != "" && !strings.EqualFold(a, "native") {
			norm, err := chains.NormalizeAddress(chain.Namespace, a)
			if err != nil {
				return MarketPolicy{}, ErrInvalidPolicy.WithMessage("market.minSalePrices: invalid asset address for " + chain.Key)
			}
			if _, ok := chains.LookupVerifiedToken(chain.Key, norm); !ok {
				return MarketPolicy{}, ErrInvalidPolicy.WithMessage("market.minSalePrices: only native coins and verified tokens are supported")
			}
			asset = norm
		}
		key := chain.Key + "|" + asset
		if seen[key] {
			return MarketPolicy{}, ErrInvalidPolicy.WithMessage("market.minSalePrices: duplicate minimum for " + key)
		}
		seen[key] = true
		if !baseUnitsRe.MatchString(sm.Amount) {
			return MarketPolicy{}, ErrInvalidPolicy.WithMessage("market.minSalePrices: amount must be a whole number of base units")
		}
		v, _ := new(big.Int).SetString(sm.Amount, 10)
		m.MinSalePrices = append(m.MinSalePrices, SaleMinimum{Chain: chain.Key, Asset: asset, Amount: v.String()})
	}
	return m, nil
}

var baseUnitsRe = regexp.MustCompile(`^[0-9]{1,78}$`)

func (s *service) IssueCredential(ctx context.Context, admin, agentID uuid.UUID, name string, scopes []string, ttl time.Duration) (IssuedCredential, error) {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return IssuedCredential{}, err
	}
	cred, err := s.newCredential(agentID, admin, name, scopes, ttl)
	if err != nil {
		return IssuedCredential{}, err
	}
	stored, err := s.Repo.CreateCredential(ctx, cred.Credential, s.keyHash(cred.Secret))
	if err != nil {
		return IssuedCredential{}, err
	}
	cred.Credential = stored
	return cred, nil
}

func (s *service) RotateCredential(ctx context.Context, admin, agentID, credID uuid.UUID) (IssuedCredential, error) {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return IssuedCredential{}, err
	}
	var old *Credential
	creds, err := s.Repo.ListCredentials(ctx, agentID)
	if err != nil {
		return IssuedCredential{}, err
	}
	for i := range creds {
		if creds[i].ID == credID && creds[i].RevokedAt == nil {
			old = &creds[i]
		}
	}
	if old == nil {
		return IssuedCredential{}, ErrCredentialNotFound
	}
	var ttl time.Duration
	if old.ExpiresAt != nil {
		ttl = old.ExpiresAt.Sub(old.CreatedAt)
	}
	scopes := make([]string, len(old.Scopes))
	for i, sc := range old.Scopes {
		scopes[i] = string(sc)
	}
	next, err := s.newCredential(agentID, admin, old.Name, scopes, ttl)
	if err != nil {
		return IssuedCredential{}, err
	}
	stored, err := s.Repo.RotateCredential(ctx, agentID, credID, next.Credential, s.keyHash(next.Secret))
	if err != nil {
		return IssuedCredential{}, err
	}
	next.Credential = stored
	return next, nil
}

func (s *service) newCredential(agentID, admin uuid.UUID, name string, scopes []string, ttl time.Duration) (IssuedCredential, error) {
	if len(scopes) == 0 {
		return IssuedCredential{}, ErrInvalidScope.WithMessage("at least one scope is required")
	}
	var sc []Scope
	for _, raw := range scopes {
		if !ValidScope(raw) {
			return IssuedCredential{}, ErrInvalidScope.WithMessage("unknown scope " + raw)
		}
		sc = append(sc, Scope(raw))
	}
	if ttl < 0 || ttl > maxCredentialTTL {
		return IssuedCredential{}, shared.ErrValidation.WithMessage("credential lifetime must be at most 365 days")
	}
	key, prefix, _, err := newKey()
	if err != nil {
		return IssuedCredential{}, shared.ErrInternal.WithError(err)
	}
	c := Credential{AgentID: agentID, Name: strings.TrimSpace(name), Prefix: prefix, Scopes: sc, CreatedBy: admin}
	if c.Name == "" {
		c.Name = "default"
	}
	if ttl > 0 {
		exp := s.now().Add(ttl)
		c.ExpiresAt = &exp
	}
	return IssuedCredential{Credential: c, Secret: key}, nil
}

// keyHash derives the stored hash from a full issued key, exactly as
// Authenticate derives it from a presented one.
func (s *service) keyHash(key string) []byte {
	_, secret, _ := parseKey(key)
	return hashSecret(s.CredentialSecret, secret)
}

func (s *service) RevokeCredential(ctx context.Context, admin, agentID, credID uuid.UUID) error {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return err
	}
	return s.Repo.RevokeCredential(ctx, agentID, credID, admin)
}

func (s *service) ClaimID(ctx context.Context, admin, agentID uuid.UUID, name, displayName string) (maxids.MaxID, error) {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return maxids.MaxID{}, err
	}
	return s.IDs.Claim(ctx, agentID, maxids.ClaimInput{Name: name, Type: maxids.TypeAgent, DisplayName: displayName})
}

// WalletChallenge lets an administrator link the agent's receiving wallet
// from their own browser: the challenge is bound to the agent account and
// must be signed by the agent's wallet.
func (s *service) WalletChallenge(ctx context.Context, admin, agentID uuid.UUID, namespace, address string) (auth.Challenge, error) {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return auth.Challenge{}, err
	}
	return s.WalletProofs.CreateChallenge(ctx, auth.ChallengeInput{
		Purpose: auth.PurposeLink, Namespace: namespace, Address: address, Caller: &agentID,
	})
}

func (s *service) WalletVerify(ctx context.Context, admin, agentID uuid.UUID, nonce, signature string) (auth.Wallet, error) {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return auth.Wallet{}, err
	}
	res, err := s.WalletProofs.Verify(ctx, auth.VerifyInput{Nonce: nonce, Signature: signature, Caller: &agentID})
	if err != nil {
		return auth.Wallet{}, err
	}
	return res.Wallet, nil
}

func (s *service) ListTransfers(ctx context.Context, admin, agentID uuid.UUID, decision *DecisionOutcome) ([]TransferView, error) {
	if _, err := s.Repo.GetForAdministrator(ctx, agentID, admin); err != nil {
		return nil, err
	}
	authz, err := s.Repo.ListAuthorizations(ctx, agentID, decision, 100)
	if err != nil {
		return nil, err
	}
	out := make([]TransferView, 0, len(authz))
	for _, a := range authz {
		p, _, err := s.Payments.Get(ctx, &agentID, a.PaymentID)
		if err != nil {
			return nil, err
		}
		out = append(out, TransferView{Payment: p, Authorization: a})
	}
	return out, nil
}

func (s *service) Approve(ctx context.Context, admin, paymentID uuid.UUID) (TransferView, error) {
	a, err := s.adminAuthorization(ctx, admin, paymentID)
	if err != nil {
		return TransferView{}, err
	}
	decided, err := s.Repo.DecideApproval(ctx, paymentID, DecisionApproved, admin)
	if err != nil {
		return TransferView{}, err
	}
	p, err := s.Payments.Release(ctx, paymentID, "approved by agent administrator")
	if err != nil {
		// The approval stands, but an expired intent never counts toward budget.
		return TransferView{}, err
	}
	return s.view(p, decided, a.AgentID)
}

func (s *service) Deny(ctx context.Context, admin, paymentID uuid.UUID, reason string) (TransferView, error) {
	a, err := s.adminAuthorization(ctx, admin, paymentID)
	if err != nil {
		return TransferView{}, err
	}
	decided, err := s.Repo.DecideApproval(ctx, paymentID, DecisionDenied, admin)
	if err != nil {
		return TransferView{}, err
	}
	msg := "denied by agent administrator"
	if r := strings.TrimSpace(reason); r != "" {
		msg += ": " + truncate(r, 200)
	}
	p, err := s.Payments.Reject(ctx, paymentID, msg)
	if err != nil {
		var cur payments.Payment
		if cur, _, err = s.Payments.Get(ctx, &a.AgentID, paymentID); err != nil {
			return TransferView{}, err
		}
		p = cur
	}
	return TransferView{Payment: p, Authorization: decided}, nil
}

// adminAuthorization loads an authorization and confirms the caller
// administers its agent; anything else is reported as not found.
func (s *service) adminAuthorization(ctx context.Context, admin, paymentID uuid.UUID) (TransferAuthorization, error) {
	a, err := s.Repo.GetAuthorization(ctx, paymentID)
	if err != nil {
		return TransferAuthorization{}, err
	}
	if _, err := s.Repo.GetForAdministrator(ctx, a.AgentID, admin); err != nil {
		return TransferAuthorization{}, ErrTransferNotFound
	}
	return a, nil
}

// ---- Agent operations ----

func (s *service) Authenticate(ctx context.Context, apiKey string) (shared.Principal, error) {
	prefix, secret, err := parseKey(apiKey)
	if err != nil {
		return shared.Principal{}, ErrCredentialInvalid
	}
	cred, hash, agentStatus, err := s.Repo.GetCredentialByPrefix(ctx, prefix)
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			return shared.Principal{}, ErrCredentialInvalid
		}
		return shared.Principal{}, err
	}
	if !secretMatches(s.CredentialSecret, secret, hash) || cred.RevokedAt != nil ||
		(cred.ExpiresAt != nil && !s.now().Before(*cred.ExpiresAt)) {
		return shared.Principal{}, ErrCredentialInvalid
	}
	if agentStatus != StatusActive {
		return shared.Principal{}, ErrAgentPaused
	}
	if err := s.Repo.TouchCredential(ctx, cred.ID); err != nil {
		s.Log.Warn("touch credential", zap.Error(err))
	}
	scopes := make([]string, len(cred.Scopes))
	for i, sc := range cred.Scopes {
		scopes[i] = string(sc)
	}
	id := cred.ID
	return shared.Principal{AccountID: cred.AgentID, AccountType: "agent", CredentialID: &id, Scopes: scopes}, nil
}

func (s *service) Propose(ctx context.Context, pr shared.Principal, in ProposeInput) (TransferView, error) {
	if pr.CredentialID == nil {
		return TransferView{}, shared.ErrForbidden
	}
	agentID := pr.AccountID
	agent, err := s.Repo.Get(ctx, agentID)
	if err != nil {
		return TransferView{}, err
	}
	if agent.Status != StatusActive {
		return TransferView{}, ErrAgentPaused
	}
	if err := s.requireAgentWallet(ctx, agentID, in.Chain, in.SenderAddress); err != nil {
		return TransferView{}, err
	}

	prep, err := s.Payments.Prepare(ctx, payments.PrepareInput{
		Caller: &agentID, Recipient: in.Recipient, Chain: in.Chain, Asset: in.Asset, Amount: in.Amount,
		SenderAddress: in.SenderAddress, IdempotencyKey: in.IdempotencyKey,
		DestinationChain: in.DestinationChain, DestinationAsset: in.DestinationAsset, AmountType: in.AmountType,
		HoldForApproval: true,
	})
	if err != nil {
		return TransferView{}, err
	}
	p := prep.Payment
	if prep.Replayed {
		a, err := s.Repo.GetAuthorization(ctx, p.ID)
		if err != nil {
			return TransferView{}, err
		}
		v, err := s.view(p, a, agentID)
		v.Replayed = true
		return v, err
	}

	policy, err := s.Repo.GetPolicy(ctx, agentID)
	if err != nil {
		return TransferView{}, err
	}
	authz, err := s.Repo.DecideUnderLock(ctx, agentID, func(spent SpendFunc) TransferAuthorization {
		prop := Proposal{
			Chain: p.Chain.Key, Asset: p.Asset.Address, Amount: p.Amount,
			Recipient: strings.TrimPrefix(p.RecipientHandle, maxids.Prefix),
		}
		ev := Evaluate(policy, agent.Status, prop, spent(prop.Chain, prop.Asset))
		return TransferAuthorization{
			PaymentID: p.ID, AgentID: agentID, CredentialID: *pr.CredentialID,
			Decision: ev.Outcome, Reasons: ev.Reasons, PolicyVersion: policy.Version,
			Chain: prop.Chain, AssetAddress: prop.Asset, Amount: p.Amount,
			Instruction: truncate(strings.TrimSpace(in.Instruction), maxInstructionLen),
		}
	})
	if err != nil {
		return TransferView{}, err
	}

	switch authz.Decision {
	case DecisionAutoApproved:
		if p, err = s.Payments.Release(ctx, p.ID, "within agent policy"); err != nil {
			return TransferView{}, err
		}
	case DecisionDenied:
		if _, err := s.Payments.Reject(ctx, p.ID, "denied by agent policy: "+strings.Join(authz.Reasons, "; ")); err != nil {
			return TransferView{}, err
		}
		return TransferView{}, ErrPolicyDenied.WithMessage("transfer denied by agent policy: " + strings.Join(authz.Reasons, "; "))
	}
	v, err := s.view(p, authz, agentID)
	v.Warnings = prep.Warnings
	return v, err
}

func (s *service) requireAgentWallet(ctx context.Context, agentID uuid.UUID, chainKey, sender string) error {
	chain, ok := chains.LookupSendable(chainKey)
	if !ok {
		return shared.NewAppError("UNSUPPORTED_CHAIN", 400, "chain is not supported")
	}
	addr, err := chains.NormalizeAddress(chain.Namespace, sender)
	if err != nil {
		return payments.ErrInvalidSender
	}
	ws, err := s.Wallets.List(ctx, agentID)
	if err != nil {
		return err
	}
	for _, w := range ws {
		if w.Namespace == chain.Namespace && w.Address == addr {
			return nil
		}
	}
	return ErrSenderNotAgentWallet
}

func (s *service) GetTransfer(ctx context.Context, pr shared.Principal, paymentID uuid.UUID) (TransferView, error) {
	a, err := s.Repo.GetAuthorization(ctx, paymentID)
	if err != nil || a.AgentID != pr.AccountID {
		return TransferView{}, ErrTransferNotFound
	}
	p, _, err := s.Payments.Get(ctx, &pr.AccountID, paymentID)
	if err != nil {
		return TransferView{}, err
	}
	return s.view(p, a, pr.AccountID)
}

func (s *service) SignedAuthorization(ctx context.Context, pr shared.Principal, paymentID uuid.UUID) (SignedAuthorization, error) {
	v, err := s.GetTransfer(ctx, pr, paymentID)
	if err != nil {
		return SignedAuthorization{}, err
	}
	if v.Signed == nil {
		if s.Attestor == nil {
			return SignedAuthorization{}, ErrSigningUnavailable
		}
		return SignedAuthorization{}, ErrNotAuthorized
	}
	return *v.Signed, nil
}

func (s *service) PublicKey() (string, []byte, bool) {
	if s.Attestor == nil {
		return "", nil, false
	}
	return s.Attestor.KeyID(), s.Attestor.PublicKey(), true
}

// view attaches signing instructions and MAX's signed authorization, but only
// while the intent is approved, signable and unexpired.
func (s *service) view(p payments.Payment, a TransferAuthorization, agentID uuid.UUID) (TransferView, error) {
	v := TransferView{Payment: p, Authorization: a}
	approved := a.Decision == DecisionAutoApproved || a.Decision == DecisionApproved
	signable := p.Status == payments.StatusAwaitingSenderSignature || p.Status == payments.StatusQuoted
	if !approved || !signable || !s.now().Before(p.ExpiresAt) {
		return v, nil
	}
	ins := s.Payments.Instructions(p)
	v.Instructions = &ins
	if s.Attestor == nil {
		return v, nil
	}
	signed, err := s.Attestor.Sign(authorizationPayload(p, a, ins, s.now()))
	if err != nil {
		return TransferView{}, shared.ErrInternal.WithError(err)
	}
	v.Signed = &signed
	return v, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
