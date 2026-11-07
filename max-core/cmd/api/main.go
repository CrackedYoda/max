package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/max/max-core/internal/agents"
	"github.com/max/max-core/internal/auth"
	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/config"
	"github.com/max/max-core/internal/database"
	"github.com/max/max-core/internal/evmrpc"
	"github.com/max/max-core/internal/market"
	"github.com/max/max-core/internal/market/settle"
	"github.com/max/max-core/internal/maxids"
	"github.com/max/max-core/internal/payments"
	"github.com/max/max-core/internal/pricing"
	"github.com/max/max-core/internal/renames"
	"github.com/max/max-core/internal/resolver"
	"github.com/max/max-core/internal/routing/nearintents"
	"github.com/max/max-core/internal/server"
	"github.com/max/max-core/internal/shared"
	"github.com/max/max-core/internal/solanarpc"
	"github.com/max/max-core/internal/verifications"
	"github.com/max/max-core/internal/wallets"
	"github.com/max/max-core/internal/xapi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	log, err := shared.NewLogger(cfg.App.Env, cfg.App.LogLevel)
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer func() { _ = log.Sync() }()
	zap.ReplaceGlobals(log)

	pool, err := database.Connect(context.Background(), cfg.Database.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	authSvc := auth.NewService(auth.NewRepository(pool), auth.Config{
		Domain:        cfg.Auth.Domain,
		URI:           cfg.Auth.URI,
		ChallengeTTL:  cfg.Auth.ChallengeTTL,
		SessionTTL:    cfg.Auth.SessionTTL,
		SessionSecret: cfg.Auth.SessionSecret,
	})
	walletSvc := wallets.NewService(wallets.NewRepository(pool))
	maxidRepo := maxids.NewRepository(pool, maxids.WithHoldPeriod(cfg.Market.HoldPeriod))
	maxidSvc := maxids.NewService(maxidRepo, newRegistrationGate(cfg, maxidRepo))
	resolverSvc := resolver.NewService(resolver.NewRepository(pool))

	cc := cfg.Payments.CrossChain
	paymentsCfg := payments.DefaultConfig()
	paymentsCfg.IntentTTL = cfg.Payments.IntentTTL
	paymentsCfg.RoutingEnabled = cc.Enabled
	paymentsCfg.MaxCrossChainUSD = cc.MaxUSD
	paymentsCfg.DefaultSlippageBps, paymentsCfg.MaxSlippageBps = cc.DefaultSlippageBps, cc.MaxSlippageBps
	// The provider is always constructed so in-flight routes keep being
	// tracked even while new quotes are switched off.
	provider := nearintents.New(nearintents.Config{BaseURL: cc.BaseURL, APIKey: cc.APIKey, Referral: "max"})
	if cc.APIKey == "" {
		log.Warn("NEAR_INTENTS_API_KEY not set: 1Click charges a platform fee on unauthenticated quotes")
	}
	paymentRepo := payments.NewRepository(pool)
	paymentSvc := payments.NewService(paymentRepo, resolverSvc, payments.NewGateway(cfg.Payments.RPCURLs), provider, paymentsCfg, log)
	for _, c := range chains.Supported {
		if _, ok := cfg.Payments.RPCURLs[c.Key]; !ok {
			log.Warn("payments disabled for chain: no RPC URL", zap.String("chain", c.Key))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if cfg.Payments.WorkerEnabled {
		go payments.NewWorker(paymentSvc, paymentRepo, log.Named("payments")).Run(ctx)
	}

	renameFees, err := renames.ParseFees(cfg.Renames.Fees)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	treasury, err := renames.ParseTreasury(cfg.Renames.TreasuryEVM, cfg.Renames.TreasurySolana, renameFees)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if len(renameFees) == 0 {
		log.Warn("RENAME_FEES not set: renames are disabled")
	}
	renameSvc := renames.NewService(renames.NewRepository(pool), paymentSvc, renames.Config{
		Quarantine: cfg.Renames.Quarantine, HoldPeriod: cfg.Market.HoldPeriod, Fees: renameFees, Treasury: treasury,
		LateSubmitGrace: paymentsCfg.LateSubmitGrace,
	}, log.Named("renames"))
	go renames.RunWorker(ctx, renameSvc, log.Named("renames"))

	xc := xapi.New(xapi.Config{ClientID: cfg.X.ClientID, ClientSecret: cfg.X.ClientSecret, RedirectURI: cfg.X.RedirectURI})
	if !xc.Enabled() {
		log.Warn("X_CLIENT_ID not set: reserved names cannot be claimed via X verification")
	}
	vcfg := verifications.DefaultConfig()
	vcfg.ResultRedirect, vcfg.ClaimFreshness = cfg.X.ResultRedirect, cfg.X.ClaimFreshness
	vcfg.NativeResultRedirect = cfg.X.NativeResultRedirect
	vcfg.ReverifyInterval, vcfg.ReverifyGrace = cfg.X.ReverifyInterval, cfg.X.ReverifyGrace
	verifySvc := verifications.NewService(verifications.NewRepository(pool), xc, vcfg, log.Named("verifications"))
	go verifications.RunWorker(ctx, verifySvc, log.Named("verifications"))

	seed := cfg.Agents.AuthorizationSeed
	if seed == nil {
		seed = make([]byte, 32)
		if _, err := rand.Read(seed); err != nil {
			return err
		}
		log.Warn("AGENT_AUTHZ_SIGNING_SEED not set: using an ephemeral key; agent authorizations will not verify after restart")
	}
	attestor, err := agents.NewAttestor(seed)
	if err != nil {
		return err
	}
	agentSvc := agents.NewService(agents.Deps{
		Repo: agents.NewRepository(pool), Payments: paymentSvc, Wallets: walletSvc, IDs: maxidSvc,
		WalletProofs: authSvc, Attestor: attestor, CredentialSecret: cfg.Agents.CredentialSecret, Log: log.Named("agents"),
	})
	log.Info("agent authorization key", zap.String("key_id", attestor.KeyID()))

	marketSvc, err := newMarket(cfg, pool, log.Named("market"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if marketSvc.Config().Enabled {
		go market.RunWorker(ctx, marketSvc, 15*time.Second, log.Named("market"))
	}

	router := server.NewRouter(server.Deps{
		Logger:         log,
		Pool:           pool,
		Production:     cfg.IsProduction(),
		CORSOrigins:    cfg.HTTP.CORSOrigins,
		AuthService:    authSvc,
		CredentialAuth: agentSvc,
		Agents:         agents.NewHandler(agentSvc),
		Renames:        renames.NewHandler(renameSvc),
		Verifications:  verifications.NewHandler(verifySvc),
		IsAdmin:        adminSet(cfg.AdminAccountIDs),
		Auth:           auth.NewHandler(authSvc, auth.CookieConfig{Secure: cfg.Auth.CookieSecure, TTL: cfg.Auth.SessionTTL}),
		Wallets:        wallets.NewHandler(walletSvc),
		MaxIDs:         maxids.NewHandler(maxidSvc),
		Resolver:       resolver.NewHandler(resolverSvc),
		Payments:       payments.NewHandler(paymentSvc),
		Market:         market.NewHandler(marketSvc),
	})

	return server.Run(cfg.HTTP.Addr, router, log)
}

func adminSet(ids []uuid.UUID) func(uuid.UUID) bool {
	set := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return func(id uuid.UUID) bool { return set[id] }
}

// newMarket validates the market settings (startup fails when an enabled
// market lacks a treasury or RPC for an asset) and builds the service with a
// Solana settlement adapter over the configured RPC URLs.
func newMarket(cfg config.Config, pool *pgxpool.Pool, log *zap.Logger) (*market.Service, error) {
	mcfg, err := market.NewConfig(cfg.Market, cfg.Payments.RPCURLs)
	if err != nil {
		return nil, err
	}
	sol := map[string]settle.SolanaRPC{}
	for _, a := range mcfg.Assets {
		if url, ok := cfg.Payments.RPCURLs[a.Chain.Key]; ok && a.Chain.Namespace == chains.NamespaceSolana {
			sol[a.Chain.Key] = solanarpc.New(url)
		}
	}
	adapters := map[chains.Namespace]settle.Adapter{chains.NamespaceSolana: settle.NewSolana(sol)}
	if !mcfg.Enabled {
		log.Info("MARKET_ENABLED=false: marketplace routes return 404")
	}
	policy := market.NewAgentPolicy(agents.NewRepository(pool), market.NewAgentStore(pool))
	return market.NewService(market.NewRepository(pool), adapters, mcfg, policy, log), nil
}

// newRegistrationGate builds the MAX ID claim balance gate. It counts wallets
// on every chain that has an RPC URL, skipping testnets in production. With a
// zero minimum it never touches wallets, RPC or the price feed.
func newRegistrationGate(cfg config.Config, wallets maxids.WalletLister) maxids.BalanceGate {
	feed := pricing.NewCached(
		pricing.NewCoinGecko(cfg.Pricing.URL, cfg.Pricing.APIKey, &http.Client{Timeout: 10 * time.Second}),
		cfg.Pricing.TTL, cfg.Pricing.MaxAge, time.Now)

	var gateChains []chains.Chain
	evm := map[string]settle.EVMRPC{}
	sol := map[string]settle.SolanaRPC{}
	for _, c := range chains.Supported {
		url, ok := cfg.Payments.RPCURLs[c.Key]
		if !ok || (c.Testnet && cfg.IsProduction()) {
			continue
		}
		switch c.Namespace {
		case chains.NamespaceEVM:
			evm[c.Key] = evmrpc.New(url)
		case chains.NamespaceSolana:
			sol[c.Key] = solanarpc.New(url)
		default:
			continue
		}
		gateChains = append(gateChains, c)
	}
	readers := map[chains.Namespace]maxids.BalanceReader{
		chains.NamespaceEVM:    settle.NewEVMBalances(evm),
		chains.NamespaceSolana: settle.NewSolana(sol),
	}
	return maxids.NewBalanceGate(cfg.Registration.MinBalanceUSD, wallets, readers, feed, gateChains)
}
