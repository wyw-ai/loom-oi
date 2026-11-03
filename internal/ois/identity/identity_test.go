package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	id, bearer, hash := NewToken()
	gotID, secret, ok := ParseToken(bearer)
	if !ok || gotID != id || !SecretMatches(secret, hash) {
		t.Fatalf("round trip failed: %s", bearer)
	}
	if SecretMatches(secret+"x", hash) || len(hash) != 32 {
		t.Fatal("hash compare")
	}
	_, bearer2, _ := NewToken()
	if bearer2 == bearer {
		t.Fatal("tokens repeat")
	}
}

func TestParseTokenRejects(t *testing.T) {
	_, good, _ := NewToken()
	bad := []string{"", "oi_", "xx_" + good[3:], strings.Replace(good, "oi_", "oi_Z", 1), good + "x", good[:len(good)-1], "oi_0123456789abcdef"}
	for _, b := range bad {
		if _, _, ok := ParseToken(b); ok {
			t.Errorf("%q accepted", b)
		}
	}
}

func TestBearer(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	if _, ok := BearerToken(r); ok {
		t.Fatal("missing header accepted")
	}
	r.Header.Set("Authorization", "Basic x")
	if _, ok := BearerToken(r); ok {
		t.Fatal("basic accepted")
	}
	r.Header.Set("Authorization", "Bearer ")
	if _, ok := BearerToken(r); ok {
		t.Fatal("empty bearer accepted")
	}
	r.Header.Set("Authorization", "Bearer abc")
	if v, ok := BearerToken(r); !ok || v != "abc" {
		t.Fatal(v)
	}
}

func TestSPKIPin(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"}, NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pin := SPKIPin(cert)
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	if pin != hex.EncodeToString(sum[:]) || !ValidPin(pin) {
		t.Fatal(pin)
	}
	if ValidPin(strings.ToUpper(pin)) || ValidPin(pin[:10]) {
		t.Fatal("bad pin accepted")
	}
}
