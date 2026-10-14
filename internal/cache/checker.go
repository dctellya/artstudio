package cache

import (
	"context"

	"github.com/dctellya/artstudio/internal/ports"
)

// Checker reports the cache in /ready.
type Checker struct{ c *LRU }

var _ ports.Checker = (*Checker)(nil)

// Checker returns the /ready check for this cache.
func (c *LRU) Checker() *Checker { return &Checker{c: c} }

// Name is "cache".
func (*Checker) Name() string { return "cache" }

// Check reports entries and capacity; the cache is in-process, so it is always OK.
func (k *Checker) Check(context.Context) ports.CheckResult {
	return ports.CheckResult{OK: true, Detail: map[string]any{
		"entries": k.c.Len(), "max_entries": k.c.cfg.CacheMaxEntries,
	}}
}
