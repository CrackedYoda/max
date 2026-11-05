package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/max/max-core/internal/chains"
)

type Config struct {
	App          AppConfig
	HTTP         HTTPConfig
	Database     DatabaseConfig
	Auth         AuthConfig
	Payments     PaymentsConfig
	Agents       AgentsConfig
	Renames      RenamesConfig
	X            XConfig
	Market       MarketConfig
	Registration RegistrationConfig
	Pricing      PricingConfig
	// AdminAccountIDs may review reserved-name claims and revoke verifications.
	AdminAccountIDs []uuid.UUID
}

// MarketConfig holds the name marketplace settings. Amounts are decimal
// strings (e.g. "1" USDC) converted to base units against the asset's decimals.
type MarketConfig struct {
	Enabled       bool
	TradingPaused bool
	// Assets is a "chain:asset,..." list of settlement assets (verified tokens only).
	Assets          string
	FeeBPS          int
	HoldPeriod      time.Duration
	BuyLock         time.Duration
	OfferPayWindow  time.Duration
	ReviewThreshold string // empty = no admin review
	MinOffer        string
	TreasuryEVM     string
	TreasurySolana  string
}

// RegistrationConfig gates new MAX ID claims on wallet balance.
type RegistrationConfig struct {
	// MinBalanceUSD of 0 disables the gate and the price feed is never called.
	MinBalanceUSD float64
}

type PricingConfig struct {
	URL    string
	TTL    time.Duration
	MaxAge time.Duration
}

type XConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURI is MAX's callback (…/v1/verifications/x/callback), registered with X.
	RedirectURI string
	// ResultRedirect is the frontend page the callback sends the browser to.
	ResultRedirect string
	// NativeResultRedirect is the mobile app's deep link for X verification.
	NativeResultRedirect string
	ClaimFreshness       time.Duration
	ReverifyInterval     time.Duration
	ReverifyGrace        time.Duration
}

// RenamesConfig holds administrator-configured rename pricing. Values are
// parsed and validated against chain metadata at startup in main.
type RenamesConfig struct {
	Fees           string // "chain:asset:amount,..."; empty disables renames
	TreasuryEVM    string
	TreasurySolana string
	Quarantine     time.Duration
}

type AgentsConfig struct {
	// CredentialSecret keys the HMAC over agent API key secrets.
	CredentialSecret string
	// AuthorizationSeed is the 32-byte ed25519 seed MAX signs transfer
	// authorizations with. Empty in development means an ephemeral key.
	AuthorizationSeed []byte
}

type AppConfig struct {
	Env      string
	LogLevel string
}

type HTTPConfig struct {
	Addr        string
	CORSOrigins []string
}

type DatabaseConfig struct {
	URL string
}

type PaymentsConfig struct {
	IntentTTL     time.Duration
	WorkerEnabled bool
	CrossChain    CrossChainConfig
	// RPCURLs maps chain key → JSON-RPC endpoint, from RPC_URL_<CHAIN_KEY>.
	RPCURLs map[string]string
}

type CrossChainConfig struct {
	// Enabled is the emergency switch for new cross-chain quotes. Routes already
	// in flight keep being monitored when it is off.
	Enabled            bool
	BaseURL            string
	APIKey             string
	MaxUSD             float64
	DefaultSlippageBps int
	MaxSlippageBps     int
}

type AuthConfig struct {
	Domain        string
	URI           string
	ChallengeTTL  time.Duration
	SessionTTL    time.Duration
	SessionSecret string
	CookieSecure  bool
}

func (c Config) IsProduction() bool { return c.App.Env == "production" }

