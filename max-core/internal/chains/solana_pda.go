package chains

import (
	"crypto/sha256"
	"errors"

	"filippo.io/edwards25519"
	"github.com/mr-tron/base58"
)

const (
	SPLTokenProgram        = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
	SPLToken2022Program    = "TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"
	AssociatedTokenProgram = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
	SystemProgram          = "11111111111111111111111111111111"
)

func IsSPLTokenProgram(p string) bool {
	return p == SPLTokenProgram || p == SPLToken2022Program
}

// FindProgramAddress mirrors solana_program::pubkey::find_program_address.
func FindProgramAddress(seeds [][]byte, programID string) (string, uint8, error) {
	for bump := 255; bump >= 0; bump-- {
		addr, err := CreateProgramAddress(append(append([][]byte{}, seeds...), []byte{byte(bump)}), programID)
		if err == nil {
			return addr, uint8(bump), nil
		}
		if !errors.Is(err, errOnCurve) {
			return "", 0, err
		}
	}
	return "", 0, errors.New("no viable bump seed")
}

var errOnCurve = errors.New("derived address is on the ed25519 curve")

// CreateProgramAddress mirrors solana_program::pubkey::create_program_address.
func CreateProgramAddress(seeds [][]byte, programID string) (string, error) {
	program, err := base58.Decode(programID)
	if err != nil || len(program) != 32 {
		return "", errors.New("invalid program id")
	}
	h := sha256.New()
	for _, s := range seeds {
		if len(s) > 32 {
			return "", errors.New("seed exceeds 32 bytes")
		}
		h.Write(s)
	}
	h.Write(program)
	h.Write([]byte("ProgramDerivedAddress"))
	sum := h.Sum(nil)
	// A PDA must lie off the ed25519 curve so no private key can exist for it.
	if _, err := new(edwards25519.Point).SetBytes(sum); err == nil {
		return "", errOnCurve
	}
	return base58.Encode(sum), nil
}

// AssociatedTokenAddress derives the ATA for owner+mint under the given token program.
func AssociatedTokenAddress(owner, mint, tokenProgram string) (string, error) {
	o, err := base58.Decode(owner)
	if err != nil || len(o) != 32 {
		return "", errors.New("invalid owner")
	}
	m, err := base58.Decode(mint)
	if err != nil || len(m) != 32 {
		return "", errors.New("invalid mint")
	}
	tp, err := base58.Decode(tokenProgram)
	if err != nil || len(tp) != 32 {
		return "", errors.New("invalid token program")
	}
	addr, _, err := FindProgramAddress([][]byte{o, tp, m}, AssociatedTokenProgram)
	return addr, err
}
