// Package identity implements the B2 §2 credential formats: bearer tokens
// "oi_<token_id>_<secret>" stored only as SHA-256, and SPKI SHA-256 pins
// that map an OIC client certificate to its Access.
package identity

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

var (
	secretRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	pinRe    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// NewToken returns a fresh token: its ID, the full bearer string and the
// SHA-256 of the 32-byte secret, which is all OIS stores.
func NewToken() (id, bearer string, hash []byte) {
	id = hex.EncodeToString(protocol.RandomBytes(8))
	secret := base64.RawURLEncoding.EncodeToString(protocol.RandomBytes(32))
	return id, "oi_" + id + "_" + secret, HashSecret(secret)
}

// ParseToken splits a bearer string into its ID and secret.
func ParseToken(bearer string) (id, secret string, ok bool) {
	parts := strings.SplitN(bearer, "_", 3)
	if len(parts) != 3 || parts[0] != "oi" || !protocol.ValidTokenID(parts[1]) || !secretRe.MatchString(parts[2]) {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// HashSecret returns SHA-256(secret).
func HashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// SecretMatches compares a presented secret with a stored hash in
// constant time.
func SecretMatches(secret string, stored []byte) bool {
	return subtle.ConstantTimeCompare(HashSecret(secret), stored) == 1
}

// BearerToken extracts the bearer credential from an Authorization header.
func BearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || token == "" {
		return "", false
	}
	return token, true
}

// SPKIPin returns the lowercase hex SHA-256 of the certificate's
// SubjectPublicKeyInfo (B2 §2).
func SPKIPin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}

// ValidPin reports whether s is a pin in SPKIPin form.
func ValidPin(s string) bool { return pinRe.MatchString(s) }