// Load reads the environment (seeded from .env when present) and refuses to
// return a config that would boot an unsafe server.
func Load() (Config, error) {
	_ = godotenv.Load()

	var errs []error
	get := func(key, def string) string {
		if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	duration := func(key, def string) time.Duration {
		d, err := time.ParseDuration(get(key, def))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
		return d
	}
	integer := func(key, def string) int {
		n, err := strconv.Atoi(get(key, def))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
		return n
	}
	number := func(key, def string) float64 {
		n, err := strconv.ParseFloat(get(key, def), 64)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
		return n
	}
	boolean := func(key, def string) bool {
		b, err := strconv.ParseBool(get(key, def))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
		return b
	}

	cfg := Config{
		App: AppConfig{
			Env:      get("APP_ENV", "development"),
			LogLevel: get("LOG_LEVEL", "info"),
		},
		HTTP: HTTPConfig{
			Addr:        get("HTTP_ADDR", ":8080"),
			CORSOrigins: splitList(get("CORS_ORIGINS", "")),
		},
		Database: DatabaseConfig{URL: get("DATABASE_URL", "")},
		Auth: AuthConfig{
			Domain:        get("AUTH_DOMAIN", ""),
			URI:           get("AUTH_URI", ""),
			ChallengeTTL:  duration("AUTH_CHALLENGE_TTL", "5m"),
			SessionTTL:    duration("SESSION_TTL", "168h"),
			SessionSecret: get("SESSION_SECRET", ""),
			CookieSecure:  boolean("SESSION_COOKIE_SECURE", "true"),
		},
		Payments: PaymentsConfig{
			IntentTTL:     duration("PAYMENT_INTENT_TTL", "15m"),
			WorkerEnabled: boolean("PAYMENTS_WORKER_ENABLED", "true"),
			RPCURLs:       map[string]string{},
			CrossChain: CrossChainConfig{
				Enabled:            boolean("CROSS_CHAIN_ENABLED", "true"),
				BaseURL:            get("NEAR_INTENTS_BASE_URL", "https://1click.chaindefuser.com"),
				APIKey:             get("NEAR_INTENTS_API_KEY", ""),
				MaxUSD:             number("CROSS_CHAIN_MAX_USD", "1000"),
				DefaultSlippageBps: integer("CROSS_CHAIN_DEFAULT_SLIPPAGE_BPS", "100"),
				MaxSlippageBps:     integer("CROSS_CHAIN_MAX_SLIPPAGE_BPS", "300"),
			},
		},
	}
	cfg.Renames = RenamesConfig{
		Fees:           get("RENAME_FEES", ""),
		TreasuryEVM:    get("RENAME_TREASURY_EVM", ""),
		TreasurySolana: get("RENAME_TREASURY_SOLANA", ""),
		Quarantine:     duration("RENAME_QUARANTINE", "720h"),
	}
	if cfg.Renames.Quarantine < 24*time.Hour {
		errs = append(errs, errors.New("RENAME_QUARANTINE must be at least 24h"))
	}

	cfg.Market = MarketConfig{
		Enabled:         boolean("MARKET_ENABLED", "false"),
		TradingPaused:   boolean("MARKET_TRADING_PAUSED", "false"),
		Assets:          get("MARKET_ASSETS", ""),
		FeeBPS:          integer("MARKET_FEE_BPS", "1000"),
		HoldPeriod:      duration("MARKET_HOLD_PERIOD", "72h"),
		BuyLock:         duration("MARKET_BUY_LOCK", "15m"),
		OfferPayWindow:  duration("MARKET_OFFER_PAY_WINDOW", "24h"),
		ReviewThreshold: get("MARKET_REVIEW_THRESHOLD", ""),
		MinOffer:        get("MARKET_MIN_OFFER", "1"),
		TreasuryEVM:     get("TREASURY_EVM", cfg.Renames.TreasuryEVM),
		TreasurySolana:  get("TREASURY_SOLANA", cfg.Renames.TreasurySolana),
	}
	if cfg.Market.FeeBPS < 0 || cfg.Market.FeeBPS > 5000 {
		errs = append(errs, errors.New("MARKET_FEE_BPS must be between 0 and 5000"))
	}
	cfg.Registration = RegistrationConfig{MinBalanceUSD: number("REGISTRATION_MIN_BALANCE_USD", "100")}
	if cfg.Registration.MinBalanceUSD < 0 {
		errs = append(errs, errors.New("REGISTRATION_MIN_BALANCE_USD must not be negative"))
	}
	cfg.Pricing = PricingConfig{
		URL:    get("PRICE_FEED_URL", "https://api.coingecko.com/api/v3/simple/price"),
		TTL:    duration("PRICE_FEED_TTL", "60s"),
		MaxAge: duration("PRICE_FEED_MAX_AGE", "5m"),
	}

	cfg.X = XConfig{
		ClientID:             get("X_CLIENT_ID", ""),
		ClientSecret:         get("X_CLIENT_SECRET", ""),
		RedirectURI:          get("X_REDIRECT_URI", ""),
		ResultRedirect:       get("X_RESULT_REDIRECT", ""),
		NativeResultRedirect: get("X_NATIVE_RESULT_REDIRECT", "max://verify/x"),
		ClaimFreshness:       duration("X_CLAIM_FRESHNESS", "15m"),
		ReverifyInterval:     duration("X_REVERIFY_INTERVAL", "4320h"),
		ReverifyGrace:        duration("X_REVERIFY_GRACE", "336h"),
	}
	if cfg.X.ClientID != "" && (cfg.X.RedirectURI == "" || cfg.X.ResultRedirect == "") {
		errs = append(errs, errors.New("X_REDIRECT_URI and X_RESULT_REDIRECT are required when X_CLIENT_ID is set"))
	}
	if cfg.X.ClaimFreshness <= 0 || cfg.X.ClaimFreshness > time.Hour {
		errs = append(errs, errors.New("X_CLAIM_FRESHNESS must be between 0 and 1h"))
	}
	for _, raw := range splitList(get("ADMIN_ACCOUNT_IDS", "")) {
		id, err := uuid.Parse(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("ADMIN_ACCOUNT_IDS: %q is not a UUID", raw))
			continue
		}
		cfg.AdminAccountIDs = append(cfg.AdminAccountIDs, id)
	}

	cfg.Agents.CredentialSecret = get("AGENT_CREDENTIAL_SECRET", "")
	if len(cfg.Agents.CredentialSecret) < 32 {
		errs = append(errs, errors.New("AGENT_CREDENTIAL_SECRET must be at least 32 characters"))
	}
	if cfg.Agents.CredentialSecret == cfg.Auth.SessionSecret {
		errs = append(errs, errors.New("AGENT_CREDENTIAL_SECRET must differ from SESSION_SECRET"))
	}
	if raw := get("AGENT_AUTHZ_SIGNING_SEED", ""); raw != "" {
		seed, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(seed) != 32 {
			errs = append(errs, errors.New("AGENT_AUTHZ_SIGNING_SEED must be base64 of exactly 32 bytes"))
		}
		cfg.Agents.AuthorizationSeed = seed
	} else if cfg.App.Env == "production" {
		errs = append(errs, errors.New("AGENT_AUTHZ_SIGNING_SEED is required in production"))
	}

	for _, c := range chains.Supported {
		key := "RPC_URL_" + strings.ToUpper(strings.ReplaceAll(c.Key, "-", "_"))
		if v := get(key, ""); v != "" {
			if !strings.HasPrefix(v, "https://") && !(cfg.App.Env == "development" && strings.HasPrefix(v, "http://")) {
				errs = append(errs, fmt.Errorf("%s must be an https URL", key))
			}
			cfg.Payments.RPCURLs[c.Key] = v
		}
	}
	if cc := cfg.Payments.CrossChain; cc.DefaultSlippageBps < 1 || cc.MaxSlippageBps > 1000 || cc.DefaultSlippageBps > cc.MaxSlippageBps {
		errs = append(errs, errors.New("cross-chain slippage must satisfy 1 <= default <= max <= 1000 bps"))
	}
	if !strings.HasPrefix(cfg.Payments.CrossChain.BaseURL, "https://") {
		errs = append(errs, errors.New("NEAR_INTENTS_BASE_URL must be https"))
	}
	if cfg.Payments.IntentTTL < time.Minute || cfg.Payments.IntentTTL > time.Hour {
		errs = append(errs, errors.New("PAYMENT_INTENT_TTL must be between 1m and 1h"))
	}

	if cfg.App.Env != "development" && cfg.App.Env != "production" {
		errs = append(errs, fmt.Errorf("APP_ENV must be development or production, got %q", cfg.App.Env))
	}
	if cfg.Database.URL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if cfg.Auth.Domain == "" || cfg.Auth.URI == "" {
		errs = append(errs, errors.New("AUTH_DOMAIN and AUTH_URI are required"))
	}
	if len(cfg.Auth.SessionSecret) < 32 {
		errs = append(errs, errors.New("SESSION_SECRET must be at least 32 characters"))
	}
	if cfg.Auth.ChallengeTTL <= 0 || cfg.Auth.ChallengeTTL > 15*time.Minute {
		errs = append(errs, errors.New("AUTH_CHALLENGE_TTL must be between 0 and 15m"))
	}
	if cfg.IsProduction() {
		if !cfg.Auth.CookieSecure {
			errs = append(errs, errors.New("SESSION_COOKIE_SECURE must be true in production"))
		}
		for _, o := range cfg.HTTP.CORSOrigins {
			if strings.Contains(o, "*") {
				errs = append(errs, errors.New("wildcard CORS origins are not allowed in production"))
			}
		}
	}

	return cfg, errors.Join(errs...)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
