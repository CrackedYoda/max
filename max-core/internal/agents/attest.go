package agents

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// AuthorizationDomain prefixes every signed message so a MAX authorization
// can never be replayed as a signature over anything else.
const AuthorizationDomain = "max-agent-authorization/v1\n"

// Attestor signs approved transfer intents. An agent's signer must verify
// this signature before signing a transaction, so the only path to a signed
// payment runs through MAX's deterministic policy — not the agent's prompt.
type Attestor struct {
	priv  ed25519.PrivateKey
	keyID string
}

func NewAttestor(seed []byte) (*Attestor, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("authorization signing seed must be 32 bytes")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	sum := sha256.Sum256(priv.Public().(ed25519.PublicKey))
	return &Attestor{priv: priv, keyID: hex.EncodeToString(sum[:8])}, nil
}

func (a *Attestor) PublicKey() ed25519.PublicKey { return a.priv.Public().(ed25519.PublicKey) }
func (a *Attestor) KeyID() string                { return a.keyID }

// SignedAuthorization carries the payload as base64 of the exact signed
// bytes (as JWS does). Re-serializing parsed JSON would change key order and
// whitespace, so signers must verify these bytes first and only then parse.
type SignedAuthorization struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Payload   string `json:"payload"`
	// Signature is base64 ed25519 over AuthorizationDomain + decoded payload bytes.
	Signature string `json:"signature"`
}

// Claims returns the payload bytes; call it only after VerifyAuthorization.
func (sa SignedAuthorization) Claims() ([]byte, error) {
	return base64.StdEncoding.DecodeString(sa.Payload)
}

func (a *Attestor) Sign(payload any) (SignedAuthorization, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return SignedAuthorization{}, err
	}
	sig := ed25519.Sign(a.priv, append([]byte(AuthorizationDomain), b...))
	return SignedAuthorization{
		Algorithm: "ed25519", KeyID: a.keyID, Payload: base64.StdEncoding.EncodeToString(b),
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// VerifyAuthorization is what a signer runs before signing. It is exported so
// the Go SDK and tests share one implementation.
func VerifyAuthorization(pub ed25519.PublicKey, sa SignedAuthorization, now time.Time) error {
	sig, err := base64.StdEncoding.DecodeString(sa.Signature)
	if err != nil {
		return errors.New("signature is not base64")
	}
	payload, err := sa.Claims()
	if err != nil {
		return errors.New("payload is not base64")
	}
	if !ed25519.Verify(pub, append([]byte(AuthorizationDomain), payload...), sig) {
		return errors.New("authorization signature is invalid")
	}
	var p struct {
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	if !now.Before(p.ExpiresAt) {
		return errors.New("authorization has expired")
	}
	return nil
}
