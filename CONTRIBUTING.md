# Contributing to Sluice

Thanks for your interest! Contributions of all kinds are welcome.

## Development setup

- Go 1.22+ (no third-party Go modules are required — the core is stdlib-only).
- Optional: Node 18+ (TypeScript SDK), Ruby 3.0+ (Ruby SDK), Docker, k6.

```bash
make build      # build the binary
make test       # unit + integration tests
make race       # tests under the race detector
make cover      # aggregate coverage
make lint       # golangci-lint + go vet
make demo       # run a local end-to-end demo
```

## Guidelines

- Keep the **core dependency-free**. New third-party Go modules need a strong
  justification in the PR description (see [ADR 0003](docs/adr/0003-zero-dependency-core.md)).
- Add or update tests for any behaviour change; keep coverage healthy.
- Run `gofmt`/`goimports`; CI enforces formatting and linting.
- Conventional commit messages are appreciated (`feat:`, `fix:`, `docs:`…).
- For user-facing changes, add a line to `CHANGELOG.md` under *Unreleased*.

## Proposing larger changes

Open an issue describing the problem and approach before a large PR. Significant
architectural decisions are recorded as ADRs in `docs/adr/`.
