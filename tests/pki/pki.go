// Package pki issues throwaway certificates for tests: a CA per run, a
// server certificate for loopback names and client certificates whose
// SPKI pins identify OIC Accesses.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// CA is a self-signed certificate authority.
type CA struct {
	Cert *x509.Certificate
	key  *ecdsa.PrivateKey
	PEM  []byte
}

// Leaf is an issued certificate with its key.
type Leaf struct {
	Cert    *x509.Certificate
	CertPEM []byte
	KeyPEM  []byte
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	return n
}

// NewCA creates a CA valid for one day.
func NewCA(name string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{Cert: cert, key: key, PEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}, nil
}

// Pool returns a pool holding only this CA.
func (ca *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.Cert)
	return p
}

// Server issues a server certificate for 127.0.0.1, ::1 and localhost.
func (ca *CA) Server(cn string) (*Leaf, error) {
	return ca.issue(cn, x509.ExtKeyUsageServerAuth, []string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback})
}

// Client issues a client certificate.
func (ca *CA) Client(cn string) (*Leaf, error) {
	return ca.issue(cn, x509.ExtKeyUsageClientAuth, nil, nil)
}

func (ca *CA) issue(cn string, usage x509.ExtKeyUsage, dns []string, ips []net.IP) (*Leaf, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage},
		DNSNames: dns, IPAddresses: ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	kb, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &Leaf{Cert: cert, CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})}, nil
}

// TLS returns the leaf as a tls.Certificate.
func (l *Leaf) TLS() (tls.Certificate, error) { return tls.X509KeyPair(l.CertPEM, l.KeyPEM) }

// Write stores the leaf as <dir>/<name>.pem and <dir>/<name>-key.pem and
// returns both paths.
func (l *Leaf) Write(dir, name string) (certFile, keyFile string, err error) {
	certFile = filepath.Join(dir, name+".pem")
	keyFile = filepath.Join(dir, name+"-key.pem")
	if err = os.WriteFile(certFile, l.CertPEM, 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, os.WriteFile(keyFile, l.KeyPEM, 0o600)
}

// Write stores the CA certificate as <dir>/<name>.pem.
func (ca *CA) Write(dir, name string) (string, error) {
	p := filepath.Join(dir, name+".pem")
	return p, os.WriteFile(p, ca.PEM, 0o600)
}
