//go:build live

package payments

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

// Run with: go test -tags live -run Live ./internal/payments/
func TestLive_Gateway(t *testing.T) {
	ctx := context.Background()
	g := NewGateway(map[string]string{
		"base-sepolia":  "https://sepolia.base.org",
		"solana-devnet": "https://api.devnet.solana.com",
	})

	usdc, err := g.TokenInfo(ctx, mustChain("base-sepolia"), "0x036cbd53842c5426634e7929541ec2318f3dcf7e")
	if err != nil || usdc.Decimals != 6 {
		t.Fatalf("base-sepolia USDC: %+v %v", usdc, err)
	}
	t.Logf("base-sepolia USDC decimals=%d (chain id verified)", usdc.Decimals)

	mint, err := g.TokenInfo(ctx, mustChain("solana-devnet"), "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
	if err != nil || mint.Decimals != 6 || mint.TokenProgram != chains.SPLTokenProgram {
		t.Fatalf("devnet USDC mint: %+v %v", mint, err)
	}
	t.Logf("devnet USDC decimals=%d program=%s", mint.Decimals, mint.TokenProgram)

	unknown := Payment{
		ID: uuid.New(), Chain: mustChain("base-sepolia"), TxHash: "0x" + strings.Repeat("0", 63) + "1",
		Asset: Asset{Kind: AssetNative}, Amount: big.NewInt(1), CreatedAt: time.Now(),
	}
	obs, err := g.Observe(ctx, unknown)
	if err != nil || obs.Found {
		t.Fatalf("unknown evm tx: %+v %v", obs, err)
	}
	unknown.Chain, unknown.TxHash = mustChain("solana-devnet"), solSig
	obs, err = g.Observe(ctx, unknown)
	if err != nil || obs.Found {
		t.Fatalf("unknown solana tx: %+v %v", obs, err)
	}
	t.Log("unknown transactions report not-found on both chains")
}
