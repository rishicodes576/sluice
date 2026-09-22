// Package config defines Sluice's configuration schema and loads it from YAML
// or JSON with environment-variable expansion, sensible defaults and
// validation. The core stays dependency-free: YAML is parsed by a small
// in-repo parser (see yaml.go).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is the top-level gateway configuration.
type Config struct {
	Server    ServerConfig     `json:"server"`
	Logging   LoggingConfig    `json:"logging"`
	Auth      AuthConfig       `json:"auth"`
	Cache     CacheConfig      `json:"cache"`
	Router    RouterConfig     `json:"router"`
	RateLimit RateLimitConfig  `json:"rate_limit"`
	Budget    BudgetConfig     `json:"budget"`
	Providers []ProviderConfig `json:"providers"`
}

// ServerConfig configures the HTTP server.
type ServerConfig struct {
	Addr            string `json:"addr"`
	ReadTimeout     string `json:"read_timeout"`
	WriteTimeout    string `json:"write_timeout"`
	ShutdownTimeout string `json:"shutdown_timeout"`
}

// LoggingConfig configures structured logging.
type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"` // json | text
}

// AuthConfig configures API-key authentication.
type AuthConfig struct {
	Keys []KeyConfig `json:"keys"`
}

// KeyConfig is a single virtual API key.
type KeyConfig struct {
	Secret string `json:"secret"`
	Owner  string `json:"owner"`
}

// CacheConfig configures the semantic response cache.
type CacheConfig struct {
	Enabled   bool    `json:"enabled"`
	Threshold float64 `json:"threshold"`
	Dim       int     `json:"dim"`
	Shards    int     `json:"shards"`
	TTL       string  `json:"ttl"`
	Index     string  `json:"index"` // hnsw | flat
}

// RouterConfig configures routing and resilience.
type RouterConfig struct {
	Strategy string        `json:"strategy"`
	Retry    RetryConfig   `json:"retry"`
	Breaker  BreakerConfig `json:"breaker"`
}

// RetryConfig configures retry backoff.
type RetryConfig struct {
	MaxAttempts int    `json:"max_attempts"`
	BaseDelay   string `json:"base_delay"`
	MaxDelay    string `json:"max_delay"`
}

// BreakerConfig configures the circuit breaker.
type BreakerConfig struct {
	FailureThreshold int    `json:"failure_threshold"`
	Cooldown         string `json:"cooldown"`
}

// RateLimitConfig configures token-bucket rate limiting.
type RateLimitConfig struct {
	Enabled  bool    `json:"enabled"`
	RPS      float64 `json:"rps"`
	Burst    int     `json:"burst"`
	PerModel bool    `json:"per_model"`
}

// BudgetConfig configures per-key spend caps.
type BudgetConfig struct {
	Enabled    bool    `json:"enabled"`
	MaxCostUSD float64 `json:"max_cost_usd"`
	MaxTokens  int64   `json:"max_tokens"`
	Window     string  `json:"window"` // daily | monthly
}

// ProviderConfig configures a single upstream backend.
type ProviderConfig struct {
	Name      string             `json:"name"`
	Type      string             `json:"type"` // mock | openai | anthropic
	BaseURL   string             `json:"base_url"`
	APIKey    string             `json:"api_key"`
	Models    []string           `json:"models"`
	Weight    int                `json:"weight"`
	CostPer1K map[string]float64 `json:"cost_per_1k"`
	Latency   string             `json:"latency"`    // mock only
	FailEvery int                `json:"fail_every"` // mock only
}

// Default returns a runnable configuration with a single mock provider, so the
// gateway boots and serves traffic with zero external dependencies or keys.
func Default() Config {
	return Config{
		Server:  ServerConfig{Addr: ":8080", ReadTimeout: "30s", WriteTimeout: "120s", ShutdownTimeout: "15s"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Cache:   CacheConfig{Enabled: true, Threshold: 0.95, Dim: 256, Shards: 16, TTL: "1h", Index: "hnsw"},
		Router:  RouterConfig{Strategy: "latency", Retry: RetryConfig{MaxAttempts: 3, BaseDelay: "50ms", MaxDelay: "2s"}, Breaker: BreakerConfig{FailureThreshold: 5, Cooldown: "10s"}},
		Providers: []ProviderConfig{{
			Name: "mock", Type: "mock", Models: []string{"mock-1", "gpt-4o-mini"},
			Weight: 1, Latency: "20ms",
			CostPer1K: map[string]float64{"mock-1": 0, "gpt-4o-mini": 0.0006},
		}},
	}
}

// Load reads a config file (.yaml/.yml or .json), expands ${ENV} variables,
// applies defaults for unset fields, then validates. An empty path returns the
// default config with environment overrides applied.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("config: %w", err)
		}
		expanded := os.ExpandEnv(string(raw))
		var jsonBytes []byte
		if strings.HasSuffix(path, ".json") {
			jsonBytes = []byte(expanded)
		} else {
			node, err := parseYAML(expanded)
			if err != nil {
				return cfg, fmt.Errorf("config: parse yaml: %w", err)
			}
			jsonBytes, err = json.Marshal(node)
			if err != nil {
				return cfg, err
			}
		}
		// Unmarshal over defaults; only present keys override.
		if err := json.Unmarshal(jsonBytes, &cfg); err != nil {
			return cfg, fmt.Errorf("config: decode: %w", err)
		}
	}
	cfg.applyEnv()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// applyEnv applies a small set of high-value environment overrides.
func (c *Config) applyEnv() {
	if v := os.Getenv("SLUICE_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if v := os.Getenv("SLUICE_LOG_LEVEL"); v != "" {
		c.Logging.Level = v
	}
	if v := os.Getenv("SLUICE_LOG_FORMAT"); v != "" {
		c.Logging.Format = v
	}
}

// Validate checks the configuration for internal consistency.
func (c *Config) Validate() error {
	if len(c.Providers) == 0 {
		return fmt.Errorf("config: at least one provider is required")
	}
	seen := map[string]bool{}
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("config: provider[%d] missing name", i)
		}
		if seen[p.Name] {
			return fmt.Errorf("config: duplicate provider name %q", p.Name)
		}
		seen[p.Name] = true
		switch p.Type {
		case "mock", "openai", "anthropic":
		default:
			return fmt.Errorf("config: provider %q has unknown type %q", p.Name, p.Type)
		}
		if len(p.Models) == 0 {
			return fmt.Errorf("config: provider %q serves no models", p.Name)
		}
	}
	if c.Cache.Enabled && (c.Cache.Threshold <= 0 || c.Cache.Threshold > 1) {
		return fmt.Errorf("config: cache.threshold must be in (0,1], got %v", c.Cache.Threshold)
	}
	return nil
}

// Dur parses a duration string, returning def on empty/invalid input.
func Dur(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}
