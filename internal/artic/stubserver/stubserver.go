// Package stubserver is a fake of the vendor API with fault injection, used
// by the chaos profile. It serves plain http on the internal network only.
package stubserver

import (
	"embed"
	"io/fs"
	"net/http"
	"sync"
	"time"
)

//go:embed fixtures
var fixtureFS embed.FS

// defaultSlow exceeds the 2s upstream attempt timeout (SPEC §5 config).
const defaultSlow = 2500 * time.Millisecond

// Option configures the stub.
type Option func(*Stub)

// WithSlow sets the delay applied by the "slow" fault.
func WithSlow(d time.Duration) Option { return func(s *Stub) { s.slow = d } }

// Stub is the http.Handler.
type Stub struct {
	mux  *http.ServeMux
	slow time.Duration

	mu    sync.RWMutex
	fault string
}

// New builds the stub handler.
func New(opts ...Option) http.Handler {
	s := &Stub{mux: http.NewServeMux(), slow: defaultSlow}
	for _, o := range opts {
		o(s)
	}
	s.mux.HandleFunc("GET /api/v1/artworks/search", s.fixture("fixtures/search.json"))
	s.mux.HandleFunc("GET /api/v1/artworks/{id}", s.artwork)
	s.mux.HandleFunc("GET /api/v1/artworks", s.fixture("fixtures/probe.json"))
	s.mux.HandleFunc("POST /__ctl", s.ctl)
	return s
}

func (s *Stub) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Stub) ctl(w http.ResponseWriter, r *http.Request) {
	f := r.URL.Query().Get("fault")
	switch f {
	case "429", "503", "slow", "timeout":
	case "clear":
		f = ""
	default:
		http.Error(w, "unknown fault", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.fault = f
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// injected applies the active fault; it reports true when the request is done.
func (s *Stub) injected(w http.ResponseWriter, r *http.Request) bool {
	s.mu.RLock()
	f := s.fault
	s.mu.RUnlock()
	switch f {
	case "429":
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, []byte(`{"status":429,"error":"Too many requests"}`))
		return true
	case "503":
		writeJSON(w, http.StatusServiceUnavailable, []byte(`{"status":503,"error":"Service unavailable"}`))
		return true
	case "timeout":
		<-r.Context().Done()
		return true
	case "slow":
		select {
		case <-time.After(s.slow):
		case <-r.Context().Done():
			return true
		}
	}
	return false
}

func (s *Stub) fixture(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.injected(w, r) {
			return
		}
		serve(w, name)
	}
}

func (s *Stub) artwork(w http.ResponseWriter, r *http.Request) {
	if s.injected(w, r) {
		return
	}
	id := r.PathValue("id")
	for _, c := range id { // ids are digits only: keeps the embed lookup path-safe
		if c < '0' || c > '9' {
			notFound(w)
			return
		}
	}
	serve(w, "fixtures/artwork-"+id+".json")
}

func serve(w http.ResponseWriter, name string) {
	b, err := fs.ReadFile(fixtureFS, name)
	if err != nil {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, []byte(`{"status":404,"error":"Not found","detail":"The item you requested cannot be found."}`))
}

func writeJSON(w http.ResponseWriter, code int, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(b) //nolint:gosec // G705: embedded fixtures and constants served as application/json
}
