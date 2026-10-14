package tlsconf_test

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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/tlsconf"
)

type pki struct{ caFile, certFile, keyFile string }

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600))
}

// newPKI writes a fresh CA and a leaf for 127.0.0.1/localhost signed by it.
func newPKI(t *testing.T, name string) pki {
	t.Helper()
	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name + " CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "api"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	require.NoError(t, err)

	p := pki{filepath.Join(dir, "ca.pem"), filepath.Join(dir, "api.pem"), filepath.Join(dir, "api-key.pem")}
	writePEM(t, p.caFile, "CERTIFICATE", caDER)
	writePEM(t, p.certFile, "CERTIFICATE", leafDER)
	writePEM(t, p.keyFile, "EC PRIVATE KEY", keyDER)
	return p
}

func startServer(t *testing.T, p pki) *httptest.Server {
	t.Helper()
	cfg, err := tlsconf.Server(p.certFile, p.keyFile)
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestServerMinTLS13(t *testing.T) {
	p := newPKI(t, "a")
	cfg, err := tlsconf.Server(p.certFile, p.keyFile)
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	assert.NotEmpty(t, cfg.Certificates)

	srv := startServer(t, p)
	pool := x509.NewCertPool()
	pem, err := os.ReadFile(p.caFile)
	require.NoError(t, err)
	require.True(t, pool.AppendCertsFromPEM(pem))

	_, err = tls.Dial("tcp", srv.Listener.Addr().String(), &tls.Config{RootCAs: pool, ServerName: "localhost", MaxVersion: tls.VersionTLS12}) //nolint:gosec // deliberate TLS 1.2 client to prove the server rejects it
	assert.Error(t, err, "a TLS 1.2-only client must be rejected")

	conn, err := tls.Dial("tcp", srv.Listener.Addr().String(), &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS13})
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS13), conn.ConnectionState().Version)
	_ = conn.Close()
}

func TestServerBadFiles(t *testing.T) {
	_, err := tlsconf.Server("/nonexistent/c.pem", "/nonexistent/k.pem")
	assert.Error(t, err)
}

func TestClientTrustsOnlyGivenCA(t *testing.T) {
	trusted := newPKI(t, "trusted")
	other := newPKI(t, "other")

	cfg, err := tlsconf.Client(trusted.caFile)
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
	assert.False(t, cfg.InsecureSkipVerify)
	require.NotNil(t, cfg.RootCAs)
	assert.Len(t, cfg.RootCAs.Subjects(), 1) //nolint:staticcheck // counts the pool: only the given CA

	get := func(srv *httptest.Server) error {
		c := &http.Client{Timeout: testTimeout, Transport: &http.Transport{TLSClientConfig: cfg.Clone()}}
		resp, err := c.Get(srv.URL)
		if err == nil {
			_ = resp.Body.Close()
		}
		return err
	}
	assert.NoError(t, get(startServer(t, trusted)), "server signed by the trusted CA must verify")
	err = get(startServer(t, other))
	require.Error(t, err, "server signed by another CA must be rejected")
	var unknown x509.UnknownAuthorityError
	assert.ErrorAs(t, err, &unknown)
}

const testTimeout = 5 * time.Second

func TestClientBadCAFile(t *testing.T) {
	_, err := tlsconf.Client("/nonexistent/ca.pem")
	assert.Error(t, err)

	junk := filepath.Join(t.TempDir(), "junk.pem")
	require.NoError(t, os.WriteFile(junk, []byte("not a pem"), 0o600))
	_, err = tlsconf.Client(junk)
	assert.Error(t, err)
}

func TestHSTSHeader(t *testing.T) {
	h := tlsconf.HSTS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, "max-age=63072000; includeSubDomains", rec.Header().Get("Strict-Transport-Security"))
	assert.Equal(t, http.StatusTeapot, rec.Code, "next handler still runs")
}
