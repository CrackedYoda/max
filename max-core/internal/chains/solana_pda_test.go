package chains

import (
	"testing"

	"github.com/mr-tron/base58"
)

// Vectors from solana-program's pubkey tests.
func TestCreateProgramAddress_SDKVectors(t *testing.T) {
	program := "BPFLoaderUpgradeab1e11111111111111111111111"
	seedKey, _ := base58.Decode("SeedPubey1111111111111111111111111111111111")

	tests := []struct {
		seeds [][]byte
		want  string
	}{
		{[][]byte{[]byte(""), {1}}, "BwqrghZA2htAcqq8dzP1WDAhTXYTYWj7CHxF5j7TDBAe"},
		{[][]byte{[]byte("☉"), {0}}, "13yWmRpaTR4r5nAktwLqMpRNr28tnVUZw26rTvPSSB19"},
		{[][]byte{[]byte("Talking"), []byte("Squirrels")}, "2fnQrngrQT4SeLcdToJAD96phoEjNL2man2kfRLCASVk"},
		{[][]byte{seedKey, {1}}, "976ymqVnfE32QFe6NfGDctSvVa36LWnvYxhU6G2232YL"},
	}
	for _, tt := range tests {
		got, err := CreateProgramAddress(tt.seeds, program)
		if err != nil {
			t.Fatalf("%q: %v", tt.seeds, err)
		}
		if got != tt.want {
			t.Errorf("got %s, want %s", got, tt.want)
		}
	}
}

func TestAssociatedTokenAddress_DeterministicAndOffCurve(t *testing.T) {
	owner := "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	mint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	a, err := AssociatedTokenAddress(owner, mint, SPLTokenProgram)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := AssociatedTokenAddress(owner, mint, SPLTokenProgram)
	c, _ := AssociatedTokenAddress(owner, mint, SPLToken2022Program)
	if a != b || a == c || a == owner {
		t.Fatalf("unexpected derivation: %s %s %s", a, b, c)
	}
}

func TestERC20Encoding(t *testing.T) {
	if ERC20TransferTopic != "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef" {
		t.Fatalf("transfer topic = %s", ERC20TransferTopic)
	}
}
