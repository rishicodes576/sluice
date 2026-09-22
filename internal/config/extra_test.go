package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sluice.json")
	js := `{"router":{"strategy":"cost"},"providers":[{"name":"mock","type":"mock","models":["m"]}]}`
	if err := os.WriteFile(path, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Router.Strategy != "cost" || cfg.Providers[0].Name != "mock" {
		t.Fatalf("unexpected cfg %+v", cfg.Router)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("SLUICE_ADDR", ":9999")
	t.Setenv("SLUICE_LOG_LEVEL", "debug")
	t.Setenv("SLUICE_LOG_FORMAT", "text")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":9999" || cfg.Logging.Level != "debug" || cfg.Logging.Format != "text" {
		t.Fatalf("env overrides not applied: %+v %+v", cfg.Server, cfg.Logging)
	}
}

func TestDurParsing(t *testing.T) {
	if Dur("250ms", time.Second) != 250*time.Millisecond {
		t.Fatal("valid duration not parsed")
	}
	if Dur("", time.Second) != time.Second {
		t.Fatal("empty should return default")
	}
	if Dur("garbage", time.Second) != time.Second {
		t.Fatal("invalid should return default")
	}
}

func TestValidateDuplicateProvider(t *testing.T) {
	cfg := Default()
	cfg.Providers = append(cfg.Providers, cfg.Providers[0])
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected duplicate provider name error")
	}
}

func TestValidateProviderWithoutModels(t *testing.T) {
	cfg := Default()
	cfg.Providers[0].Models = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for provider with no models")
	}
}

func TestParseYAMLNullAndFlowEmpty(t *testing.T) {
	node, err := parseYAML("a: null\nb: []\n")
	if err != nil {
		t.Fatal(err)
	}
	m := node.(map[string]any)
	if m["a"] != nil {
		t.Fatalf("a = %v, want nil", m["a"])
	}
	if arr, ok := m["b"].([]any); !ok || len(arr) != 0 {
		t.Fatalf("b = %v, want empty slice", m["b"])
	}
}
