package chains

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/mr-tron/base58"
)

func TestChecksumEVMAddress_EIP55Vectors(t *testing.T) {
	vectors := []string{
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
	}
	for _, want := range vectors {
		norm, err := NormalizeEVMAddress(want)
		if err != nil {
			t.Fatalf("normalize %s: %v", want, err)
		}
		if got := ChecksumEVMAddress(norm); got != want {
			t.Errorf("checksum = %s, want %s", got, want)
		}
	}
}

func TestNormalizeEVMAddress(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"lowercase", "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed", "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed", false},
		{"valid checksum", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed", false},
		{"bad checksum", "0x5AAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", "", true},
		{"short", "0x1234", "", true},
		{"no prefix", "5aaeb6053f3e94c9b9a09f33669435e7ef1beaed00", "", true},
		{"non hex", "0xzzaeb6053f3e94c9b9a09f33669435e7ef1beaed", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeEVMAddress(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func evmSign(t *testing.T, priv *secp256k1.PrivateKey, msg string) string {
	t.Helper()
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msg))
	digest := keccak256([]byte(prefix), []byte(msg))
	compact := ecdsa.SignCompact(priv, digest, false)
	// Convert decred's [v||r||s] layout to Ethereum's [r||s||v].
	sig := append(append([]byte{}, compact[1:]...), compact[0])
	return "0x" + hex.EncodeToString(sig)
}

func TestVerifyEVMPersonalSign(t *testing.T) {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	addr := pubkeyToEVMAddress(priv.PubKey())
	other, _ := secp256k1.GeneratePrivateKey()
	msg := "localhost:3000 wants you to sign in with your Ethereum account"

	tests := []struct {
		name    string
		addr    string
		msg     string
		sig     string
		wantErr bool
	}{
		{"valid", addr, msg, evmSign(t, priv, msg), false},
		{"tampered message", addr, msg + "!", evmSign(t, priv, msg), true},
		{"wrong signer", addr, msg, evmSign(t, other, msg), true},
		{"garbage", addr, msg, "0xdeadbeef", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyEVMPersonalSign(tt.addr, tt.msg, tt.sig)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestVerifySolanaSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	addr := base58.Encode(pub)
	msg := "localhost:3000 wants you to sign in with your Solana account"
	sig := base58.Encode(ed25519.Sign(priv, []byte(msg)))

	if _, err := NormalizeSolanaAddress(addr); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := VerifySolanaSignature(addr, msg, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifySolanaSignature(addr, msg+"x", sig); err == nil {
		t.Fatal("tampered message accepted")
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifySolanaSignature(base58.Encode(otherPub), msg, sig); err == nil {
		t.Fatal("wrong address accepted")
	}
}
