// Package iac parses and validates the v2 .dev/config.yaml schema:
// the user-facing infrastructure-as-code declaration that drives
// env-manager's deploy pipeline.
//
// One Config per repo. The parser is the single source of truth for
// the schema; downstream packages (services, hooks, proxy/labels) consume
// the typed result.
//
// All validation errors wrap ErrInvalidConfig so callers can use errors.Is.
package iac
