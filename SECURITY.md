# Security Policy

## Reporting a vulnerability

Please report security issues privately via GitHub's **"Report a vulnerability"**
(Security advisories) feature on this repository, rather than opening a public
issue. We aim to acknowledge reports within 72 hours.

## Scope and hardening notes

Sluice sits on the request path for LLM traffic, so it is built defensively:

- **Secrets** are never stored in config files — provider keys are injected via
  `${ENV}` expansion at load time.
- **API keys** are compared in constant time (`crypto/subtle`) to avoid timing
  side channels.
- The container image is **distroless + non-root** with a read-only config mount.
- Panics in handlers are recovered and returned as `500`s rather than crashing
  the process.
- The core has **no third-party Go dependencies**, minimising supply-chain risk.

## Supported versions

The latest minor release receives security fixes.
