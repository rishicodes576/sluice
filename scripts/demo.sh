#!/usr/bin/env bash
# End-to-end local demo: starts the gateway with the mock provider (no API keys
# needed), sends two identical prompts to show a semantic cache hit, then prints
# cache stats. Requires: a built ./bin/sluice (run `make build`) and curl.
set -euo pipefail

ADDR="127.0.0.1:8080"
BIN="${BIN:-./bin/sluice}"

"$BIN" serve --addr "$ADDR" &
PID=$!
trap 'kill "$PID" 2>/dev/null || true' EXIT
sleep 1

BODY='{"model":"mock-1","messages":[{"role":"user","content":"What is the capital of France?"}]}'

echo "== First request (expect cache MISS) =="
curl -sS -D - -o /dev/null "http://$ADDR/v1/chat/completions" -d "$BODY" | grep -i "x-sluice-cache" || true

echo "== Second identical request (expect cache HIT) =="
curl -sS -D - -o /dev/null "http://$ADDR/v1/chat/completions" -d "$BODY" | grep -i "x-sluice-cache" || true

echo "== Streaming request =="
curl -sS -N "http://$ADDR/v1/chat/completions" \
  -d '{"model":"mock-1","stream":true,"messages":[{"role":"user","content":"stream a few words"}]}' | head -n 5

echo
echo "== Cache stats =="
curl -sS "http://$ADDR/admin/stats"
echo
