package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/agents"
	"github.com/max/max-core/internal/auth"
	"github.com/max/max-core/internal/market"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/middleware"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/renames"
	"github.com/max/max-core/internal/resolver"
	"github.com/max/max-core/internal/shared"
	"github.com/max/max-core/internal/verifications"
	"github.com/max/max-core/internal/wallets"
)

type Deps struct {
	Logger      *zap.Logger
	Pool        *pgxpool.Pool
	Production  bool
	CORSOrigins []string

	AuthService    auth.Service
	CredentialAuth middleware.CredentialAuthenticator
	Auth           *auth.Handler
	Agents         *agents.Handler
	Renames        *renames.Handler
	Verifications  *verifications.Handler
	IsAdmin        func(uuid.UUID) bool
	Wallets        *wallets.Handler
	MaxIDs         *maxids.Handler
	Resolver       *resolver.Handler
	Payments       *payments.Handler
	Market         *market.Handler
}

func NewRouter(d Deps) *gin.Engine {
	if d.Production {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.HandleMethodNotAllowed = true
	_ = r.SetTrustedProxies(nil)

	r.Use(
		middleware.RequestID(),
		middleware.RequestLogger(d.Logger),
		middleware.Recovery(),
		middleware.SecurityHeaders(d.Production),
		middleware.CORS(d.CORSOrigins),
	)

	r.NoRoute(func(c *gin.Context) { shared.Fail(c, shared.ErrNotFound) })
	r.NoMethod(func(c *gin.Context) {
		shared.Fail(c, shared.NewAppError("METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed, "method not allowed"))
	})

	r.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := d.Pool.Ping(ctx); err != nil {
			shared.Fail(c, shared.NewAppError("UNHEALTHY", http.StatusServiceUnavailable, "database unavailable").WithError(err))
			return
		}
		shared.OK(c, gin.H{"status": "ok"})
	})

	authn := middleware.Auth{Sessions: d.AuthService, Credentials: d.CredentialAuth}
	optional, required := authn.Optional(), authn.Required()
	sessionOnly, agentOnly := middleware.SessionOnly(), middleware.AgentOnly()
	scope := func(s agents.Scope) gin.HandlerFunc { return middleware.Scope(string(s)) }

	authLimit := middleware.RateLimit(30, time.Minute)
	lookupLimit := middleware.RateLimit(300, time.Minute)
	payLimit := middleware.RateLimit(60, time.Minute)

	v1 := r.Group("/v1")

	// Public
	v1.GET("/chains", d.Resolver.Chains)
	v1.GET("/resolve/:maxId", lookupLimit, d.Resolver.Resolve)
	v1.GET("/profiles/:maxId", lookupLimit, d.Resolver.Profile)
	v1.GET("/ids/availability", lookupLimit, d.MaxIDs.Availability)
	v1.GET("/agent-authorizations/keys", d.Agents.PublicKeys)

	// Wallet proofs: login is anonymous, link requires a session or a wallets:link key.
	v1.POST("/wallets/challenge", authLimit, optional, scope(agents.ScopeWalletsLink), d.Auth.Challenge)
	v1.POST("/wallets/verify", authLimit, optional, scope(agents.ScopeWalletsLink), d.Auth.Verify)

	// Transfers: signed-in senders get ownership checks; anonymous senders
	// address their intent by its unguessable id. Agents cannot use prepare;
	// they propose through the policy engine under /v1/agent.
	v1.POST("/transfers/prepare", payLimit, optional, d.Payments.Prepare)
	v1.POST("/transfers/:id/submit", payLimit, optional, scope(agents.ScopePaymentsSubmit), d.Payments.Submit)
	v1.GET("/transfers/:id", lookupLimit, optional, scope(agents.ScopePaymentsRead), d.Payments.Get)

	// Authenticated: humans (session) or agents (scoped key) acting on their own account.
	me := v1.Group("", required)
	me.GET("/auth/me", sessionOnly, d.Auth.Me)
	me.POST("/auth/logout", sessionOnly, d.Auth.Logout)

	me.GET("/wallets", scope(agents.ScopeWalletsLink), d.Wallets.List)
	me.DELETE("/wallets/:id", sessionOnly, d.Wallets.Remove)

	ids := me.Group("/ids", scope(agents.ScopeIDsWrite))
	ids.POST("", d.MaxIDs.Claim)
	ids.GET("", d.MaxIDs.List)
	ids.GET("/:id", d.MaxIDs.Get)
	ids.PATCH("/:id", d.MaxIDs.UpdateProfile)
	ids.PUT("/:id/destinations/:namespace", d.MaxIDs.SetDestination)
	ids.DELETE("/:id/destinations/:namespace", d.MaxIDs.RemoveDestination)
	ids.PUT("/:id/settlement", d.MaxIDs.SetSettlement)
	ids.DELETE("/:id/settlement", d.MaxIDs.RemoveSettlement)
	ids.GET("/:id/history", d.Renames.History)
	// Renames move money, so they need a signed-in human (agent keys would
	// bypass agent spend policy).
	ids.GET("/:id/rename", sessionOnly, d.Renames.Eligibility)
	ids.POST("/:id/rename", sessionOnly, d.Renames.Request)
	ids.GET("/:id/rename/:requestId", sessionOnly, d.Renames.Get)

	me.GET("/transfers", scope(agents.ScopePaymentsRead), d.Payments.List)

	// Agent administration: signed-in humans only.
	admin := v1.Group("", required, sessionOnly)
	admin.POST("/agents", d.Agents.Create)
	admin.GET("/agents", d.Agents.List)
	admin.GET("/agents/:agentId", d.Agents.Get)
	admin.PATCH("/agents/:agentId", d.Agents.Update)
	admin.PUT("/agents/:agentId/policy", d.Agents.SetPolicy)
	admin.POST("/agents/:agentId/credentials", d.Agents.IssueCredential)
	admin.POST("/agents/:agentId/credentials/:credentialId/rotate", d.Agents.RotateCredential)
	admin.DELETE("/agents/:agentId/credentials/:credentialId", d.Agents.RevokeCredential)
	admin.POST("/agents/:agentId/ids", d.Agents.ClaimID)
	admin.POST("/agents/:agentId/wallets/challenge", authLimit, d.Agents.WalletChallenge)
	admin.POST("/agents/:agentId/wallets/verify", authLimit, d.Agents.WalletVerify)
	admin.GET("/agents/:agentId/transfers", d.Agents.ListTransfers)
	admin.POST("/agent-approvals/:paymentId/approve", d.Agents.Approve)
	admin.POST("/agent-approvals/:paymentId/deny", d.Agents.Deny)

	// X verification and reserved-name claims (signed-in humans).
	v1.POST("/verifications/x/start", authLimit, required, sessionOnly, d.Verifications.StartX)
	v1.GET("/verifications/x/callback", authLimit, optional, d.Verifications.CallbackX)
	v1.POST("/verifications/x/complete", authLimit, required, sessionOnly, d.Verifications.CompleteX)
	admin.GET("/verifications", d.Verifications.ListVerifications)
	admin.DELETE("/verifications/:id", d.Verifications.RevokeVerification)
	admin.POST("/reserved-claims", d.Verifications.Claim)
	admin.GET("/reserved-claims", d.Verifications.ListClaims)

	// Platform administration (ADMIN_ACCOUNT_IDS).
	ops := v1.Group("/admin", required, sessionOnly, middleware.Admin(d.IsAdmin))
	ops.GET("/reserved-claims", d.Verifications.AdminListClaims)
	ops.POST("/reserved-claims/:id/approve", d.Verifications.AdminApprove)
	ops.POST("/reserved-claims/:id/deny", d.Verifications.AdminDeny)
	ops.POST("/reserved-claims/:id/revoke", d.Verifications.AdminRevokeClaim)
	ops.POST("/verifications/:id/revoke", d.Verifications.AdminRevokeVerification)

	// Marketplace admin (spec §9 Admin); 404 while the market is disabled.
	mops := ops.Group("/market", d.Market.Gate())
	mops.GET("/listings", d.Market.AdminListings)
	mops.GET("/offers", d.Market.AdminOffers)
	mops.GET("/sales", d.Market.AdminSales)
	mops.POST("/listings/:id/delist", d.Market.AdminDelist)
	mops.POST("/sales/:id/approve", d.Market.AdminApproveSale)
	mops.POST("/sales/:id/deny", d.Market.AdminDenySale)
	mops.POST("/sales/:id/cancel", d.Market.AdminCancelSale)
	mops.POST("/sales/:id/reverse", d.Market.AdminReverseSale)
	mops.GET("/restrictions", d.Market.AdminRestrictions)
	mops.POST("/restrictions", d.Market.AdminRestrict)
	mops.DELETE("/restrictions/:id", d.Market.AdminLiftRestriction)

	// Marketplace (spec §9). Every route 404s while the market is disabled;
	// Trading() refuses new activity while paused, but reads and submit stay
	// open so in-flight sales can finish.
	listLimit := middleware.RateLimit(30, time.Minute)
	mkt := v1.Group("/market", d.Market.Gate())
	mkt.GET("/listings", lookupLimit, d.Market.Search)
	mkt.GET("/names/:name", lookupLimit, d.Market.Name)
	// Session or agent key (spec §8, §9): agents act under their scopes and
	// the agent market policy.
	mine := mkt.Group("", required)
	trade := d.Market.Trading()
	read, sell, buy := scope(agents.ScopeMarketRead), scope(agents.ScopeMarketSell), scope(agents.ScopeMarketBuy)
	mine.GET("/eligibility/:maxId", read, d.Market.Eligibility)
	mine.POST("/listings", sell, listLimit, trade, d.Market.CreateListing)
	mine.PATCH("/listings/:id", sell, listLimit, trade, d.Market.UpdateListing)
	mine.DELETE("/listings/:id", sell, listLimit, trade, d.Market.DeleteListing)
	mine.POST("/listings/:id/buy", buy, payLimit, trade, d.Market.Buy)
	mine.POST("/offers", buy, listLimit, trade, d.Market.MakeOffer)
	mine.DELETE("/offers/:id", buy, listLimit, trade, d.Market.WithdrawOffer)
	mine.POST("/offers/:id/accept", sell, listLimit, trade, d.Market.AcceptOffer)
	mine.POST("/offers/:id/decline", sell, listLimit, trade, d.Market.DeclineOffer)
	// Paying an accepted offer, like submit, stays open while paused.
	mine.POST("/offers/:id/pay", buy, payLimit, d.Market.PayOffer)
	mine.POST("/sales/:id/submit", buy, payLimit, d.Market.Submit)
	mine.GET("/sales/:id", read, d.Market.GetSale)
	mine.GET("/me", read, d.Market.Me)

	// Held agent market actions: the agent's administrator (session) decides.
	admin.GET("/agents/:agentId/market-authorizations", d.Market.Gate(), d.Market.ListAgentAuthorizations)
	approvals := admin.Group("/agent-approvals/market", d.Market.Gate())
	approvals.POST("/:authorizationId/approve", d.Market.ApproveAgentAction)
	approvals.POST("/:authorizationId/deny", d.Market.DenyAgentAction)

	// Agent runtime: API keys only.
	agent := v1.Group("/agent", required, agentOnly)
	agent.GET("/me", d.Agents.Me)
	agent.POST("/transfers", payLimit, scope(agents.ScopePaymentsPropose), d.Agents.Propose)
	agent.GET("/transfers/:id", scope(agents.ScopePaymentsRead), d.Agents.GetTransfer)
	agent.GET("/transfers/:id/authorization", scope(agents.ScopeAuthorizationsRead), d.Agents.Authorization)

	return r
}
