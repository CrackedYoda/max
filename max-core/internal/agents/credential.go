package agents

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Keys look like maxk_<16 hex prefix>_<43 char secret>. The prefix is stored
// in clear for lookup; only an HMAC of the secret is stored.
const keyPrefix = "maxk_"

var errMalformedKey = errors.New("malformed API key")

func IsAPIKey(token string) bool { return strings.HasPrefix(token, keyPrefix) }

func newKey() (key, prefix, secret string, err error) {
	p := make([]byte, 8)
	s := make([]byte, 32)
	if _, err = rand.Read(p); err != nil {
		return
	}
	if _, err = rand.Read(s); err != nil {
		return
	}
	prefix = hex.EncodeToString(p)
	secret = base64.RawURLEncoding.EncodeToString(s)
	return keyPrefix + prefix + "_" + secret, prefix, secret, nil
}

func parseKey(key string) (prefix, secret string, err error) {
	rest, ok := strings.CutPrefix(key, keyPrefix)
	if !ok {
		return "", "", errMalformedKey
	}
	prefix, secret, ok = strings.Cut(rest, "_")
	if !ok || len(prefix) != 16 || len(secret) != 43 {
		return "", "", errMalformedKey
	}
	if _, err := hex.DecodeString(prefix); err != nil {
		return "", "", errMalformedKey
	}
	return prefix, secret, nil
}

func hashSecret(serverSecret, secret string) []byte {
	m := hmac.New(sha256.New, []byte(serverSecret))
	m.Write([]byte("agent-credential\x00"))
	m.Write([]byte(secret))
	return m.Sum(nil)
}

func secretMatches(serverSecret, secret string, stored []byte) bool {
	return subtle.ConstantTimeCompare(hashSecret(serverSecret, secret), stored) == 1
}
