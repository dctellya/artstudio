package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/dctellya/artstudio/internal/httpapi"
)

const (
	problemType        = "about:blank"
	problemContentType = "application/problem+json"
	// minRetryAfterSeconds is the granularity of the Retry-After header.
	minRetryAfterSeconds = 1
)

// writeProblem writes an RFC 9457 problem. detail is always server-authored text:
// vendor error text never reaches it.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	p := httpapi.Problem{
		Type:      problemType,
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Instance:  r.URL.Path,
		RequestId: w.Header().Get(headerRequestID),
	}
	body, err := json.Marshal(p)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeJSON marshals v before writing so a failure can still become a problem.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "response could not be encoded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// setRetryAfter sets Retry-After as whole seconds, rounded up, at least one.
func setRetryAfter(w http.ResponseWriter, d time.Duration) {
	secs := int((d + time.Second - 1) / time.Second)
	if secs < minRetryAfterSeconds {
		secs = minRetryAfterSeconds
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
}
