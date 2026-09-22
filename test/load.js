// k6 load test for Sluice. Run against a locally running gateway:
//   k6 run test/load.js
//
// It mixes a small set of prompts so the semantic cache is exercised: repeated
// prompts should serve from cache and drive the hit ratio up over the run.
import http from "k6/http";
import { check, sleep } from "k6";
import { Rate } from "k6/metrics";

const cacheHits = new Rate("sluice_cache_hits");

export const options = {
  scenarios: {
    ramp: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "15s", target: 20 },
        { duration: "30s", target: 50 },
        { duration: "15s", target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

const BASE = __ENV.SLUICE_URL || "http://localhost:8080";
const PROMPTS = [
  "What is the capital of France?",
  "Explain semantic caching in one sentence.",
  "Write a haiku about databases.",
  "What is the capital of France?", // repeat -> cache hit
];

export default function () {
  const prompt = PROMPTS[Math.floor(Math.random() * PROMPTS.length)];
  const res = http.post(
    `${BASE}/v1/chat/completions`,
    JSON.stringify({ model: "mock-1", messages: [{ role: "user", content: prompt }] }),
    { headers: { "Content-Type": "application/json" } },
  );
  check(res, { "status is 200": (r) => r.status === 200 });
  cacheHits.add(res.headers["X-Sluice-Cache"] === "hit");
  sleep(0.1);
}
