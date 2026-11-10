package chains

import (
	"errors"
	"strings"
)

// Segwit address checks per BIP-173 (bech32, witness v0) and BIP-350
// (bech32m, witness v1+).

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

const (
	bech32Const  = 1
	bech32mConst = 0x2bc830a3
)

func bech32Polymod(values []byte) uint32 {
	gen := [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>i)&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

// checkSegwit validates a lowercase segwit address for the given HRP.
func checkSegwit(hrp, addr string) error {
	if len(addr) > 90 {
		return errors.New("too long")
	}
	sep := strings.LastIndexByte(addr, '1')
	if sep < 1 || addr[:sep] != hrp || len(addr)-sep-1 < 7 {
		return errors.New("wrong network prefix")
	}
	data := make([]byte, 0, len(addr)-sep-1)
	for _, c := range addr[sep+1:] {
		i := strings.IndexRune(bech32Charset, c)
		if i < 0 {
			return errors.New("invalid character")
		}
		data = append(data, byte(i))
	}
	expanded := make([]byte, 0, 2*len(hrp)+1+len(data))
	for _, c := range hrp {
		expanded = append(expanded, byte(c)>>5)
	}
	expanded = append(expanded, 0)
	for _, c := range hrp {
		expanded = append(expanded, byte(c)&31)
	}
	expanded = append(expanded, data...)
	constant := bech32Polymod(expanded)

	version := data[0]
	if version > 16 {
		return errors.New("invalid witness version")
	}
	if (version == 0 && constant != bech32Const) || (version > 0 && constant != bech32mConst) {
		return errors.New("bad checksum")
	}
	program, err := convertBits(data[1:len(data)-6], 5, 8)
	if err != nil {
		return err
	}
	if len(program) < 2 || len(program) > 40 {
		return errors.New("invalid witness program length")
	}
	if version == 0 && len(program) != 20 && len(program) != 32 {
		return errors.New("invalid witness v0 program length")
	}
	if version == 1 && len(program) != 32 {
		return errors.New("invalid taproot program length")
	}
	return nil
}

// convertBits regroups 5-bit words into bytes, rejecting non-zero padding.
func convertBits(data []byte, from, to uint) ([]byte, error) {
	var acc, bits uint
	maxv := uint(1)<<to - 1
	out := make([]byte, 0, len(data)*int(from)/int(to))
	for _, v := range data {
		acc = acc<<from | uint(v)
		bits += from
		for bits >= to {
			bits -= to
			out = append(out, byte(acc>>bits&maxv))
		}
	}
	if bits >= from || (acc<<(to-bits))&maxv != 0 {
		return nil, errors.New("invalid padding")
	}
	return out, nil
}
