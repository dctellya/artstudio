package cache

import (
	"fmt"
	"strings"

	"github.com/dctellya/artstudio/internal/ports"
)

// NormalizeQuery trims, lowercases and collapses runs of whitespace.
func NormalizeQuery(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(q), " "))
}

// SearchKey is the cache key of a search page.
func SearchKey(q ports.SearchQuery) string {
	return fmt.Sprintf("search:v1:%s:%d:%d", NormalizeQuery(q.Q), q.Limit, q.Page)
}

// ArtworkKey is the cache key of one artwork.
func ArtworkKey(id int) string {
	return fmt.Sprintf("artwork:v1:%d", id)
}
