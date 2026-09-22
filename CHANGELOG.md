# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-09-22

### Added
- OpenAI-compatible gateway: `/v1/chat/completions` (streaming + non-streaming),
  `/v1/embeddings`, `/v1/models`.
- Semantic response cache with a from-scratch HNSW index and a local embedder;
  configurable similarity threshold and cost-saved accounting.
- Provider adapters: `mock` (offline/deterministic), `openai`, `anthropic`.
- Router with round-robin / weighted / latency (EWMA) / cost strategies,
  circuit breaker and retries with full-jitter backoff.
- API-key auth, token-bucket rate limiting, per-key budgets.
- Prometheus-format metrics, structured logging, request tracing.
- TypeScript and Ruby client SDKs.
- Docker image, docker-compose stack, Helm chart, Grafana dashboard, k6 load test.
