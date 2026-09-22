package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultIsValid(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) == 0 {
		t.Fatal("default config should have a provider")
	}
}

func TestLoadFileWithEnvExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sluice.yaml")
	yaml := `
providers:
  - name: openai
    type: openai
    api_key: ${TEST_KEY}
    models: [gpt-4o]
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_KEY", "sk-secret")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers[0].APIKey != "sk-secret" {
		t.Fatalf("api_key = %q, want expanded", cfg.Providers[0].APIKey)
	}
}

func TestValidateRejectsUnknownProviderType(t *testing.T) {
	cfg := Default()
	cfg.Providers[0].Type = "bogus"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for unknown type")
	}
}

func TestValidateRejectsBadThreshold(t *testing.T) {
	cfg := Default()
	cfg.Cache.Enabled = true
	cfg.Cache.Threshold = 2
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for threshold > 1")
	}
}
