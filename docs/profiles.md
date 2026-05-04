# Profiles — multi-provider routing for marc

Status: spec, not yet implemented.

## Goal

Let one `marc` install talk to multiple LLM providers (Anthropic, Minimax, OpenAI, etc.) with AWS-style profile selection:

```
marc --profile minimax -p "..."
marc --profile anthropic --continue
marc -p "..."                          # uses default_profile
MARC_PROFILE=minimax marc --continue   # env override
```

All profiles still flow through a **single marc proxy listener**, capturing to a **single shared `capture.jsonl`** with a `profile` field per event.

## Non-goals

- Multiple proxy daemons / multiple ports — explicitly rejected (the user picked single-port).
- Per-profile capture files — the denoise pipeline already supports a `profile` column; one file is simpler.

## Config schema

`~/.marc/config.toml`:

```toml
default_profile = "anthropic"

[profiles.anthropic]
base_url      = "https://api.anthropic.com"
api_key_env   = "ANTHROPIC_API_KEY"
auth_style    = "x-api-key"        # "x-api-key" | "bearer"
proxy_capture = true               # write to capture.jsonl

[profiles.minimax]
base_url      = "https://api.minimax.chat/v1"
api_key_env   = "MINIMAX_API_KEY"
auth_style    = "bearer"
proxy_capture = true

# Optional per-profile header overrides — useful for providers that
# require pinned anthropic-version, model aliases, etc.
[profiles.minimax.header_overrides]
"anthropic-version" = "2023-06-01"

[proxy]
listen_addr = "127.0.0.1:8082"     # single listener, all profiles share it

[paths]
capture_file = "~/.marc/capture.jsonl"
```

**Backward compatibility:** if `[profiles]` is absent, `LoadClient` synthesizes `profiles["anthropic"]` from the legacy `[proxy]` + `[anthropic]` blocks. Existing configs keep working with zero edit.

## Profile resolution — AWS precedence

1. Explicit `--profile <name>` flag in `marc`'s argv
2. `MARC_PROFILE` environment variable
3. `default_profile` field in config
4. Hardcoded fallback `"anthropic"`

If the resolved name doesn't exist in `profiles` → exit non-zero with a clear message listing available profile names.

## Single-port routing — path prefix

The proxy listens on **one port** (`proxy.listen_addr`). Routing happens via the URL path:

```
Client (claude) sends to:
  POST http://127.0.0.1:8082/<profile-name>/v1/messages
                              ^^^^^^^^^^^^^
                              prefix added by clauderun.Run via ANTHROPIC_BASE_URL

Proxy strips prefix, forwards to:
  POST <profiles[name].base_url>/v1/messages
```

Implementation:

- `clauderun.Run` sets `ANTHROPIC_BASE_URL=http://127.0.0.1:8082/<profile>` for the spawned `claude` process.
- `internal/proxy` parses the leading path segment, looks up the profile, rewrites:
  - target URL = `profile.base_url` + remaining path
  - auth header style per `profile.auth_style`:
    - `x-api-key`: forward incoming `x-api-key` header verbatim (current behavior)
    - `bearer`: replace `x-api-key` with `Authorization: Bearer <api_key>` from env
  - applies `header_overrides` last (so providers like Minimax that need pinned versions stay pinned)
- If the path's first segment doesn't match any profile → 404 with a JSON error body listing available profiles.

**Trade-off accepted:** the proxy now does light path manipulation. ~30 LOC in the handler.

## Capture format — unchanged

`~/.marc/capture.jsonl` keeps its current schema. **Profile is NOT recorded** in the capture event.

Reasoning: the captured corpus is the input to denoise → retrieval → active-learning question generation. Tagging events by provider would let downstream consumers (intentionally or not) condition on `profile`, biasing the corpus toward provider-specific patterns. A captured event is "what the LLM said in this conversation" regardless of which LLM. The active-learning loop should learn from the conversation, not from which API key paid for it.

