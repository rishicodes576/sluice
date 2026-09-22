/**
 * Sluice TypeScript client.
 *
 * A tiny, dependency-free client for the Sluice gateway's OpenAI-compatible
 * API. Works in Node 18+ and modern browsers (uses the global `fetch`).
 */

export interface Message {
  role: "system" | "user" | "assistant";
  content: string;
}

export interface ChatRequest {
  model: string;
  messages: Message[];
  temperature?: number;
  max_tokens?: number;
  user?: string;
}

export interface Usage {
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
}

export interface ChatResponse {
  id: string;
  object: string;
  created: number;
  model: string;
  choices: { index: number; message: Message; finish_reason: string }[];
  usage: Usage;
}

export interface ChatResult extends ChatResponse {
  /** True when the gateway served this from its semantic cache. */
  cached: boolean;
  /** The upstream provider that served the request (from X-Sluice-Provider). */
  provider: string;
}

export interface EmbeddingResponse {
  object: string;
  data: { object: string; index: number; embedding: number[] }[];
  model: string;
  usage: Usage;
}

export interface SluiceOptions {
  /** Base URL of the gateway, e.g. "http://localhost:8080". */
  baseURL?: string;
  /** API key sent as `Authorization: Bearer <key>`. */
  apiKey?: string;
  /** Custom fetch implementation (useful for tests). */
  fetch?: typeof fetch;
}

/** Error thrown for non-2xx responses, carrying the gateway's error envelope. */
export class SluiceError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly type?: string,
  ) {
    super(message);
    this.name = "SluiceError";
  }
}

export class Sluice {
  private readonly baseURL: string;
  private readonly apiKey?: string;
  private readonly fetchImpl: typeof fetch;

  constructor(opts: SluiceOptions = {}) {
    this.baseURL = (opts.baseURL ?? "http://localhost:8080").replace(/\/$/, "");
    this.apiKey = opts.apiKey;
    this.fetchImpl = opts.fetch ?? globalThis.fetch;
    if (!this.fetchImpl) {
      throw new Error("no fetch implementation available; pass options.fetch");
    }
  }

  private headers(): Record<string, string> {
    const h: Record<string, string> = { "content-type": "application/json" };
    if (this.apiKey) h["authorization"] = `Bearer ${this.apiKey}`;
    return h;
  }

  private async request(path: string, body: unknown, stream = false): Promise<Response> {
    const res = await this.fetchImpl(`${this.baseURL}${path}`, {
      method: "POST",
      headers: this.headers(),
      body: JSON.stringify(stream ? { ...(body as object), stream: true } : body),
    });
    if (!res.ok) {
      let msg = `request failed with status ${res.status}`;
      let type: string | undefined;
      try {
        const j = (await res.json()) as { error?: { message?: string; type?: string } };
        if (j.error?.message) msg = j.error.message;
        type = j.error?.type;
      } catch {
        /* non-JSON error body */
      }
      throw new SluiceError(msg, res.status, type);
    }
    return res;
  }

  /** Perform a non-streaming chat completion. */
  async chat(req: ChatRequest): Promise<ChatResult> {
    const res = await this.request("/v1/chat/completions", req);
    const data = (await res.json()) as ChatResponse;
    return {
      ...data,
      cached: res.headers.get("x-sluice-cache") === "hit",
      provider: res.headers.get("x-sluice-provider") ?? data.model,
    };
  }

  /** Stream a chat completion, yielding content deltas as they arrive. */
  async *stream(req: ChatRequest): AsyncGenerator<string, void, unknown> {
    const res = await this.request("/v1/chat/completions", req, true);
    if (!res.body) throw new SluiceError("no response body for stream", 500);
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed.startsWith("data:")) continue;
        const payload = trimmed.slice(5).trim();
        if (payload === "[DONE]") return;
        try {
          const chunk = JSON.parse(payload) as {
            choices?: { delta?: { content?: string } }[];
          };
          const delta = chunk.choices?.[0]?.delta?.content;
          if (delta) yield delta;
        } catch {
          /* ignore keep-alive / partial lines */
        }
      }
    }
  }

  /** Create embeddings for one or more inputs. */
  async embeddings(model: string, input: string | string[]): Promise<EmbeddingResponse> {
    const res = await this.request("/v1/embeddings", { model, input });
    return (await res.json()) as EmbeddingResponse;
  }

  /** List models the gateway can serve. */
  async models(): Promise<{ id: string; owned_by: string }[]> {
    const res = await this.fetchImpl(`${this.baseURL}/v1/models`, { headers: this.headers() });
    const data = (await res.json()) as { data: { id: string; owned_by: string }[] };
    return data.data;
  }
}

export default Sluice;
