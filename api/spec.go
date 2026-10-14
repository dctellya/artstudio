// Package api embeds the OpenAPI document, the source of truth of the HTTP contract.
package api

import _ "embed"

// Spec is api/openapi.yaml.
//
//go:embed openapi.yaml
var Spec []byte
