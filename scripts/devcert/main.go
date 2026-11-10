// devcert creates a deployment-specific CA and IP-SAN server certificate.
// Private keys stay in the chosen ignored deployment directory.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func serial() *big.Int {
	n, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		panic(e)
	}
	return n
}
func write(path, kind string, der []byte) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0600)
}
func run() error {
	out := flag.String("out", ".local/tls", "new certificate directory")
	hosts := flag.String("hosts", "121.199.175.151,127.0.0.1,localhost", "comma-separated IP or DNS SANs")
	flag.Parse()
	if e := os.MkdirAll(*out, 0700); e != nil {
		return e
	}
	for _, name := range []string{"ca.crt", "ca.key", "server.crt", "server.key"} {
		if _, e := os.Stat(filepath.Join(*out, name)); e == nil {
			return fmt.Errorf("refusing to replace existing %s", name)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	caKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "loom-oi beta deployment CA"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, e := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if e != nil {
		return e
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	server := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "loom-oi beta OIS"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(0, 3, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	for _, h := range strings.Split(*hosts, ",") {
		h = strings.TrimSpace(h)
		if ip := net.ParseIP(h); ip != nil {
			server.IPAddresses = append(server.IPAddresses, ip)
		} else if h != "" {
			server.DNSNames = append(server.DNSNames, h)
		}
	}
	serverDER, e := x509.CreateCertificate(rand.Reader, server, ca, &key.PublicKey, caKey)
	if e != nil {
		return e
	}
	caPrivate, e := x509.MarshalPKCS8PrivateKey(caKey)
	if e != nil {
		return e
	}
	private, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return e
	}
	for _, f := range []struct {
		name, kind string
		data       []byte
	}{{"ca.crt", "CERTIFICATE", caDER}, {"ca.key", "PRIVATE KEY", caPrivate}, {"server.crt", "CERTIFICATE", serverDER}, {"server.key", "PRIVATE KEY", private}} {
		if e = write(filepath.Join(*out, f.name), f.kind, f.data); e != nil {
			return e
		}
	}
	fmt.Println("Created CA and server certificate in", *out)
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
