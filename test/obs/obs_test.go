//go:build obs

// Package obs_test is the live check of the observability stack: a request's
// X-Request-ID leads to its access-log line in Loki, whose trace_id leads to
// the trace in Tempo, and Grafana's Loki datasource links trace_id to Tempo.
// Run with the stack up: make test-obs (GRAFANA_ADMIN_PASSWORD from .env).
package obs_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	apiURL      = "https://localhost:8443/art/monet"
	grafanaURL  = "https://localhost:3000"
	adminUser   = "admin"
	lokiUID     = "loki"
	tempoUID    = "tempo"
	lokiLabel   = `service_name="artstudio"`
	ingestWait  = 60 * time.Second
	pollEvery   = 2 * time.Second
	logLookback = 5 * time.Minute
)

var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func tlsClient(t *testing.T) *http.Client {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	caPath := filepath.Join(filepath.Dir(file), "..", "..", "deploy", "certs", "ca.pem")
	pem, err := os.ReadFile(caPath)
	require.NoError(t, err, "CA missing: run `make certs`")
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(pem), "%s holds no certificate", caPath)
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}},
	}
}

func grafanaGet(t *testing.T, c *http.Client, password, path string) (int, []byte, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, grafanaURL+path, nil)
	require.NoError(t, err)
	req.SetBasicAuth(adminUser, password)
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

// eventually retries fn until it returns nil or the ingestion deadline passes.
func eventually(t *testing.T, what string, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(ingestWait)
	var last error
	for {
		if last = fn(); last == nil {
			return
		}
		if time.Now().After(deadline) {
			require.NoError(t, last, "%s: not satisfied within %s", what, ingestWait)
		}
		time.Sleep(pollEvery)
	}
}

type lokiResult struct {
	Data struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// accessLogTraceID finds the access-log line for requestID and returns its trace_id.
// Fields are read from stream labels / structured metadata and from a JSON line body.
func accessLogTraceID(t *testing.T, c *http.Client, password, requestID string) (string, error) {
	t.Helper()
	filters := []string{
		fmt.Sprintf(`| request_id=%q`, requestID), // OTLP structured metadata
		fmt.Sprintf(`|= %q`, requestID),           // stdout JSON line
	}
	for _, f := range filters {
		q := url.Values{}
		q.Set("query", "{"+lokiLabel+"} "+f)
		q.Set("start", strconv.FormatInt(time.Now().Add(-logLookback).UnixNano(), 10))
		q.Set("end", strconv.FormatInt(time.Now().Add(time.Minute).UnixNano(), 10))
		q.Set("limit", "100")
		status, body, err := grafanaGet(t, c, password, "/api/datasources/proxy/uid/"+lokiUID+"/loki/api/v1/query_range?"+q.Encode())
		if err != nil {
			return "", fmt.Errorf("loki query via grafana: %w", err)
		}
		if status != http.StatusOK {
			return "", fmt.Errorf("loki query via grafana: HTTP %d: %.200s", status, body)
		}
		var res lokiResult
		if err := json.Unmarshal(body, &res); err != nil {
			return "", fmt.Errorf("loki response: %w", err)
		}
		for _, s := range res.Data.Result {
			for _, v := range s.Values {
				line, _ := v[1].(string)
				fields := map[string]string{}
				for k, val := range s.Stream {
					fields[k] = val
				}
				var obj map[string]any
				if json.Unmarshal([]byte(line), &obj) == nil {
					for k, val := range obj {
						if sv, ok := val.(string); ok {
							fields[k] = sv
						}
					}
				}
				isAccess := fields["msg"] == "request" || line == "request"
				if isAccess && fields["request_id"] == requestID && fields["trace_id"] != "" {
					return fields["trace_id"], nil
				}
			}
		}
	}
	return "", fmt.Errorf("no access log line (msg=request) with request_id=%s and trace_id in Loki {%s}", requestID, lokiLabel)
}

func TestRequestIDToLokiToTempo(t *testing.T) {
	password := os.Getenv("GRAFANA_ADMIN_PASSWORD")
	require.NotEmpty(t, password, "GRAFANA_ADMIN_PASSWORD must be set (see .env)")
	c := tlsClient(t)

	resp, err := c.Get(apiURL)
	require.NoError(t, err, "api not reachable at %s: is the stack up?", apiURL)
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	requestID := resp.Header.Get("X-Request-ID")
	require.NotEmpty(t, requestID, "response has no X-Request-ID")

	var traceID string
	eventually(t, "access log line in Loki", func() error {
		var err error
		traceID, err = accessLogTraceID(t, c, password, requestID)
		return err
	})
	assert.Regexp(t, traceIDPattern, traceID, "trace_id must be 32 lowercase hex")

	eventually(t, "trace in Tempo", func() error {
		status, body, err := grafanaGet(t, c, password, "/api/datasources/proxy/uid/"+tempoUID+"/api/traces/"+traceID)
		if err != nil {
			return fmt.Errorf("tempo query via grafana: %w", err)
		}
		if status != http.StatusOK {
			return fmt.Errorf("tempo trace %s: HTTP %d: %.200s", traceID, status, body)
		}
		if !strings.Contains(string(body), `"artstudio"`) {
			return fmt.Errorf("tempo trace %s has no service named artstudio", traceID)
		}
		return nil
	})
}

func TestLokiDerivedFieldTargetsTempo(t *testing.T) {
	password := os.Getenv("GRAFANA_ADMIN_PASSWORD")
	require.NotEmpty(t, password, "GRAFANA_ADMIN_PASSWORD must be set (see .env)")
	c := tlsClient(t)

	eventually(t, "grafana datasource loki", func() error {
		status, body, err := grafanaGet(t, c, password, "/api/datasources/uid/"+lokiUID)
		if err != nil {
			return fmt.Errorf("grafana not reachable at %s: %w", grafanaURL, err)
		}
		if status != http.StatusOK {
			return fmt.Errorf("grafana datasource %s: HTTP %d: %.200s", lokiUID, status, body)
		}
		var ds struct {
			JSONData struct {
				DerivedFields []struct {
					MatcherRegex  string `json:"matcherRegex"`
					DatasourceUID string `json:"datasourceUid"`
				} `json:"derivedFields"`
			} `json:"jsonData"`
		}
		if err := json.Unmarshal(body, &ds); err != nil {
			return err
		}
		for _, d := range ds.JSONData.DerivedFields {
			if d.DatasourceUID == tempoUID && strings.Contains(d.MatcherRegex, "trace_id") {
				return nil
			}
		}
		return fmt.Errorf("loki datasource has no derived field on trace_id targeting datasourceUid %q: %+v", tempoUID, ds.JSONData.DerivedFields)
	})
}
