package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseYAMLScalarsAndNesting(t *testing.T) {
	src := `
server:
  addr: ":9000"
  read_timeout: 30s
cache:
  enabled: true
  threshold: 0.9
  dim: 128
`
	node, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	m := node.(map[string]any)
	server := m["server"].(map[string]any)
	if server["addr"] != ":9000" {
		t.Fatalf("addr = %v", server["addr"])
	}
	cache := m["cache"].(map[string]any)
	if cache["enabled"] != true {
		t.Fatalf("enabled = %v", cache["enabled"])
	}
	if cache["threshold"].(float64) != 0.9 {
		t.Fatalf("threshold = %v", cache["threshold"])
	}
	if cache["dim"].(int64) != 128 {
		t.Fatalf("dim = %v (%T)", cache["dim"], cache["dim"])
	}
}

func TestParseYAMLSequenceOfMaps(t *testing.T) {
	src := `
providers:
  - name: mock
    type: mock
    models: [a, b, c]
    weight: 2
  - name: openai
    type: openai
    models:
      - gpt-4o
`
	node, _ := parseYAML(src)
	m := node.(map[string]any)
	provs := m["providers"].([]any)
	if len(provs) != 2 {
		t.Fatalf("got %d providers", len(provs))
	}
	p0 := provs[0].(map[string]any)
	if p0["name"] != "mock" || p0["weight"].(int64) != 2 {
		t.Fatalf("p0 = %+v", p0)
	}
	models := p0["models"].([]any)
	if len(models) != 3 || models[0] != "a" {
		t.Fatalf("inline models = %+v", models)
	}
	p1 := provs[1].(map[string]any)
	blockModels := p1["models"].([]any)
	if len(blockModels) != 1 || blockModels[0] != "gpt-4o" {
		t.Fatalf("block models = %+v", blockModels)
	}
}

func TestParseYAMLComments(t *testing.T) {
	src := `
# a comment
logging:
  level: debug  # inline comment
  format: "json # not a comment"
`
	node, _ := parseYAML(src)
	logging := node.(map[string]any)["logging"].(map[string]any)
	if logging["level"] != "debug" {
		t.Fatalf("level = %v", logging["level"])
	}
	if logging["format"] != "json # not a comment" {
		t.Fatalf("format = %v", logging["format"])
	}
}

// TestYAMLRoundTripsToConfig ensures parsed YAML decodes into the typed Config.
func TestYAMLRoundTripsToConfig(t *testing.T) {
	src := `
router:
  strategy: cost
providers:
  - name: mock
    type: mock
    models: [m]
`
	node, _ := parseYAML(src)
	b, _ := json.Marshal(node)
	cfg := Default()
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Router.Strategy != "cost" {
		t.Fatalf("strategy = %q", cfg.Router.Strategy)
	}
	if !reflect.DeepEqual(cfg.Providers[0].Models, []string{"m"}) {
		t.Fatalf("models = %+v", cfg.Providers[0].Models)
	}
}
