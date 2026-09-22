# ADR 0001: Expose an OpenAI-compatible API

- Status: Accepted
- Date: 2026-09-22

## Context

Teams already use OpenAI SDKs across many languages. A gateway that invents its
own protocol forces every caller to change code, which kills adoption.

## Decision

Sluice implements the OpenAI HTTP surface (`/v1/chat/completions`,
`/v1/embeddings`, `/v1/models`), including SSE streaming and the standard error
envelope. Sluice-specific signals are conveyed through response headers
(`X-Sluice-Cache`, `X-Sluice-Provider`) so the response *body* stays a faithful
OpenAI response.

## Consequences

- Any OpenAI client works by changing only the base URL — near-zero migration.
- Non-OpenAI upstreams (e.g. Anthropic) are adapted to this surface internally.
- We inherit some OpenAI quirks (e.g. the polymorphic `input` field), handled in
  `provider` with a custom JSON unmarshaller.
