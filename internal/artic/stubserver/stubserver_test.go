package stubserver_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/artic/stubserver"
)

// knownIDs are real artic ids; the coder's fixtures must include at least one.
var knownIDs = []string{"27992", "111628", "129884", "28560", "20684"}

const unknownID = "999999999"

func start(t *testing.T, opts ...stubserver.Option) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(stubserver.New(opts...))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func ctl(t *testing.T, srv *httptest.Server, fault string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/__ctl?fault="+fault, "", nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Less(t, resp.StatusCode, 300, "ctl %s", fault)
}

func jsonBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m), string(b))
	return m
}

func TestServesSearchFixture(t *testing.T) {
	srv := start(t)
	resp := get(t, srv.URL+"/api/v1/artworks/search?q=monet")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	body := jsonBody(t, resp)
	assert.IsType(t, []any{}, body["data"])
	assert.Contains(t, body, "pagination")
}

func TestServesArtworkFixtureAndUnknown404(t *testing.T) {
	srv := start(t)
	served := false
	for _, id := range knownIDs {
		resp := get(t, srv.URL+"/api/v1/artworks/"+id)
		if resp.StatusCode != http.StatusOK {
			continue
		}
		served = true
		data, _ := jsonBody(t, resp)["data"].(map[string]any)
		require.NotNil(t, data, "artwork response has a data object")
		assert.EqualValues(t, mustNum(t, id), data["id"])
		break
	}
	assert.True(t, served, "none of the known ids %v is served with 200", knownIDs)

	resp := get(t, srv.URL+"/api/v1/artworks/"+unknownID)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func mustNum(t *testing.T, s string) float64 {
	t.Helper()
	var f float64
	require.NoError(t, json.Unmarshal([]byte(s), &f))
	return f
}

func TestProbeEndpoint(t *testing.T) {
	srv := start(t)
	resp := get(t, srv.URL+"/api/v1/artworks?limit=1&fields=id")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestCtl429SendsRetryAfter(t *testing.T) {
	srv := start(t)
	ctl(t, srv, "429")
	resp := get(t, srv.URL+"/api/v1/artworks/search?q=monet")
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "1", resp.Header.Get("Retry-After"))
}

func TestCtl503(t *testing.T) {
	srv := start(t)
	ctl(t, srv, "503")
	assert.Equal(t, http.StatusServiceUnavailable, get(t, srv.URL+"/api/v1/artworks/search?q=monet").StatusCode)
	assert.Equal(t, http.StatusServiceUnavailable, get(t, srv.URL+"/api/v1/artworks/"+knownIDs[0]).StatusCode)
}

func TestCtlSlowConfigurableDelay(t *testing.T) {
	const delay = 200 * time.Millisecond
	srv := start(t, stubserver.WithSlow(delay))
	ctl(t, srv, "slow")
	begin := time.Now()
	resp := get(t, srv.URL+"/api/v1/artworks/search?q=monet")
	assert.GreaterOrEqual(t, time.Since(begin), delay)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "slow still answers")
}

func TestCtlSlowDefaultJustOverTwoSeconds(t *testing.T) {
	srv := start(t)
	ctl(t, srv, "slow")
	begin := time.Now()
	resp := get(t, srv.URL+"/api/v1/artworks/search?q=monet")
	elapsed := time.Since(begin)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Greater(t, elapsed, 2*time.Second, "must exceed the 2s attempt timeout")
	assert.Less(t, elapsed, 4*time.Second)
}

func TestCtlTimeoutHangsUntilClientGivesUp(t *testing.T) {
	srv := start(t)
	ctl(t, srv, "timeout")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/artworks/search?q=monet", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	// srv.Close in cleanup must not hang: the handler returns once the client is gone.
}

func TestCtlClearRestoresService(t *testing.T) {
	srv := start(t)
	ctl(t, srv, "503")
	require.Equal(t, http.StatusServiceUnavailable, get(t, srv.URL+"/api/v1/artworks/search?q=monet").StatusCode)
	ctl(t, srv, "clear")
	assert.Equal(t, http.StatusOK, get(t, srv.URL+"/api/v1/artworks/search?q=monet").StatusCode)
}
