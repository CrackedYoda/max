package chains

import (
	"crypto/ed25519"
	"errors"
	"strings"

	"github.com/mr-tron/base58"
)

// NormalizeSolanaAddress validates a base58 ed25519 public key. Solana
// addresses are case-sensitive, so the input is returned unchanged.
func NormalizeSolanaAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	raw, err := base58.Decode(addr)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return "", errors.New("solana address must be a base58-encoded 32-byte public key")
	}
	return addr, nil
}

// VerifySolanaSignature checks a base58-encoded ed25519 signature over the
// exact UTF-8 message bytes (as produced by Phantom signMessage).
func VerifySolanaSignature(addr, message, sigB58 string) error {
	pub, err := base58.Decode(addr)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid solana address")
	}
	sig, err := base58.Decode(strings.TrimSpace(sigB58))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("signature must be a base58-encoded 64-byte ed25519 signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(message), sig) {
		return errors.New("signature does not match address")
	}
	return nil
}
