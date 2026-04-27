// Package client implements the interactive setup wizard for the marc client
// binary. It writes ~/.marc/config.toml (mode 0600) and validates the resulting
// configuration through four ordered steps: DNS resolution, TLS verification,
// credential authentication, and bucket write access.
//
// Import graph rule: this package must only import internal/config,
// internal/minioclient, and internal/jsonl. It must never import
// internal/clickhouse, internal/sqlitedb, internal/ollama, or
// internal/telegram so that cmd/marc stays clean.
package client
