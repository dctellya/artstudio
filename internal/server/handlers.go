package server

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dctellya/artstudio/api"
	"github.com/dctellya/artstudio/internal/httpapi"
	"github.com/dctellya/artstudio/internal/ports"
)

// Contract defaults and bounds of GET /art/{query}; they mirror api/openapi.yaml.
const (
	defaultLimit = 12
	defaultPage  = 1
	// maxWindow is the vendor's result window: limit x page above it is rejected with 403.
	maxWindow = 1000
)

//go:embed swaggerui
var swaggerUI embed.FS

const swaggerIndex = "swaggerui/index.html"

var assetTypes = map[string]string{
	".css": "text/css; charset=utf-8",
	".js":  "text/javascript; charset=utf-8",
}

// handlers implements httpapi.ServerInterface.
type handlers struct {
	s *Server
}

var _ httpapi.ServerInterface = handlers{}

func (h handlers) SearchArt(w http.ResponseWriter, r *http.Request, query string, params httpapi.SearchArtParams) {
	limit, page := defaultLimit, defaultPage
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Page != nil {
		page = *params.Page
	}
	if limit*page > maxWindow {
		writeProblem(w, r, http.StatusBadRequest,
			"limit x page must not exceed "+strconv.Itoa(maxWindow))
		return
	}
	res, meta, err := h.s.lookup.Search(r.Context(), ports.SearchQuery{Q: normalise(query), Limit: limit, Page: page})
	if err != nil {
		h.s.writeLookupError(w, r, meta, err)
		return
	}
	items := make([]httpapi.Artwork, len(res.Items))
	for i, a := range res.Items {
		items[i] = toArtwork(a)
	}
	setMetaHeaders(w, r, meta)
	writeJSON(w, r, http.StatusOK, httpapi.SearchResponse{
		Data:       items,
		Pagination: httpapi.Pagination{Total: res.Total, Limit: res.Limit, Page: res.Page},
		Meta:       toMeta(r.Context(), meta),
	})
}

func (h handlers) GetArtwork(w http.ResponseWriter, r *http.Request, id int) {
	art, meta, err := h.s.lookup.Artwork(r.Context(), id)
	if err != nil {
		h.s.writeLookupError(w, r, meta, err)
		return
	}
	setMetaHeaders(w, r, meta)
	writeJSON(w, r, http.StatusOK, httpapi.ArtworkResponse{Data: toArtwork(art), Meta: toMeta(r.Context(), meta)})
}

func (h handlers) GetHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, httpapi.Health{Status: httpapi.HealthStatusOk})
}

// vendorCheck never gates readiness: the service degrades to stale data instead.
const vendorCheck = "vendor"

func (h handlers) GetReady(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]httpapi.ReadyCheck, len(h.s.checkers))
	status, code := httpapi.ReadyStatusOk, http.StatusOK
	for _, c := range h.s.checkers {
		res := c.Check(r.Context())
		checks[c.Name()] = httpapi.ReadyCheck{Ok: res.OK, AdditionalProperties: res.Detail}
		if !res.OK && c.Name() != vendorCheck {
			status, code = httpapi.ReadyStatusUnavailable, http.StatusServiceUnavailable
		}
	}
	writeJSON(w, r, code, httpapi.Ready{Status: status, Checks: checks})
}

func (h handlers) GetVersion(w http.ResponseWriter, r *http.Request) {
	v := h.s.version
	writeJSON(w, r, http.StatusOK, httpapi.VersionInfo{
		Version: v.Version, Commit: v.Commit, GoVersion: v.GoVersion, BuildDate: v.BuildDate,
	})
}

func (h handlers) GetOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(api.Spec)
}

func (h handlers) GetDocs(w http.ResponseWriter, r *http.Request) {
	serveEmbedded(w, r, swaggerIndex, "text/html; charset=utf-8")
}

func (h handlers) GetDocsAsset(w http.ResponseWriter, r *http.Request, file httpapi.GetDocsAssetParamsFile) {
	name := string(file)
	serveEmbedded(w, r, path.Join("swaggerui", name), assetTypes[path.Ext(name)])
}

func serveEmbedded(w http.ResponseWriter, r *http.Request, name, contentType string) {
	b, err := fs.ReadFile(swaggerUI, name)
	if err != nil {
		writeProblem(w, r, http.StatusNotFound, "no such document")
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(b)
}

// writeLookupError maps ports errors to the HTTP contract (SPEC §5). The detail
// is fixed text: the error's own text may hold a vendor payload and is never sent.
func (s *Server) writeLookupError(w http.ResponseWriter, r *http.Request, meta ports.Meta, err error) {
	if meta.Cache != "" {
		setMetaHeaders(w, r, meta)
	}
	var ra ports.RetryAfterError
	switch {
	case errors.Is(err, ports.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "the artwork was not found")
	case errors.Is(err, ports.ErrBadUpstream):
		writeProblem(w, r, http.StatusBadGateway, "the upstream service returned an unusable response")
	case errors.Is(err, ports.ErrBudgetExceeded):
		writeProblem(w, r, http.StatusGatewayTimeout, "the upstream service did not answer in time")
	case errors.Is(err, ports.ErrBreakerOpen), errors.Is(err, ports.ErrBulkheadFull),
		errors.Is(err, ports.ErrLimiterRejected), errors.Is(err, ports.ErrUpstreamRateLimited),
		errors.As(err, &ra):
		setRetryAfter(w, s.retryAfter(err))
		writeProblem(w, r, http.StatusServiceUnavailable, "the upstream service is temporarily unavailable")
	default:
		writeProblem(w, r, http.StatusInternalServerError, "internal error")
	}
}

// retryAfter is the wait the error asks for; else the breaker's open timeout
// when the breaker is open, else the limiter's maximum wait.
func (s *Server) retryAfter(err error) time.Duration {
	var ra ports.RetryAfterError
	switch {
	case errors.As(err, &ra):
		return ra.RetryAfter()
	case errors.Is(err, ports.ErrBreakerOpen):
		return s.cfg.BreakerOpenTimeout
	default:
		return s.cfg.UpstreamLimiterMaxWait
	}
}

func setMetaHeaders(w http.ResponseWriter, r *http.Request, m ports.Meta) {
	cache := string(m.Cache)
	w.Header().Set("X-Cache", cache)
	w.Header().Set("Server-Timing", "upstream;dur="+strconv.FormatInt(m.UpstreamLatency.Milliseconds(), 10))
	infoFrom(r.Context()).cache = cache
}

func toMeta(ctx context.Context, m ports.Meta) httpapi.Meta {
	return httpapi.Meta{
		RequestId:         ports.RequestIDFrom(ctx),
		Cache:             httpapi.CacheStatus(m.Cache),
		UpstreamLatencyMs: int(m.UpstreamLatency.Milliseconds()),
	}
}

func toArtwork(a ports.Artwork) httpapi.Artwork {
	out := httpapi.Artwork{Id: a.ID, Title: a.Title, Artist: a.Artist, Date: a.Date, ImageId: a.ImageID}
	if a.ImageURL != "" {
		out.ImageUrl = &a.ImageURL
	}
	return out
}

// normalise trims, lower-cases and collapses whitespace (ports.SearchQuery.Q).
func normalise(q string) string {
	return strings.Join(strings.Fields(strings.ToLower(q)), " ")
}
