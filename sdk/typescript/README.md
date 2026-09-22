# @sluice/client

TypeScript client for the [Sluice](../../README.md) LLM inference gateway.
Dependency-free, works in Node 18+ and modern browsers.

```bash
npm install @sluice/client
```

```ts
import { Sluice } from "@sluice/client";

const client = new Sluice({ baseURL: "http://localhost:8080", apiKey: process.env.SLUICE_API_KEY });

// Non-streaming — note the `cached` flag surfaced from the gateway.
const res = await client.chat({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Explain HNSW in one sentence." }],
});
console.log(res.choices[0].message.content, "(cached:", res.cached, ")");

// Streaming
for await (const delta of client.stream({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Write a haiku about caches." }],
})) {
  process.stdout.write(delta);
}

// Embeddings
const emb = await client.embeddings("mock-1", ["hello", "world"]);
```

## Development

```bash
npm install
npm test        # vitest
npm run build   # tsup -> dist (ESM + CJS + types)
```
