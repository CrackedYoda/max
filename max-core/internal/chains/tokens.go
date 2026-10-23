package chains

import "strings"

// Token is an entry in MAX's curated list. Tokens outside the list can still
// be sent, but are flagged as unverified because symbols and names are
// attacker-controlled.
type Token struct {
	Chain    string
	Address  string // canonical form: lowercase for EVM, base58 for Solana
	Symbol   string
	Decimals int
}

var verifiedTokens = []Token{
	{Chain: "ethereum", Address: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", Symbol: "USDC", Decimals: 6},
	{Chain: "ethereum", Address: "0xdac17f958d2ee523a2206206994597c13d831ec7", Symbol: "USDT", Decimals: 6},
	{Chain: "base", Address: "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", Symbol: "USDC", Decimals: 6},
	{Chain: "arbitrum", Address: "0xaf88d065e77c8cc2239327c5edb3a432268e5831", Symbol: "USDC", Decimals: 6},
	{Chain: "optimism", Address: "0x0b2c639c533813f4aa9d7837caf62653d097ff85", Symbol: "USDC", Decimals: 6},
	{Chain: "polygon", Address: "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359", Symbol: "USDC", Decimals: 6},
	{Chain: "base-sepolia", Address: "0x036cbd53842c5426634e7929541ec2318f3dcf7e", Symbol: "USDC", Decimals: 6},
	{Chain: "sepolia", Address: "0x1c7d4b196cb0c7b01d743fbc6116a902379c7238", Symbol: "USDC", Decimals: 6},
	{Chain: "solana", Address: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Symbol: "USDC", Decimals: 6},
	{Chain: "solana", Address: "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", Symbol: "USDT", Decimals: 6},
	{Chain: "solana", Address: "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263", Symbol: "BONK", Decimals: 5},
	{Chain: "solana", Address: "EKpQGSJtjMFqKZ9KQanSqYXRcF8fBopzLHYxdM65zcjm", Symbol: "WIF", Decimals: 6},
	{Chain: "solana-devnet", Address: "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU", Symbol: "USDC", Decimals: 6},
}

// LookupVerifiedToken matches by chain and contract/mint address only.
func LookupVerifiedToken(chainKey, canonicalAddr string) (Token, bool) {
	for _, t := range verifiedTokens {
		if t.Chain == chainKey && strings.EqualFold(t.Address, canonicalAddr) {
			return t, true
		}
	}
	return Token{}, false
}
