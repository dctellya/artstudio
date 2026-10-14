// Package skeleton holds the naive walking-skeleton implementations of
// ports.Lookup and ports.Auditor: a plain HTTP client, no cache, no audit.
// Deleted by C at wiring, when internal/lookup, internal/artic and
// internal/audit land.
package skeleton

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dctellya/artstudio/internal/config"
	"github.com/dctellya/artstudio/internal/ports"
)

const (
	userAgentHeader = "AIC-User-Agent"
	fields          = "id,title,artist_display,date_display,image_id"
	maxBody         = 4 << 20
)

// Lookup calls the vendor directly on every request.
type Lookup struct {
	base   string
	agent  string
	client *http.Client
}

var _ ports.Lookup = (*Lookup)(nil)

// NewLookup builds a Lookup from the config; redirects are never followed.
func NewLookup(c config.Config) *Lookup {
	return &Lookup{
		base:  strings.TrimRight(c.UpstreamBaseURL, "/"),
		agent: fmt.Sprintf("artstudio (%s)", c.ContactEmail),
		client: &http.Client{
			Timeout: c.UpstreamBudget,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type vendorArtwork struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Artist  string `json:"artist_display"`
	Date    string `json:"date_display"`
	ImageID string `json:"image_id"`
}

type vendorConfig struct {
	IIIFURL string `json:"iiif_url"`
}

func (v vendorArtwork) toArtwork(iiif string) ports.Artwork {
	a := ports.Artwork{ID: v.ID, Title: v.Title, Artist: v.Artist, Date: v.Date, ImageID: v.ImageID}
	if v.ImageID != "" {
		a.ImageURL = fmt.Sprintf("%s/%s/full/843,/0/default.jpg", iiif, v.ImageID)
	}
	return a
}

func (l *Lookup) get(ctx context.Context, path string, q url.Values, out any) (ports.Meta, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return ports.Meta{}, err
	}
	req.Header.Set(userAgentHeader, l.agent)
	resp, err := l.client.Do(req)
	if err != nil {
		return ports.Meta{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body
	meta := ports.Meta{Cache: ports.CacheBypass, UpstreamLatency: time.Since(start), Attempts: 1}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return meta, ports.ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return meta, ports.ErrUpstreamRateLimited
	case resp.StatusCode != http.StatusOK:
		return meta, ports.ErrBadUpstream
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, maxBody)).Decode(out); err != nil {
		return meta, ports.ErrBadUpstream
	}
	return meta, nil
}

// Search implements ports.Lookup.
func (l *Lookup) Search(ctx context.Context, q ports.SearchQuery) (ports.SearchResult, ports.Meta, error) {
	var body struct {
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
		Data   []vendorArtwork `json:"data"`
		Config vendorConfig    `json:"config"`
	}
	params := url.Values{
		"q":      {q.Q},
		"limit":  {strconv.Itoa(q.Limit)},
		"page":   {strconv.Itoa(q.Page)},
		"fields": {fields},
	}
	meta, err := l.get(ctx, "/artworks/search", params, &body)
	if err != nil {
		return ports.SearchResult{}, meta, err
	}
	res := ports.SearchResult{Items: make([]ports.Artwork, 0, len(body.Data)), Total: body.Pagination.Total, Limit: q.Limit, Page: q.Page}
	for _, a := range body.Data {
		res.Items = append(res.Items, a.toArtwork(body.Config.IIIFURL))
	}
	return res, meta, nil
}

// Artwork implements ports.Lookup.
func (l *Lookup) Artwork(ctx context.Context, id int) (ports.Artwork, ports.Meta, error) {
	var body struct {
		Data   vendorArtwork `json:"data"`
		Config vendorConfig  `json:"config"`
	}
	meta, err := l.get(ctx, "/artworks/"+strconv.Itoa(id), url.Values{"fields": {fields}}, &body)
	if err != nil {
		return ports.Artwork{}, meta, err
	}
	return body.Data.toArtwork(body.Config.IIIFURL), meta, nil
}

// NoopAuditor discards every record.
type NoopAuditor struct{}

var _ ports.Auditor = NoopAuditor{}

// Record implements ports.Auditor.
func (NoopAuditor) Record(ports.VendorCall) {}

// Close implements ports.Auditor.
func (NoopAuditor) Close(context.Context) error { return nil }
