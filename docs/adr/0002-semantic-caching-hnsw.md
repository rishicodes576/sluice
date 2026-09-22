# ADR 0002: Semantic caching backed by an in-repo HNSW index

- Status: Accepted
- Date: 2026-09-22

## Context

Exact-match caching of LLM prompts almost never hits — real prompts vary by
whitespace, punctuation and phrasing. Caching on *meaning* captures far more
reuse, cutting cost and tail latency. This requires nearest-neighbour search
over embeddings, which must stay fast as the cache grows.

## Decision

- Embed prompts to unit vectors and cache on **cosine similarity ≥ threshold**
  (default 0.95), namespaced by model.
- Implement **HNSW** (Hierarchical Navigable Small World graphs) from scratch for
  approximate NN search — logarithmic-ish query time with high recall — with an
  exact brute-force `Flat` index as a reference/fallback for small sets.
- Ship a deterministic **local embedder** (feature hashing over unigrams+bigrams)
  so the cache works offline and in CI; production can swap in a provider-backed
  embedder behind the `embed.Embedder` interface.

## Consequences

- Cache hits return in microseconds and avoid an upstream call entirely.
- Recall is tunable via `M` / `efConstruction`; correctness is guarded by a
  recall test against exact search (`≥ 0.90 recall@1`).
- The local embedder is a demonstration-grade approximation of semantics; for
  production quality, configure a real embedding model.