Profile selection therefore lives entirely on the **client side**:
- `clauderun.Run` resolves it
- The proxy uses it to pick upstream + auth style
- It never makes it into capture events, MinIO blobs, or ClickHouse

If someone later wants per-provider analytics, they can timestamp-correlate against an external log (e.g., proxy access log), not by mining the corpus.

**No server-side changes whatsoever.** marc-server, `internal/process/process.go`, ClickHouse schema all stay untouched.

## Implementation phases

| Phase | LOC | Files | Deliverable |
|---|---|---|---|
| 1 | ~100 | `internal/config/client.go`, `_test.go` | Profile types + auto-migration of legacy config |
| 2 | ~80  | `internal/clauderun/profile.go`, `_test.go` | `ParseProfileFlag` + `ResolveProfile` |
| 3 | ~30  | `internal/clauderun/clauderun.go`, `cmd/marc/main.go` | `clauderun.Run` uses resolved profile; passthrough whitelists `--profile` |
| 4 | ~120 | `internal/proxy/handler.go` | Path-prefix routing + per-profile auth_style + header_overrides |
| 5 | ~40  | `internal/doctor/doctor.go` | Doctor checks each profile resolves and base_url is reachable |
| 6 | ~50  | `README.md`, `CHANGELOG.md`, `docs/profiles.md` | Document end-to-end |

**Total: ~420 LOC + tests. Client-side only.** No marc-server changes, no ClickHouse migration, no capture schema change. The captured corpus stays provider-agnostic by design.

Phases 1–3 ship without proxy changes — they hit the existing single Anthropic upstream regardless of profile. Useful as a first-PR milestone (client-side resolution + flag parsing). Phase 4 is the one that actually unlocks Minimax/OpenAI; can ship as second PR.

## Edge cases / decisions

- **Profile name in URL path is lowercased.** Profile lookup is case-insensitive in `ResolveProfile`; the path always uses the lowercase canonical name.
- **Streaming responses (SSE)**: path rewriting happens once on request entry, then the existing streaming forwarder runs unchanged. No per-chunk overhead.
- **Errors during forward**: capture event still gets written (with `error` field) so we don't lose the request record. Same as today.
- **`--profile` with unknown name**: exit code 2, error message lists known profiles. Don't silently fall back.
- **Conflict with claude's own flags**: `--profile` is not a claude flag (verified against current claude CLI). Safe to intercept.
- **API key per profile**: `api_key_env` is preferred (read at proxy start). `api_key` inline supported for testing, refused if config file is mode > 0600.

## Migration for the operator

- Existing `~/.marc/config.toml` keeps working — auto-migration synthesizes `profiles.anthropic` on first load.
- To add Minimax: append the `[profiles.minimax]` block, set `MINIMAX_API_KEY` env, restart `marc proxy`.
- No env var changes needed in shell rcs — `marc` keeps reading `ANTHROPIC_BASE_URL`-style overrides via `clauderun.Run`.

## Testing strategy

- Unit tests for `ParseProfileFlag`: `--profile X`, `--profile=X`, missing, after other args, with `--continue`.
- Unit tests for `ResolveProfile`: each precedence level, unknown profile, empty default.
- Integration test: spin up two fake upstream servers, configure two profiles, verify path-prefix routing reaches the right one and capture event has correct `profile`.
- Doctor: profile listener + DNS resolve + TCP dial each `base_url`.

## What this enables

- Run `marc --profile minimax -p` for cheap exploratory work, `marc --profile anthropic` for production drafts — both captured to one corpus.
- Future: A/B comparison of providers on the same prompts (the corpus has both, the denoise pipeline can join on session).
- Future: per-profile rate limits / circuit breakers in the proxy without touching the client.

---

End of spec. Ready to implement when prioritized — Phases 1–3 unblock the client-side flag (no proxy work needed). Phase 4 is the actual provider switch.
