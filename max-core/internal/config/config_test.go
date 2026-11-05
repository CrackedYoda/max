package config

import (
	"strings"
	"testing"
	"time"
)

// baseEnv sets the variables Load requires and clears every market-related
// variable so tests are deterministic regardless of a local .env file.
func baseEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"MARKET_ENABLED", "MARKET_TRADING_PAUSED", "MARKET_ASSETS", "MARKET_FEE_BPS",
		"MARKET_HOLD_PERIOD", "MARKET_BUY_LOCK", "MARKET_OFFER_PAY_WINDOW",
		"MARKET_REVIEW_THRESHOLD", "MARKET_MIN_OFFER", "TREASURY_EVM", "TREASURY_SOLANA",
		"RENAME_TREASURY_EVM", "RENAME_TREASURY_SOLANA", "REGISTRATION_MIN_BALANCE_USD",
		"PRICE_FEED_URL", "PRICE_FEED_TTL", "PRICE_FEED_MAX_AGE",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("DATABASE_URL", "postgres://x:x@localhost:5433/x")
	t.Setenv("AUTH_DOMAIN", "localhost")
	t.Setenv("AUTH_URI", "http://localhost")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 40))
	t.Setenv("AGENT_CREDENTIAL_SECRET", strings.Repeat("a", 40))
}

func TestLoad_MarketDefaults(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.Market
	if m.Enabled || m.TradingPaused {
		t.Fatalf("market must default to disabled: %+v", m)
	}
	if m.FeeBPS != 1000 || m.HoldPeriod != 72*time.Hour || m.BuyLock != 15*time.Minute ||
		m.OfferPayWindow != 24*time.Hour || m.MinOffer != "1" {
		t.Fatalf("unexpected market defaults: %+v", m)
	}
	if cfg.Registration.MinBalanceUSD != 100 {
		t.Fatalf("MinBalanceUSD = %v", cfg.Registration.MinBalanceUSD)
	}
	if cfg.Pricing.TTL != 60*time.Second || cfg.Pricing.MaxAge != 5*time.Minute {
		t.Fatalf("pricing defaults: %+v", cfg.Pricing)
	}
}

func TestLoad_TreasuryFallback(t *testing.T) {
	baseEnv(t)
	t.Setenv("RENAME_TREASURY_SOLANA", "X")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Market.TreasurySolana != "X" {
		t.Fatalf("TreasurySolana = %q", cfg.Market.TreasurySolana)
	}
	t.Setenv("TREASURY_SOLANA", "Y")
	cfg, _ = Load()
	if cfg.Market.TreasurySolana != "Y" {
		t.Fatalf("explicit TREASURY_SOLANA must win, got %q", cfg.Market.TreasurySolana)
	}
}

func TestLoad_FeeBPSRange(t *testing.T) {
	for _, v := range []string{"-1", "5001"} {
		baseEnv(t)
		t.Setenv("MARKET_FEE_BPS", v)
		if _, err := Load(); err == nil {
			t.Fatalf("MARKET_FEE_BPS=%s must fail", v)
		}
	}
	for _, v := range []string{"0", "5000"} {
		baseEnv(t)
		t.Setenv("MARKET_FEE_BPS", v)
		if _, err := Load(); err != nil {
			t.Fatalf("MARKET_FEE_BPS=%s: %v", v, err)
		}
	}
}
