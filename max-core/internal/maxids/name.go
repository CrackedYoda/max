package maxids

import (
	"errors"
	"strings"
)

const (
	Prefix    = "max."
	MinLength = 3
	MaxLength = 32
)

var (
	errNameLength    = errors.New("name must be 3–32 characters")
	errNameCharset   = errors.New("name may contain only a–z, 0–9 and single hyphens")
	errNameHyphenEnd = errors.New("name cannot start or end with a hyphen")
	errNameHyphenRun = errors.New("name cannot contain consecutive hyphens")
)

// Normalize turns user input such as "MAX.Yoda" or " yoda " into the canonical
// registry key "yoda". The charset is deliberately ASCII-only: any Unicode is
// rejected outright so homoglyph lookalikes can never be registered.
func Normalize(input string) (string, error) {
	s := strings.TrimSpace(input)
	if len(s) >= len(Prefix) && strings.EqualFold(s[:len(Prefix)], Prefix) {
		s = s[len(Prefix):]
	}

	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	s = string(b)

	if len(s) < MinLength || len(s) > MaxLength {
		return "", errNameLength
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-':
			if i > 0 && s[i-1] == '-' {
				return "", errNameHyphenRun
			}
		default:
			return "", errNameCharset
		}
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return "", errNameHyphenEnd
	}
	return s, nil
}

func Handle(canonical string) string { return Prefix + canonical }
