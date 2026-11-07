package maxids

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

const (
	DefaultStyleRandom = "random"
	DefaultStyleWords  = "words"
)

const (
	randomAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	randomLength   = 16
	randomMinClass = 3
)

var wordsShape = regexp.MustCompile(`^[a-z]{3,10}-[a-z]{3,10}-[0-9]{4}$`)

// DefaultNamer generates and recognises the names given to default MAX IDs.
type DefaultNamer interface {
	Generate() (string, error)
	Owns(canonical string) bool
}

// NewDefaultNamer returns the namer for a style. "words" is reserved but not
// built yet.
func NewDefaultNamer(style string) (DefaultNamer, error) {
	switch style {
	case DefaultStyleRandom:
		return randomNamer{}, nil
	case DefaultStyleWords:
		return nil, fmt.Errorf("default ID style %q is not implemented", style)
	default:
		return nil, fmt.Errorf("unknown default ID style %q", style)
	}
}

// IsDefaultShape reports whether any style owns the name, so users can never
// claim a name that a default generator could produce.
func IsDefaultShape(canonical string) bool {
	return randomNamer{}.Owns(canonical) || wordsShape.MatchString(canonical)
}

type randomNamer struct{}

func (randomNamer) Owns(s string) bool {
	if len(s) != randomLength {
		return false
	}
	digits, letters := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(randomAlphabet, c) < 0 {
			return false
		}
		if c >= '0' && c <= '9' {
			digits++
		} else {
			letters++
		}
	}
	return digits >= randomMinClass && letters >= randomMinClass
}

func (n randomNamer) Generate() (string, error) {
	max := big.NewInt(int64(len(randomAlphabet)))
	buf := make([]byte, randomLength)
	for {
		for i := range buf {
			v, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", fmt.Errorf("generate default id: %w", err)
			}
			buf[i] = randomAlphabet[v.Int64()]
		}
		if s := string(buf); n.Owns(s) {
			return s, nil
		}
	}
}
