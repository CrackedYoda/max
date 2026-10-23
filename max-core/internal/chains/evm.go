package chains

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

func keccak256(data ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, d := range data {
		h.Write(d)
	}
	return h.Sum(nil)
}

// NormalizeEVMAddress validates a 0x address and returns it lowercased, which
// is the canonical storage form. Mixed-case input must carry a valid EIP-55
// checksum so typos are caught instead of silently accepted.
func NormalizeEVMAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if len(addr) != 42 || !strings.HasPrefix(addr, "0x") && !strings.HasPrefix(addr, "0X") {
		return "", errors.New("evm address must be 0x followed by 40 hex characters")
	}
	body := addr[2:]
	if _, err := hex.DecodeString(body); err != nil {
		return "", errors.New("evm address contains non-hex characters")
	}
	lower := strings.ToLower(body)
	if body != lower && body != strings.ToUpper(body) {
		if ChecksumEVMAddress("0x"+lower) != "0x"+body {
			return "", errors.New("evm address has an invalid EIP-55 checksum")
		}
	}
	return "0x" + lower, nil
}

// ChecksumEVMAddress renders a lowercase address in EIP-55 mixed case.
func ChecksumEVMAddress(lowerAddr string) string {
	body := strings.TrimPrefix(strings.ToLower(lowerAddr), "0x")
	hash := hex.EncodeToString(keccak256([]byte(body)))
	out := make([]byte, len(body))
	for i := range body {
		c := body[i]
		if c >= 'a' && c <= 'f' && hash[i] >= '8' {
			c -= 'a' - 'A'
		}
		out[i] = c
	}
	return "0x" + string(out)
}

// VerifyEVMPersonalSign checks an EIP-191 personal_sign signature (as produced
// by MetaMask / wagmi signMessage) against the expected lowercase address.
// Smart-contract wallets (EIP-1271) are not supported yet.
func VerifyEVMPersonalSign(lowerAddr, message, sigHex string) error {
	sig, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(sigHex), "0x"))
	if err != nil || len(sig) != 65 {
		return errors.New("signature must be 65 bytes hex-encoded")
	}

	v := sig[64]
	if v >= 27 {
		v -= 27
	}
	if v > 1 {
		return errors.New("invalid signature recovery id")
	}

	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(message))
	digest := keccak256([]byte(prefix), []byte(message))

	compact := make([]byte, 65)
	compact[0] = 27 + v
	copy(compact[1:], sig[:64])

	pub, _, err := ecdsa.RecoverCompact(compact, digest)
	if err != nil {
		return fmt.Errorf("recover public key: %w", err)
	}

	if got := pubkeyToEVMAddress(pub); got != lowerAddr {
		return errors.New("signature does not match address")
	}
	return nil
}

func pubkeyToEVMAddress(pub *secp256k1.PublicKey) string {
	uncompressed := pub.SerializeUncompressed()
	return "0x" + hex.EncodeToString(keccak256(uncompressed[1:])[12:])
}
