package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/stretchr/testify/require"

	"github.com/dctellya/artstudio/internal/ports"
)

// The generated router rejects out-of-range parameters with 400 problem+json.
func TestContract_InvalidParamsRejectedWith400Problem(t *testing.T) {
	e := newEnv(t)
	for _, target := range []string{
		"/art/monet?limit=0",
		"/art/monet?limit=51",
		"/art/monet?page=51",
		"/art/monet?page=0",
		"/art/monet?limit=50&page=21", // limit*page > 1000
		"/art/monet?limit=abc",
		"/artworks/0",
		"/artworks/-3",
		"/artworks/abc",
	} {
		t.Run(target, func(t *testing.T) {
			rec := e.get(t, target)
			requireProblem(t, rec, http.StatusBadRequest)
		})
	}
	require.Empty(t, e.lookup.lastCtxIDs, "lookup must not be reached on invalid input")
}

func TestContract_BoundaryParamsAccepted(t *testing.T) {
	e := newEnv(t)
	for _, target := range []string{
		"/art/monet?limit=1&page=1",
		"/art/monet?limit=50&page=20", // exactly 1000
		"/art/monet?limit=20&page=50", // exactly 1000
		"/artworks/1",
	} {
		require.Equal(t, http.StatusOK, e.get(t, target).Code, target)
	}
}

// Every response the server produces validates against api/openapi.yaml.
func TestContract_ResponsesValidateAgainstSchema(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	require.NoError(t, err)
	require.NoError(t, doc.Validate(context.Background()))
	router, err := legacy.NewRouter(doc)
	require.NoError(t, err)

	cases := []struct {
		name   string
		target string
		err    error
		status int
	}{
		{"search 200", "/art/monet?limit=5&page=2", nil, 200},
		{"artwork 200", "/artworks/27992", nil, 200},
		{"artwork 404", "/artworks/1", ports.ErrNotFound, 404},
		{"unavailable 503", "/art/monet", ports.ErrBreakerOpen, 503},
		{"bad upstream 502", "/art/monet", ports.ErrBadUpstream, 502},
		{"budget 504", "/art/monet", ports.ErrBudgetExceeded, 504},
		{"invalid 400", "/art/monet?limit=0", nil, 400},
		{"health", "/health", nil, 200},
		{"ready", "/ready", nil, 200},
		{"version", "/version", nil, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.lookup.err = c.err
			req := httptest.NewRequest(http.MethodGet, c.target, nil)
			rec := httptest.NewRecorder()
			e.h.ServeHTTP(rec, req)
			require.Equal(t, c.status, rec.Code, rec.Body.String())

			route, params, err := router.FindRoute(req)
			require.NoError(t, err)
			in := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: &openapi3filter.RequestValidationInput{
					Request: req, PathParams: params, Route: route,
				},
				Status:  rec.Code,
				Header:  rec.Header(),
				Options: &openapi3filter.Options{IncludeResponseStatus: true},
			}
			in.SetBodyBytes(rec.Body.Bytes())
			require.NoError(t, openapi3filter.ValidateResponse(context.Background(), in))
		})
	}
}

// 400 details are human-readable only: no schema JSON, raw input or Go parser text.
func TestBadRequestDetailLeaksNothing(t *testing.T) {
	e := newEnv(t)
	for _, target := range []string{"/art/monet?limit=0", "/art/monet?limit=abc", "/artworks/abc"} {
		t.Run(target, func(t *testing.T) {
			p := requireProblem(t, e.get(t, target), http.StatusBadRequest)
			detail, _ := p["detail"].(string)
			require.NotEmpty(t, detail)
			for _, leak := range []string{"Schema:", "Value:", "strconv", "parsing"} {
				require.NotContains(t, detail, leak)
			}
		})
	}
}
