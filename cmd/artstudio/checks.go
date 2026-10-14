package main

import (
	"context"

	"github.com/dctellya/artstudio/internal/ports"
)

// configChecker reports the configuration; it was validated at start, so it is always OK.
type configChecker struct{}

func (configChecker) Name() string { return "config" }

func (configChecker) Check(context.Context) ports.CheckResult {
	return ports.CheckResult{OK: true}
}

// disabledChecker stands in for a dependency that is switched off by configuration.
type disabledChecker struct{ name string }

func (d disabledChecker) Name() string { return d.name }

func (disabledChecker) Check(context.Context) ports.CheckResult {
	return ports.CheckResult{OK: true, Detail: map[string]any{"enabled": false}}
}
