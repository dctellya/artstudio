// Package tlsconf builds the TLS configurations and the HSTS middleware.
package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
)

const hstsValue = "max-age=63072000; includeSubDomains"

// Server returns a TLS 1.3 minimum server config for the given key pair.
func Server(certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsconf: load key pair: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}, nil
}

// Client returns a TLS 1.3 minimum client config that trusts only caFile.
func Client(caFile string) (*tls.Config, error) {
	pem, err := os.ReadFile(caFile) //nolint:gosec // path comes from configuration
	if err != nil {
		return nil, fmt.Errorf("tlsconf: read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("tlsconf: no certificate found in CA file")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool}, nil
}

// HSTS sets Strict-Transport-Security on every response.
func HSTS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", hstsValue)
		next.ServeHTTP(w, r)
	})
}
