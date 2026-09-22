# ADR 0003: Zero third-party dependencies in the core

- Status: Accepted
- Date: 2026-09-22

## Context

Sluice runs on the critical path of production LLM traffic. Every dependency is
attack surface (supply chain), operational risk (CVEs, breaking changes) and
build friction. Go's standard library is unusually capable for HTTP services.

## Decision

The Go core uses **only the standard library**. Where a feature would normally
pull in a module, we implement a small, focused, unit-tested version in-repo:

- **Metrics**: a Prometheus text-exposition registry (`observability`).
- **Config**: a YAML-subset parser that decodes into typed structs via JSON
  (`config`), plus `${ENV}` expansion.
- **Vector search**: HNSW + brute-force cosine (`vectorstore`).
- **CLI**: `flag`-based subcommands (no cobra).

Client SDKs (TypeScript, Ruby) are likewise dependency-free at runtime.

## Consequences

- `go.mod` has no `require` block and there is no `go.sum` to audit — trivial,
  reproducible builds; distroless-static images.
- We own more code (parsers, metrics). This is deliberate: it's small, tested,
  and exactly fits our needs. New dependencies require justification in review.
- Some ecosystem niceties (full OTLP tracing, a YAML superset) are out of scope;
  tracing is represented by propagated trace ids and structured logs.
