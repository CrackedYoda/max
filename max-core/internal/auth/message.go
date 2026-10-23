package auth

import (
	"fmt"
	"strings"
	"time"

	"github.com/max/max-core/internal/chains"
)

const (
	statementLogin = "Sign in to MAX. This proves you control this wallet. It does not authorize any payment or transaction."
	statementLink  = "Link this wallet to your MAX account so it can receive payments sent to your MAX IDs. This does not authorize any payment or transaction."
)

type messageParams struct {
	Domain    string
	URI       string
	Namespace chains.Namespace
	Address   string
	Purpose   Purpose
	Nonce     string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// buildMessage renders an EIP-4361 (SIWE) message for EVM wallets and the
// equivalent Sign-In-With-Solana layout for Solana. The server stores the exact
// text, so verification never has to parse a client-supplied message.
func buildMessage(p messageParams) string {
	statement := statementLogin
	if p.Purpose == PurposeLink {
		statement = statementLink
	}

	var b strings.Builder
	switch p.Namespace {
	case chains.NamespaceEVM:
		fmt.Fprintf(&b, "%s wants you to sign in with your Ethereum account:\n", p.Domain)
		b.WriteString(chains.ChecksumEVMAddress(p.Address))
	default:
		fmt.Fprintf(&b, "%s wants you to sign in with your Solana account:\n", p.Domain)
		b.WriteString(p.Address)
	}
	fmt.Fprintf(&b, "\n\n%s\n\n", statement)
	fmt.Fprintf(&b, "URI: %s\n", p.URI)
	b.WriteString("Version: 1\n")
	if p.Namespace == chains.NamespaceEVM {
		// Ownership proofs are chain-agnostic across EVM; mainnet id is used as the label.
		b.WriteString("Chain ID: 1\n")
	}
	fmt.Fprintf(&b, "Nonce: %s\n", p.Nonce)
	fmt.Fprintf(&b, "Issued At: %s\n", p.IssuedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Expiration Time: %s", p.ExpiresAt.UTC().Format(time.RFC3339))
	return b.String()
}
