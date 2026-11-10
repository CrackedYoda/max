package chains

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"

	"github.com/mr-tron/base58"
)

// utxoParams describes a Bitcoin-family mainnet's address encodings.
type utxoParams struct {
	name     string
	versions []byte // accepted base58check version bytes
	hrp      string // bech32 human-readable part
}

var (
	bitcoinParams  = utxoParams{name: "bitcoin", versions: []byte{0x00, 0x05}, hrp: "bc"}
	litecoinParams = utxoParams{name: "litecoin", versions: []byte{0x30, 0x32, 0x05}, hrp: "ltc"}
)

// normalizeUTXOAddress accepts legacy base58check and segwit (bech32/bech32m)
// addresses. Bech32 addresses are stored lowercase; base58 ones as given.
func normalizeUTXOAddress(p utxoParams, addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(strings.ToLower(addr), p.hrp+"1") {
		lower := strings.ToLower(addr)
		if addr != lower && addr != strings.ToUpper(addr) {
			return "", errors.New(p.name + " address mixes upper and lower case")
		}
		if err := checkSegwit(p.hrp, lower); err != nil {
			return "", errors.New(p.name + " address: " + err.Error())
		}
		return lower, nil
	}
	payload, err := decodeBase58Check(addr)
	if err != nil || len(payload) != 21 || bytes.IndexByte(p.versions, payload[0]) < 0 {
		return "", errors.New("not a valid " + p.name + " address")
	}
	return addr, nil
}

// NormalizeTronAddress validates a base58check Tron address (version 0x41).
func NormalizeTronAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	payload, err := decodeBase58Check(addr)
	if err != nil || len(payload) != 21 || payload[0] != 0x41 {
		return "", errors.New("not a valid tron address")
	}
	return addr, nil
}

func decodeBase58Check(s string) ([]byte, error) {
	raw, err := base58.Decode(s)
	if err != nil || len(raw) < 5 {
		return nil, errors.New("invalid base58")
	}
	payload, sum := raw[:len(raw)-4], raw[len(raw)-4:]
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	if !bytes.Equal(second[:4], sum) {
		return nil, errors.New("bad checksum")
	}
	return payload, nil
}
