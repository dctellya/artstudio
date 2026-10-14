// Command gencert writes a local-dev CA and the api, grafana and alloy leaf
// certificates (ECDSA P-256) into deploy/certs. Idempotent: existing files are
// kept unless -force is given.
//
// Private keys are written 0644 on purpose: the containers run as uid 65532
// and read them through a read-only bind mount. These are throwaway local-dev
// certificates, git-ignored, never used outside the compose stack.
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
	"time"
)

const (
	validity = 825 * 24 * time.Hour
	fileMode = 0o644 // see package comment
	dirMode  = 0o755
)

type leaf struct {
	name string
	dns  []string
	ips  []net.IP
}

var leaves = []leaf{
	{"api", []string{"api.127.0.0.1.nip.io", "api", "localhost"}, []net.IP{net.IPv4(127, 0, 0, 1)}},
	{"grafana", []string{"grafana.127.0.0.1.nip.io", "grafana", "localhost"}, []net.IP{net.IPv4(127, 0, 0, 1)}},
	{"alloy", []string{"alloy"}, nil},
}

func main() {
	dir := flag.String("out", "deploy/certs", "output directory")
	force := flag.Bool("force", false, "regenerate even if files exist")
	flag.Parse()
	if err := run(*dir, *force); err != nil {
		fmt.Fprintln(os.Stderr, "gencert:", err)
		os.Exit(1)
	}
}

func run(dir string, force bool) error {
	if err := os.MkdirAll(dir, dirMode); err != nil { //nolint:gosec // local-dev cert dir, see package comment
		return err
	}
	files := []string{"ca.pem"}
	for _, l := range leaves {
		files = append(files, l.name+".pem", l.name+"-key.pem")
	}
	if !force && allExist(dir, files) {
		fmt.Println("gencert: certificates present, skipping (use -force to regenerate)")
		return nil
	}
	caKey, caCert, caDER, err := newCA()
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, "ca.pem"), "CERTIFICATE", caDER); err != nil {
		return err
	}
	for _, l := range leaves {
		if err := writeLeaf(dir, l, caKey, caCert); err != nil {
			return err
		}
	}
	fmt.Println("gencert: wrote", dir)
	return nil
}

func allExist(dir string, files []string) bool {
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
}

func newCA() (*ecdsa.PrivateKey, *x509.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	sn, err := serial()
	if err != nil {
		return nil, nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          sn,
		Subject:               pkix.Name{CommonName: "artstudio local dev CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(validity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	return key, cert, der, err
}

func writeLeaf(dir string, l leaf, caKey *ecdsa.PrivateKey, ca *x509.Certificate) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	sn, err := serial()
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: l.name},
		DNSNames:     l.dns,
		IPAddresses:  l.ips,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(dir, l.name+".pem"), "CERTIFICATE", der); err != nil {
		return err
	}
	return writePEM(filepath.Join(dir, l.name+"-key.pem"), "EC PRIVATE KEY", keyDER)
}

func writePEM(path, typ string, der []byte) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), fileMode) //nolint:gosec // see package comment
}
