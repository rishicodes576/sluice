import { describe, it, expect } from "vitest";
import { Sluice, SluiceError } from "../src/index.js";

/** Build a fake fetch that returns a canned Response. */
function fakeFetch(status: number, body: string, headers: Record<string, string> = {}) {
  return async () =>
    new Response(body, { status, headers: { "content-type": "application/json", ...headers } });
}

describe("Sluice client", () => {
  it("parses a chat completion and cache header", async () => {
    const body = JSON.stringify({
      id: "1",
      object: "chat.completion",
      created: 0,
      model: "mock-1",
      choices: [{ index: 0, message: { role: "assistant", content: "hi" }, finish_reason: "stop" }],
      usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
    });
    const client = new Sluice({
      fetch: fakeFetch(200, body, { "x-sluice-cache": "hit", "x-sluice-provider": "mock" }) as typeof fetch,
    });
    const res = await client.chat({ model: "mock-1", messages: [{ role: "user", content: "hey" }] });
    expect(res.choices[0].message.content).toBe("hi");
    expect(res.cached).toBe(true);
    expect(res.provider).toBe("mock");
  });

  it("throws SluiceError on error responses", async () => {
    const client = new Sluice({
      fetch: fakeFetch(429, JSON.stringify({ error: { message: "slow down", type: "rate_limit" } })) as typeof fetch,
    });
    await expect(
      client.chat({ model: "m", messages: [{ role: "user", content: "x" }] }),
    ).rejects.toMatchObject({ status: 429, type: "rate_limit" } satisfies Partial<SluiceError>);
  });

  it("streams content deltas", async () => {
    const sse =
      'data: {"choices":[{"delta":{"content":"Hello "}}]}\n\n' +
      'data: {"choices":[{"delta":{"content":"world"}}]}\n\n' +
      "data: [DONE]\n\n";
    const client = new Sluice({
      fetch: (async () => new Response(sse, { status: 200 })) as typeof fetch,
    });
    let out = "";
    for await (const delta of client.stream({ model: "m", messages: [{ role: "user", content: "x" }] })) {
      out += delta;
    }
    expect(out).toBe("Hello world");
  });
});
